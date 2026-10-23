package gateway

import (
	"sort"
	"time"

	"github.com/darshmahadevia/portunus/internal/fakeprovider"
)

type ProviderSnapshot struct {
	ID                  string                 `json:"id"`
	Name                string                 `json:"name"`
	Kind                string                 `json:"kind"`
	Model               string                 `json:"model"`
	Circuit             string                 `json:"circuit"`
	Active              int                    `json:"active"`
	Requests            uint64                 `json:"requests"`
	Successes           uint64                 `json:"successes"`
	Failures            uint64                 `json:"failures"`
	ConsecutiveFailures int                    `json:"consecutiveFailures"`
	RetryAt             *time.Time             `json:"retryAt"`
	CostMicros          string                 `json:"costMicros"`
	Fake                *fakeprovider.Snapshot `json:"fake"`
}
type ClientSnapshot struct {
	ID              string `json:"id"`
	RPM             int    `json:"rpm"`
	Burst           int    `json:"burst"`
	BudgetMicros    string `json:"budgetMicros"`
	SpentMicros     string `json:"spentMicros"`
	ReservedMicros  string `json:"reservedMicros"`
	RemainingMicros string `json:"remainingMicros"`
	Requests        uint64 `json:"requests"`
	Rejected        uint64 `json:"rejected"`
}
type CacheSnapshot struct {
	Entries    int    `json:"entries"`
	Bytes      int    `json:"bytes"`
	Hits       uint64 `json:"hits"`
	Misses     uint64 `json:"misses"`
	Evictions  uint64 `json:"evictions"`
	TTLMs      int64  `json:"ttlMs"`
	MaxEntries int    `json:"maxEntries"`
	MaxBytes   int    `json:"maxBytes"`
}
type LatencySnapshot struct {
	Count     int      `json:"count"`
	P50Ms     *float64 `json:"p50Ms"`
	P95Ms     *float64 `json:"p95Ms"`
	TTFTP50Ms *float64 `json:"ttftP50Ms"`
	TTFTP95Ms *float64 `json:"ttftP95Ms"`
}
type DemoSnapshot struct {
	Enabled         bool     `json:"enabled"`
	DefaultClientID string   `json:"defaultClientId"`
	Routing         []string `json:"routing"`
}
type RetentionSnapshot struct {
	MaxRequests    int `json:"maxRequests"`
	LatencySamples int `json:"latencySamples"`
}
type Snapshot struct {
	SchemaVersion int                `json:"schemaVersion"`
	Sequence      uint64             `json:"sequence"`
	CapturedAt    time.Time          `json:"capturedAt"`
	UptimeMs      float64            `json:"uptimeMs"`
	Totals        Totals             `json:"totals"`
	Latency       LatencySnapshot    `json:"latency"`
	Providers     []ProviderSnapshot `json:"providers"`
	Clients       []ClientSnapshot   `json:"clients"`
	Cache         CacheSnapshot      `json:"cache"`
	Demo          DemoSnapshot       `json:"demo"`
	Retention     RetentionSnapshot  `json:"retention"`
}

func (g *Engine) Snapshot() Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.expireCache()
	s := Snapshot{SchemaVersion: 1, Sequence: g.seq, CapturedAt: g.config.Now().UTC(), UptimeMs: ms(g.config.Now().Sub(g.started)), Totals: g.totals, Providers: []ProviderSnapshot{}, Clients: []ClientSnapshot{}, Demo: DemoSnapshot{Routing: append([]string{}, g.config.Routes["general"]...)}}
	s.Totals.CostMicros = micros(g.totalCost)
	values, ttft := []float64{}, []float64{}
	for _, r := range g.records {
		if r.Status != "rejected" {
			values = append(values, r.DurationMs)
			if r.TTFTMs != nil {
				ttft = append(ttft, *r.TTFTMs)
			}
		}
	}
	s.Latency = LatencySnapshot{len(values), percentile(values, .5), percentile(values, .95), percentile(ttft, .5), percentile(ttft, .95)}
	s.Retention = RetentionSnapshot{g.config.MaxRecords, len(values)}
	for _, pc := range g.config.Providers {
		p := g.providers[pc.ID]
		v := ProviderSnapshot{ID: pc.ID, Name: pc.Name, Kind: pc.Kind, Model: pc.Model, Circuit: p.state, Active: p.active, Requests: p.requests, Successes: p.successes, Failures: p.failures, ConsecutiveFailures: p.consecutive, CostMicros: micros(p.cost)}
		if !p.retryAt.IsZero() {
			t := p.retryAt.UTC()
			v.RetryAt = &t
		}
		if pc.Fake != nil {
			f := pc.Fake.Snapshot()
			v.Fake = &f
			s.Demo.Enabled = true
		}
		s.Providers = append(s.Providers, v)
	}
	for _, cl := range g.clients {
		c := cl.config
		s.Clients = append(s.Clients, ClientSnapshot{c.ID, c.RPM, c.Burst, micros(c.BudgetMicros), micros(cl.spent), micros(cl.reserved), micros(available(cl)), cl.requests, cl.rejected})
		if c.ID == "demo" {
			s.Demo.DefaultClientID = "demo"
		}
	}
	sort.Slice(s.Clients, func(i, j int) bool { return s.Clients[i].ID < s.Clients[j].ID })
	s.Cache = CacheSnapshot{len(g.cache), g.cacheBytes, g.cacheHits, g.cacheMisses, g.evictions, g.config.Cache.TTL.Milliseconds(), g.config.Cache.MaxEntries, g.config.Cache.MaxBytes}
	return s
}

type RequestPage struct {
	Items      []RequestRecord `json:"items"`
	NextCursor *string         `json:"nextCursor"`
}

func (g *Engine) Requests(limit int, before string) RequestPage {
	g.mu.Lock()
	defer g.mu.Unlock()
	if limit < 1 {
		limit = 50
	}
	limit = min(limit, 1000)
	records := append([]RequestRecord(nil), g.records...)
	sort.Slice(records, func(i, j int) bool { return records[i].ID > records[j].ID })
	p := RequestPage{Items: []RequestRecord{}}
	for _, r := range records {
		if before != "" && r.ID >= before {
			continue
		}
		if len(p.Items) == limit {
			id := p.Items[len(p.Items)-1].ID
			p.NextCursor = &id
			break
		}
		r.Attempts = append([]AttemptRecord{}, r.Attempts...)
		p.Items = append(p.Items, r)
	}
	return p
}
