package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"
)

type distribution struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

// percentile uses nearest rank, retaining observed values rather than interpolation.
func percentile(a []float64, p float64) float64 {
	if len(a) == 0 {
		return 0
	}
	b := append([]float64(nil), a...)
	sort.Float64s(b)
	i := int(float64(len(b)) * p)
	if float64(i) < float64(len(b))*p {
		i++
	}
	if i < 1 {
		i = 1
	}
	if i > len(b) {
		i = len(b)
	}
	return b[i-1]
}
func dist(a []float64) distribution {
	return distribution{percentile(a, .5), percentile(a, .95), percentile(a, .99)}
}

type spread struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}
type phase struct {
	Non200               int           `json:"non200Responses"`
	Incomplete           int           `json:"incompleteResponses"`
	TransportFailures    int           `json:"transportFailures"`
	CompletionFraction   float64       `json:"completionFraction"`
	Round                int           `json:"round"`
	Name                 string        `json:"name"`
	ElapsedMS            float64       `json:"elapsedMs"`
	Success              int           `json:"verifiedCompletions"`
	Errors               int           `json:"errors"`
	ResponseBytes        int64         `json:"responseBytes"`
	Latency              distribution  `json:"successfulTotalLatencyMs"`
	TTFT                 distribution  `json:"successfulTTFTMs"`
	Throughput           float64       `json:"completedRequestsPerSecond"`
	Work                 *work         `json:"work"`
	Observations         []observation `json:"observations"`
	Failures             []string      `json:"failures"`
	CircuitOpened        bool          `json:"circuitOpened,omitempty"`
	SyntheticCost        string        `json:"syntheticEstimatedCostMicros"`
	ProviderCallsSaved   int           `json:"providerCallsSaved,omitempty"`
	SyntheticCostAvoided string        `json:"syntheticEstimatedCostAvoidedMicros,omitempty"`
}

func (h *harness) load(n int, base, key, prompt string, cache bool) []observation {
	out := make([]observation, n)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < h.o.concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				o := h.chat(base, key, cache, true, prompt)
				o.Index = i
				out[i] = o
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out
}
func summarize(p *phase) {
	var totals, first []float64
	var cost int64
	for _, o := range p.Observations {
		p.ResponseBytes += o.Bytes
		if o.Status == 0 {
			p.TransportFailures++
		} else if o.Status != 200 {
			p.Non200++
		}
		if o.Status == 200 && !o.Complete {
			p.Incomplete++
		}
		if o.Error != "" || !o.Complete {
			p.Errors++
			continue
		}
		p.Success++
		totals = append(totals, o.TotalMS)
		if o.TTFTMS != nil {
			first = append(first, *o.TTFTMS)
		}
		if o.Cost != "" {
			v, e := strconv.ParseInt(o.Cost, 10, 64)
			if e != nil || v < 0 {
				p.Failures = append(p.Failures, "invalid costMicros "+o.Cost)
			} else {
				cost += v
			}
		}
	}
	p.SyntheticCost = strconv.FormatInt(cost, 10)
	if len(p.Observations) > 0 {
		p.CompletionFraction = float64(p.Success) / float64(len(p.Observations))
	}
	p.Latency = dist(totals)
	p.TTFT = dist(first)
	if p.ElapsedMS > 0 {
		p.Throughput = float64(p.Success) * 1000 / p.ElapsedMS
	}
	if p.Errors > 0 {
		p.Failures = append(p.Failures, fmt.Sprintf("%d failed/incomplete responses", p.Errors))
	}
}
func (h *harness) phase(round int, name, base, key, prompt string, cache bool) (p phase, err error) {
	p = phase{Round: round, Name: name, Failures: []string{}}
	defer func() {
		if err != nil {
			p.Failures = append(p.Failures, "work verification unavailable: "+err.Error())
		}
		summarize(&p)
	}()
	a, e := h.snapshot()
	if e != nil {
		return p, e
	}
	start := time.Now()
	p.Observations = h.load(h.o.requests, base, key, prompt, cache)
	p.ElapsedMS = ms(time.Since(start))
	b, e := h.snapshot()
	if e != nil {
		return p, e
	}
	w := delta(a, b)
	p.Work = &w
	n := int64(h.o.requests)
	switch name {
	case "direct":
		if p.Work.Requests != 0 || p.Work.Attempts != 0 || p.Work.Fake["primary"] != n || p.Work.Fake["backup"] != 0 || p.Work.Completed["primary"] != n {
			p.Failures = append(p.Failures, "direct work mismatch")
		}
	case "gateway":
		if p.Work.Requests != n || p.Work.Attempts != n || p.Work.Fake["primary"] != n || p.Work.Fake["backup"] != 0 || p.Work.Completed["primary"] != n {
			p.Failures = append(p.Failures, "gateway work mismatch")
		}
		for _, o := range p.Observations {
			if o.Provider != "primary" || o.CacheHit {
				p.Failures = append(p.Failures, "baseline must use primary and bypass cache")
				break
			}
		}
	case "cache":
		if p.Work.Requests != n || p.Work.Attempts != 0 || p.Work.fakeTotal() != 0 {
			p.Failures = append(p.Failures, "cache hit workload performed provider work")
		}
		for _, o := range p.Observations {
			if !o.CacheHit || o.Cost != "0" {
				p.Failures = append(p.Failures, "cache hit metadata/cost mismatch")
				break
			}
		}
		p.ProviderCallsSaved = h.o.requests
	}
	return p, nil
}
func addPhase(r *report, p phase) {
	r.Phases = append(r.Phases, p)
	for _, f := range p.Failures {
		r.Failures = append(r.Failures, fmt.Sprintf("round %d %s: %s", p.Round, p.Name, f))
	}
	if p.Work == nil {
		fmt.Fprintf(stderr, "round %d %-7s completed=%d errors=%d work=unavailable\n", p.Round, p.Name, p.Success, p.Errors)
	} else {
		fmt.Fprintf(stderr, "round %d %-7s completed=%d errors=%d attempts=%d fake=%d\n", p.Round, p.Name, p.Success, p.Errors, p.Work.Attempts, p.Work.fakeTotal())
	}
}
func (h *harness) benchmark(r *report) error {
	for round := 1; round <= h.o.rounds; round++ {
		if e := h.recover(); e != nil {
			return e
		}
		if e := h.warmBaselines(r); e != nil {
			return e
		}
		names := []string{"direct", "gateway"}
		if round%2 == 0 {
			names = []string{"gateway", "direct"}
		}
		for _, name := range names {
			base, key := h.direct, ""
			if name == "gateway" {
				base, key = h.gateway, "demo-key"
			}
			p, e := h.phase(round, name, base, key, "baseline fixed prompt", false)
			addPhase(r, p)
			if e != nil {
				return e
			}
			if len(p.Failures) > 0 {
				return fmt.Errorf("%s phase invalid", name)
			}
		}
		prompt := fmt.Sprintf("cache %s round %d", r.Timestamp, round)
		prime := h.chat(h.gateway, "demo-key", true, true, prompt)
		if prime.Error != "" || prime.CacheHit || prime.Cost == "" {
			return fmt.Errorf("cache prime invalid: %+v", prime)
		}
		cost, e := strconv.ParseInt(prime.Cost, 10, 64)
		if e != nil || cost < 0 || cost > math.MaxInt64/int64(h.o.requests) {
			return fmt.Errorf("invalid prime cost %q", prime.Cost)
		}
		p, e := h.phase(round, "cache", h.gateway, "demo-key", prompt, true)
		if e != nil {
			addPhase(r, p)
			return e
		}
		p.SyntheticCostAvoided = strconv.FormatInt(cost*int64(h.o.requests), 10)
		addPhase(r, p)
		if len(p.Failures) > 0 {
			return fmt.Errorf("cache phase invalid")
		}
		p, e = h.outage(round)
		addPhase(r, p)
		if e != nil {
			return e
		}
		if len(p.Failures) > 0 {
			return fmt.Errorf("outage phase invalid")
		}
		if e = h.recover(); e != nil {
			return e
		}
	}
	r.AcrossRounds = map[string]spread{}
	for _, name := range []string{"direct", "gateway", "cache", "outage"} {
		for _, metric := range []string{"p50Ms", "p95Ms", "p99Ms", "ttftP50Ms", "ttftP95Ms", "ttftP99Ms", "completedRequestsPerSecond"} {
			var a []float64
			for _, p := range r.Phases {
				if p.Name == name {
					v := p.Latency.P50
					switch metric {
					case "p95Ms":
						v = p.Latency.P95
					case "p99Ms":
						v = p.Latency.P99
					case "ttftP50Ms":
						v = p.TTFT.P50
					case "ttftP95Ms":
						v = p.TTFT.P95
					case "ttftP99Ms":
						v = p.TTFT.P99
					case "completedRequestsPerSecond":
						v = p.Throughput
					}
					a = append(a, v)
				}
			}
			if len(a) > 0 {
				sort.Float64s(a)
				r.AcrossRounds[name+"."+metric] = spread{percentile(a, .5), a[0], a[len(a)-1]}
			}
		}
	}
	return nil
}
func (h *harness) outage(round int) (p phase, err error) {
	p = phase{Round: round, Name: "outage", Failures: []string{}}
	defer func() {
		if err != nil {
			p.Failures = append(p.Failures, "work verification unavailable: "+err.Error())
		}
		summarize(&p)
	}()
	if e := h.control("primary", 503, 0, 2, -1); e != nil {
		return p, e
	}
	defer h.cleanupHealthy()
	a, e := h.snapshot()
	if e != nil {
		return p, e
	}
	start := time.Now()
	// Retain elapsed time even if the intermediate circuit snapshot fails.
	defer func() {
		if p.ElapsedMS == 0 {
			p.ElapsedMS = ms(time.Since(start))
		}
	}()
	// Force the threshold through actual sequential requests before concurrent load.
	for i := 0; i < 3; i++ {
		o := h.chat(h.gateway, "demo-key", false, true, "outage")
		o.Index = i
		p.Observations = append(p.Observations, o)
	}
	opened, e := h.snapshot()
	if e != nil {
		return p, e
	}
	p.CircuitOpened = circuit(opened, "primary") == "open"
	n := h.o.requests
	if n < 3 {
		n = 3
	}
	if n > 3 {
		rest := h.load(n-3, h.gateway, "demo-key", "outage", false)
		for i := range rest {
			rest[i].Index += 3
		}
		p.Observations = append(p.Observations, rest...)
	}
	p.ElapsedMS = ms(time.Since(start))
	b, e := h.snapshot()
	if e != nil {
		return p, e
	}
	w := delta(a, b)
	p.Work = &w
	if !p.CircuitOpened || p.Work.Requests != int64(n) || p.Work.Fake["backup"] != int64(n) || p.Work.Completed["backup"] != int64(n) || p.Work.Fake["primary"] < 3 || p.Work.Attempts != p.Work.fakeTotal() {
		p.Failures = append(p.Failures, "outage circuit/work mismatch")
	}
	for _, o := range p.Observations {
		if o.Provider != "backup" {
			p.Failures = append(p.Failures, "outage did not complete on backup")
			break
		}
	}
	return p, nil
}

// One full worker wave per path warms the configured concurrent connections.
func (h *harness) warmBaselines(r *report) error {
	for _, path := range []struct{ base, key string }{{h.direct, ""}, {h.gateway, "demo-key"}} {
		observations := h.load(h.o.concurrency, path.base, path.key, "baseline warmup", false)
		count, _ := r.Workload["effectiveBaselineWarmupRequests"].(int)
		r.Workload["effectiveBaselineWarmupRequests"] = count + len(observations)
		for _, o := range observations {
			if o.Error != "" || !o.Complete {
				return fmt.Errorf("baseline warmup: %+v", o)
			}
		}
	}
	return nil
}
