# Dashboard and gateway contracts

This is the shared implementation contract for the Go backend and TypeScript dashboard. Times are UTC RFC3339 strings; durations are milliseconds. Unavailable measurements are null. Money is an integer number of micro-US dollars represented as a decimal JSON string. No endpoint exposes provider credentials, client keys, prompts, or generated text in telemetry.

## Chat

`POST /v1/chat` requires `Authorization: Bearer <configured-client-key>` and JSON. The demo key is `demo-key`; it is a local demonstration credential, not a provider key.

Fields are `messages`, optional `stream`, optional logical `model` defaulting to `general`, optional `max_output_tokens` defaulting to 256, optional `temperature` defaulting to 0, and optional `cache` defaulting to false. Messages retain `role` and `content`. Enforce the existing 64 KiB public body limit, message validation, output bounds, and finite temperature range.

JSON success includes `text`, `requestId`, `provider`, `model`, `usage`, `costMicros`, and `cacheHit`. Usage includes `inputTokens`, `outputTokens`, and `authoritative`. On a cache miss, usage sums known counts across possibly executed attempts; `authoritative` is false if any such attempt has unknown usage. A cache hit returns the original generation's usage metadata with zero new cost and no provider attempts. Streaming retains `event: delta` with `{text}`, followed by `event: done` with success metadata. An interrupted committed stream may emit `event: error` with `{code,message}`, then ends without `done`. EOF without `done` is failure. Request and provider identity headers may supplement, but never replace, these completion rules.

Errors retain `{error:{code,message}}`. Use 400 for invalid input, 401 for missing/invalid client keys, 402 for insufficient budget, 429 for client rate limiting, 503 for unavailable providers/capacity, 502 for protocol/transport failures, and 504 for exhausted deadlines. No client-facing error contains a provider response body or credential.

## Snapshot

`GET /api/v1/snapshot` returns the following shape. The dashboard APIs are restricted to the local operator surface, with same-origin mutation protection and no CORS permissions.

```text
{
  schemaVersion: 1, sequence: number, capturedAt: string, uptimeMs: number,
  totals: {
    requests: number, succeeded: number, failed: number, rejected: number,
    active: number, cacheHits: number, providerAttempts: number, fallbacks: number,
    inputTokens: number, outputTokens: number, costMicros: string
  },
  latency: {
    count: number, p50Ms: number|null, p95Ms: number|null,
    ttftP50Ms: number|null, ttftP95Ms: number|null
  },
  providers: [{
    id: string, name: string, kind: string, model: string,
    circuit: "closed"|"open"|"half_open", active: number,
    requests: number, successes: number, failures: number,
    consecutiveFailures: number, retryAt: string|null, costMicros: string,
    fake: null|{
      status: number, firstChunkDelayMs: number, chunkDelayMs: number,
      cutAfter: number, revision: number,
      requests: number, completed: number, canceled: number
    }
  }],
  clients: [{
    id: string, rpm: number, burst: number,
    budgetMicros: string, spentMicros: string, reservedMicros: string,
    remainingMicros: string, requests: number, rejected: number
  }],
  cache: {
    entries: number, bytes: number, hits: number, misses: number,
    evictions: number, ttlMs: number, maxEntries: number, maxBytes: number
  },
  demo: {enabled: boolean, defaultClientId: string, routing: string[]},
  retention: {maxRequests: number, latencySamples: number}
}
```

Counters are lifetime counters for this process. Latency percentiles are over the bounded retained request window, excluding admission rejections; null means no observations. Cache hits remain client requests and consume client rate-limit capacity, but incur no upstream charge. Provider work counters come from the fake server, not inferred request totals.

## Request records

`GET /api/v1/requests?limit=50` returns `{items,nextCursor}`. Items are newest first; `before=<id>` requests the next page. A request item has `id`, `startedAt`, `clientId`, `model`, `status`, `errorCode`, `provider`, `cacheHit`, `durationMs`, nullable `ttftMs`, `inputTokens`, `outputTokens`, `usageAuthoritative`, decimal-string `costMicros`, and `attempts`.

Status is `success`, `error`, `rejected`, or `canceled`. Each attempt has `provider`, `model`, `startedAt`, `durationMs`, nullable `ttftMs`, `status`, `errorCode`, nullable `httpStatus`, `inputTokens`, `outputTokens`, `usageAuthoritative`, decimal-string `reservedMicros`, and decimal-string `costMicros`. Attempt status is `success`, `error`, or `canceled`. Empty error codes and providers are empty strings rather than null.

## Fake controls

`POST /api/v1/demo/providers/{id}/control` accepts the full JSON object `{status,firstChunkDelayMs,chunkDelayMs,cutAfter}`. Status is 200, 429, or 503. Delays are bounded nonnegative milliseconds. Cut-after is -1 or a bounded number of deltas. Return the effective fake configuration and incremented revision. Controls never apply to real providers or alter billing policy.

The dashboard sends normal authenticated chat requests to `/v1/chat` for its playground and demo. It must not simulate success or add invented records. Provider recovery uses real subsequent requests after the circuit cooldown.

## Frontend boundary

The dashboard package exports Go `dashboard.Handler() http.Handler`. Assets live in `internal/dashboard/dist` and are embedded. Source lives in `web/`, using React, TypeScript, and Vite. The backend owns API routes and mounts the asset handler at `/`. An empty installation displays truthful empty states until requests occur.
