<img src="assets/icon.svg" width="64" height="64" alt="">

# Portunus

Portunus is a fault-tolerant LLM gateway built as a software engineering portfolio project. Applications use one API while the gateway handles provider communication and, in later stages, failures and traffic policies.

The first stage is a streaming HTTP proxy with a configurable fake provider. It needs no model credentials and makes no paid API calls.

## Run locally

Install Go 1.22 or later, then start both local HTTP servers:

```sh
go run ./cmd/portunus
```

The gateway listens on `127.0.0.1:8080`. The fake provider listens on `127.0.0.1:8081`. Press Ctrl-C to stop both servers.

In another terminal, request a streamed answer:

```sh
curl --no-buffer http://127.0.0.1:8080/v1/chat \
	-H 'Content-Type: application/json' \
	-d '{"messages":[{"role":"user","content":"Hello"}],"stream":true}'
```

You should see `delta` events arrive incrementally, followed by a `done` event. Only `done` establishes successful completion. A closed connection without `done` means the answer is incomplete.

For one JSON response, omit `stream` or set it to `false`:

```sh
curl http://127.0.0.1:8080/v1/chat \
	-H 'Content-Type: application/json' \
	-d '{"messages":[{"role":"user","content":"Hello"}]}'
```

The fake returns fixed text rather than answering the prompt. The prompt still passes through request validation and the real HTTP provider connection.

Inspect available configuration:

```sh
go run ./cmd/portunus -help
```

## Demonstrate failures

Stop the running instance before starting a different scenario.

To return a provider outage before any answer starts:

```sh
go run ./cmd/portunus -fake-status 503
```

Run the chat request with `curl -i` to inspect the forwarded HTTP status and JSON error. Stage one exposes this failure to the caller. Automatic fallback comes in stage two.

To cut a stream after two chunks:

```sh
go run ./cmd/portunus -fake-cut-after 2
```

Run the streaming request again. It returns partial text without a `done` event. Curl can also report a transfer error because the provider deliberately aborts its HTTP response.

To time out before the provider begins its answer:

```sh
go run ./cmd/portunus -timeout 1s -fake-first-chunk-delay 2s
```

The gateway returns a timeout error. After an answer starts, a timeout interrupts the stream instead of replacing it with JSON.

## Verify behavior

```sh
make verify
```

Verification checks formatting, runs `go vet`, and runs tests with the race detector. The HTTP tests cover incremental delivery, cancellation, explicit completion, interrupted streams, validation, and upstream failures.

## Understand the design

The gateway and fake provider use separate HTTP listeners in one process. The gateway validates requests and forwards responses with Go's standard HTTP reverse proxy. It does not accumulate streaming answers in memory.

Read [the architecture and tradeoffs](docs/architecture.md) for ownership boundaries, completion semantics, cancellation, and exercises for explaining the code in an interview.

## Scope and next stages

Stage one establishes the transport behavior. It has no real model integration, retries, fallback, caching, client limits, spending budgets, or dashboard. Local concurrency and request bounds protect the demo, but do not implement per-client traffic policies.

The next stage adds classified failures, bounded retries, provider fallback, and circuit recovery. Caching and client policies follow. Metrics, a dashboard, and reproducible benchmarks come afterward.

No latency, throughput, availability, or cost-saving results have been measured yet. Future results must include their workload and measurement method.
