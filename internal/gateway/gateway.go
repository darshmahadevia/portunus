package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/darshmahadevia/portunus/internal/wire"
)

type Config struct {
	ProviderURL   string
	Timeout       time.Duration
	WriteTimeout  time.Duration
	MaxConcurrent int
}

func New(config Config) (http.Handler, error) {
	target, err := url.Parse(config.ProviderURL)
	if err != nil || target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" || target.Opaque != "" {
		return nil, fmt.Errorf("provider URL must be an absolute http(s) URL without credentials, query, or fragment")
	}
	if port := target.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, fmt.Errorf("provider URL port must be 1-65535")
		}
	}
	if strings.HasSuffix(target.Host, ":") || (strings.Contains(target.Hostname(), ":") && net.ParseIP(target.Hostname()) == nil) {
		return nil, fmt.Errorf("invalid provider URL host")
	}
	if config.Timeout < 0 || config.WriteTimeout < 0 || config.MaxConcurrent < 0 {
		return nil, fmt.Errorf("gateway timeouts and concurrency limit must be positive")
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.WriteTimeout == 0 {
		if config.Timeout > time.Duration(1<<63-1)-5*time.Second {
			return nil, fmt.Errorf("gateway timeout is too large")
		}
		config.WriteTimeout = config.Timeout + 5*time.Second
	}
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = 64
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: min(config.Timeout, 5*time.Second), KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = min(config.Timeout, 10*time.Second)
	transport.TLSHandshakeTimeout = min(config.Timeout, 5*time.Second)
	transport.MaxIdleConnsPerHost = config.MaxConcurrent
	transport.DisableCompression = true
	proxy := &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1,
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.URL.Path = "/v1/chat"
			request.Out.URL.RawPath = ""
			request.Out.URL.RawQuery = ""
			request.Out.URL.ForceQuery = false
			request.Out.Header = make(http.Header)
			request.Out.Header.Set("Content-Type", "application/json")
			request.Out.Header.Set("Accept", "application/json, text/event-stream")
			request.Out.Trailer = nil
			request.Out.TransferEncoding = nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			status, code, message := http.StatusBadGateway, "bad_gateway", "Provider request failed."
			var networkError net.Error
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(r.Context().Err(), context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
				status, code, message = http.StatusGatewayTimeout, "gateway_timeout", "Provider request timed out."
			}
			wire.WriteError(w, status, code, message)
		},
	}
	slots := make(chan struct{}, config.MaxConcurrent)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", wire.Healthz)
	mux.HandleFunc("/v1/chat", func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			wire.WriteError(w, http.StatusServiceUnavailable, "overloaded", "Gateway is at capacity.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), config.Timeout)
		defer cancel()
		r = r.WithContext(ctx)
		controller := http.NewResponseController(w)
		// Context cancellation alone cannot interrupt a blocked client write or body read.
		_ = controller.SetReadDeadline(time.Now().Add(config.Timeout))
		defer controller.SetReadDeadline(time.Time{})
		_ = controller.SetWriteDeadline(time.Now().Add(config.WriteTimeout))
		defer controller.SetWriteDeadline(time.Time{})
		_, body, ok := wire.ReadChatRequest(w, r)
		if !ok {
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		defer r.Body.Close()
		proxy.ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		wire.WriteError(w, http.StatusNotFound, "not_found", "Endpoint not found.")
	})
	return mux, nil
}
