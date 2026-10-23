package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/darshmahadevia/portunus/internal/provider"
	"github.com/darshmahadevia/portunus/internal/wire"
)

func (g *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		wire.Healthz(w, r)
	case r.URL.Path == "/v1/chat":
		g.chat(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/"):
		g.operator(w, r)
	default:
		if g.config.Dashboard != nil {
			g.config.Dashboard.ServeHTTP(w, r)
		} else {
			wire.WriteError(w, 404, "not_found", "Endpoint not found.")
		}
	}
}
func emit(w http.ResponseWriter, event string, data any) error {
	b, e := json.Marshal(data)
	if e != nil {
		return e
	}
	if _, e = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); e != nil {
		return e
	}
	return http.NewResponseController(w).Flush()
}
func startStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
}
func (g *Engine) chat(w http.ResponseWriter, r *http.Request) {
	record := g.begin()
	defer func() { g.finish(record) }()
	ctx, cancel := context.WithTimeout(r.Context(), g.config.Timeout)
	defer cancel()
	r = r.WithContext(ctx)
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(g.config.Timeout))
	defer controller.SetReadDeadline(time.Time{})
	_ = controller.SetWriteDeadline(time.Now().Add(g.config.WriteTimeout))
	defer controller.SetWriteDeadline(time.Time{})
	reject := func(status int, code, message string) {
		record.Status = "rejected"
		record.ErrorCode = code
		wire.WriteError(w, status, code, message)
	}
	key := ""
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		key = strings.TrimPrefix(auth, "Bearer ")
	}
	// Authenticate before parsing or spending rate-limit capacity.
	g.mu.Lock()
	known := g.clients[key]
	g.mu.Unlock()
	if known == nil {
		reject(401, "unauthorized", "A configured client bearer key is required.")
		return
	}
	record.ClientID = known.config.ID
	g.mu.Lock()
	if g.running >= g.config.MaxConcurrent {
		g.mu.Unlock()
		reject(503, "overloaded", "Gateway is at capacity.")
		return
	}
	g.running++
	g.mu.Unlock()
	defer func() { g.mu.Lock(); g.running--; g.mu.Unlock() }()
	req, _, ok := wire.ReadChatRequest(w, r)
	if !ok {
		record.Status = "rejected"
		record.ErrorCode = "invalid_request"
		return
	}
	record.Model = req.Model
	targets := g.config.Routes[req.Model]
	if len(targets) == 0 {
		reject(400, "invalid_model", "Unknown model alias.")
		return
	}
	cl, status := g.admit(key)
	if status != 0 {
		code := "rate_limited"
		if status == 402 {
			code = "insufficient_budget"
		}
		reject(status, code, "Client admission denied.")
		return
	}
	w.Header().Set("X-Request-ID", record.ID)
	eligible := req.Cache && req.Temperature == 0
	ck := cacheKey(cl.config.ID, req, g.config.Revision, targets)
	if eligible {
		if cached, hit := g.getCache(ck); hit {
			cached.RequestID = record.ID
			cached.CacheHit = true
			cached.CostMicros = "0"
			record.Status = "success"
			record.CacheHit = true
			record.Provider = cached.Provider
			record.UsageAuthoritative = cached.Usage.Authoritative
			v := ms(g.config.Now().Sub(record.StartedAt))
			record.TTFTMs = &v
			if req.Stream {
				startStream(w)
				if emit(w, "delta", wire.Delta{Text: cached.Text}) != nil {
					record.Status = "canceled"
					return
				}
				cached.Text = ""
				if emit(w, "done", cached) != nil {
					record.Status = "canceled"
				}
			} else {
				w.Header().Set("Content-Type", "application/json")
				if json.NewEncoder(w).Encode(cached) != nil {
					record.Status = "canceled"
				}
			}
			return
		}
	}
	committed := false
	finalStatus := 503
	finalCode := "providers_unavailable"
	attempts := 0
	cursor := 0
	fallbackCounted := false
	totalCost := int64(0)
	aggregateUsage := wire.Usage{Authoritative: true}
	for attempts < g.config.MaxAttempts && ctx.Err() == nil {
		var l *lease
		budgetDenied := false
		for checked := 0; checked < len(targets); checked++ {
			id := targets[cursor%len(targets)]
			cursor++
			var s int
			l, s = g.acquire(id, cl, req)
			if s == 402 {
				budgetDenied = true
			}
			if l != nil {
				break
			}
		}
		if l == nil {
			if budgetDenied {
				finalStatus = 402
				finalCode = "insufficient_budget"
			}
			break
		}
		attempts++
		p := l.p.config
		if p.ID != targets[0] && !fallbackCounted {
			fallbackCounted = true
			g.mu.Lock()
			g.totals.Fallbacks++
			g.mu.Unlock()
		}
		ar := AttemptRecord{Provider: p.ID, Model: p.Model, StartedAt: g.config.Now().UTC(), Status: "error", ReservedMicros: micros(l.reservation), CostMicros: "0"}
		actx, acancel := context.WithCancel(ctx)
		var firstState atomic.Int32
		timer := time.AfterFunc(g.config.FirstChunkTimeout, func() {
			if firstState.CompareAndSwap(0, 2) {
				acancel()
			}
		})
		session, err := p.Adapter.Open(actx, provider.AttemptRequest{Request: req, Model: p.Model})
		var usage wire.Usage
		var text strings.Builder
		done := false
		first := false
		outputBytes := 0
		downstreamFailed := false
		if err == nil {
			statusOK := http.StatusOK
			ar.HTTPStatus = &statusOK
			for {
				ev, e := session.Next()
				if e != nil {
					err = e
					break
				}
				switch ev.Kind {
				case "delta":
					if ev.Text == "" {
						continue
					}
					if !first {
						if !firstState.CompareAndSwap(0, 1) {
							err = context.DeadlineExceeded
							break
						}
						timer.Stop()
						first = true
						v := ms(g.config.Now().Sub(ar.StartedAt))
						ar.TTFTMs = &v
						if record.TTFTMs == nil {
							v := ms(g.config.Now().Sub(record.StartedAt))
							record.TTFTMs = &v
						}
					}
					outputBytes += len(ev.Text)
					if outputBytes > provider.MaxOutputBytes {
						err = errors.New("output bound exceeded")
						break
					}
					if !req.Stream || eligible {
						text.WriteString(ev.Text)
					}
					if req.Stream {
						if !committed {
							startStream(w)
							w.Header().Set("X-Provider-ID", p.ID)
							committed = true
						}
						if e := emit(w, "delta", wire.Delta{Text: ev.Text}); e != nil {
							downstreamFailed = true
							err = e
						}
					}
				case "usage":
					usage = ev.Usage
				case "done":
					usage = ev.Usage
					done = true
				default:
					err = errors.New("invalid normalized event")
				}
				if err != nil || done {
					break
				}
			}
			_ = session.Close()
		}
		timer.Stop()
		if firstState.Load() == 2 && !first && ctx.Err() == nil {
			err = context.DeadlineExceeded
		}
		acancel()
		if err == nil && !done {
			err = io.ErrUnexpectedEOF
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		canceled := errors.Is(ctx.Err(), context.Canceled) || downstreamFailed
		success := err == nil && done
		var pe *provider.Error
		unstarted := false
		retryAfter := time.Duration(0)
		if errors.As(err, &pe) {
			ar.HTTPStatus = &pe.Status
			unstarted = pe.Unstarted
			retryAfter = pe.RetryAfter
		}
		cost := g.settle(l, usage, success, canceled, unstarted, provider.Transient(err), retryAfter)
		totalCost += cost
		record.CostMicros = micros(totalCost)
		ar.DurationMs = ms(g.config.Now().Sub(ar.StartedAt))
		ar.InputTokens = usage.InputTokens
		ar.OutputTokens = usage.OutputTokens
		ar.UsageAuthoritative = usage.Authoritative
		ar.CostMicros = micros(cost)
		if !unstarted {
			aggregateUsage.InputTokens = addBounded(aggregateUsage.InputTokens, usage.InputTokens)
			aggregateUsage.OutputTokens = addBounded(aggregateUsage.OutputTokens, usage.OutputTokens)
			aggregateUsage.Authoritative = aggregateUsage.Authoritative && usage.Authoritative
		}
		record.InputTokens = aggregateUsage.InputTokens
		record.OutputTokens = aggregateUsage.OutputTokens
		record.UsageAuthoritative = aggregateUsage.Authoritative
		record.Provider = p.ID
		if success {
			ar.Status = "success"
			record.Status = "success"
		} else if canceled {
			ar.Status = "canceled"
			ar.ErrorCode = "canceled"
		} else {
			ar.ErrorCode = "bad_gateway"
			if errors.Is(err, context.DeadlineExceeded) {
				ar.ErrorCode = "gateway_timeout"
			}
		}
		record.Attempts = append(record.Attempts, ar)
		if success {
			result := wire.ChatResponse{Text: text.String(), RequestID: record.ID, Provider: p.ID, Model: req.Model, Usage: aggregateUsage, CostMicros: record.CostMicros}
			if eligible && usage.Authoritative && p.ID == targets[0] && attempts == 1 {
				g.putCache(ck, result)
			}
			if req.Stream {
				if !committed {
					startStream(w)
					committed = true
				}
				result.Text = ""
				if emit(w, "done", result) != nil {
					record.Status = "canceled"
					record.ErrorCode = "downstream_write"
				}
			} else {
				w.Header().Set("Content-Type", "application/json")
				if json.NewEncoder(w).Encode(result) != nil {
					record.Status = "canceled"
					record.ErrorCode = "downstream_write"
				}
			}
			return
		}
		finalStatus = 502
		finalCode = ar.ErrorCode
		if canceled {
			record.Status = "canceled"
			record.ErrorCode = "canceled"
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			finalStatus = 504
			finalCode = "gateway_timeout"
		}
		if committed || !provider.Transient(err) {
			break
		}
		if attempts >= g.config.MaxAttempts {
			break
		}
		// Prefer another ready route target; delay only after a complete pass.
		if cursor%len(targets) == 0 {
			delay := time.Duration(10+rand.Intn(20)) * time.Millisecond
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
			case <-t.C:
			}
		}
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			record.Status = "canceled"
			record.ErrorCode = "canceled"
			return
		}
		finalStatus = 504
		finalCode = "gateway_timeout"
	}
	record.ErrorCode = finalCode
	if finalStatus == 402 {
		g.mu.Lock()
		cl.rejected++
		g.mu.Unlock()
		record.Status = "rejected"
	}
	if committed {
		_ = emit(w, "error", wire.APIError{Code: finalCode, Message: "Provider response interrupted."})
	} else {
		wire.WriteError(w, finalStatus, finalCode, "Provider request could not complete.")
	}
}
func LiteralLoopbackHost(host string) bool {
	h, _, e := net.SplitHostPort(host)
	if e != nil {
		h = host
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
func (g *Engine) operator(w http.ResponseWriter, r *http.Request) {
	if !LiteralLoopbackHost(r.Host) {
		wire.WriteError(w, 403, "forbidden", "Operator API requires a literal loopback host.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "POST" {
		origin := r.Header.Get("Origin")
		scheme := "http://"
		if r.TLS != nil {
			scheme = "https://"
		}
		if (origin != "" && origin != scheme+r.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			wire.WriteError(w, 403, "forbidden", "Same-origin requests required.")
			return
		}
	}
	switch {
	case r.URL.Path == "/api/v1/snapshot" && r.Method == "GET":
		writeJSON(w, g.Snapshot())
	case r.URL.Path == "/api/v1/requests" && r.Method == "GET":
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || n > 1000 {
				wire.WriteError(w, 400, "invalid_request", "Invalid page limit.")
				return
			}
			limit = n
		}
		writeJSON(w, g.Requests(limit, r.URL.Query().Get("before")))
	case strings.HasPrefix(r.URL.Path, "/api/v1/demo/providers/") && strings.HasSuffix(r.URL.Path, "/control") && r.Method == "POST":
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/demo/providers/"), "/control")
		p := g.providers[id]
		if p == nil || p.config.Fake == nil {
			wire.WriteError(w, 404, "not_found", "Fake provider not found.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		defer r.Body.Close()
		var raw struct {
			Status *int   `json:"status"`
			First  *int64 `json:"firstChunkDelayMs"`
			Chunk  *int64 `json:"chunkDelayMs"`
			Cut    *int   `json:"cutAfter"`
		}
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&raw) != nil || d.Decode(new(any)) != io.EOF || raw.Status == nil || raw.First == nil || raw.Chunk == nil || raw.Cut == nil {
			wire.WriteError(w, 400, "invalid_request", "Full fake control object required.")
			return
		}
		c := p.config.Fake.Snapshot().Control
		c.Status = *raw.Status
		c.FirstChunkDelayMs = *raw.First
		c.ChunkDelayMs = *raw.Chunk
		c.CutAfter = *raw.Cut
		v, e := p.config.Fake.Update(c)
		if e != nil {
			wire.WriteError(w, 400, "invalid_request", "Invalid fake control bounds.")
			return
		}
		g.mu.Lock()
		g.seq++
		g.mu.Unlock()
		writeJSON(w, v)
	default:
		wire.WriteError(w, 404, "not_found", "Endpoint not found.")
	}
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
