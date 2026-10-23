# The first streaming gateway

Portunus is a portfolio project for studying failure handling at the boundary between an application and a model provider. The first stage establishes the HTTP and streaming behavior that later retry and fallback policies must preserve.

## Two HTTP servers, one executable

The local executable starts a gateway and a fake provider on separate loopback listeners. Requests travel over HTTP even though the servers share a process. This keeps startup and cleanup small while exposing transport errors, response buffering, and cancellation.

This arrangement cannot demonstrate independent process crashes. It also does not provide isolation between the gateway and the fake provider. A later deployment can separate the servers without changing their HTTP contract.

## The gateway owns admission and transport

The gateway accepts a small Portunus chat format, validates the request, and forwards it to a fixed provider endpoint. It does not accept an arbitrary upstream URL from a caller.

After validation, the gateway preserves the original request bytes. Re-encoding JSON can expand characters such as `<` into escape sequences. A prompt below the gateway's body limit could then exceed the same limit at the provider. Forwarding the validated bytes keeps that limit consistent and avoids an unnecessary transformation.

The fake provider speaks the same wire format. The gateway can therefore use Go's `httputil.ReverseProxy` without parsing and rebuilding each response event. The standard library handles HTTP transport details, including streaming response copies and connection errors.

The alternative design used a provider stream interface and converted private provider events into public events. That would support heterogeneous protocols, but stage one has only one protocol. Introducing that interface now would require choosing abstractions before a real second provider establishes what they must hide.

Two independent design sketches considered these approaches. An independent review favored the transparent proxy for its smaller implementation and direct HTTP tests. The design retained explicit stream completion and tests that observe upstream cancellation. Provider normalization remains a future decision.

## Completion is part of the application protocol

A streaming response contains `delta` events followed by one `done` event. A client must observe `done` before treating an answer as complete. An HTTP status of 200 alone is insufficient.

The provider can fail after the gateway sends response headers or answer text. The gateway cannot then replace the response with a different HTTP status or a JSON error body. It must end the incomplete stream. A transport error or an end of stream without `done` is a failed generation.

The transparent proxy does not validate provider event syntax or verify that a terminal event arrived. The client owns that check in stage one. A future provider adapter can enforce the upstream protocol while preserving the public completion rule.

## Cancellation and bounds belong in the first stage

The outgoing request inherits the incoming request's context. A client disconnect or the gateway's request deadline must stop upstream work. Fake delays must also observe that context rather than sleeping unconditionally.

The gateway bounds incoming request size, admitted concurrency, request duration, and downstream writes. Response copying uses bounded buffers rather than accumulating the answer. A slow reader can still occupy an admitted request, so deadlines matter even when memory per copy is bounded.

Go's [reverse proxy documentation](https://pkg.go.dev/net/http/httputil#ReverseProxy) describes flushing and response-copy behavior. The [HTTP response controller](https://pkg.go.dev/net/http#ResponseController) provides write deadlines. These are transport mechanisms. The explicit `done` event supplies the application-level completion contract.

## Later policies must respect response commitment

Retry and fallback decisions must happen before the gateway commits the downstream response. After commitment, switching models could append a different answer to text the client already received.

A single client request may produce multiple provider attempts. Later telemetry must record those separately so a successful fallback does not hide primary-provider failures. Attempts must share one request deadline and a finite attempt allowance.

Timeouts do not establish whether an upstream provider stopped working or incurred a charge. Portunus must not claim exactly-once model execution.

## Build in verifiable stages

1. Prove streaming delivery, completion, cancellation, validation, and interrupted responses through real HTTP connections.
2. Add error classification, a finite attempt policy, provider selection, and circuit recovery. Demonstrate fallback before response commitment.
3. Add eligible exact-match caching, client rate limits, and atomic budget reservations. State the limitations of one gateway instance.
4. Record request and attempt metrics, then build a small dashboard around those records.
5. Compare the direct provider path with the gateway under a documented workload. Publish measured results and limitations.

Stage one has no performance results. Functional streaming tests establish behavior, not throughput or gateway overhead.

## Exercises for understanding the code

Trace a request context from the gateway handler into the outgoing provider request. Explain what closes the provider response body when the client disconnects.

Find the point where the gateway commits response headers. Explain which failures can still become a JSON error and which must interrupt an existing stream.

For stage two, write the retry classifier and attempt policy yourself. Start with concrete cases: invalid input, an upstream rate limit, a temporary service error, a deadline, and a response that already started. Review those decisions before connecting them to network calls.
