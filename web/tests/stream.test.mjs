import { createServer } from "node:http";
import { once } from "node:events";
import assert from "node:assert/strict";
import { test } from "node:test";
import { streamChat } from "../src/api.ts";

// Exercise actual HTTP streams, including split UTF-8 bytes and transport EOF.
test("stream completion, framing, interruption, errors and abort", async () => {
  let scenario = "complete";
  const done = {
    requestId: "observed",
    provider: "primary",
    model: "general",
    usage: { inputTokens: 1, outputTokens: 1, authoritative: false },
    costMicros: "2",
    cacheHit: false,
  };
  const server = createServer(async (req, res) => {
    for await (const _chunk of req) {
      /* Drain the real request body. */
    }
    if (scenario === "rejected") {
      res.writeHead(429, { "content-type": "application/json" });
      res.end(
        JSON.stringify({
          error: { code: "rate_limited", message: "Wait before retrying." },
        }),
      );
      return;
    }
    res.writeHead(200, { "content-type": "text/event-stream" });
    if (scenario === "abort") {
      res.write(": waiting\n\n");
      return;
    }
    const delta = Buffer.from('event: delta\r\ndata: {"text":"héllo"}\r\n\r\n');
    for (const byte of delta) res.write(Buffer.from([byte]));
    if (scenario === "complete")
      res.write(`event: done\r\ndata: ${JSON.stringify(done)}\r\n\r\n`);
    if (scenario === "error")
      res.write(
        'event: error\ndata: {"code":"upstream_cut","message":"Interrupted."}\n\n',
      );
    res.end();
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Missing test address");
  const original = globalThis.fetch;
  globalThis.fetch = (url, init) =>
    original(new URL(url, `http://127.0.0.1:${address.port}`), init);
  try {
    let output = "";
    assert.equal(
      (
        await streamChat(
          {},
          "demo-key",
          new AbortController().signal,
          (value) => {
            output = value;
          },
        )
      ).requestId,
      "observed",
    );
    assert.equal(output, "héllo");
    scenario = "eof";
    await assert.rejects(
      streamChat({}, "demo-key", new AbortController().signal, () => {}),
      /without a done event/,
    );
    scenario = "error";
    await assert.rejects(
      streamChat({}, "demo-key", new AbortController().signal, () => {}),
      /upstream_cut/,
    );
    scenario = "rejected";
    await assert.rejects(
      streamChat({}, "demo-key", new AbortController().signal, () => {}),
      /rate_limited/,
    );
    scenario = "abort";
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 30);
    await assert.rejects(
      streamChat({}, "demo-key", controller.signal, () => {}),
      /abort/i,
    );
    clearTimeout(timer);
  } finally {
    globalThis.fetch = original;
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});
