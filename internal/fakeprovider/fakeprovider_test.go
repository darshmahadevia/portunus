package fakeprovider_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darshmahadevia/portunus/internal/fakeprovider"
)

const requestBody = `{"messages":[{"role":"user","content":"hello"}],"stream":true}`

func TestInvalidConfig(t *testing.T) {
	for _, mutate := range []func(*fakeprovider.Config){
		func(c *fakeprovider.Config) { c.Status = 500 },
		func(c *fakeprovider.Config) { c.ChunkDelay = -1 },
		func(c *fakeprovider.Config) { c.FirstChunkDelay = -1 },
		func(c *fakeprovider.Config) { c.CutAfter = -2 },
		func(c *fakeprovider.Config) { c.Chunks = nil },
	} {
		config := fakeprovider.DefaultConfig()
		mutate(&config)
		if _, err := fakeprovider.New(config); err == nil {
			t.Errorf("accepted invalid config %+v", config)
		}
	}
}

func TestConfigIsCopied(t *testing.T) {
	config := fakeprovider.DefaultConfig()
	config.Chunks = []string{"original"}
	config.ChunkDelay = 0
	handler, err := fakeprovider.New(config)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	mutated := make(chan struct{})
	go func() {
		defer close(mutated)
		for i := 0; i < 1000; i++ {
			config.Chunks[0] = "changed"
		}
	}()
	response, err := http.Post(server.URL+"/v1/chat", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	<-mutated
	if err != nil || string(body) != "event: delta\ndata: {\"text\":\"original\"}\n\nevent: done\ndata: {}\n\n" {
		t.Fatalf("config mutation affected response: %q, %v", body, err)
	}
}

// ReadChatRequest closes the original body after reading and validating the request.
type bodyCloseSignal struct {
	io.ReadCloser
	closed chan struct{}
}

func (b *bodyCloseSignal) Close() error {
	err := b.ReadCloser.Close()
	close(b.closed)
	return err
}

func TestDelaysCancel(t *testing.T) {
	for _, first := range []bool{true, false} {
		name := "between chunks"
		if first {
			name = "first chunk"
		}
		t.Run(name, func(t *testing.T) {
			config := fakeprovider.DefaultConfig()
			config.ChunkDelay = time.Hour
			if first {
				config.FirstChunkDelay = time.Hour
			}
			handler, err := fakeprovider.New(config)
			if err != nil {
				t.Fatal(err)
			}
			parsed := make(chan struct{})
			finished := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Body = &bodyCloseSignal{ReadCloser: r.Body, closed: parsed}
				defer close(finished)
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat", strings.NewReader(requestBody))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			firstDelta := make(chan struct{})
			result := make(chan error, 1)
			go func() {
				response, err := http.DefaultClient.Do(request)
				if err == nil {
					defer response.Body.Close()
					if !first {
						want := "event: delta\ndata: {\"text\":\"Hello\"}\n\n"
						body := make([]byte, len(want))
						_, err = io.ReadFull(response.Body, body)
						if err == nil && string(body) != want {
							err = fmt.Errorf("first delta = %q, want %q", body, want)
						}
						if err != nil {
							result <- err
							return
						}
						close(firstDelta)
					}
					_, err = io.Copy(io.Discard, response.Body)
				}
				result <- err
			}()
			ready := parsed
			if !first {
				ready = firstDelta
			}
			select {
			case <-ready:
			case err := <-result:
				t.Fatalf("request ended before delay was reached: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("provider did not reach delay")
			}
			cancel()
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("fake delay ignored cancellation")
			}
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("canceled request succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("client did not finish after cancellation")
			}
		})
	}
}

func TestNonstreamCutAbortsBeforeJSON(t *testing.T) {
	config := fakeprovider.DefaultConfig()
	config.CutAfter = 1
	handler, err := fakeprovider.New(config)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := (&http.Client{Timeout: 5 * time.Second}).Post(server.URL+"/v1/chat", "application/json", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}]}`))
	if response != nil {
		response.Body.Close()
	}
	if err == nil {
		t.Fatal("cut nonstream response appeared successful")
	}
}
