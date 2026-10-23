# Run and investigate the local dashboard

Use this guide to reproduce fallback, circuit recovery, stream interruption, cache reuse, and policy rejection through the dashboard.
The default fake providers return `Hello, world!` for any prompt. They exercise real HTTP and streaming without generating model answers.
For the project purpose and engineering overview, return to the [Portunus README](../README.md).

## Prerequisites

You need Go 1.22 or later, a browser, and free loopback ports 8080, 8081, and 8082.
Run all commands from the repository root, including commands in this guide.
The walkthroughs assume the default configuration and a fresh process unless a step says otherwise.
You do not need provider credentials. Node 22.12 or later and npm are needed for frontend rebuilds and full validation.

## Find a task

- [Follow requests and reproduce failure scenarios](#follow-a-request-through-the-three-pages).
- [Interpret counters, latency, and retained history](#read-the-observations-correctly).
- [Call the API without the dashboard](#send-requests-without-the-dashboard).
- [Configure routing or a compatible provider](#configure-routing-and-providers).
- [Understand the gateway's boundaries](#understand-the-gateways-boundaries).
- [Troubleshoot the demo](#troubleshoot-a-demo).
- [Rebuild and verify](#rebuild-and-verify).
- [Reproduce local measurements](#reproduce-the-local-measurements).

## Start the local demo

Start the gateway and both fake HTTP providers with one command:

```sh
go run ./cmd/portunus
```

Wait for the terminal to print:

```text
ready gateway=http://127.0.0.1:8080 fake=http://127.0.0.1:8081 backup=http://127.0.0.1:8082
```

Open [localhost:8080](http://localhost:8080). The gateway and both fake providers bind to loopback. The compiled frontend is checked in and embedded in Go, so this command needs no frontend development server.

A fresh process has no request history. Empty charts and unavailable latency values mean there are no observations yet. Press Ctrl-C to stop all three servers and cancel in-flight requests. Restarting resets the demo's counters, circuits, budgets, cache, and provider settings.

## Follow a request through the three pages

Use the pages in this order when investigating behavior:

1. Open **Overview** at `/` to monitor request outcomes, provider routing, latency, and estimated cost.
2. Open **Playground** at `/playground` to send requests and change local fake-provider behavior.
3. Open **Requests** at `/requests` to search, filter by outcome, and expand a request's provider attempts.

Playground keeps your prompt, output, and demo results when you switch pages. A browser reload resets this local form state. Request records remain in the gateway until its retention limit or a process restart. **Inspect request** searches retained history automatically, including records outside the latest 100. If a record has expired, Requests explains why it is unavailable.

Overview keeps monitoring in one place. Playground groups actions that create traffic or change failures. Requests gives investigation its own space, because one successful client request can contain a failed primary attempt and a successful backup attempt.

### Send a healthy request

1. Start a fresh demo and open **Playground**.
2. Keep **Client key** set to `demo-key` and **Model alias** set to `general`. The alias routes to `primary`, then `backup`.
3. Enter a message. Set **Maximum output** to 64 and leave **Use exact cache** unchecked.
4. Click **Send request**. Expect `Hello, world!` to arrive in chunks, followed by a completed response with provider `primary` and a request ID.
5. Click **Inspect request** beside the response to open its expanded record in **Requests**. Expect one successful primary attempt, duration, time to first text, usage, and estimated cost.
6. Return to **Overview**. Expect the client request and provider attempt totals to increase, with a real latency observation in the chart.

The client key selects a traffic policy. It is a local demonstration credential, not a provider key or user account. The output cap also affects the cost reserved before an attempt, even when the fake returns a short answer.

### Make the primary fail with HTTP 503

1. In **Playground**, keep `demo-key`, `general`, and caching disabled. Keep the backup healthy.
2. Click **Run failover demo**. It sets the primary to HTTP 503 and sends four uncached JSON requests through the normal chat endpoint.
3. Expect all four requests to complete through `backup`. The primary remains configured to return 503 after the run.
4. In **Requests**, expand the records. Early requests show a failed primary attempt with HTTP 503 followed by a successful backup attempt. After the primary circuit opens, later requests can go directly to the backup.
5. Check the primary's circuit state and failure count in **Overview**.

By default, three consecutive provider failures open the circuit. An open circuit skips the failing provider during its cooldown so later requests do not repeat work known to fail. The gateway shares a maximum of three attempts and a 30-second deadline across each request. It does not retry indefinitely.

A successful request does not erase its failed attempts. This is why client outcomes and provider attempts have separate counters and records.

### Recover the primary through a real request

1. Click **Restore primary** in **Playground**. This changes the fake's configuration to healthy. It does not close the circuit.
2. Wait until the displayed probe eligibility time has passed. The default cooldown is two seconds after the failure that opened the circuit.
3. Send another uncached request with `demo-key`.
4. Expect a successful primary attempt and a closed circuit. Expand the request to confirm that `primary` actually ran.

Only one request can take the half-open recovery probe. If the probe fails, the circuit opens again. There is no background request that proves recovery, and waiting alone does not establish that the provider is healthy. The half-open state can be too brief to see between dashboard polls.

### Compare a slow start with an interrupted stream

Begin each scenario with a healthy primary and a closed circuit. If needed, follow the recovery steps above. Leave caching disabled.

1. Apply **Slow first chunk** to the primary in **Playground**. This delays its first output by five seconds.
2. Send a request. With the default 300 ms first-content timeout, expect the backup to complete it. Expand the record to inspect the primary timeout and backup attempt.
3. Apply **Healthy** to the primary. If its circuit is open, wait for eligibility and send a recovery request.
4. Apply **Cut after 2** to the primary and send another streaming request.
5. Expect partial text and an interrupted response. In **Requests**, expect a failed primary attempt with no backup attempt for that request.
6. Apply **Healthy** again when finished.

Before the first public response write, Portunus can discard a failed attempt and try another provider. After text reaches the client, switching providers could append an unrelated answer. Portunus ends that stream as interrupted instead. A slow start can therefore fall back, while a cut after output starts cannot.

For server-sent events, only `done` establishes successful completion. An `error` event or EOF without `done` means failure, even if the HTTP status was 200. The browser checks this terminal event rather than treating received text as success. **Stop request** cancels the browser request and propagates cancellation upstream.

### Reuse a complete answer through the exact cache

1. Restore the primary and confirm a successful recovery request if needed.
2. In **Playground**, use `demo-key`, `general`, and a new short message. Enable **Use exact cache**.
3. Send the request once. Expect an uncached completion through the primary.
4. Send the identical request again within 60 seconds. Keep the client key, message, model alias, and output cap unchanged.
5. Expect a cache hit with zero new cost. Expand the second record in **Requests**. It has no provider attempts. The client request count increases, while provider work counts stay unchanged.

Caching is opt-in and requires temperature zero, which the playground sends. Only a bounded, complete first-attempt response from the primary with authoritative usage can enter the cache. A fallback completion or interrupted response does not populate it.

The key includes client identity, messages, model alias, generation settings, route targets, and configuration revision. Stream framing is excluded, so an eligible response can be replayed as JSON or streaming events. The default cache expires entries after 60 seconds and uses LRU eviction within 128 entries and 4 MiB. A hit still consumes the client's rate allowance. Its usage metadata describes the original generation, not new provider work.

### Reject work before it reaches a provider

The default `limited-key` client has a two-request burst, a refill rate of two requests per minute, and a $0.001 estimated budget. Use a fresh process for each test so earlier rate or budget use does not change the outcome.

1. For a rate rejection, select `limited-key`, enter `Hi`, set the output cap to 64, and disable caching. Send three requests in quick succession, within 30 seconds. Expect the third to return HTTP 429.
2. For a budget rejection, restart the demo. Select `limited-key`, enter `Hi`, and set the output cap to 1024. Send once. Expect HTTP 402 because the reservation for the requested output exceeds the budget.
3. Inspect each rejected request in **Requests**. Expect no provider attempt. Restore `demo-key` for the other demos.

Rate admission bounds incoming traffic. Budget admission reserves a conservative cost before dispatch so concurrent requests cannot spend the same remaining balance. A small actual answer does not make an oversized reservation eligible. The snapshot separates spent, reserved, and remaining funds so you can distinguish settled cost from in-flight commitments.

## Read the observations correctly

All charts use observed requests. The demo does not seed traffic, invent history, or fill empty charts with sample values.

- Request, attempt, fallback, cache-hit, and cost totals cover the current process lifetime. The provider control panel also shows work counters from the fake HTTP servers.
- Request history is bounded to the latest 1,000 records by default. Snapshot latency percentiles use retained records, excluding admission rejections. Time-to-first-text percentiles use records that have that measurement.
- The dashboard fetches the latest 100 records initially. Charts based on those records describe that set, not all lifetime traffic. **Requests** can load earlier retained records. Search and outcome filters apply to loaded records.
- Missing latency means unavailable, not zero. A chart's observation count can differ from a lifetime request total or the snapshot's retained sample count.

The dashboard polls while visible. If the connection fails, it can show the last known data with a connection warning. Refresh after restoring the server before treating those values as current.

Telemetry excludes prompts, generated text, client keys, and provider credentials. See [the API contract](api-contract.md) for fields, status codes, pagination, and retention metadata.

## Send requests without the dashboard

In a second terminal, send a streaming request:

```sh
curl --no-buffer http://127.0.0.1:8080/v1/chat \
	-H 'Authorization: Bearer demo-key' \
	-H 'Content-Type: application/json' \
	-d '{"messages":[{"role":"user","content":"Explain provider fallback."}],"stream":true}'
```

Expect `delta` events followed by `done`. Omit `stream` for one complete JSON response. Optional fields include logical `model`, `max_output_tokens`, `temperature`, and explicit `cache` opt-in. The defaults are `general`, 256 output tokens, temperature zero, and no caching.

Use [the API contract](api-contract.md) when integrating a client. In particular, consume a stream through its terminal event and distinguish policy rejection from provider failure.

## Configure routing and providers

To load the demo configuration explicitly:

```sh
go run ./cmd/portunus -config configs/demo.json
```

[configs/demo.json](../configs/demo.json) sets the `general` route and the two client policies. Unspecified fields inherit gateway defaults. Configuration supports provider targets and prices, route aliases, client keys or key environment-variable names, traffic limits, cache bounds, retention, and circuit settings.

JSON duration fields use numeric nanoseconds. CLI duration flags use values such as `300ms` and `30s`. With a configuration file, set request timeouts in that file. The `-timeout` and `-first-chunk-timeout` flags apply only when no configuration file is supplied.

For an optional real service, copy [the compatible-provider example](../configs/openaicompat.json), choose a supported model, set its referenced environment variables locally, and update the illustrative prices. Never commit credentials. A real provider configuration can make paid calls. Fake failure controls apply only to local fake providers.

The compatible adapter supports the text-only Chat Completions SSE subset with `max_tokens`, streamed usage, and the terminal `[DONE]` marker. Check that the selected provider and model support this subset. Protocol fixture tests do not establish live-service compatibility.

## Understand the gateway's boundaries

One Go process hosts the gateway, two fake HTTP listeners, and the embedded dashboard. `internal/gateway` owns the attempt loop, policy state, cache, and request records. Provider adapters open, read, normalize, and close one generation session. They never retry, so the gateway can enforce one deadline and one attempt allowance.

State is bounded and synchronized inside one instance. Network calls and backoff happen outside the state lock. State resets on restart and is not shared across replicas. The fake listeners share a process, so these demonstrations do not establish independent process or host fault tolerance.

Fallback models can produce different answers. Cancellation or a timeout does not prove that an external provider stopped generating or charging. Uncertain execution retains a conservative estimated charge. Unexpected reported usage can exceed a reservation, so the local spending policy is not a provider billing guarantee. Synthetic token counts and configured costs are not real bills.

The request format is text-only. Tools, images, semantic caching, durable audit storage, accounts, and billing are outside scope. Read [the architecture notes](architecture.md) for circuit leases, reservation settlement, cache identity, and failure boundaries. [The build plan](build-plan.md) and [decision trail](decisions.tsv) retain implementation choices and verification evidence.

## Troubleshoot a demo

| Symptom | Action |
| --- | --- |
| Startup reports an address already in use | Stop the previous process, or choose free loopback ports with `-gateway-listen`, `-fake-listen`, and `-backup-listen`. Open the chosen gateway address. |
| The dashboard shows no requests or latency | Send a request in Playground. A new process starts empty, and `make demo` uses its own isolated instance. |
| The dashboard shows stale data or a connection warning | Check that the Go process is running at the page's address. Restart it if needed, then refresh the data. |
| A restored primary still routes to backup | Disable caching, wait until probe eligibility, and send another request. Restoring the fake does not reset its circuit. |
| A failure preset appears to have no effect | Disable caching and inspect the attempt record. A cache hit makes no provider call, and an open circuit skips the primary. |
| A stream starts but does not complete | Treat it as interrupted. Check for an active cut preset, cancellation, or a timeout. Restore the primary to healthy before retrying. |
| A request returns 401 | Use `demo-key` or the key from the loaded configuration. The chat endpoint requires bearer authentication. |
| A request returns 402 or 429 | Inspect the client's budget or rate policy. Reduce the output cap for reservation failures. Wait for rate refill, or use `demo-key` for normal demos. Restart to reset spent budgets. |
| Both providers fail or no attempt is eligible | Restore both fake providers, wait for circuit cooldowns, and send an uncached request. Expand its record for the actual error. |
| Frontend edits do not appear | Run `make web`, then restart or rebuild the Go program. Go embeds the compiled assets at build time. |
| Node commands fail | Check `node --version`. Use Node 22.12 or later and npm, then rerun the Make target. |

## Rebuild and verify

To rebuild the dashboard, install Node 22.12 or later and npm, then run:

```sh
make web
```

This installs locked frontend dependencies and builds into `internal/dashboard/dist`. To rebuild the Go executable with those assets:

```sh
go build -o /tmp/portunus ./cmd/portunus
/tmp/portunus
```

Stop an existing demo before starting the rebuilt executable on the same ports. You can also rerun `go run ./cmd/portunus` after `make web`.

Run the full frontend and Go checks:

```sh
make verify
```

This type-checks React, tests the browser stream parser against real HTTP, rebuilds the frontend, checks Go formatting, runs `go vet`, and runs Go tests with the race detector. For Go-only checks, run `make verify-go`. CI also checks that committed frontend assets match the build and runs the scenario verifier.

To verify the failure scenarios through real HTTP:

```sh
make demo
```

The verifier starts its own isolated local instance and checks healthy JSON and streaming responses, fallback, circuit recovery, slow starts, interrupted streams, cache reuse, policy rejection, and accounting consistency. Its rate and budget cases use dedicated test clients. Expect `PASS` lines and a report at `artifacts/verification.json`.

## Reproduce the local measurements

```sh
make benchmark
```

The benchmark compares direct-provider and gateway streaming paths, checks answer content and terminal completion, and records time to first text. Separate phases exercise cache reuse and outages. Raw observations include failures and actual provider work counts.

Read [the recorded results and methodology](benchmarks/README.md), [the raw report](benchmarks/latest.json), and [the diagnostic notes](benchmarks/diagnostics.md) with their workload and measurement limits. These local loopback runs do not establish Internet-provider latency, production capacity, availability guarantees, or real billing savings.

`make benchmark` regenerates the Markdown summary with Node. For Go-only measurement without that summary step, run:

```sh
go run ./cmd/portunus-bench -output results.json
```
