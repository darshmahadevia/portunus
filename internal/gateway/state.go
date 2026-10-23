package gateway

import (
	"container/list"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/darshmahadevia/portunus/internal/wire"
)

type AttemptRecord struct {
	Provider           string    `json:"provider"`
	Model              string    `json:"model"`
	StartedAt          time.Time `json:"startedAt"`
	DurationMs         float64   `json:"durationMs"`
	TTFTMs             *float64  `json:"ttftMs"`
	Status             string    `json:"status"`
	ErrorCode          string    `json:"errorCode"`
	HTTPStatus         *int      `json:"httpStatus"`
	InputTokens        int64     `json:"inputTokens"`
	OutputTokens       int64     `json:"outputTokens"`
	UsageAuthoritative bool      `json:"usageAuthoritative"`
	ReservedMicros     string    `json:"reservedMicros"`
	CostMicros         string    `json:"costMicros"`
}
type RequestRecord struct {
	ID                 string          `json:"id"`
	StartedAt          time.Time       `json:"startedAt"`
	ClientID           string          `json:"clientId"`
	Model              string          `json:"model"`
	Status             string          `json:"status"`
	ErrorCode          string          `json:"errorCode"`
	Provider           string          `json:"provider"`
	CacheHit           bool            `json:"cacheHit"`
	DurationMs         float64         `json:"durationMs"`
	TTFTMs             *float64        `json:"ttftMs"`
	InputTokens        int64           `json:"inputTokens"`
	OutputTokens       int64           `json:"outputTokens"`
	UsageAuthoritative bool            `json:"usageAuthoritative"`
	CostMicros         string          `json:"costMicros"`
	Attempts           []AttemptRecord `json:"attempts"`
}
type Totals struct {
	Requests         uint64 `json:"requests"`
	Succeeded        uint64 `json:"succeeded"`
	Failed           uint64 `json:"failed"`
	Rejected         uint64 `json:"rejected"`
	Active           int    `json:"active"`
	CacheHits        uint64 `json:"cacheHits"`
	ProviderAttempts uint64 `json:"providerAttempts"`
	Fallbacks        uint64 `json:"fallbacks"`
	InputTokens      int64  `json:"inputTokens"`
	OutputTokens     int64  `json:"outputTokens"`
	CostMicros       string `json:"costMicros"`
}
type clientState struct {
	config             ClientConfig
	tokens             float64
	last               time.Time
	spent, reserved    int64
	requests, rejected uint64
}
type providerState struct {
	config                        ProviderConfig
	active                        int
	requests, successes, failures uint64
	consecutive                   int
	state                         string
	retryAt                       time.Time
	generation                    uint64
	probe                         bool
	cost                          int64
}
type cacheEntry struct {
	key      [32]byte
	response wire.ChatResponse
	expires  time.Time
	size     int
}
type Engine struct {
	mu                                sync.Mutex
	config                            Config
	started                           time.Time
	seq                               uint64
	totals                            Totals
	totalCost                         int64
	running                           int
	clients                           map[string]*clientState
	providers                         map[string]*providerState
	records                           []RequestRecord
	cache                             map[[32]byte]*list.Element
	lru                               *list.List
	cacheBytes                        int
	cacheHits, cacheMisses, evictions uint64
}

func New(c Config) (*Engine, error) {
	c, e := prepare(c)
	if e != nil {
		return nil, e
	}
	now := c.Now()
	g := &Engine{config: c, started: now, clients: map[string]*clientState{}, providers: map[string]*providerState{}, cache: map[[32]byte]*list.Element{}, lru: list.New(), records: make([]RequestRecord, 0, c.MaxRecords)}
	for _, cl := range c.Clients {
		g.clients[cl.Key] = &clientState{config: cl, tokens: float64(cl.Burst), last: now}
	}
	for _, p := range c.Providers {
		g.providers[p.ID] = &providerState{config: p, state: "closed"}
	}
	return g, nil
}
func micros(v int64) string      { return strconv.FormatInt(v, 10) }
func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
func price(input, output, inPrice, outPrice int64) (int64, error) {
	if input < 0 || output < 0 || inPrice < 0 || outPrice < 0 {
		return 0, fmt.Errorf("invalid price")
	}
	calc := func(n, p int64) (int64, error) {
		if p != 0 && n > (math.MaxInt64-999999)/p {
			return 0, fmt.Errorf("price overflow")
		}
		return (n*p + 999999) / 1000000, nil
	}
	a, e := calc(input, inPrice)
	if e != nil {
		return 0, e
	}
	b, e := calc(output, outPrice)
	if e != nil || a > math.MaxInt64-b {
		return 0, fmt.Errorf("price overflow")
	}
	return a + b, nil
}
func addBounded(a, b int64) int64 {
	if b > math.MaxInt64-a {
		return math.MaxInt64
	}
	return a + b
}
func available(c *clientState) int64 {
	if c.spent >= c.config.BudgetMicros {
		return 0
	}
	left := c.config.BudgetMicros - c.spent
	if c.reserved >= left {
		return 0
	}
	return left - c.reserved
}
func inputBound(r wire.ChatRequest) int64 {
	n := int64(64)
	for _, m := range r.Messages {
		n += int64(len(m.Content) + len(m.Role) + 32)
	}
	return n
}
func (g *Engine) begin() RequestRecord {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq++
	g.totals.Requests++
	g.totals.Active++
	return RequestRecord{ID: fmt.Sprintf("%020d", g.seq), StartedAt: g.config.Now().UTC(), Status: "error", CostMicros: "0", Attempts: []AttemptRecord{}}
}
func (g *Engine) finish(r RequestRecord) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r.DurationMs = ms(g.config.Now().Sub(r.StartedAt))
	g.totals.Active--
	switch r.Status {
	case "success":
		g.totals.Succeeded++
	case "rejected":
		g.totals.Rejected++
	default:
		g.totals.Failed++
	}
	if r.CacheHit {
		g.totals.CacheHits++
	}
	g.totals.InputTokens = addBounded(g.totals.InputTokens, r.InputTokens)
	g.totals.OutputTokens = addBounded(g.totals.OutputTokens, r.OutputTokens)
	cost, _ := strconv.ParseInt(r.CostMicros, 10, 64)
	g.totalCost = addBounded(g.totalCost, cost)
	g.totals.CostMicros = micros(g.totalCost)
	if len(g.records) == g.config.MaxRecords {
		copy(g.records, g.records[1:])
		g.records[len(g.records)-1] = r
	} else {
		g.records = append(g.records, r)
	}
	g.seq++
}
func (g *Engine) admit(key string) (*clientState, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cl := g.clients[key]
	if cl == nil {
		return nil, 401
	}
	cl.requests++
	now := g.config.Now()
	elapsed := max(0, now.Sub(cl.last).Seconds())
	cl.tokens = min(float64(cl.config.Burst), cl.tokens+elapsed*float64(cl.config.RPM)/60)
	cl.last = now
	if cl.tokens < 1 {
		cl.rejected++
		return cl, 429
	}
	cl.tokens--
	return cl, 0
}

type lease struct {
	p           *providerState
	generation  uint64
	probe       bool
	reservation int64
	client      *clientState
}

func (g *Engine) acquire(id string, cl *clientState, r wire.ChatRequest) (*lease, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := g.providers[id]
	now := g.config.Now()
	if p.active >= p.config.MaxConcurrent || p.retryAt.After(now) {
		return nil, 503
	}
	probe := p.state != "closed"
	if probe && p.probe {
		return nil, 503
	}
	reserve, e := price(inputBound(r), int64(r.MaxOutputTokens), p.config.InputPrice, p.config.OutputPrice)
	if e != nil || available(cl) == 0 || reserve > available(cl) {
		return nil, 402
	}
	cl.reserved += reserve
	p.active++
	p.requests++
	g.totals.ProviderAttempts++
	g.seq++
	if probe {
		p.probe = true
		p.state = "half_open"
	}
	return &lease{p, p.generation, probe, reserve, cl}, 0
}
func (g *Engine) settle(l *lease, u wire.Usage, success, canceled, unstarted, circuitFailure bool, retryAfter time.Duration) int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := l.p
	p.active--
	g.seq++
	l.client.reserved -= l.reservation
	cost := l.reservation
	if unstarted {
		cost = 0
	} else if u.Authoritative {
		if n, e := price(u.InputTokens, u.OutputTokens, p.config.InputPrice, p.config.OutputPrice); e == nil {
			cost = n
		}
	}
	// Charge actual authoritative usage even if a provider exceeded the estimate.
	if cost > math.MaxInt64-l.client.spent {
		cost = math.MaxInt64 - l.client.spent
	}
	l.client.spent += cost
	p.cost = addBounded(p.cost, cost)
	if success {
		p.successes++
	} else if !canceled {
		p.failures++
	}
	if p.generation != l.generation {
		return cost
	}
	if l.probe {
		p.probe = false
	}
	if canceled {
		if l.probe {
			p.state = "open"
		}
		return cost
	}
	// A permanent request error proves the provider is reachable without
	// indicating a provider outage, including when it completes a probe.
	if success || !circuitFailure {
		p.consecutive = 0
		p.state = "closed"
		p.retryAt = time.Time{}
		if l.probe {
			p.generation++
		}
	} else {
		p.consecutive++
		if l.probe || p.consecutive >= g.config.CircuitThreshold {
			p.state = "open"
			p.retryAt = g.config.Now().Add(max(g.config.CircuitCooldown, retryAfter))
			p.generation++
			p.probe = false
		} else if retryAfter > 0 {
			p.retryAt = g.config.Now().Add(retryAfter)
		}
	}
	return cost
}
func cacheKey(client string, r wire.ChatRequest, revision string, targets []string) [32]byte {
	r.Stream = false
	r.Cache = false
	b, _ := json.Marshal(struct {
		Client   string
		Request  wire.ChatRequest
		Revision string
		Targets  []string
	}{client, r, revision, targets})
	return sha256.Sum256(b)
}
func (g *Engine) removeCache(el *list.Element, evict bool) {
	v := el.Value.(cacheEntry)
	delete(g.cache, v.key)
	g.cacheBytes -= v.size
	g.lru.Remove(el)
	if evict {
		g.evictions++
	}
}
func (g *Engine) expireCache() {
	now := g.config.Now()
	for el := g.lru.Back(); el != nil; {
		prev := el.Prev()
		if !el.Value.(cacheEntry).expires.After(now) {
			g.removeCache(el, false)
		}
		el = prev
	}
}
func (g *Engine) getCache(key [32]byte) (wire.ChatResponse, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.expireCache()
	if el := g.cache[key]; el != nil {
		g.lru.MoveToFront(el)
		g.cacheHits++
		return el.Value.(cacheEntry).response, true
	}
	g.cacheMisses++
	return wire.ChatResponse{}, false
}
func (g *Engine) putCache(key [32]byte, r wire.ChatResponse) {
	g.mu.Lock()
	defer g.mu.Unlock()
	size := len(r.Text) + 256
	if size > g.config.Cache.MaxBytes {
		return
	}
	g.expireCache()
	if el := g.cache[key]; el != nil {
		g.removeCache(el, false)
	}
	for len(g.cache) >= g.config.Cache.MaxEntries || g.cacheBytes+size > g.config.Cache.MaxBytes {
		g.removeCache(g.lru.Back(), true)
	}
	g.cache[key] = g.lru.PushFront(cacheEntry{key, r, g.config.Now().Add(g.config.Cache.TTL), size})
	g.cacheBytes += size
}
func percentile(v []float64, q float64) *float64 {
	if len(v) == 0 {
		return nil
	}
	sort.Float64s(v)
	n := v[int(math.Ceil(float64(len(v))*q))-1]
	return &n
}
