package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darshmahadevia/portunus/internal/provider"
	"github.com/darshmahadevia/portunus/internal/wire"
)

func TestCompatibleProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer provider-secret" || r.Header.Get("Cookie") != "" || r.URL.RawQuery != "" || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("invalid upstream boundary")
		}
		var body struct {
			Model    string          `json:"model"`
			Max      int             `json:"max_tokens"`
			Options  map[string]bool `json:"stream_options"`
			Messages []wire.Message  `json:"messages"`
			Stream   bool            `json:"stream"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "configured-model" || body.Max != 42 || !body.Stream || !body.Options["include_usage"] || body.Messages[0].Content != "hello" {
			t.Errorf("invalid compatible payload: %+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	a := provider.HTTP{URL: server.URL + "/v1/chat/completions", Kind: "openaicompat", APIKey: "provider-secret"}
	s, e := a.Open(context.Background(), provider.AttemptRequest{Model: "configured-model", Request: wire.ChatRequest{Messages: []wire.Message{{Role: "user", Content: "hello"}}, MaxOutputTokens: 42}})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	text := ""
	usage := wire.Usage{}
	done := false
	for {
		ev, e := s.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if ev.Kind == "delta" {
			text += ev.Text
		}
		if ev.Kind == "done" {
			done = true
			usage = ev.Usage
		}
	}
	if text != "hello" || !done || !usage.Authoritative || usage.InputTokens != 7 || usage.OutputTokens != 3 {
		t.Fatalf("incomplete protocol: %q %+v %v", text, usage, done)
	}
}
func TestProtocolRejectsMalformedAndTruncated(t *testing.T) {
	for name, body := range map[string]string{
		"done without finish":  "data: [DONE]\n\n",
		"usage without finish": "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n",
		"truncated":            "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
		"malformed":            "data: {\n\n",
		"partial usage":        "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1}}\n\n",
		"unframed terminal":    "data: [DONE]", "empty": "data: {}\n\n", "negative usage": "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":-1,\"completion_tokens\":0}}\n\n", "large frame": "data: " + strings.Repeat("x", provider.MaxFrameBytes) + "\n\n", "tool": "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{}]}}]}\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, body)
			}))
			defer server.Close()
			a := provider.HTTP{URL: server.URL, Kind: "openaicompat"}
			s, e := a.Open(context.Background(), provider.AttemptRequest{})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			for i := 0; i < 5; i++ {
				ev, e := s.Next()
				if e != nil {
					if e == io.EOF {
						t.Fatal("truncation accepted")
					}
					return
				}
				if ev.Kind == "done" {
					t.Fatal("invalid stream completed")
				}
			}
			t.Fatal("invalid stream did not fail")
		})
	}
}
func TestStatusAndRedirectPrivacy(t *testing.T) {
	for _, status := range []int{400, 401, 429, 500, 503, 307} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			called := false
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
			defer destination.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "2")
				w.Header().Set("Location", destination.URL)
				w.WriteHeader(status)
				fmt.Fprint(w, "secret private response")
			}))
			defer server.Close()
			a := provider.HTTP{URL: server.URL, APIKey: "secret"}
			_, e := a.Open(context.Background(), provider.AttemptRequest{})
			var p *provider.Error
			if !errors.As(e, &p) || p.Status != status || p.RetryAfter != 2*time.Second || strings.Contains(e.Error(), "private") || called {
				t.Fatalf("unsafe status result: %v called=%v", e, called)
			}
			if provider.Transient(e) != (status == 429 || status >= 500) {
				t.Fatal("incorrect transient classification")
			}
		})
	}
}
func TestOutputIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 40; i++ {
			fmt.Fprintf(w, "event: delta\ndata: {\"text\":\"%s\"}\n\n", strings.Repeat("x", 65536))
		}
		fmt.Fprint(w, "event: done\ndata: {}\n\n")
	}))
	defer server.Close()
	a := provider.HTTP{URL: server.URL, Kind: "fake"}
	s, e := a.Open(context.Background(), provider.AttemptRequest{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	total := 0
	for {
		ev, e := s.Next()
		if e != nil {
			break
		}
		if ev.Kind == "done" {
			t.Fatal("oversized output accepted")
		}
		total += len(ev.Text)
	}
	if total > provider.MaxOutputBytes {
		t.Fatal("read beyond bound")
	}
}
