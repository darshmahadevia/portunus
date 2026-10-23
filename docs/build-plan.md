# Complete Portunus

## Completion criteria

The app is complete when one local command starts the gateway, two independently controlled fake HTTP providers, and an embedded TypeScript dashboard. A repeatable demonstration must exercise provider fallback, circuit opening and recovery, partial-stream failure, cache reuse, client rate limits, and budget rejection. The dashboard must show records produced by those requests.

The gateway must also support a configured OpenAI-compatible HTTP provider. Protocol fixtures establish its behavior without credentials or paid API calls. Real service credentials remain optional.

Verification must cover cancellation, response commitment, bounded retries, circuit concurrency, cache isolation, atomic budget reservations, and request/attempt accounting. Benchmarks must check response completion and upstream work counts, retain raw results, and distinguish simulated usage and estimated prices from bills.

## Delivery units

1. Establish the normalized provider session, public/API contracts, and bounded state model. Preserve the useful stage-one streaming tests.
2. Build the fake and OpenAI-compatible adapters, one attempt loop, provider routing, deadlines, retries, and circuit leases.
3. Add exact opt-in caching, configured client keys, token buckets, atomic spending reservations, and request/attempt telemetry.
4. Build the TypeScript dashboard and connect its request and fault controls to the real engine. Inspect desktop and mobile rendering and keyboard interaction.
5. Run the scenario verifier and at least five benchmark rounds. Record methodology, actual results, and limitations. Complete independent review before publishing to main.

## Architectural decisions

The implementation remains a single Go gateway instance with bounded in-memory policy, cache, and telemetry state. This keeps concurrency and failure behavior inspectable. State resets on restart; multiple replicas and durable billing are outside scope.

Provider normalization is now justified by multiple protocols. A provider opens one session; the engine consumes validated text, usage, and completion events. Adapters never retry. The engine owns one finite attempt allowance and one total request deadline.

Opening an upstream connection does not commit the downstream response. The first public write does. Retries and fallback stop after commitment. Sessions close and leases settle on every exit path.

The dashboard uses the requested dark, minimal design. Compiled assets are checked in and embedded in Go. Rebuilding requires Node; running the app requires Go only.

## Throughput checkpoint

- Blocking first steps: settle session and API shapes before delegates implement either side.
- Independent workstreams: one backend owner writes the coupled Go engine; a dashboard owner writes only frontend and embedded assets; a later benchmark owner writes the workload tools.
- Shared mutable state: each file has one implementation owner. Reviewers are read-only. Policy mutation is synchronized and never holds a lock during network I/O.
- Smallest safe decomposition: backend modules share contracts and are implemented together; UI and measurement tooling can proceed independently after those contracts exist.

## Rigor

Use real HTTP fixtures and race-enabled tests for core policies, browser interaction for the dashboard, and a compiled executable for end-to-end scenarios. Budget and circuit tests must force concurrent interleavings. Streaming tests must observe completion rather than infer it from HTTP status. Results that cannot establish their claim are marked inconclusive.
