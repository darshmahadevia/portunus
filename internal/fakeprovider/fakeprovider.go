package fakeprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/darshmahadevia/portunus/internal/wire"
)

type Config struct {
	Chunks          []string
	ChunkDelay      time.Duration
	FirstChunkDelay time.Duration
	Status          int
	CutAfter        int
}
type Control struct {
	Status            int   `json:"status"`
	FirstChunkDelayMs int64 `json:"firstChunkDelayMs"`
	ChunkDelayMs      int64 `json:"chunkDelayMs"`
	CutAfter          int   `json:"cutAfter"`
}
type Snapshot struct {
	Control
	Revision  uint64 `json:"revision"`
	Requests  uint64 `json:"requests"`
	Completed uint64 `json:"completed"`
	Canceled  uint64 `json:"canceled"`
}
type Server struct {
	mu                                      sync.Mutex
	config                                  Config
	revision, requests, completed, canceled uint64
}

func DefaultConfig() Config {
	return Config{Chunks: []string{"Hello", ", ", "world", "!"}, ChunkDelay: 20 * time.Millisecond, Status: 200, CutAfter: -1}
}
func validate(c Config) error {
	if c.ChunkDelay < 0 || c.FirstChunkDelay < 0 || c.ChunkDelay > time.Hour || c.FirstChunkDelay > time.Hour || c.CutAfter < -1 || c.CutAfter > 4096 || len(c.Chunks) == 0 || len(c.Chunks) > 4096 {
		return fmt.Errorf("invalid fake bounds")
	}
	if c.Status != 200 && c.Status != 429 && c.Status != 503 {
		return fmt.Errorf("fake status must be 200, 429 or 503")
	}
	total := 0
	for _, s := range c.Chunks {
		if !utf8.ValidString(s) {
			return fmt.Errorf("invalid chunk UTF-8")
		}
		total += len(s)
	}
	if total > 2<<20 {
		return fmt.Errorf("fake output too large")
	}
	return nil
}
func New(c Config) (*Server, error) {
	if e := validate(c); e != nil {
		return nil, e
	}
	c.Chunks = append([]string(nil), c.Chunks...)
	return &Server{config: c, revision: 1}, nil
}
func (s *Server) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.config
	return Snapshot{Control: Control{c.Status, c.FirstChunkDelay.Milliseconds(), c.ChunkDelay.Milliseconds(), c.CutAfter}, Revision: s.revision, Requests: s.requests, Completed: s.completed, Canceled: s.canceled}
}
func (s *Server) Update(c Control) (Snapshot, error) {
	s.mu.Lock()
	next := s.config
	next.Status = c.Status
	next.CutAfter = c.CutAfter
	if c.FirstChunkDelayMs < 0 || c.FirstChunkDelayMs > 3600000 || c.ChunkDelayMs < 0 || c.ChunkDelayMs > 3600000 {
		s.mu.Unlock()
		return Snapshot{}, fmt.Errorf("delays must be 0..3600000 milliseconds")
	}
	next.FirstChunkDelay = time.Duration(c.FirstChunkDelayMs) * time.Millisecond
	next.ChunkDelay = time.Duration(c.ChunkDelayMs) * time.Millisecond
	if e := validate(next); e != nil {
		s.mu.Unlock()
		return Snapshot{}, e
	}
	s.config = next
	s.revision++
	s.mu.Unlock()
	return s.Snapshot(), nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		wire.Healthz(w, r)
		return
	}
	if r.URL.Path != "/v1/chat" {
		wire.WriteError(w, 404, "not_found", "Endpoint not found.")
		return
	}
	req, _, ok := wire.ReadChatRequest(w, wire.InternalRequest(r))
	if !ok {
		return
	}
	s.mu.Lock()
	c := s.config
	s.requests++
	s.mu.Unlock()
	complete := false
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if complete {
			s.completed++
		} else if r.Context().Err() != nil {
			s.canceled++
		}
	}()
	if c.Status != 200 {
		if c.Status == 429 {
			w.Header().Set("Retry-After", "1")
		}
		wire.WriteError(w, c.Status, "provider_unavailable", "Provider unavailable.")
		return
	}
	if !wait(r.Context(), c.FirstChunkDelay) {
		return
	}
	// Fake output tokens are synthetic UTF-8 byte units. Never split a rune.
	remaining := req.MaxOutputTokens
	chunks := make([]string, 0, len(c.Chunks))
	for _, chunk := range c.Chunks {
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
			for !utf8.ValidString(chunk) && len(chunk) > 0 {
				chunk = chunk[:len(chunk)-1]
			}
		}
		if chunk != "" {
			chunks = append(chunks, chunk)
			remaining -= len(chunk)
		}
		if remaining == 0 {
			break
		}
	}
	usage := wire.Usage{Authoritative: true, OutputTokens: int64(req.MaxOutputTokens - remaining)}
	for _, m := range req.Messages {
		usage.InputTokens += int64(len(m.Content) + len(m.Role) + 16)
	}
	if !req.Stream {
		if c.CutAfter >= 0 && c.CutAfter <= len(chunks) {
			panic(http.ErrAbortHandler)
		}
		w.Header().Set("Content-Type", "application/json")
		if json.NewEncoder(w).Encode(wire.ChatResponse{Text: strings.Join(chunks, ""), Usage: usage}) == nil {
			complete = true
		}
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	controller := http.NewResponseController(w)
	for i, chunk := range chunks {
		if c.CutAfter == i {
			panic(http.ErrAbortHandler)
		}
		if i > 0 && !wait(r.Context(), c.ChunkDelay) {
			return
		}
		data, _ := json.Marshal(wire.Delta{Text: chunk})
		if _, e := fmt.Fprintf(w, "event: delta\ndata: %s\n\n", data); e != nil {
			return
		}
		if controller.Flush() != nil {
			return
		}
	}
	if c.CutAfter == len(chunks) {
		panic(http.ErrAbortHandler)
	}
	data, _ := json.Marshal(struct {
		Usage wire.Usage `json:"usage"`
	}{usage})
	if _, e := fmt.Fprintf(w, "event: done\ndata: %s\n\n", data); e == nil && controller.Flush() == nil {
		complete = true
	}
}
func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return ctx.Err() == nil
	}
}
