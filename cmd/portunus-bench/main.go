// Command portunus-bench verifies the public demo API and measures completed work.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type options struct {
	verify                        bool
	rounds, requests, concurrency int
	output, gateway, direct       string
	cooldown, firstTimeout        time.Duration
}

func parse(args []string) (options, error) {
	o := options{}
	f := flag.NewFlagSet("portunus-bench", flag.ContinueOnError)
	f.BoolVar(&o.verify, "verify", false, "run HTTP scenario verification instead of benchmarks")
	f.IntVar(&o.rounds, "rounds", 5, "benchmark rounds (5-100)")
	f.IntVar(&o.requests, "requests", 100, "requests per phase (1-10000)")
	f.IntVar(&o.concurrency, "concurrency", 8, "workers (1-64)")
	f.StringVar(&o.output, "output", "", "write complete JSON report to this path (otherwise stdout)")
	f.StringVar(&o.gateway, "gateway", "", "external demo gateway base URL; requires -direct")
	f.StringVar(&o.direct, "direct", "", "external primary fake base URL; requires -gateway")
	f.DurationVar(&o.cooldown, "cooldown", 200*time.Millisecond, "configured circuit cooldown (external server must match)")
	f.DurationVar(&o.firstTimeout, "first-chunk-timeout", 150*time.Millisecond, "configured first chunk timeout (external server must match)")
	if e := f.Parse(args); e != nil {
		return o, e
	}
	if f.NArg() != 0 {
		return o, errors.New("unexpected positional arguments")
	}
	if o.rounds < 5 || o.rounds > 100 || o.requests < 1 || o.requests > 10000 || o.concurrency < 1 || o.concurrency > 64 || o.rounds*o.requests > 100000 {
		return o, errors.New("bounds: rounds 5-100, requests 1-10000, concurrency 1-64, rounds*requests <=100000")
	}
	if o.cooldown <= 0 || o.cooldown > 10*time.Second || o.firstTimeout <= 0 || o.firstTimeout > 5*time.Second {
		return o, errors.New("cooldown must be (0,10s], first-chunk-timeout (0,5s]")
	}
	if (o.gateway == "") != (o.direct == "") {
		return o, errors.New("-gateway and -direct must be supplied together")
	}
	for _, s := range []string{o.gateway, o.direct} {
		if s == "" {
			continue
		}
		u, e := url.Parse(s)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return o, fmt.Errorf("invalid endpoint %q", s)
		}
	}
	return o, nil
}

type report struct {
	HostLoad     string            `json:"hostLoad"`
	Timestamp    string            `json:"timestamp"`
	GoVersion    string            `json:"goVersion"`
	Platform     string            `json:"platform"`
	CPUs         int               `json:"logicalCPUs"`
	Requests     int               `json:"requestsPerPhase"`
	Concurrency  int               `json:"concurrency"`
	Rounds       int               `json:"rounds"`
	External     bool              `json:"external"`
	Workload     map[string]any    `json:"workload"`
	Limits       []string          `json:"knownLimits"`
	Scenarios    []scenario        `json:"scenarios,omitempty"`
	Phases       []phase           `json:"phases,omitempty"`
	AcrossRounds map[string]spread `json:"acrossRounds,omitempty"`
	Failures     []string          `json:"failures"`
	Valid        bool              `json:"valid"`
}

func main() {
	o, e := parse(os.Args[1:])
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(2)
	}
	if e = run(o); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(o options) error {
	output, err := prepareOutput(o.output)
	if err != nil {
		return fmt.Errorf("prepare output: %w", err)
	}
	defer output.close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := report{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, CPUs: runtime.NumCPU(), Requests: o.requests, Concurrency: o.concurrency, Rounds: o.rounds, External: o.gateway != "", Failures: []string{}, Workload: map[string]any{"stream": true, "temperature": 0, "chunkDelayMs": 2, "firstChunkDelayMs": 0, "maxOutputTokens": 256, "outageRequestsPerRound": max(3, o.requests), "percentileMethod": "nearest rank", "firstChunkTimeoutMs": float64(o.firstTimeout) / float64(time.Millisecond), "circuitCooldownMs": float64(o.cooldown) / float64(time.Millisecond), "generatorMaxIdleConns": 128, "generatorMaxIdleConnsPerHost": 64, "baselineWarmupRequestsPerPathPerRound": o.concurrency, "effectiveBaselineWarmupRequests": 0, "initialDirectProbeRequests": 1, "controlRestoration": "restore initial fake configurations with independent bounded context", "costLabel": "synthetic/estimated microUSD; not bills or real savings"}, Limits: []string{"Loopback synthetic four-chunk workload; not a production capacity estimate.", "No profiling or limiter attribution: descriptive timings only, no speedup or winner claim.", "Client and server share host resources; OS scheduling and other activity can affect results.", "External mode mutates demo controls and requires exclusive access, primary/backup routing, threshold 3, matching timeouts and demo/rate/budget keys.", "Throughput uses verified completions divided by phase wall time; failed requests remain in raw observations and failure counts.", "Outage elapsed includes the circuit snapshot after three sequential threshold requests; at least three outage requests run per round.", "HTTP response bytes exclude headers and framing; TTFT starts before sending request and ends at first nonempty delta."}}
	loadCtx, loadCancel := context.WithTimeout(ctx, time.Second)
	load, loadErr := exec.CommandContext(loadCtx, "uptime").Output()
	loadCancel()
	if loadErr == nil {
		r.HostLoad = strings.TrimSpace(string(load))
	} else {
		r.HostLoad = "unavailable: " + loadErr.Error()
	}
	h := &harness{ctx: ctx, o: o, client: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConns: 128, MaxIdleConnsPerHost: 64}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	defer h.client.CloseIdleConnections()
	e := func() error {
		if o.gateway == "" {
			cleanup, err := h.start()
			if err != nil {
				return err
			}
			defer func() {
				if err := cleanup(); err != nil {
					r.Failures = append(r.Failures, "shutdown: "+err.Error())
				}
			}()
		} else {
			h.gateway = strings.TrimSuffix(o.gateway, "/")
			h.direct = strings.TrimSuffix(o.direct, "/")
		}
		original, err := h.snapshot()
		if err != nil {
			return err
		}
		defer func() {
			if err := h.restoreControls(original); err != nil {
				r.Failures = append(r.Failures, "restore original controls: "+err.Error())
			}
		}()
		if err := h.healthy(); err != nil {
			return err
		}
		warm := h.chat(h.direct, "", false, true, "warmup")
		if warm.Error != "" || warm.Text == "" {
			return fmt.Errorf("direct warmup: %+v", warm)
		}
		h.expected = warm.Text
		r.Workload["expectedText"] = warm.Text
		if o.verify {
			return h.verify(&r)
		}
		return h.benchmark(&r)
	}()
	if e != nil {
		r.Failures = append(r.Failures, e.Error())
	}
	r.Failures = append(r.Failures, h.cleanupErrors...)
	r.Valid = len(r.Failures) == 0
	if err := output.write(r); err != nil {
		return err
	}
	if !r.Valid {
		return fmt.Errorf("verification invalid: %s", strings.Join(r.Failures, "; "))
	}
	return nil
}

type harness struct {
	cleanupErrors                     []string
	ctx                               context.Context
	o                                 options
	client                            *http.Client
	gateway, direct, backup, expected string
}

func (h *harness) start() (func() error, error) {
	dir, e := os.MkdirTemp("", "portunus-bench-*")
	if e != nil {
		return nil, e
	}
	cleanupDir := func() { os.RemoveAll(dir) }
	config := map[string]any{"clients": []any{map[string]any{"id": "demo", "key": "demo-key", "rpm": 100000000, "burst": 1000000, "budgetMicros": 1000000000000}, map[string]any{"id": "rate", "key": "rate-key", "rpm": 1, "burst": 1, "budgetMicros": 1000000000}, map[string]any{"id": "budget", "key": "budget-key", "rpm": 1000000, "burst": 100000, "budgetMicros": 0}}, "firstChunkTimeout": int64(h.o.firstTimeout), "timeout": int64(10 * time.Second), "circuitCooldown": int64(h.o.cooldown), "circuitThreshold": 3, "maxAttempts": 3, "cache": map[string]any{"maxEntries": 128, "maxBytes": 4 << 20, "ttl": int64(time.Hour)}}
	b, _ := json.Marshal(config)
	path := filepath.Join(dir, "config.json")
	if e = os.WriteFile(path, b, 0600); e != nil {
		cleanupDir()
		return nil, e
	}
	buildCtx, cancel := context.WithTimeout(h.ctx, 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", filepath.Join(dir, "portunus"), "./cmd/portunus")
	if b, e = build.CombinedOutput(); e != nil {
		cleanupDir()
		return nil, fmt.Errorf("build app: %w: %s", e, b)
	}
	cmd := exec.Command(filepath.Join(dir, "portunus"), "-gateway-listen", "127.0.0.1:0", "-fake-listen", "127.0.0.1:0", "-backup-listen", "127.0.0.1:0", "-config", path, "-fake-chunks", "Hello|, |world|!")
	stderr, e := cmd.StderrPipe()
	if e != nil {
		cleanupDir()
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		cleanupDir()
		return nil, e
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ready := make(chan string, 1)
	logs := make(chan string, 1)
	go func() {
		s := bufio.NewScanner(stderr)
		var tail strings.Builder
		for s.Scan() {
			line := s.Text()
			if strings.HasPrefix(line, "ready ") {
				select {
				case ready <- line:
				default:
				}
			}
			if tail.Len() < 8192 {
				tail.WriteString(line + "\n")
			}
		}
		logs <- tail.String()
	}()
	cleanup := func() error {
		signalErr := cmd.Process.Signal(syscall.SIGTERM)
		if errors.Is(signalErr, os.ErrProcessDone) {
			signalErr = nil
		}
		var waitErr error
		select {
		case waitErr = <-done:
		case <-time.After(7 * time.Second):
			_ = cmd.Process.Kill()
			waitErr = errors.Join(errors.New("SIGTERM shutdown timed out; forced kill"), <-done)
		}
		return errors.Join(signalErr, waitErr, os.RemoveAll(dir))
	}
	select {
	case line := <-ready:
		for _, f := range strings.Fields(line) {
			k, v, ok := strings.Cut(f, "=")
			if ok {
				switch k {
				case "gateway":
					h.gateway = v
				case "fake":
					h.direct = v
				case "backup":
					h.backup = v
				}
			}
		}
		if h.gateway == "" || h.direct == "" || h.backup == "" {
			cleanup()
			return nil, fmt.Errorf("invalid ready line: %s", line)
		}
		return cleanup, nil
	case e := <-done:
		cleanupDir()
		return nil, fmt.Errorf("app exited before ready: %v: %s", e, <-logs)
	case <-time.After(15 * time.Second):
		cleanup()
		return nil, errors.New("app ready timeout")
	case <-h.ctx.Done():
		cleanup()
		return nil, h.ctx.Err()
	}
}

// Stage output beside its destination so the final rename replaces it atomically.
type reportOutput struct {
	path string
	file *os.File
}

func prepareOutput(path string) (*reportOutput, error) {
	out := &reportOutput{path: path}
	if path == "" {
		return out, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("destination must be a regular file: %s", path)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	out.file, err = os.CreateTemp(filepath.Dir(path), ".portunus-report-*.json")
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (out *reportOutput) close() {
	if out.file != nil {
		out.file.Close()
		os.Remove(out.file.Name())
	}
}
func (out *reportOutput) write(r report) error {
	if out.file == nil {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	enc := json.NewEncoder(out.file)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return err
	}
	if err := out.file.Sync(); err != nil {
		return err
	}
	if err := out.file.Close(); err != nil {
		return err
	}
	return os.Rename(out.file.Name(), out.path)
}
