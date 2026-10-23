package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOutputPreparedBeforeWork(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "fixture snapshot unavailable", 503)
	}))
	defer server.Close()
	o, _ := parse([]string{"-gateway=" + server.URL, "-direct=" + server.URL})
	dir := t.TempDir()
	o.output = dir
	if err := run(o); err == nil {
		t.Fatal("directory destination accepted")
	}
	if calls.Load() != 0 {
		t.Fatalf("performed %d requests before rejecting output", calls.Load())
	}
	o.output = filepath.Join(dir, "missing", "nested", "report.json")
	if err := run(o); err == nil {
		t.Fatal("expected fixture failure")
	}
	data, err := os.ReadFile(o.output)
	if err != nil {
		t.Fatal(err)
	}
	var report report
	if err := json.Unmarshal(data, &report); err != nil || report.Valid || len(report.Failures) == 0 {
		t.Fatalf("invalid report missing: %s %v", data, err)
	}
}

func TestSnapshotFailureRetainsPhase(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
		outage bool
		want   int
	}{
		{"direct", 2, false, 2}, {"gateway", 2, false, 2}, {"cache", 2, false, 2}, {"outage intermediate", 2, true, 3}, {"outage final", 3, true, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshots := 0
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/snapshot":
					snapshots++
					if snapshots == tc.failAt {
						http.Error(w, "snapshot unavailable", 503)
						return
					}
					fmt.Fprint(w, `{"providers":[{"id":"primary","circuit":"open","fake":{}},{"id":"backup","fake":{}}]}`)
				case "/v1/chat":
					request := requests.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					if request == 2 {
						fmt.Fprint(w, "event: delta\ndata: {\"text\":\"partial\"}\n\n")
						return
					}
					fmt.Fprint(w, "event: delta\ndata: {\"text\":\"ok\"}\n\nevent: done\ndata: {\"provider\":\"backup\",\"costMicros\":\"0\"}\n\n")
				default:
					fmt.Fprint(w, `{}`)
				}
			}))
			defer server.Close()
			h := &harness{ctx: context.Background(), client: server.Client(), gateway: server.URL, direct: server.URL, o: options{requests: 2, concurrency: 2}}
			var p phase
			var err error
			if tc.outage {
				h.o.requests = 4
				p, err = h.outage(1)
			} else {
				p, err = h.phase(1, tc.name, server.URL, "demo-key", "test", false)
			}
			r := &report{}
			addPhase(r, p)
			if err == nil || len(r.Phases) != 1 || len(p.Observations) != tc.want || p.Success != tc.want-1 || p.Errors != 1 || p.ElapsedMS <= 0 || len(r.Failures) == 0 {
				t.Fatalf("lost evidence: phase=%+v error=%v report=%+v", p, err, r)
			}
			data, _ := json.Marshal(p)
			if !strings.Contains(string(data), `"work":null`) {
				t.Fatalf("unavailable work represented as counters: %s", data)
			}
		})
	}
}

func TestRestoreControlsAfterCancellation(t *testing.T) {
	var bodies []map[string]int
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]int
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		paths = append(paths, r.URL.Path)
		if strings.Contains(r.URL.Path, "backup") {
			http.Error(w, "restore refused", 503)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	var original snapshot
	if err := json.Unmarshal([]byte(`{"providers":[{"id":"primary","fake":{"status":429,"firstChunkDelayMs":7,"chunkDelayMs":9,"cutAfter":2}},{"id":"backup","fake":{"status":503,"firstChunkDelayMs":1,"chunkDelayMs":3,"cutAfter":-1}}]}`), &original); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := &harness{ctx: ctx, client: server.Client(), gateway: server.URL}
	err := h.restoreControls(original)
	if err == nil || !strings.Contains(err.Error(), "restore refused") || len(bodies) != 2 {
		t.Fatalf("restoration requests=%v error=%v", paths, err)
	}
	if bodies[0]["status"] != 429 || bodies[0]["firstChunkDelayMs"] != 7 || bodies[0]["chunkDelayMs"] != 9 || bodies[0]["cutAfter"] != 2 {
		t.Fatalf("initial configuration not restored: %+v", bodies)
	}
}

func TestBaselineWarmupUsesConcurrency(t *testing.T) {
	const workers = 4
	var active, peak, calls atomic.Int64
	gate := make(chan struct{})
	var release sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		if n == workers {
			release.Do(func() { close(gate) })
		}
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: delta\ndata: {\"text\":\"ok\"}\n\nevent: done\ndata: {}\n\n")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	h := &harness{ctx: ctx, client: server.Client(), gateway: server.URL, direct: server.URL, o: options{concurrency: workers}, expected: "ok"}
	r := &report{Workload: map[string]any{}}
	if err := h.warmBaselines(r); err != nil {
		t.Fatal(err)
	}
	if peak.Load() != workers || calls.Load() != 2*workers || len(r.Phases) != 0 || r.Workload["effectiveBaselineWarmupRequests"] != 2*workers {
		t.Fatalf("warmup peak=%d calls=%d report=%+v", peak.Load(), calls.Load(), r)
	}
}

func TestReportOutputPreservesPriorUntilPublish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte("prior report"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := prepareOutput(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "prior report" {
		t.Fatalf("prior report changed during preparation: %q %v", data, err)
	}
	out.close()
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "prior report" {
		t.Fatalf("abandoned output changed prior report: %q %v", data, err)
	}
	out, err = prepareOutput(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.close()
	if err := out.write(report{Valid: true}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	var result report
	if err != nil || json.Unmarshal(data, &result) != nil || !result.Valid {
		t.Fatalf("report not published: %q %v", data, err)
	}
}

func TestRunRetainsFailedPhaseInReport(t *testing.T) {
	var measured atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/snapshot":
			if measured.Load() > 0 {
				http.Error(w, "post-work snapshot unavailable", 503)
				return
			}
			fmt.Fprint(w, `{"providers":[{"id":"primary","circuit":"closed","fake":{"status":200,"cutAfter":-1}},{"id":"backup","fake":{"status":200,"cutAfter":-1}}]}`)
		case "/v1/chat":
			var body struct{ Messages []struct{ Content string } }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Messages) > 0 && body.Messages[0].Content == "baseline fixed prompt" {
				measured.Add(1)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: delta\ndata: {\"text\":\"ok\"}\n\nevent: done\ndata: {\"provider\":\"primary\",\"costMicros\":\"0\"}\n\n")
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "new", "report.json")
	o, err := parse([]string{"-gateway=" + server.URL, "-direct=" + server.URL, "-requests=2", "-concurrency=2", "-cooldown=1ms", "-output=" + path})
	if err != nil {
		t.Fatal(err)
	}
	if err = run(o); err == nil {
		t.Fatal("snapshot failure reported valid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result report
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Valid || len(result.Failures) == 0 || len(result.Phases) != 1 || len(result.Phases[0].Observations) != 2 || result.Phases[0].Success != 2 || result.Phases[0].Work != nil {
		t.Fatalf("phase absent from invalid report: %s", data)
	}
}

func TestObservePrimaryCancellation(t *testing.T) {
	for _, settled := range []bool{true, false} {
		t.Run(fmt.Sprint(settled), func(t *testing.T) {
			var polls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := polls.Add(1)
				canceled := 0
				if settled && n >= 2 {
					canceled = 1
				}
				fmt.Fprintf(w, `{"providers":[{"id":"primary","fake":{"requests":1,"canceled":%d}},{"id":"backup","fake":{}}]}`, canceled)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			h := &harness{ctx: ctx, gateway: server.URL, client: server.Client()}
			after, err := h.waitForPrimaryCancellation(snapshot{})
			if (err == nil) != settled {
				t.Fatalf("settled=%v snapshot=%+v error=%v", settled, after, err)
			}
			if settled && polls.Load() < 2 {
				t.Fatal("did not wait for cancellation evidence")
			}
		})
	}
}

func TestInvalidPrimeCostPreventsMeasuredCacheWork(t *testing.T) {
	for _, cost := range []string{"malformed", "-1", "9223372036854775808", "4611686018427387904"} {
		t.Run(cost, func(t *testing.T) {
			var mu sync.Mutex
			var requests, attempts, primary, directMeasured, gatewayMeasured, primes, cacheHits int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch r.URL.Path {
				case "/api/v1/snapshot":
					fmt.Fprintf(w, `{"totals":{"requests":%d,"providerAttempts":%d},"providers":[{"id":"primary","circuit":"closed","fake":{"requests":%d,"completed":%d,"status":200,"cutAfter":-1}},{"id":"backup","fake":{"status":200,"cutAfter":-1}}]}`, requests, attempts, primary, primary)
				case "/v1/chat", "/direct/v1/chat":
					var body struct{ Messages []struct{ Content string } }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						return
					}
					if len(body.Messages) != 1 {
						t.Errorf("unexpected messages: %+v", body.Messages)
						return
					}
					prompt := body.Messages[0].Content
					gateway := r.URL.Path == "/v1/chat"
					responseCost, hit := "0", false
					if strings.HasPrefix(prompt, "cache ") {
						if primes == 0 {
							primes++
							responseCost = cost
						} else {
							cacheHits++
							hit = true
						}
					}
					if gateway {
						requests++
						if !hit {
							attempts++
						}
					}
					if !hit {
						primary++
					}
					if prompt == "baseline fixed prompt" {
						if gateway {
							gatewayMeasured++
						} else {
							directMeasured++
						}
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "event: delta\ndata: {\"text\":\"ok\"}\n\nevent: done\ndata: {\"provider\":\"primary\",\"costMicros\":%q,\"cacheHit\":%t}\n\n", responseCost, hit)
				default:
					fmt.Fprint(w, `{}`)
				}
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "report.json")
			o, err := parse([]string{"-gateway=" + server.URL, "-direct=" + server.URL + "/direct", "-requests=2", "-concurrency=2", "-cooldown=1ms", "-output=" + path})
			if err != nil {
				t.Fatal(err)
			}
			if err = run(o); err == nil || !strings.Contains(err.Error(), "invalid prime cost") {
				t.Fatalf("expected invalid prime cost, got %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var result report
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if primes != 1 || cacheHits != 0 || directMeasured != 2 || gatewayMeasured != 2 {
				t.Fatalf("unexpected work: primes=%d cacheHits=%d direct=%d gateway=%d", primes, cacheHits, directMeasured, gatewayMeasured)
			}
			if result.Valid || len(result.Failures) == 0 || len(result.Phases) != 2 {
				t.Fatalf("invalid report lost baseline work: %s", data)
			}
			for i, name := range []string{"direct", "gateway"} {
				p := result.Phases[i]
				if p.Name != name || len(p.Observations) != 2 || p.Success != 2 || p.Work == nil || p.Work.Fake["primary"] != 2 || len(p.Failures) != 0 {
					t.Fatalf("completed %s evidence missing: %+v", name, p)
				}
			}
		})
	}
}
