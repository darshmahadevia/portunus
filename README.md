<img src="assets/icon.svg" width="48" height="48" alt="">

# Portunus

Portunus is an LLM gateway that routes chat requests across providers and makes each attempt visible in an operator console.
It handles streaming fallback, circuit recovery, client rate limits, and estimated spending budgets.
A successful client response can retain a failed primary attempt, so the console separates request outcomes from provider work.

The backend uses Go and `net/http`. The dashboard uses React, TypeScript, and Vite, with compiled assets embedded in the Go executable.
The default demo runs two fake HTTP providers that return `Hello, world!` for any prompt.
They reproduce timeouts, HTTP failures, and interrupted streams without model credentials or paid API calls.

![Portunus dashboard with observed latency, outcomes, provider traffic, and client budgets](assets/dashboard.png)

## Start the local demo

Install Go 1.22 or later, then run this command from the repository root:

```sh
go run ./cmd/portunus
```

Wait for the readiness message:

```text
ready gateway=http://127.0.0.1:8080 fake=http://127.0.0.1:8081 backup=http://127.0.0.1:8082
```

Open [localhost:8080](http://localhost:8080).
The checked-in dashboard needs no Node installation or frontend server to run.
A fresh process starts with empty charts. Send a request to create observations.
Press Ctrl-C to stop the gateway and both fake providers.

See the [dashboard guide](docs/dashboard-guide.md) for prerequisites, configuration, and troubleshooting.

## Engineering decisions

### One owner for bounded retries

The gateway owns the attempt loop. Provider adapters open, read, and close one generation session without retrying.
By default, a request shares a three-attempt allowance and a 30-second deadline across providers.
A separate 300 ms deadline covers the first meaningful content. Transient failures can trigger another eligible attempt before response commitment.
Backoff observes cancellation, and provider retry delays prevent immediate reuse of a throttled target.

### Recovery requires a circuit probe

Three consecutive transient provider failures open the circuit by default.
After cooldown, one incoming request can acquire the half-open probe. Waiting or restoring the fake provider alone does not prove recovery.
Generation-tagged leases prevent an older concurrent attempt from overwriting a newer circuit state.
The circuit skips unavailable targets while allowing actual requests to test recovery.

### Public output commits the provider choice

Before the first public response write, the gateway can discard a failed attempt and fall back.
After streaming text starts, it reports interruption instead of appending another provider's answer.
JSON responses stay buffered until completion, so they retain the fallback option longer.
Only a terminal `done` event establishes streaming success. HTTP 200 or partial text alone does not.

**Stop request**, a client disconnect, or the request deadline cancels upstream work through Go contexts and session cleanup.
Cancellation does not prove that an external provider stopped generating or charging.

### Budget reservations precede dispatch

Attempt acquisition checks circuit eligibility, provider capacity, and the client's remaining budget under one mutex.
It atomically reserves estimated cost using input bounds, configured prices, and the requested output cap.
Concurrent requests cannot reserve the same remaining funds. Network calls and backoff run outside the lock.

Authoritative usage settles the reservation. Uncertain execution retains a conservative estimated charge, while proven unstarted work costs zero.
Money uses integer micro-US dollars with overflow checks and upward rounding.
Reported usage can exceed the estimate, so this admission policy is not a guarantee about an external provider's bill.

### Cache admission requires a complete primary answer

Exact caching is opt-in and requires temperature zero.
Only a bounded, complete first-attempt answer from the primary with authoritative usage can populate the cache.
Fallback completions and interrupted responses cannot enter it.
Keys include client identity, generation settings, route targets, and configuration revision.
TTL, LRU eviction, entry limits, and byte limits bound storage.
A cache hit still consumes rate allowance but creates no provider attempt or new provider cost.

### Observations distinguish requests from attempts

Each request record contains its provider attempts, durations, usage certainty, reservations, and estimated cost.
One successful request may include a failed primary and a successful backup. A rejected request or cache hit may have no attempts.
Telemetry excludes prompts, generated text, client keys, and provider credentials.

Lifetime counters and retained latency samples have different scopes.
The dashboard names its observation windows and leaves missing latency unavailable rather than displaying zero.
The [observation guide](docs/dashboard-guide.md#read-the-observations-correctly) explains retention and chart samples.

## Follow a request

Use this path to inspect the default demo:

1. Open **Playground**. Keep `demo-key` and `general`, set **Maximum output** to 64, and leave **Use exact cache** unchecked.
2. Click **Send request**, then **Inspect request**. In **Requests**, confirm one successful primary attempt.
3. Return to **Playground** and click **Run failover demo**. Inspect the backup completions and failed primary attempts.
4. Open **Overview** to compare client outcomes, provider attempts, circuit state, and estimated spending.
5. Click **Restore primary** in **Playground**. After probe eligibility, send an uncached request to test recovery.

The [full walkthrough](docs/dashboard-guide.md#follow-a-request-through-the-three-pages) also covers slow starts, interrupted streams, cancellation, exact caching, and rate and budget rejection.
It explains why the same failure can produce fallback before output and interruption after output.

## Validate the behavior

Full validation requires Node 22.12 or later and npm in addition to Go:

```sh
make verify
make demo
```

`make verify` checks frontend types, runs frontend tests, builds the dashboard, checks Go formatting, runs `go vet`, and runs Go tests with the race detector.
`make demo` starts an isolated instance and verifies scenarios through real HTTP, including fallback, circuit recovery, interruption, cache reuse, policy rejection, and accounting consistency.
It prints `PASS` lines and writes `artifacts/verification.json`.
The [CI workflow](.github/workflows/verify.yml) also checks that committed frontend assets match the build.

For Go-only checks, run `make verify-go`. To rebuild embedded frontend assets, run `make web`, then restart or rebuild the Go program.
See [rebuild instructions](docs/dashboard-guide.md#rebuild-and-verify) for the complete procedure.

## Read further

- [Architecture](docs/architecture.md) explains request ownership, response commitment, circuit leases, reservations, and cache identity.
- [API contract](docs/api-contract.md) documents chat requests, stream events, operator APIs, status codes, and retention.
- [Dashboard guide](docs/dashboard-guide.md) preserves the detailed how-to and explanations for each demo scenario.
- [Recorded benchmarks and methodology](docs/benchmarks/README.md) link raw observations and measurement limits. Run `make benchmark` to reproduce the local workload.
- [Build plan](docs/build-plan.md) and [decision trail](docs/decisions.tsv) record implementation choices and verification evidence.
- [Demo configuration](configs/demo.json) and [compatible-provider example](configs/openaicompat.json) define routes and policies. Read the [configuration guide](docs/dashboard-guide.md#configure-routing-and-providers) before using a real service.

## Scope and limits

Portunus keeps bounded, synchronized state in one instance. Restarting resets counters, circuits, budgets, cache entries, and request history.
State is not durable or shared across replicas. The two fake listeners share the gateway process, so the demo does not establish host or process fault tolerance.

The default providers generate deterministic test text with synthetic usage and illustrative prices.
Local loopback measurements do not establish Internet-provider latency, production capacity, or billing savings.
The optional compatible adapter supports a text-only Chat Completions streaming subset. Fixture tests do not establish live-service compatibility.
Tools, images, accounts, durable audit storage, and billing are outside scope.
