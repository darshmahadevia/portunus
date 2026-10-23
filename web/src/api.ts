import { z } from "zod";
const count = z.number().nonnegative();
const micros = z.string().regex(/^\d+$/);
const time = z.string().datetime({ offset: true });
export const fakeSchema = z.object({
  status: z.union([z.literal(200), z.literal(429), z.literal(503)]),
  firstChunkDelayMs: count,
  chunkDelayMs: count,
  cutAfter: z.number().int().min(-1),
  revision: count,
  requests: count.optional(),
  completed: count.optional(),
  canceled: count.optional(),
});
const usage = z.object({
  inputTokens: count,
  outputTokens: count,
  authoritative: z.boolean(),
});
export const successSchema = z.object({
  text: z.string().optional(),
  requestId: z.string(),
  provider: z.string(),
  model: z.string(),
  usage,
  costMicros: micros,
  cacheHit: z.boolean(),
});
const attemptSchema = z.object({
  provider: z.string(),
  model: z.string(),
  startedAt: time,
  durationMs: count,
  ttftMs: count.nullable(),
  status: z.enum(["success", "error", "canceled"]),
  errorCode: z.string(),
  httpStatus: z.number().nullable(),
  inputTokens: count,
  outputTokens: count,
  usageAuthoritative: z.boolean(),
  reservedMicros: micros,
  costMicros: micros,
});
export const requestSchema = z.object({
  id: z.string(),
  startedAt: time,
  clientId: z.string(),
  model: z.string(),
  status: z.enum(["success", "error", "rejected", "canceled"]),
  errorCode: z.string(),
  provider: z.string(),
  cacheHit: z.boolean(),
  durationMs: count,
  ttftMs: count.nullable(),
  inputTokens: count,
  outputTokens: count,
  usageAuthoritative: z.boolean(),
  costMicros: micros,
  attempts: z.array(attemptSchema),
});
export const requestsSchema = z.object({
  items: z.array(requestSchema),
  nextCursor: z.string().nullable(),
});
export const snapshotSchema = z.object({
  schemaVersion: z.literal(1),
  sequence: count,
  capturedAt: time,
  uptimeMs: count,
  totals: z.object({
    requests: count,
    succeeded: count,
    failed: count,
    rejected: count,
    active: count,
    cacheHits: count,
    providerAttempts: count,
    fallbacks: count,
    inputTokens: count,
    outputTokens: count,
    costMicros: micros,
  }),
  latency: z.object({
    count,
    p50Ms: count.nullable(),
    p95Ms: count.nullable(),
    ttftP50Ms: count.nullable(),
    ttftP95Ms: count.nullable(),
  }),
  providers: z.array(
    z.object({
      id: z.string(),
      name: z.string(),
      kind: z.string(),
      model: z.string(),
      circuit: z.enum(["closed", "open", "half_open"]),
      active: count,
      requests: count,
      successes: count,
      failures: count,
      consecutiveFailures: count,
      retryAt: time.nullable(),
      costMicros: micros,
      fake: fakeSchema.nullable(),
    }),
  ),
  clients: z.array(
    z.object({
      id: z.string(),
      rpm: count,
      burst: count,
      budgetMicros: micros,
      spentMicros: micros,
      reservedMicros: micros,
      remainingMicros: micros,
      requests: count,
      rejected: count,
    }),
  ),
  cache: z.object({
    entries: count,
    bytes: count,
    hits: count,
    misses: count,
    evictions: count,
    ttlMs: count,
    maxEntries: count,
    maxBytes: count,
  }),
  demo: z.object({
    enabled: z.boolean(),
    defaultClientId: z.string(),
    routing: z.array(z.string()),
  }),
  retention: z.object({ maxRequests: count, latencySamples: count }),
});
export type Snapshot = z.infer<typeof snapshotSchema>;
export type RequestRecord = z.infer<typeof requestSchema>;
export type Fake = z.infer<typeof fakeSchema>;
export type Success = z.infer<typeof successSchema>;
const errorSchema = z.object({
  error: z.object({ code: z.string(), message: z.string() }),
});
export function message(error: unknown): string {
  return error instanceof Error
    ? error.message
    : "Unexpected response. Retry the request.";
}
export async function json<T>(
  url: string,
  schema: z.ZodType<T>,
  init?: RequestInit,
): Promise<T> {
  const response = await fetch(url, init);
  const value: unknown = await response.json();
  if (!response.ok) {
    const parsed = errorSchema.safeParse(value);
    throw new Error(
      parsed.success
        ? `${parsed.data.error.code}: ${parsed.data.error.message}`
        : `HTTP ${response.status}: request failed`,
    );
  }
  return schema.parse(value);
}
const deltaSchema = z.object({ text: z.string() });
const streamError = z.object({ code: z.string(), message: z.string() });
export async function streamChat(
  body: object,
  key: string,
  signal: AbortSignal,
  onText: (text: string) => void,
): Promise<Success> {
  const response = await fetch("/v1/chat", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${key}`,
    },
    body: JSON.stringify(body),
    signal,
  });
  if (!response.ok) {
    const value: unknown = await response.json();
    const parsed = errorSchema.safeParse(value);
    throw new Error(
      parsed.success
        ? `${parsed.data.error.code}: ${parsed.data.error.message}`
        : `HTTP ${response.status}`,
    );
  }
  if (
    !response.body ||
    !response.headers.get("content-type")?.includes("text/event-stream")
  )
    throw new Error("Expected an event stream; no completion received.");
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let output = "";
  let completed: Success | undefined;
  const consume = () => {
    let boundary: RegExpExecArray | null;
    while ((boundary = /\r?\n\r?\n/.exec(buffer)) !== null) {
      const frame = buffer.slice(0, boundary.index);
      buffer = buffer.slice(boundary.index + boundary[0].length);
      let event = "message";
      const data: string[] = [];
      for (const line of frame.split(/\r?\n/)) {
        if (line.startsWith("event:")) event = line.slice(6).trim();
        if (line.startsWith("data:"))
          data.push(line.slice(5).replace(/^ /, ""));
      }
      if (!data.length) continue;
      const value: unknown = JSON.parse(data.join("\n"));
      if (event === "delta") {
        output = (output + deltaSchema.parse(value).text).slice(0, 32000);
        onText(output);
      } else if (event === "done") {
        completed = successSchema.parse(value);
        return;
      } else if (event === "error") {
        const failure = streamError.parse(value);
        throw new Error(`${failure.code}: ${failure.message}`);
      }
    }
    if (buffer.length > 131072)
      throw new Error("Stream frame exceeded the display limit.");
  };
  try {
    while (!completed) {
      const chunk = await reader.read();
      if (chunk.done) {
        buffer += decoder.decode();
        consume();
        break;
      }
      buffer += decoder.decode(chunk.value, { stream: true });
      consume();
    }
    if (!completed)
      throw new Error("Interrupted: stream ended without a done event.");
    return completed;
  } finally {
    await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}
