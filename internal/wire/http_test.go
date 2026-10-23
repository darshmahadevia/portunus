package wire_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darshmahadevia/portunus/internal/fakeprovider"
	"github.com/darshmahadevia/portunus/internal/gateway"
	"github.com/darshmahadevia/portunus/internal/wire"
)

func TestHTTPValidation(t *testing.T) {
	config := fakeprovider.DefaultConfig()
	config.ChunkDelay = 0
	fake, err := fakeprovider.New(config)
	if err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(fake)
	defer provider.Close()
	proxy, err := gateway.New(gateway.Config{ProviderURL: provider.URL})
	if err != nil {
		t.Fatal(err)
	}
	gatewayServer := httptest.NewServer(proxy)
	defer gatewayServer.Close()
	valid := `{"messages":[{"role":"user","content":"hello"}]}`
	message := `{"role":"user","content":"hello"}`
	cases := []struct {
		name, method, mediaType, body string
		status                        int
	}{
		{"valid", "POST", "application/json", valid, 200},
		{"charset", "POST", "application/json; charset=utf-8", valid, 200},
		{"all roles", "POST", "application/json", `{"messages":[{"role":"system","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":"c"}]}`, 200},
		{"32 messages", "POST", "application/json", `{"messages":[` + strings.Repeat(message+",", 31) + message + `]}`, 200},
		{"method", "GET", "application/json", valid, 405},
		{"media type", "POST", "text/plain", valid, 415},
		{"missing media type", "POST", "", valid, 415},
		{"bad media parameter", "POST", "application/json; charset", valid, 415},
		{"empty", "POST", "application/json", "", 400},
		{"null", "POST", "application/json", "null", 400},
		{"array", "POST", "application/json", "[]", 400},
		{"syntax", "POST", "application/json", "{", 400},
		{"missing messages", "POST", "application/json", `{}`, 400},
		{"no messages", "POST", "application/json", `{"messages":[]}`, 400},
		{"33 messages", "POST", "application/json", `{"messages":[` + strings.Repeat(message+",", 32) + message + `]}`, 400},
		{"unknown field", "POST", "application/json", `{"messages":[` + message + `],"model":"fake"}`, 400},
		{"unknown message field", "POST", "application/json", `{"messages":[{"role":"user","content":"hello","extra":0}]}`, 400},
		{"invalid role", "POST", "application/json", `{"messages":[{"role":"tool","content":"hello"}]}`, 400},
		{"missing role", "POST", "application/json", `{"messages":[{"content":"hello"}]}`, 400},
		{"blank content", "POST", "application/json", `{"messages":[{"role":"user","content":" \n\t"}]}`, 400},
		{"null message", "POST", "application/json", `{"messages":[null]}`, 400},
		{"wrong content type", "POST", "application/json", `{"messages":[{"role":"user","content":3}]}`, 400},
		{"wrong stream type", "POST", "application/json", `{"messages":[` + message + `],"stream":"yes"}`, 400},
		{"trailing JSON", "POST", "application/json", valid + " {}", 400},
		{"trailing junk", "POST", "application/json", valid + " x", 400},
		{"oversized", "POST", "application/json", `{"messages":[{"role":"user","content":"` + strings.Repeat("x", wire.MaxRequestBytes) + `"}]}`, 413},
		{"oversized whitespace", "POST", "application/json", valid + strings.Repeat(" ", wire.MaxRequestBytes), 413},
	}
	for name, endpoint := range map[string]string{"fake": provider.URL, "gateway": gatewayServer.URL} {
		t.Run(name, func(t *testing.T) {
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					request, err := http.NewRequest(test.method, endpoint+"/v1/chat", strings.NewReader(test.body))
					if err != nil {
						t.Fatal(err)
					}
					request.Header.Set("Content-Type", test.mediaType)
					response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
					if err != nil {
						t.Fatal(err)
					}
					defer response.Body.Close()
					body, err := io.ReadAll(response.Body)
					if err != nil || response.StatusCode != test.status {
						t.Fatalf("got %s %q, %v; want %d", response.Status, body, err, test.status)
					}
					if test.status != 200 {
						var result wire.ErrorResponse
						if err := json.Unmarshal(body, &result); err != nil || result.Error.Code == "" || result.Error.Message == "" {
							t.Fatalf("invalid error envelope %q: %v", body, err)
						}
					}
					if test.status == 405 && response.Header.Get("Allow") != "POST" {
						t.Fatal("missing Allow header")
					}
				})
			}
		})
	}
}

func TestHealthAndUnknownRoutes(t *testing.T) {
	provider, _ := fakeprovider.New(fakeprovider.DefaultConfig())
	proxy, _ := gateway.New(gateway.Config{ProviderURL: "http://127.0.0.1:1"})
	for name, handler := range map[string]http.Handler{"fake": provider, "gateway": proxy} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			for _, test := range []struct {
				method, path string
				status       int
			}{{"GET", "/healthz", 200}, {"POST", "/healthz", 405}, {"GET", "/unknown", 404}} {
				request, _ := http.NewRequest(test.method, server.URL+test.path, nil)
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || response.StatusCode != test.status || !json.Valid(body) {
					t.Fatal(fmt.Sprintf("%s %s: %s %q, %v", test.method, test.path, response.Status, body, err))
				}
			}
		})
	}
}
