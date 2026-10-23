package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type observation struct {
	ErrorCode string   `json:"errorCode,omitempty"`
	Index     int      `json:"index"`
	Status    int      `json:"httpStatus"`
	TotalMS   float64  `json:"totalMs"`
	TTFTMS    *float64 `json:"ttftMs"`
	Bytes     int64    `json:"responseBytes"`
	Complete  bool     `json:"complete"`
	CacheHit  bool     `json:"cacheHit"`
	Provider  string   `json:"provider"`
	Cost      string   `json:"costMicros"`
	Text      string   `json:"text"`
	Error     string   `json:"error,omitempty"`
}
type metadata struct {
	Text     string `json:"text"`
	CacheHit bool   `json:"cacheHit"`
	Provider string `json:"provider"`
	Cost     string `json:"costMicros"`
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

const maxResponse = 1 << 20
const maxFrames = 8192

type countedReader struct {
	r io.Reader
	n int64
}

func (c *countedReader) Read(p []byte) (int, error) {
	n, e := c.r.Read(p)
	c.n += int64(n)
	return n, e
}

// readSSE consumes through EOF, requires a terminal done, and bounds frames and bytes.
func readSSE(r io.Reader, start time.Time, o *observation) error {
	c := &countedReader{r: io.LimitReader(r, maxResponse+1)}
	defer func() { o.Bytes = c.n }()
	s := bufio.NewScanner(c)
	s.Buffer(make([]byte, 4096), 64<<10)
	var event string
	var data []string
	frames := 0
	var text strings.Builder
	terminal := false
	defer func() { o.Text = text.String() }()
	dispatch := func() error {
		if event == "" && len(data) == 0 {
			return nil
		}
		frames++
		if frames > maxFrames {
			return errors.New("SSE frame limit")
		}
		if terminal {
			return errors.New("frame after terminal done")
		}
		payload := []byte(strings.Join(data, "\n"))
		switch event {
		case "delta":
			var d struct {
				Text string `json:"text"`
			}
			if e := json.Unmarshal(payload, &d); e != nil {
				return fmt.Errorf("delta JSON: %w", e)
			}
			if d.Text != "" && o.TTFTMS == nil {
				x := ms(time.Since(start))
				o.TTFTMS = &x
			}
			if text.Len()+len(d.Text) > maxResponse {
				return errors.New("text limit")
			}
			text.WriteString(d.Text)
		case "done":
			var m metadata
			if e := json.Unmarshal(payload, &m); e != nil {
				return fmt.Errorf("done JSON: %w", e)
			}
			o.Complete = true
			o.TotalMS = ms(time.Since(start))
			o.CacheHit = m.CacheHit
			o.Provider = m.Provider
			o.Cost = m.Cost
			terminal = true
		case "error":
			return fmt.Errorf("SSE error: %s", payload)
		default:
			return fmt.Errorf("unknown SSE event %q", event)
		}
		return nil
	}
	for s.Scan() {
		line := s.Text()
		if line == "" {
			if e := dispatch(); e != nil {
				return e
			}
			event = ""
			data = nil
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		k, v, _ := strings.Cut(line, ":")
		v = strings.TrimPrefix(v, " ")
		switch k {
		case "event":
			event = v
		case "data":
			data = append(data, v)
		}
	}
	if e := s.Err(); e != nil {
		return e
	}
	if c.n > maxResponse {
		return errors.New("response byte limit")
	}
	if event != "" || len(data) > 0 {
		return errors.New("unterminated SSE frame")
	}
	if !terminal {
		return errors.New("EOF without done")
	}
	return nil
}
func (h *harness) chat(base, key string, cache, stream bool, prompt string) observation {
	start := time.Now()
	o := observation{}
	body, _ := json.Marshal(map[string]any{"messages": []any{map[string]string{"role": "user", "content": prompt}}, "stream": stream, "cache": cache, "temperature": 0, "max_output_tokens": 256})
	req, e := http.NewRequestWithContext(h.ctx, "POST", strings.TrimSuffix(base, "/")+"/v1/chat", bytes.NewReader(body))
	if e != nil {
		o.Error = e.Error()
		return o
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, e := h.client.Do(req)
	if e != nil {
		o.TotalMS = ms(time.Since(start))
		o.Error = e.Error()
		return o
	}
	defer resp.Body.Close()
	o.Status = resp.StatusCode
	if resp.StatusCode != 200 {
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
		o.Bytes = int64(len(b))
		o.TotalMS = ms(time.Since(start))
		var wire struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(b, &wire) == nil {
			o.ErrorCode = wire.Error.Code
		}
		o.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, b)
		if err != nil {
			o.Error += "; " + err.Error()
		}
		return o
	}
	if stream {
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
			e = errors.New("stream response is not text/event-stream")
		} else {
			e = readSSE(resp.Body, start, &o)
		}
	} else {
		var b []byte
		b, e = io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
		o.Bytes = int64(len(b))
		if e == nil && len(b) > maxResponse {
			e = errors.New("response byte limit")
		}
		if e == nil {
			var m metadata
			e = json.Unmarshal(b, &m)
			if e == nil {
				o.Text = m.Text
				o.Provider = m.Provider
				o.Cost = m.Cost
				o.CacheHit = m.CacheHit
				o.Complete = true
			}
		}
		o.TotalMS = ms(time.Since(start))
	}
	if e != nil {
		o.Error = e.Error()
		o.TotalMS = ms(time.Since(start))
	}
	if o.Error == "" && h.expected != "" && o.Text != h.expected {
		o.Error = fmt.Sprintf("response mismatch: got %q want %q", o.Text, h.expected)
	}
	return o
}

type snapshot struct {
	Totals    struct{ Requests, Succeeded, Failed, Rejected, Active, CacheHits, ProviderAttempts, Fallbacks int64 }
	Providers []struct {
		ID, Circuit      string
		Requests, Active int64
		Fake             *struct {
			Requests, Completed, Canceled                     int64
			Status, FirstChunkDelayMs, ChunkDelayMs, CutAfter int
		}
	}
}
type work struct {
	Requests  int64            `json:"requests"`
	Attempts  int64            `json:"engineAttempts"`
	Fake      map[string]int64 `json:"fakeRequests"`
	Completed map[string]int64 `json:"fakeCompleted"`
	Canceled  map[string]int64 `json:"fakeCanceled"`
}

func delta(a, b snapshot) work {
	w := work{Requests: b.Totals.Requests - a.Totals.Requests, Attempts: b.Totals.ProviderAttempts - a.Totals.ProviderAttempts, Fake: map[string]int64{}, Completed: map[string]int64{}, Canceled: map[string]int64{}}
	for _, p := range b.Providers {
		if p.Fake != nil {
			w.Fake[p.ID] = p.Fake.Requests
			w.Completed[p.ID] = p.Fake.Completed
			w.Canceled[p.ID] = p.Fake.Canceled
		}
	}
	for _, p := range a.Providers {
		if p.Fake != nil {
			w.Fake[p.ID] -= p.Fake.Requests
			w.Completed[p.ID] -= p.Fake.Completed
			w.Canceled[p.ID] -= p.Fake.Canceled
		}
	}
	return w
}
func (w work) fakeTotal() int64 {
	var n int64
	for _, v := range w.Fake {
		n += v
	}
	return n
}
func (h *harness) api(method, path string, body any, out any) error {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req, e := http.NewRequestWithContext(h.ctx, method, h.gateway+path, bytes.NewReader(b))
	if e != nil {
		return e
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, e := h.client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	b, e = io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if e != nil {
		return e
	}
	if len(b) > maxResponse {
		return errors.New("API response limit")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, b)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}
func (h *harness) snapshot() (snapshot, error) {
	var s snapshot
	e := h.api("GET", "/api/v1/snapshot", nil, &s)
	if e == nil {
		seen := map[string]bool{}
		for _, p := range s.Providers {
			seen[p.ID] = p.Fake != nil
		}
		if !seen["primary"] || !seen["backup"] {
			e = errors.New("snapshot must expose primary and backup fake counters")
		}
	}
	return s, e
}
func (h *harness) control(id string, status, first, chunk, cut int) error {
	return h.api("POST", "/api/v1/demo/providers/"+id+"/control", map[string]int{"status": status, "firstChunkDelayMs": first, "chunkDelayMs": chunk, "cutAfter": cut}, nil)
}
func (h *harness) healthy() error {
	a := h.control("primary", 200, 0, 2, -1)
	b := h.control("backup", 200, 0, 2, -1)
	return errors.Join(a, b)
}
func circuit(s snapshot, id string) string {
	for _, p := range s.Providers {
		if p.ID == id {
			return p.Circuit
		}
	}
	return ""
}
func (h *harness) recover() error {
	if e := h.healthy(); e != nil {
		return e
	}
	timer := time.NewTimer(h.o.cooldown + 25*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-h.ctx.Done():
		return h.ctx.Err()
	}
	o := h.chat(h.gateway, "demo-key", false, true, "recovery probe")
	if o.Error != "" || !o.Complete || o.Provider != "primary" {
		return fmt.Errorf("recovery probe: %+v", o)
	}
	s, e := h.snapshot()
	if e != nil {
		return e
	}
	if circuit(s, "primary") != "closed" {
		return fmt.Errorf("recovery circuit = %q", circuit(s, "primary"))
	}
	return nil
}

// Restoration must still run after the work context is canceled.
func (h *harness) restoreControls(original snapshot) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cleanup := *h
	cleanup.ctx = ctx
	var errs []error
	for _, p := range original.Providers {
		if p.Fake != nil {
			f := p.Fake
			if err := cleanup.control(p.ID, f.Status, f.FirstChunkDelayMs, f.ChunkDelayMs, f.CutAfter); err != nil {
				errs = append(errs, fmt.Errorf("restore %s: %w", p.ID, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (h *harness) cleanupHealthy() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cleanup := *h
	cleanup.ctx = ctx
	if err := cleanup.healthy(); err != nil {
		h.cleanupErrors = append(h.cleanupErrors, "reset controls to healthy: "+err.Error())
	}
}

// Cancellation counters can settle just after the backup response completes.
func (h *harness) waitForPrimaryCancellation(before snapshot) (snapshot, error) {
	ctx, cancel := context.WithTimeout(h.ctx, time.Second)
	defer cancel()
	probe := *h
	probe.ctx = ctx
	for {
		after, err := probe.snapshot()
		if err != nil {
			return snapshot{}, fmt.Errorf("observe primary cancellation: %w", err)
		}
		w := delta(before, after)
		for _, p := range after.Providers {
			if p.ID == "primary" && p.Active == 0 && w.Fake[p.ID] == 1 && w.Canceled[p.ID] == 1 && w.Completed[p.ID] == 0 {
				return after, nil
			}
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return snapshot{}, fmt.Errorf("primary cancellation/quiescence not observed: %w", ctx.Err())
		case <-timer.C:
		}
	}
}
