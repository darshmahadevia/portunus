package main

import (
	"strings"
	"testing"
	"time"
)

func TestSSECompletion(t *testing.T) {
	for _, tc := range []struct {
		name, body, text string
		ok               bool
	}{
		{"complete CRLF", "event: delta\r\ndata: {\"text\":\"hello\"}\r\n\r\nevent: done\r\ndata: {\"provider\":\"primary\",\"costMicros\":\"7\"}\r\n\r\n", "hello", true},
		{"incomplete", "event: delta\ndata: {\"text\":\"hello\"}\n\n", "hello", false},
		{"error", "event: delta\ndata: {\"text\":\"partial\"}\n\nevent: error\ndata: {\"code\":\"cut\"}\n\n", "partial", false},
		{"no terminating blank", "event: done\ndata: {}\n", "", false},
		{"malformed delta", "event: delta\ndata: oops\n\nevent: done\ndata: {}\n\n", "", false},
		{"after done", "event: done\ndata: {}\n\nevent: delta\ndata: {\"text\":\"late\"}\n\n", "", false},
		{"duplicate done", "event: done\ndata: {}\n\nevent: done\ndata: {}\n\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var o observation
			e := readSSE(strings.NewReader(tc.body), time.Now(), &o)
			if (e == nil) != tc.ok {
				t.Fatalf("error=%v observation=%+v", e, o)
			}
			if o.Text != tc.text {
				t.Fatalf("text=%q", o.Text)
			}
			if o.Bytes != int64(len(tc.body)) {
				t.Fatalf("bytes=%d want %d", o.Bytes, len(tc.body))
			}
			if tc.ok && (!o.Complete || o.TTFTMS == nil || o.Provider != "primary" || o.Cost != "7") {
				t.Fatalf("metadata=%+v", o)
			}
		})
	}
}
func TestSSEBoundsAndFirstNonempty(t *testing.T) {
	var o observation
	if e := readSSE(strings.NewReader("event: delta\ndata: {\"text\":\"\"}\n\nevent: done\ndata: {}\n\n"), time.Now(), &o); e != nil || o.TTFTMS != nil {
		t.Fatalf("empty delta TTFT: %+v %v", o, e)
	}
	if e := readSSE(strings.NewReader("data: "+strings.Repeat("x", 70<<10)), time.Now(), &o); e == nil {
		t.Fatal("accepted oversized frame")
	}
	if e := readSSE(strings.NewReader(strings.Repeat("event: delta\ndata: {}\n\n", maxFrames+1)), time.Now(), &o); e == nil {
		t.Fatal("accepted too many frames")
	}
}
func TestPercentile(t *testing.T) {
	a := []float64{9, 1, 5, 3, 7}
	for _, tc := range []struct{ p, want float64 }{{0, 1}, {.5, 5}, {.95, 9}, {.99, 9}, {1, 9}} {
		if got := percentile(a, tc.p); got != tc.want {
			t.Fatalf("p=%v got=%v", tc.p, got)
		}
	}
	if a[0] != 9 {
		t.Fatal("modified observations")
	}
	if percentile(nil, .5) != 0 {
		t.Fatal("empty percentile")
	}
}
func TestBounds(t *testing.T) {
	for _, args := range [][]string{{"-rounds=4"}, {"-rounds=101"}, {"-requests=0"}, {"-requests=10001"}, {"-concurrency=0"}, {"-concurrency=65"}, {"-gateway=http://localhost"}, {"-direct=http://localhost"}, {"-rounds=100", "-requests=10000"}, {"-cooldown=0"}, {"-first-chunk-timeout=6s"}, {"oops"}, {"-gateway=file:///tmp/x", "-direct=http://localhost"}} {
		if _, e := parse(args); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if o, e := parse(nil); e != nil || o.rounds != 5 || o.requests != 100 || o.concurrency != 8 {
		t.Fatalf("defaults %+v %v", o, e)
	}
}
