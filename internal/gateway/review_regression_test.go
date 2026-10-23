package gateway_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darshmahadevia/portunus/internal/gateway"
	"github.com/darshmahadevia/portunus/internal/wire"
)

const reviewChat = `{"messages":[{"role":"user","content":"hello"}],"max_output_tokens":1,"cache":true}`
const reviewCompletion = "event: delta\ndata: {\"text\":\"ok\"}\n\nevent: done\ndata: {\"usage\":{\"inputTokens\":1,\"outputTokens\":1,\"authoritative\":true}}\n\n"

func reviewEngine(t *testing.T, c gateway.Config) (*gateway.Engine, *httptest.Server) {
	t.Helper()
	e, err := gateway.New(c)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(e)
	t.Cleanup(s.Close)
	return e, s
}

func TestPermanentErrorsDoNotOpenCircuit(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 3 {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, reviewCompletion)
	}))
	defer up.Close()
	e, s := reviewEngine(t, gateway.Config{Providers: testProviders(up.URL), MaxAttempts: 1})
	for i := 0; i < 3; i++ {
		assertError(t, post(t, s.URL, reviewChat), 502, "bad_gateway")
	}
	res := post(t, s.URL, reviewChat)
	if res.StatusCode != 200 || calls.Load() != 4 {
		t.Fatalf("fourth request status=%d upstream calls=%d", res.StatusCode, calls.Load())
	}
	readBody(t, res)
	p := e.Snapshot().Providers[0]
	if p.Failures != 3 || p.ConsecutiveFailures != 0 || p.Circuit != "closed" {
		t.Fatalf("permanent failure telemetry: %+v", p)
	}
}

func TestPermanentHalfOpenProbeReleasesCircuit(t *testing.T) {
	clock := &testClock{t: time.Now()}
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(503)
			return
		case 2:
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, reviewCompletion)
	}))
	defer up.Close()
	e, s := reviewEngine(t, gateway.Config{Now: clock.Now, Providers: testProviders(up.URL), MaxAttempts: 1, CircuitThreshold: 1})
	assertError(t, post(t, s.URL, reviewChat), 502, "bad_gateway")
	clock.Advance(3 * time.Second)
	assertError(t, post(t, s.URL, reviewChat), 502, "bad_gateway")
	if p := e.Snapshot().Providers[0]; p.Circuit != "closed" || p.Active != 0 {
		t.Fatalf("permanent probe stranded circuit: %+v", p)
	}
	if res := post(t, s.URL, reviewChat); res.StatusCode != 200 {
		t.Fatalf("next request status=%d", res.StatusCode)
	}
}

func TestExhaustedBudgetAllowsCachedReplay(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: done\ndata: {\"usage\":{\"inputTokens\":105,\"outputTokens\":1,\"authoritative\":true}}\n\n")
	}))
	defer up.Close()
	ps := testProviders(up.URL)
	ps[0].InputPrice = 1000000
	ps[0].OutputPrice = 1000000
	e, s := reviewEngine(t, gateway.Config{Providers: ps, Clients: []gateway.ClientConfig{{ID: "demo", Key: "demo-key", RPM: 10000, Burst: 100, BudgetMicros: 106}}})
	for i := 0; i < 2; i++ {
		res := post(t, s.URL, reviewChat)
		var reply wire.ChatResponse
		if err := json.NewDecoder(res.Body).Decode(&reply); err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 || reply.CacheHit != (i == 1) {
			t.Fatalf("replay %d status=%d reply=%+v", i, res.StatusCode, reply)
		}
	}
	assertError(t, post(t, s.URL, `{"messages":[{"role":"user","content":"new"}],"max_output_tokens":1}`), 402, "insufficient_budget")
	if calls.Load() != 1 || e.Snapshot().Clients[0].SpentMicros != "106" {
		t.Fatalf("cache incurred charge/work: %+v calls=%d", e.Snapshot().Clients, calls.Load())
	}
}

func TestCompatibleCompletionRequiresTextFinishAndDoesNotCacheMalformed(t *testing.T) {
	for _, reason := range []string{"", "tool_calls", "function_call", "content_filter", "unknown", "stop", "length"} {
		t.Run("finish="+reason, func(t *testing.T) {
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
				if reason != "" {
					fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":%q}]}\n\n", reason)
				}
				fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
			}))
			defer up.Close()
			ps := testProviders(up.URL)
			ps[0].Kind = "openaicompat"
			e, s := reviewEngine(t, gateway.Config{Providers: ps, MaxAttempts: 1})
			valid := reason == "stop" || reason == "length"
			for i := 0; i < 2; i++ {
				res := post(t, s.URL, reviewChat)
				if !valid {
					assertError(t, res, 502, "bad_gateway")
					continue
				}
				var reply wire.ChatResponse
				if err := json.NewDecoder(res.Body).Decode(&reply); err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != 200 || reply.Usage.InputTokens != 1 || reply.Usage.OutputTokens != 1 || !reply.Usage.Authoritative || reply.CacheHit != (i == 1) {
					t.Fatalf("trailing usage/completion: status=%d %+v", res.StatusCode, reply)
				}
			}
			wantCalls := int32(2)
			wantEntries := 0
			if valid {
				wantCalls = 1
				wantEntries = 1
			}
			if calls.Load() != wantCalls || e.Snapshot().Cache.Entries != wantEntries {
				t.Fatalf("cache calls=%d entries=%d", calls.Load(), e.Snapshot().Cache.Entries)
			}
		})
	}
}

func TestFallbackAggregatesUsageCertainty(t *testing.T) {
	for _, tc := range []struct {
		name, kind    string
		status        int
		cost          string
		authoritative bool
	}{
		{"uncertain503", "fake", 503, "108", false},
		{"compatible429", "openaicompat", 429, "108", false},
		{"fake429", "fake", 429, "2", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) }))
			defer primary.Close()
			backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, reviewCompletion)
			}))
			defer backup.Close()
			ps := testProviders(primary.URL)
			ps[0].Kind = tc.kind
			ps[0].InputPrice = 1000000
			ps[0].OutputPrice = 1000000
			ps = append(ps, gateway.ProviderConfig{ID: "backup", Kind: "fake", URL: backup.URL, Model: "backup", InputPrice: 1000000, OutputPrice: 1000000})
			e, s := reviewEngine(t, gateway.Config{Providers: ps, Routes: map[string][]string{"general": {"primary", "backup"}}, MaxAttempts: 2})
			res := post(t, s.URL, reviewChat)
			var reply wire.ChatResponse
			if err := json.NewDecoder(res.Body).Decode(&reply); err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != 200 || reply.CostMicros != tc.cost || reply.Usage.Authoritative != tc.authoritative || reply.Usage.InputTokens != 1 || reply.Usage.OutputTokens != 1 {
				t.Fatalf("aggregate response status=%d %+v", res.StatusCode, reply)
			}
			records := e.Requests(1, "").Items
			if len(records) != 1 {
				t.Fatalf("records=%+v", records)
			}
			r := records[0]
			if r.CostMicros != tc.cost || r.UsageAuthoritative != tc.authoritative || r.InputTokens != 1 || r.OutputTokens != 1 || len(r.Attempts) != 2 || r.Attempts[0].UsageAuthoritative || !r.Attempts[1].UsageAuthoritative || r.Attempts[1].CostMicros != "2" {
				t.Fatalf("aggregate/attempt records: %+v", r)
			}
			if e.Snapshot().Cache.Entries != 0 || e.Snapshot().Clients[0].ReservedMicros != "0" || e.Snapshot().Clients[0].SpentMicros != tc.cost {
				t.Fatalf("settlement: %+v", e.Snapshot())
			}
		})
	}
}
