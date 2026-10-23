package gateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darshmahadevia/portunus/internal/fakeprovider"
	"github.com/darshmahadevia/portunus/internal/gateway"
)

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }
func fixture(t *testing.T, c gateway.Config) (*gateway.Engine, *httptest.Server, *fakeprovider.Server) {
	t.Helper()
	fc := fakeprovider.DefaultConfig()
	fc.ChunkDelay = 0
	fake, e := fakeprovider.New(fc)
	if e != nil {
		t.Fatal(e)
	}
	up := httptest.NewServer(fake)
	t.Cleanup(up.Close)
	if len(c.Providers) == 0 {
		c.Providers = testProviders(up.URL)
		c.Providers[0].Fake = fake
		c.Providers[0].InputPrice = 1000000
		c.Providers[0].OutputPrice = 1000000
	}
	engine, e := gateway.New(c)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return engine, server, fake
}
func TestAuthAndConfiguredIsolation(t *testing.T) {
	engine, server, _ := fixture(t, gateway.Config{})
	for _, key := range []string{"", "provider-secret", "demo-key extra", "Bearer demo-key"} {
		r, _ := http.NewRequest("POST", server.URL+"/v1/chat", strings.NewReader(chatJSON))
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		assertError(t, res, 401, "unauthorized")
	}
	if engine.Snapshot().Totals.ProviderAttempts != 0 || engine.Snapshot().Totals.Rejected != 4 {
		t.Fatal("unauthorized calls reached provider")
	}
}
func TestAtomicBudgetReservationAndUncertainCharge(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, delta) // Deliberately no terminal usage.
	}))
	defer up.Close()
	p := testProviders(up.URL)
	p[0].InputPrice = 1000000
	p[0].OutputPrice = 1000000
	e, err := gateway.New(gateway.Config{Providers: p, Clients: []gateway.ClientConfig{{ID: "demo", Key: "demo-key", RPM: 10000, Burst: 100, BudgetMicros: 361}}, MaxAttempts: 1, FirstChunkTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(e)
	defer server.Close()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		res, err := send(http.DefaultClient, server.URL, chatJSON)
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
	}()
	waitSignal(t, entered)
	s := e.Snapshot()
	if s.Clients[0].ReservedMicros != "361" || s.Providers[0].Active != 1 {
		t.Fatalf("missing reservation: %+v", s.Clients)
	}
	assertError(t, post(t, server.URL, chatJSON), 402, "insufficient_budget")
	close(release)
	waitSignal(t, finished)
	s = e.Snapshot()
	if s.Clients[0].SpentMicros != "361" || s.Clients[0].ReservedMicros != "0" || s.Totals.ProviderAttempts != 1 {
		t.Fatalf("uncertain execution released or double charged: %+v", s)
	}
	assertError(t, post(t, server.URL, chatJSON), 402, "insufficient_budget")
}
func TestTokenBucketRefillsAndCacheConsumesCapacity(t *testing.T) {
	clock := &testClock{t: time.Now()}
	e, server, f := fixture(t, gateway.Config{Now: clock.Now, Clients: []gateway.ClientConfig{{ID: "demo", Key: "demo-key", RPM: 60, Burst: 1, BudgetMicros: 100000}}})
	body := `{"messages":[{"role":"user","content":"hello"}],"cache":true}`
	if post(t, server.URL, body).StatusCode != 200 {
		t.Fatal("initial rejected")
	}
	assertError(t, post(t, server.URL, body), 429, "rate_limited")
	clock.Advance(time.Second)
	res := post(t, server.URL, body)
	var reply map[string]any
	if json.NewDecoder(res.Body).Decode(&reply) != nil || reply["cacheHit"] != true {
		t.Fatalf("cache replay: %+v", reply)
	}
	if f.Snapshot().Requests != 1 || e.Snapshot().Totals.CacheHits != 1 {
		t.Fatal("cache incurred work")
	}
	assertError(t, post(t, server.URL, body), 429, "rate_limited")
}
func TestCacheTTLSettingsIsolationAndEviction(t *testing.T) {
	clock := &testClock{t: time.Now()}
	e, server, f := fixture(t, gateway.Config{Now: clock.Now, Cache: gateway.CacheConfig{MaxEntries: 1, MaxBytes: 4096, TTL: time.Second}, Clients: []gateway.ClientConfig{{ID: "demo", Key: "demo-key", RPM: 10000, Burst: 100, BudgetMicros: 100000}, {ID: "other", Key: "other-key", RPM: 10000, Burst: 100, BudgetMicros: 100000}}})
	call := func(body, key string, hit bool) {
		t.Helper()
		r, _ := http.NewRequest("POST", server.URL+"/v1/chat", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+key)
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var reply struct {
			CacheHit bool `json:"cacheHit"`
		}
		if json.NewDecoder(res.Body).Decode(&reply) != nil || res.StatusCode != 200 || reply.CacheHit != hit {
			t.Fatalf("cache got %+v status %d wanted %v", reply, res.StatusCode, hit)
		}
	}
	a := `{"messages":[{"role":"user","content":"a"}],"cache":true}`
	call(a, "demo-key", false)
	call(a, "demo-key", true)
	stream := strings.TrimSuffix(a, "}") + `,"stream":true}`
	res := post(t, server.URL, stream)
	body := readBody(t, res)
	if !strings.Contains(body, `"cacheHit":true`) || !strings.Contains(body, "event: done") {
		t.Fatal("JSON entry did not replay to SSE")
	}
	call(a, "other-key", false)
	call(a, "demo-key", false)
	call(strings.TrimSuffix(a, "}")+`,"max_output_tokens":2}`, "demo-key", false)
	call(strings.TrimSuffix(a, "}")+`,"temperature":1}`, "demo-key", false)
	call(strings.TrimSuffix(a, "}")+`,"temperature":1}`, "demo-key", false)
	call(a, "demo-key", false)
	clock.Advance(2 * time.Second)
	call(a, "demo-key", false)
	s := e.Snapshot()
	if s.Cache.Entries != 1 || s.Cache.Bytes > 4096 || s.Cache.Evictions < 2 || f.Snapshot().Requests != 8 {
		t.Fatalf("cache accounting: %+v fake=%+v", s.Cache, f.Snapshot())
	}
}
func TestFallbackAttemptsAndNoPartialCaching(t *testing.T) {
	primaryConfig := fakeprovider.DefaultConfig()
	primaryConfig.Status = 503
	primary, _ := fakeprovider.New(primaryConfig)
	backupConfig := fakeprovider.DefaultConfig()
	backupConfig.ChunkDelay = 0
	backup, _ := fakeprovider.New(backupConfig)
	p1 := httptest.NewServer(primary)
	defer p1.Close()
	p2 := httptest.NewServer(backup)
	defer p2.Close()
	ps := testProviders(p1.URL)
	ps[0].Fake = primary
	ps = append(ps, gateway.ProviderConfig{ID: "backup", Kind: "fake", URL: p2.URL + "/v1/chat", Model: "backup", Fake: backup})
	e, err := gateway.New(gateway.Config{Providers: ps, Routes: map[string][]string{"general": {"primary", "backup"}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(e)
	defer server.Close()
	body := `{"messages":[{"role":"user","content":"hello"}],"cache":true}`
	for i := 0; i < 2; i++ {
		res := post(t, server.URL, body)
		if res.StatusCode != 200 {
			t.Fatal(readBody(t, res))
		}
	}
	s := e.Snapshot()
	page := e.Requests(50, "")
	if s.Totals.ProviderAttempts != 4 || s.Totals.Fallbacks != 2 || s.Cache.Entries != 0 || len(page.Items[0].Attempts) != 2 || page.Items[0].Attempts[0].Status != "error" || page.Items[0].Attempts[1].Status != "success" {
		t.Fatalf("incorrect actual attempt accounting: %+v %+v", s.Totals, page)
	}
	_, err = primary.Update(fakeprovider.Control{Status: 200, FirstChunkDelayMs: 0, ChunkDelayMs: 0, CutAfter: 1})
	if err != nil {
		t.Fatal(err)
	}
	res := post(t, server.URL, strings.TrimSuffix(body, "}")+`,"stream":true}`)
	got := readBody(t, res)
	if !strings.Contains(got, "event: error") || strings.Contains(got, "event: done") || backup.Snapshot().Requests != 2 {
		t.Fatalf("committed failure retried: %q", got)
	}
	if e.Snapshot().Cache.Entries != 0 {
		t.Fatal("cached partial output")
	}
}
func TestFirstContentDeadlineFallsBackAndStopsAfterDelta(t *testing.T) {
	fc := fakeprovider.DefaultConfig()
	fc.FirstChunkDelay = time.Second
	slow, _ := fakeprovider.New(fc)
	up := httptest.NewServer(slow)
	defer up.Close()
	fastConfig := fakeprovider.DefaultConfig()
	fastConfig.ChunkDelay = 30 * time.Millisecond
	fast, _ := fakeprovider.New(fastConfig)
	up2 := httptest.NewServer(fast)
	defer up2.Close()
	p := testProviders(up.URL)
	p = append(p, gateway.ProviderConfig{ID: "backup", Kind: "fake", URL: up2.URL + "/v1/chat", Model: "backup"})
	e, err := gateway.New(gateway.Config{Providers: p, Routes: map[string][]string{"general": {"primary", "backup"}}, FirstChunkTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(e)
	defer server.Close()
	res := post(t, server.URL, streamJSON)
	got := readBody(t, res)
	if !strings.Contains(got, "event: done") || !strings.Contains(got, `"provider":"backup"`) {
		t.Fatalf("deadline/fallback failed: %q", got)
	}
	records := e.Requests(10, "").Items
	if len(records) != 1 || len(records[0].Attempts) != 2 || records[0].Attempts[0].ErrorCode != "gateway_timeout" || records[0].Attempts[0].TTFTMs != nil {
		t.Fatalf("deadline records: %+v", records)
	}
}
func TestCircuitStaleCompletionSingleProbeAndCancellation(t *testing.T) {
	clock := &testClock{t: time.Now()}
	var count atomic.Int32
	firstEntered := make(chan struct{})
	firstRelease := make(chan struct{})
	probeEntered := make(chan struct{})
	probeCanceled := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		switch count.Add(1) {
		case 1:
			close(firstEntered)
			<-firstRelease
		case 2:
			w.WriteHeader(503)
			return
		case 3:
			close(probeEntered)
			<-r.Context().Done()
			close(probeCanceled)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, delta+done)
	}))
	defer up.Close()
	e, err := gateway.New(gateway.Config{Now: clock.Now, Providers: testProviders(up.URL), CircuitThreshold: 1, MaxAttempts: 1, FirstChunkTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(e)
	defer server.Close()
	firstDone := make(chan struct{})
	go func() {
		res, err := send(http.DefaultClient, server.URL, chatJSON)
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
		close(firstDone)
	}()
	waitSignal(t, firstEntered)
	assertError(t, post(t, server.URL, chatJSON), 502, "bad_gateway")
	close(firstRelease)
	waitSignal(t, firstDone)
	if e.Snapshot().Providers[0].Circuit != "open" {
		t.Fatal("stale success closed a newer circuit")
	}
	clock.Advance(3 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat", strings.NewReader(chatJSON))
	req.Header.Set("Authorization", "Bearer demo-key")
	req.Header.Set("Content-Type", "application/json")
	probeDone := make(chan struct{})
	go func() {
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			res.Body.Close()
		}
		close(probeDone)
	}()
	waitSignal(t, probeEntered)
	if e.Snapshot().Providers[0].Circuit != "half_open" {
		t.Fatal("probe state not exposed")
	}
	assertError(t, post(t, server.URL, chatJSON), 503, "providers_unavailable")
	if count.Load() != 3 {
		t.Fatal("more than one probe dispatched")
	}
	cancel()
	waitSignal(t, probeCanceled)
	waitSignal(t, probeDone)
	// Wait for request bookkeeping using a bounded observable condition.
	deadline := time.Now().Add(time.Second)
	for e.Snapshot().Providers[0].Active != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if res := post(t, server.URL, chatJSON); res.StatusCode != 200 {
		t.Fatal(readBody(t, res))
	}
	s := e.Snapshot()
	if s.Providers[0].Circuit != "closed" || s.Providers[0].Failures != 1 || s.Providers[0].Active != 0 {
		t.Fatalf("probe lease/recovery: %+v", s.Providers[0])
	}
}

func TestOperatorValidationAndTelemetryPrivacy(t *testing.T) {
	e, server, f := fixture(t, gateway.Config{MaxRecords: 2})
	control := `{"status":503,"firstChunkDelayMs":0,"chunkDelayMs":0,"cutAfter":-1}`
	for _, tc := range []struct {
		host, origin, site, body string
		status                   int
	}{{"evil.test", "", "", control, 403}, {"127.0.0.1", "http://evil.test", "cross-site", control, 403}, {"127.0.0.1", "https://127.0.0.1", "", control, 403}, {"127.0.0.1", "", "", `{"status":200}`, 400}, {"127.0.0.1", "", "", `{"status":500,"firstChunkDelayMs":0,"chunkDelayMs":0,"cutAfter":-1}`, 400}, {"127.0.0.1", "", "", `{"status":200,"firstChunkDelayMs":-1,"chunkDelayMs":0,"cutAfter":-1}`, 400}, {"127.0.0.1", "http://127.0.0.1", "same-origin", control, 200}} {
		r, _ := http.NewRequest("POST", server.URL+"/api/v1/demo/providers/primary/control", strings.NewReader(tc.body))
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body := readBody(t, res)
		if res.StatusCode != tc.status || !json.Valid([]byte(body)) || res.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("control = %d %s", res.StatusCode, body)
		}
	}
	if f.Snapshot().Status != 503 || f.Snapshot().Revision != 2 {
		t.Fatalf("invalid control mutated state: %+v", f.Snapshot())
	}
	if _, err := f.Update(fakeprovider.Control{Status: 200, CutAfter: -1}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		res := post(t, server.URL, `{"messages":[{"role":"user","content":"SECRET_PROMPT"}]}`)
		if res.StatusCode != 200 {
			t.Fatal(readBody(t, res))
		}
		io.Copy(io.Discard, res.Body)
	}
	page := e.Requests(1, "")
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("missing cursor: %+v", page)
	}
	next := e.Requests(1, *page.NextCursor)
	if len(next.Items) != 1 || next.Items[0].ID >= page.Items[0].ID || next.NextCursor != nil {
		t.Fatalf("unstable pagination: %+v", next)
	}
	snap := e.Snapshot()
	if snap.Totals.Requests != 4 || snap.Retention.LatencySamples != 2 || snap.Latency.Count != 2 || snap.Latency.P50Ms == nil || snap.Latency.TTFTP95Ms == nil || snap.Providers[0].Fake.Requests != 4 {
		t.Fatalf("invalid telemetry: %+v", snap)
	}
	for _, path := range []string{"/api/v1/snapshot", "/api/v1/requests"} {
		res, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body := readBody(t, res)
		if res.StatusCode != 200 || strings.Contains(body, "SECRET_PROMPT") || strings.Contains(body, "demo-key") || strings.Contains(body, "Hello") || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("telemetry leaked private data or invalid response: %s", body)
		}
	}
}
func TestMissingUsageChargesReservationAndDoesNotCache(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, delta+done)
	}))
	defer up.Close()
	p := testProviders(up.URL)
	p[0].InputPrice = 1000000
	p[0].OutputPrice = 1000000
	e, err := gateway.New(gateway.Config{Providers: p})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(e)
	defer server.Close()
	for i := 0; i < 2; i++ {
		res := post(t, server.URL, `{"messages":[{"role":"user","content":"hello"}],"cache":true}`)
		var reply struct {
			Cost string `json:"costMicros"`
		}
		if json.NewDecoder(res.Body).Decode(&reply) != nil || reply.Cost != "361" || res.StatusCode != 200 {
			t.Fatalf("missing usage was not conservatively charged: %+v", reply)
		}
	}
	snap := e.Snapshot()
	if calls.Load() != 2 || snap.Cache.Entries != 0 || snap.Clients[0].SpentMicros != "722" || snap.Clients[0].ReservedMicros != "0" {
		t.Fatalf("incorrect conservative settlement: %+v", snap)
	}
}
func TestOneFiniteAttemptAllowanceAndPermanentFailure(t *testing.T) {
	for _, status := range []int{400, 401, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(status) }))
			defer up.Close()
			e, err := gateway.New(gateway.Config{Providers: testProviders(up.URL), MaxAttempts: 3, CircuitThreshold: 10})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(e)
			defer server.Close()
			res := post(t, server.URL, chatJSON)
			assertError(t, res, 502, "bad_gateway")
			want := int32(1)
			if status == 503 {
				want = 3
			}
			if calls.Load() != want || e.Snapshot().Totals.ProviderAttempts != uint64(want) || len(e.Requests(10, "").Items[0].Attempts) != int(want) {
				t.Fatalf("attempt allowance: calls=%d want=%d", calls.Load(), want)
			}
		})
	}
}

func TestCompatibleClientAndProviderCredentialBoundary(t *testing.T) {
	t.Setenv("PORTUNUS_FIXTURE_KEY", "provider-secret")
	var seen atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Add(1)
		if r.Header.Get("Authorization") != "Bearer provider-secret" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Fake-Status") != "" {
			t.Error("client credentials or headers crossed provider boundary")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
	}))
	defer up.Close()
	e, err := gateway.New(gateway.Config{Providers: []gateway.ProviderConfig{{ID: "real", Kind: "openaicompat", URL: up.URL, Model: "configured", APIKeyEnv: "PORTUNUS_FIXTURE_KEY", InputPrice: 1000000, OutputPrice: 1000000}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(e)
	defer server.Close()
	r, _ := http.NewRequest("POST", server.URL+"/v1/chat?url=http://evil.test", strings.NewReader(chatJSON))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer demo-key")
	r.Header.Set("Cookie", "private")
	r.Header.Set("X-Fake-Status", "503")
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, res)
	if res.StatusCode != 200 || !strings.Contains(body, `"costMicros":"11"`) || !strings.Contains(body, `"authoritative":true`) || seen.Load() != 1 {
		t.Fatalf("compatible gateway result: %d %q", res.StatusCode, body)
	}
	snapshot, _ := json.Marshal(e.Snapshot())
	if strings.Contains(string(snapshot), "provider-secret") || strings.Contains(string(snapshot), "demo-key") {
		t.Fatal("credentials leaked into snapshot")
	}
}
func TestCacheOptInAndModelAliasIdentity(t *testing.T) {
	e, server, f := fixture(t, gateway.Config{Routes: map[string][]string{"general": {"primary"}, "other": {"primary"}}})
	for _, body := range []string{chatJSON, chatJSON, `{"messages":[{"role":"user","content":"hello"}],"cache":true}`, `{"messages":[{"role":"user","content":"hello"}],"cache":true,"model":"other"}`} {
		res := post(t, server.URL, body)
		if res.StatusCode != 200 {
			t.Fatal(readBody(t, res))
		}
	}
	if f.Snapshot().Requests != 4 || e.Snapshot().Cache.Entries != 2 {
		t.Fatal("cache was implicit or aliased model identity")
	}
}

func TestOpenPrimaryCountsFallbackPerRequest(t *testing.T) {
	fc := fakeprovider.DefaultConfig()
	fc.Status = 503
	p, _ := fakeprovider.New(fc)
	up := httptest.NewServer(p)
	defer up.Close()
	bc := fakeprovider.DefaultConfig()
	bc.ChunkDelay = 0
	b, _ := fakeprovider.New(bc)
	backup := httptest.NewServer(b)
	defer backup.Close()
	ps := testProviders(up.URL)
	ps = append(ps, gateway.ProviderConfig{ID: "backup", Kind: "fake", URL: backup.URL + "/v1/chat", Model: "backup"})
	e, err := gateway.New(gateway.Config{Providers: ps, Routes: map[string][]string{"general": {"primary", "backup"}}, CircuitThreshold: 1})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(e)
	defer server.Close()
	empty := e.Snapshot()
	if empty.Latency.Count != 0 || empty.Latency.P50Ms != nil || empty.Latency.TTFTP50Ms != nil {
		t.Fatal("empty percentiles invented samples")
	}
	for i := 0; i < 2; i++ {
		res := post(t, server.URL, chatJSON)
		if res.StatusCode != 200 {
			t.Fatal(readBody(t, res))
		}
		io.Copy(io.Discard, res.Body)
	}
	s := e.Snapshot()
	if s.Totals.Fallbacks != 2 || s.Totals.ProviderAttempts != 3 || p.Snapshot().Requests != 1 || b.Snapshot().Requests != 2 {
		t.Fatalf("open primary fallback accounting: %+v", s.Totals)
	}
}
