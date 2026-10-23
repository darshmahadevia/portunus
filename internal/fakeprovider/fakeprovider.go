package fakeprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/darshmahadevia/portunus/internal/wire"
)

type Config struct {
	Chunks          []string
	ChunkDelay      time.Duration
	FirstChunkDelay time.Duration
	Status          int
	CutAfter        int
}

func DefaultConfig() Config {
	return Config{
		Chunks:     []string{"Hello", ", ", "world", "!"},
		ChunkDelay: 200 * time.Millisecond,
		Status:     http.StatusOK,
		CutAfter:   -1,
	}
}

func New(config Config) (http.Handler, error) {
	if config.ChunkDelay < 0 || config.FirstChunkDelay < 0 {
		return nil, fmt.Errorf("fake delays must not be negative")
	}
	if config.Status != 200 && config.Status != 429 && config.Status != 503 {
		return nil, fmt.Errorf("fake status must be 200, 429, or 503")
	}
	if config.CutAfter < -1 || len(config.Chunks) == 0 {
		return nil, fmt.Errorf("fake cut-after must be at least -1 and chunks must not be empty")
	}
	config.Chunks = append([]string(nil), config.Chunks...)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", wire.Healthz)
	mux.HandleFunc("/v1/chat", func(w http.ResponseWriter, r *http.Request) {
		request, _, ok := wire.ReadChatRequest(w, r)
		if !ok {
			return
		}
		if config.Status != http.StatusOK {
			code := "provider_unavailable"
			if config.Status == http.StatusTooManyRequests {
				code = "rate_limited"
			}
			wire.WriteError(w, config.Status, code, http.StatusText(config.Status))
			return
		}
		if !wait(r.Context(), config.FirstChunkDelay) {
			return
		}
		if !request.Stream {
			// Abort before headers so the gateway can return a provider error.
			if config.CutAfter >= 0 && config.CutAfter <= len(config.Chunks) {
				panic(http.ErrAbortHandler)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(wire.ChatResponse{Text: strings.Join(config.Chunks, "")})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		controller := http.NewResponseController(w)
		w.WriteHeader(http.StatusOK)
		if err := controller.Flush(); err != nil {
			return
		}
		for i, chunk := range config.Chunks {
			if config.CutAfter == i {
				panic(http.ErrAbortHandler)
			}
			if i > 0 && !wait(r.Context(), config.ChunkDelay) {
				return
			}
			payload, _ := json.Marshal(wire.Delta{Text: chunk})
			if _, err := fmt.Fprintf(w, "event: delta\ndata: %s\n\n", payload); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
		if config.CutAfter == len(config.Chunks) {
			panic(http.ErrAbortHandler)
		}
		_, _ = fmt.Fprint(w, "event: done\ndata: {}\n\n")
		_ = controller.Flush()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		wire.WriteError(w, http.StatusNotFound, "not_found", "Endpoint not found.")
	})
	return mux, nil
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
