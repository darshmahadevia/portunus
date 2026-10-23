// Package provider normalizes bounded, pull-based HTTP generation sessions.
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/darshmahadevia/portunus/internal/wire"
)

const MaxOutputBytes = 2 << 20
const MaxFrameBytes = 256 << 10

type Event struct {
	Kind  string
	Text  string
	Usage wire.Usage
}
type AttemptRequest struct {
	Request wire.ChatRequest
	Model   string
}
type Session interface {
	Next() (Event, error)
	Close() error
}
type Adapter interface {
	Open(context.Context, AttemptRequest) (Session, error)
}
type Error struct {
	Status     int
	RetryAfter time.Duration
	Unstarted  bool
	Message    string
}

func (e *Error) Error() string { return e.Message }
func Transient(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Status == 0 || e.Status == 429 || e.Status >= 500
	}
	return true
}

type HTTP struct {
	URL, Kind, APIKey string
	Client            *http.Client
}

func ValidateURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(u.Host, ":") {
		return errors.New("provider URL must be absolute http(s) without credentials, query or fragment")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return errors.New("invalid provider port")
		}
	}
	return nil
}
func (h *HTTP) Open(ctx context.Context, a AttemptRequest) (Session, error) {
	sessionContext, cancel := context.WithCancel(ctx)
	opened := false
	defer func() {
		if !opened {
			cancel()
		}
	}()
	var payload any
	a.Request.Stream = true
	if h.Kind == "openaicompat" {
		payload = struct {
			Model       string          `json:"model"`
			Messages    []wire.Message  `json:"messages"`
			Stream      bool            `json:"stream"`
			MaxTokens   int             `json:"max_tokens"`
			Temperature float64         `json:"temperature"`
			Options     map[string]bool `json:"stream_options"`
		}{a.Model, a.Request.Messages, true, a.Request.MaxOutputTokens, a.Request.Temperature, map[string]bool{"include_usage": true}}
	} else {
		payload = a.Request
	}
	body, e := json.Marshal(payload)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(sessionContext, "POST", h.URL, bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if h.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.APIKey)
	}
	client := h.Client
	if client == nil {
		client = &http.Client{Transport: http.DefaultTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		delay := time.Duration(0)
		if n, e := strconv.Atoi(res.Header.Get("Retry-After")); e == nil && n > 0 && n < 86400 {
			delay = time.Duration(n) * time.Second
		} else if t, e := http.ParseTime(res.Header.Get("Retry-After")); e == nil {
			delay = max(0, time.Until(t))
		}
		return nil, &Error{Status: res.StatusCode, RetryAfter: delay, Unstarted: h.Kind == "fake" && res.StatusCode == 429, Message: "provider HTTP failure"}
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		res.Body.Close()
		return nil, errors.New("provider did not return SSE")
	}
	scan := bufio.NewScanner(io.LimitReader(res.Body, 16<<20))
	scan.Buffer(make([]byte, 4096), MaxFrameBytes)
	opened = true
	return &httpSession{body: res.Body, scan: scan, kind: h.Kind, cancel: cancel}, nil
}

type httpSession struct {
	cancel   context.CancelFunc
	body     io.ReadCloser
	scan     *bufio.Scanner
	kind     string
	terminal bool
	bytes    int
	frames   int
	usage    wire.Usage
	finished bool
}

func (s *httpSession) Close() error { s.cancel(); return s.body.Close() }
func (s *httpSession) Next() (Event, error) {
	if s.terminal {
		return Event{}, io.EOF
	}
	for {
		event, data := "", ""
		size := 0
		terminated := false
		scanned := false
		for s.scan.Scan() {
			scanned = true
			line := s.scan.Text()
			size += len(line)
			if size > MaxFrameBytes {
				return Event{}, errors.New("oversized provider frame")
			}
			if line == "" {
				terminated = true
				break
			}
			if strings.HasPrefix(line, "event:") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			}
			if strings.HasPrefix(line, "data:") {
				if data != "" {
					data += "\n"
				}
				data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if !scanned || !terminated {
			if e := s.scan.Err(); e != nil {
				return Event{}, e
			}
			return Event{}, io.ErrUnexpectedEOF
		}
		if data == "" {
			continue
		}
		if data == "null" {
			return Event{}, errors.New("null provider event")
		}
		if !utf8.ValidString(data) {
			return Event{}, errors.New("invalid event UTF-8")
		}
		s.frames++
		if s.frames > 32768 {
			return Event{}, errors.New("too many provider frames")
		}
		var ev Event
		if s.kind == "openaicompat" {
			if data == "[DONE]" {
				if !s.finished {
					return Event{}, errors.New("provider completion without text finish")
				}
				s.terminal = true
				return Event{Kind: "done", Usage: s.usage}, nil
			}
			var frame struct {
				Choices []struct {
					Delta struct {
						Content   *string         `json:"content"`
						Role      string          `json:"role"`
						ToolCalls json.RawMessage `json:"tool_calls"`
					} `json:"delta"`
					Finish *string `json:"finish_reason"`
					Index  int     `json:"index"`
				} `json:"choices"`
				Usage *struct {
					Input  *int64 `json:"prompt_tokens"`
					Output *int64 `json:"completion_tokens"`
				} `json:"usage"`
				Error json.RawMessage `json:"error"`
			}
			if json.Unmarshal([]byte(data), &frame) != nil || len(frame.Error) > 0 || len(frame.Choices) > 1 {
				return Event{}, errors.New("malformed provider event")
			}
			if frame.Usage != nil {
				if frame.Usage.Input == nil || frame.Usage.Output == nil || !validTokens(*frame.Usage.Input, *frame.Usage.Output) {
					return Event{}, errors.New("invalid usage")
				}
				s.usage = wire.Usage{InputTokens: *frame.Usage.Input, OutputTokens: *frame.Usage.Output, Authoritative: true}
			}
			if len(frame.Choices) == 0 {
				if frame.Usage == nil {
					return Event{}, errors.New("empty provider event")
				}
				return Event{Kind: "usage", Usage: s.usage}, nil
			}
			c := frame.Choices[0]
			if c.Index != 0 || len(c.Delta.ToolCalls) > 0 {
				return Event{}, errors.New("unsupported provider event")
			}
			if s.finished && c.Delta.Content != nil && *c.Delta.Content != "" {
				return Event{}, errors.New("content after finish")
			}
			if c.Finish != nil {
				if *c.Finish != "stop" && *c.Finish != "length" {
					return Event{}, errors.New("unsupported provider finish reason")
				}
				s.finished = true
			}
			if c.Delta.Content == nil || *c.Delta.Content == "" {
				continue
			}
			ev = Event{Kind: "delta", Text: *c.Delta.Content}
		} else {
			switch event {
			case "delta":
				var d wire.Delta
				if json.Unmarshal([]byte(data), &d) != nil || d.Text == "" {
					return Event{}, errors.New("malformed delta")
				}
				ev = Event{Kind: "delta", Text: d.Text}
			case "done":
				var d struct {
					Usage wire.Usage `json:"usage"`
				}
				if json.Unmarshal([]byte(data), &d) != nil || !validTokens(d.Usage.InputTokens, d.Usage.OutputTokens) {
					return Event{}, errors.New("malformed completion")
				}
				s.terminal = true
				return Event{Kind: "done", Usage: d.Usage}, nil
			case "usage":
				var u wire.Usage
				if json.Unmarshal([]byte(data), &u) != nil || !validTokens(u.InputTokens, u.OutputTokens) {
					return Event{}, errors.New("malformed usage")
				}
				return Event{Kind: "usage", Usage: u}, nil
			default:
				return Event{}, errors.New("unknown provider event")
			}
		}
		s.bytes += len(ev.Text)
		if s.bytes > MaxOutputBytes {
			return Event{}, fmt.Errorf("provider output exceeds limit")
		}
		return ev, nil
	}
}

func validTokens(input, output int64) bool {
	return input >= 0 && output >= 0 && input <= 1<<30 && output <= 1<<30
}
