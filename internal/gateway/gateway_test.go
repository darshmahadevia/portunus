package gateway_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darshmahadevia/portunus/internal/fakeprovider"
	"github.com/darshmahadevia/portunus/internal/gateway"
	"github.com/darshmahadevia/portunus/internal/wire"
)

const chatJSON = `{"messages":[{"role":"user","content":"hello"}]}`
const streamJSON = `{"messages":[{"role":"user","content":"hello"}],"stream":true}`
const delta = "event: delta\ndata: {\"text\":\"first\"}\n\n"
const done = "event: done\ndata: {}\n\n"

func proxyServer(t *testing.T, provider http.Handler, config gateway.Config) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(provider)
	t.Cleanup(upstream.Close)
	config.ProviderURL = upstream.URL + "/ignored-prefix"
	handler, err := gateway.New(config)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func post(t *testing.T, endpoint, body string) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Post(endpoint+"/v1/chat", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func waitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for HTTP behavior")
	}
}

func TestJSONAndSSE(t *testing.T) {
	config := fakeprovider.DefaultConfig()
	config.ChunkDelay = 0
	provider, err := fakeprovider.New(config)
	if err != nil {
		t.Fatal(err)
	}
	server := proxyServer(t, provider, gateway.Config{})
	response := post(t, server.URL, chatJSON)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("JSON response: %s %v", response.Status, response.Header)
	}
	var result wire.ChatResponse
	if err := json.Unmarshal([]byte(readBody(t, response)), &result); err != nil || result.Text != "Hello, world!" {
		t.Fatalf("JSON result: %+v, %v", result, err)
	}
	response = post(t, server.URL, streamJSON)
	want := "event: delta\ndata: {\"text\":\"Hello\"}\n\nevent: delta\ndata: {\"text\":\", \"}\n\nevent: delta\ndata: {\"text\":\"world\"}\n\nevent: delta\ndata: {\"text\":\"!\"}\n\n" + done
	if body := readBody(t, response); body != want || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE response: %v %q", response.Header, body)
	}
}

func TestFirstDeltaArrivesBeforeProviderMayFinish(t *testing.T) {
	finish := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(finish) }) }
	defer release()
	server := proxyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, delta)
		w.(http.Flusher).Flush()
		select {
		case <-finish:
			fmt.Fprint(w, done)
		case <-r.Context().Done():
		}
		close(finished)
	}), gateway.Config{})
	response := post(t, server.URL, streamJSON)
	reader := bufio.NewReader(response.Body)
	var first strings.Builder
	for i := 0; i < 3; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		first.WriteString(line)
	}
	if first.String() != delta {
		t.Fatalf("first event = %q", first.String())
	}
	select {
	case <-finished:
		t.Fatal("provider finished before permission")
	default:
	}
	release()
	rest, err := io.ReadAll(reader)
	if err != nil || string(rest) != done {
		t.Fatalf("terminal event = %q, %v", rest, err)
	}
	waitSignal(t, finished)
}

func TestClosingDownstreamCancelsUpstream(t *testing.T) {
	canceled := make(chan struct{})
	server := proxyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, delta)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(canceled)
	}), gateway.Config{})
	response := post(t, server.URL, streamJSON)
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, canceled)
}

func TestUpstreamErrorsArePreserved(t *testing.T) {
	for _, status := range []int{429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			config := fakeprovider.DefaultConfig()
			config.Status = status
			provider, err := fakeprovider.New(config)
			if err != nil {
				t.Fatal(err)
			}
			upstream := httptest.NewServer(provider)
			defer upstream.Close()
			direct := post(t, upstream.URL, streamJSON)
			want := readBody(t, direct)
			server := proxyServer(t, provider, gateway.Config{})
			response := post(t, server.URL, streamJSON)
			if got := readBody(t, response); response.StatusCode != status || got != want {
				t.Fatalf("got %s %q, want %d %q", response.Status, got, status, want)
			}
		})
	}
}

func TestPreheaderFailureMapping(t *testing.T) {
	t.Run("unavailable", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := listener.Addr().String()
		listener.Close()
		handler, err := gateway.New(gateway.Config{ProviderURL: "http://" + address})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(handler)
		defer server.Close()
		response := post(t, server.URL, chatJSON)
		assertError(t, response, 502, "bad_gateway")
		health, err := http.Get(server.URL + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		defer health.Body.Close()
		if health.StatusCode != 200 {
			t.Fatalf("gateway liveness depends on upstream: %s", health.Status)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		canceled := make(chan struct{})
		server := proxyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
			close(canceled)
		}), gateway.Config{Timeout: 50 * time.Millisecond})
		response := post(t, server.URL, chatJSON)
		assertError(t, response, 504, "gateway_timeout")
		waitSignal(t, canceled)
	})
}

func assertError(t *testing.T, response *http.Response, status int, code string) {
	t.Helper()
	body := readBody(t, response)
	var result wire.ErrorResponse
	if err := json.Unmarshal([]byte(body), &result); err != nil || response.StatusCode != status || result.Error.Code != code || result.Error.Message == "" {
		t.Fatalf("error response = %s %q, decode %v; want %d %s", response.Status, body, err, status, code)
	}
	if response.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("error content type = %q", response.Header.Get("Content-Type"))
	}
}

func TestIncompleteStreamsNeverBecomeSuccess(t *testing.T) {
	for _, cut := range []int{0, 1, 4} {
		t.Run(fmt.Sprintf("cut-%d", cut), func(t *testing.T) {
			config := fakeprovider.DefaultConfig()
			config.ChunkDelay = 0
			config.CutAfter = cut
			provider, err := fakeprovider.New(config)
			if err != nil {
				t.Fatal(err)
			}
			server := proxyServer(t, provider, gateway.Config{})
			response := post(t, server.URL, streamJSON)
			body, err := io.ReadAll(response.Body)
			if err == nil || strings.Contains(string(body), "event: done") || strings.Contains(string(body), `"error"`) || strings.Count(string(body), "event: delta") != cut {
				t.Fatalf("truncated response = %q, %v", body, err)
			}
		})
	}
	t.Run("clean EOF without done", func(t *testing.T) {
		server := proxyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, delta)
		}), gateway.Config{})
		response := post(t, server.URL, streamJSON)
		if body := readBody(t, response); body != delta {
			t.Fatalf("clean incomplete body = %q", body)
		}
	})
	t.Run("deadline after commit", func(t *testing.T) {
		server := proxyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, delta)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			panic(http.ErrAbortHandler)
		}), gateway.Config{Timeout: 50 * time.Millisecond})
		response := post(t, server.URL, streamJSON)
		body, err := io.ReadAll(response.Body)
		if response.StatusCode != 200 || string(body) != delta || err == nil {
			t.Fatalf("committed response = %s %q, %v", response.Status, body, err)
		}
	})
}

func TestLargeValidRequestsReachFakeProvider(t *testing.T) {
	config := fakeprovider.DefaultConfig()
	config.ChunkDelay = 0
	provider, err := fakeprovider.New(config)
	if err != nil {
		t.Fatal(err)
	}
	server := proxyServer(t, provider, gateway.Config{})
	for _, test := range []struct {
		name, content string
	}{
		{"less-than", strings.Repeat("<", 62000)},
		{"unicode-line-separator", "hello" + strings.Repeat("\u2028", 20000)},
		{"unicode-paragraph-separator", "hello" + strings.Repeat("\u2029", 20000)},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `{"messages":[{"role":"user","content":"` + test.content + `"}]}`
			if len(body) > wire.MaxRequestBytes || !json.Valid([]byte(body)) {
				t.Fatalf("regression input must be valid JSON within 64 KiB: %d bytes", len(body))
			}
			response := post(t, server.URL, body)
			resultBody := readBody(t, response)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("%d-byte request: %s %s", len(body), response.Status, resultBody)
			}
			var result wire.ChatResponse
			if err := json.Unmarshal([]byte(resultBody), &result); err != nil || result.Text != "Hello, world!" {
				t.Fatalf("JSON result: %+v, %v", result, err)
			}
		})
	}
}

func TestOriginalValidatedRequestAndProviderBoundary(t *testing.T) {
	observed := make(chan *http.Request, 1)
	bodies := make(chan string, 1)
	server := proxyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		observed <- r.Clone(context.Background())
		bodies <- string(body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"ok"}`)
	}), gateway.Config{})
	originalBody := " { \"messages\" : [{\"content\":\"hello <\u2028\u2029世界\",\"role\":\"user\"}] } "
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/chat?destination=http://evil.invalid&fault=503", strings.NewReader(originalBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	for _, name := range []string{"Authorization", "Cookie", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "X-Fake-Status", "X-Fault-Control"} {
		request.Header.Set(name, "spoofed")
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	readBody(t, response)
	upstream := <-observed
	body := <-bodies
	if upstream.URL.Path != "/v1/chat" || upstream.URL.RawQuery != "" || upstream.ContentLength != int64(len(originalBody)) || body != originalBody {
		t.Fatalf("outbound request: %s length %d body %q", upstream.URL, upstream.ContentLength, body)
	}
	for name := range request.Header {
		if name != "Content-Type" && upstream.Header.Get(name) != "" {
			t.Errorf("forwarded caller header %s", name)
		}
	}
}

func TestConcurrentAdmissionAndRecovery(t *testing.T) {
	const limit = 4
	entered := make(chan struct{}, limit)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	server := proxyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
			fmt.Fprint(w, `{"text":"ok"}`)
		case <-r.Context().Done():
		}
	}), gateway.Config{MaxConcurrent: limit})
	results := make(chan error, limit)
	for i := 0; i < limit; i++ {
		go func() {
			client := &http.Client{Timeout: 5 * time.Second}
			response, err := client.Post(server.URL+"/v1/chat", "application/json", strings.NewReader(chatJSON))
			if err == nil {
				defer response.Body.Close()
				_, err = io.Copy(io.Discard, response.Body)
				if response.StatusCode != 200 {
					err = fmt.Errorf("admitted request returned %s", response.Status)
				}
			}
			results <- err
		}()
	}
	for i := 0; i < limit; i++ {
		waitSignal(t, entered)
	}
	assertError(t, post(t, server.URL, chatJSON), 503, "overloaded")
	unblock()
	for i := 0; i < limit; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if response := post(t, server.URL, chatJSON); response.StatusCode != 200 {
		t.Fatalf("capacity did not recover: %s", response.Status)
	}
}

func TestStalledConsumerIsBounded(t *testing.T) {
	canceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(canceled)
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := strings.Repeat("x", 64<<10)
		for r.Context().Err() == nil {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	defer upstream.Close()
	handler, err := gateway.New(gateway.Config{ProviderURL: upstream.URL, Timeout: 30 * time.Second, WriteTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	connection, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, err = fmt.Fprintf(connection, "POST /v1/chat HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(streamJSON), streamJSON)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	upstreamDone, gatewayDone := canceled, handlerDone
	for upstreamDone != nil || gatewayDone != nil {
		select {
		case <-upstreamDone:
			upstreamDone = nil
		case <-gatewayDone:
			gatewayDone = nil
		case <-deadline.C:
			t.Fatalf("stalled consumer: upstream canceled = %t, gateway handler completed = %t", upstreamDone == nil, gatewayDone == nil)
		}
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, config := range []gateway.Config{
		{}, {ProviderURL: "ftp://localhost"}, {ProviderURL: "http://"},
		{ProviderURL: "http://user:secret@localhost"}, {ProviderURL: "http://localhost?fault=1"},
		{ProviderURL: "http://localhost#fragment"}, {ProviderURL: "http://localhost:65536"},
		{ProviderURL: "http://localhost:"}, {ProviderURL: "http://localhost:0"}, {ProviderURL: "http://localhost", Timeout: -1},
		{ProviderURL: "http://localhost", WriteTimeout: -1}, {ProviderURL: "http://localhost", MaxConcurrent: -1},
	} {
		if _, err := gateway.New(config); err == nil {
			t.Errorf("accepted invalid config %+v", config)
		}
	}
}
