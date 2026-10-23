import React, {
  Fragment,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { createRoot } from "react-dom/client";
import {
  fakeSchema,
  json,
  message,
  requestsSchema,
  snapshotSchema,
  streamChat,
  successSchema,
} from "./api";
import type { Fake, RequestRecord, Snapshot, Success } from "./api";
import "./style.css";

type Data = {
  snapshot: Snapshot;
  requests: RequestRecord[];
  nextCursor: string | null;
};
type Feed =
  | { kind: "loading" }
  | { kind: "live"; data: Data }
  | { kind: "stale"; data: Data; error: string }
  | { kind: "error"; error: string };
type Operation =
  | { kind: "idle" }
  | { kind: "pending"; label: string }
  | { kind: "success"; label: string }
  | { kind: "error"; label: string };
type Output =
  | { kind: "idle" }
  | { kind: "streaming"; text: string }
  | { kind: "completed"; text: string; result: Success }
  | { kind: "interrupted"; text: string; error: string }
  | { kind: "error"; text: string; error: string };
type Page = "overview" | "requests" | "playground";
function pageFromPath(): Page {
  return location.pathname === "/requests"
    ? "requests"
    : location.pathname === "/playground"
      ? "playground"
      : "overview";
}
function Icon({
  name,
}: {
  name: "overview" | "requests" | "playground" | "refresh" | "arrow";
}) {
  const paths = {
    overview: "M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z",
    requests: "M8 5h13M8 12h13M8 19h13M3 5h.01M3 12h.01M3 19h.01",
    playground: "m7 5-5 7 5 7M13 5l-3 14M17 5l5 7-5 7",
    refresh: "M20 7v5h-5M4 17v-5h5M6 6a8 8 0 0 1 14 6M18 18a8 8 0 0 1-14-6",
    arrow: "m9 5 7 7-7 7",
  };
  return (
    <svg
      width="17"
      height="17"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={paths[name]} />
    </svg>
  );
}
const money = (value: string) => {
  const n = BigInt(value);
  const dollars = n / 1000000n;
  const fraction = (n % 1000000n).toString().padStart(6, "0");
  return `$${dollars}.${fraction}`;
};
const ms = (value: number | null) =>
  value === null ? "—" : `${Math.round(value).toLocaleString()} ms`;
const clock = (value: string) =>
  new Date(value).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
function Badge({
  children,
  tone = "",
}: {
  children: React.ReactNode;
  tone?: string;
}) {
  return <span className={`badge ${tone}`}>{children}</span>;
}
function useFeed() {
  const [feed, setFeed] = useState<Feed>({ kind: "loading" });
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let disposed = false;
    let active: AbortController | undefined;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      if (document.hidden) {
        timer = setTimeout(poll, 2000);
        return;
      }
      active = new AbortController();
      const timeout = setTimeout(() => active?.abort(), 8000);
      try {
        const [snapshot, records] = await Promise.all([
          json("/api/v1/snapshot", snapshotSchema, { signal: active.signal }),
          json("/api/v1/requests?limit=100", requestsSchema, {
            signal: active.signal,
          }),
        ]);
        if (!disposed)
          setFeed({
            kind: "live",
            data: {
              snapshot,
              requests: records.items,
              nextCursor: records.nextCursor,
            },
          });
      } catch (error) {
        if (!disposed)
          setFeed((previous) => ({
            ...("data" in previous
              ? { kind: "stale", data: previous.data }
              : { kind: "error" }),
            error: message(error),
          }));
      } finally {
        clearTimeout(timeout);
        active?.abort();
        if (!disposed) timer = setTimeout(poll, 2000);
      }
    };
    void poll();
    const visible = () => {
      if (!document.hidden) {
        clearTimeout(timer);
        active?.abort();
        setTick((n) => n + 1);
      }
    };
    document.addEventListener("visibilitychange", visible);
    return () => {
      disposed = true;
      clearTimeout(timer);
      active?.abort();
      document.removeEventListener("visibilitychange", visible);
    };
  }, [tick]);
  return { feed, refresh: useCallback(() => setTick((n) => n + 1), []) };
}
function Latency({ records }: { records: RequestRecord[] }) {
  const measured = records.filter((r) => r.status !== "rejected");
  const max = Math.max(1, ...measured.map((r) => r.durationMs));
  const buckets = Array.from(
    { length: 8 },
    (_, i) =>
      measured.filter(
        (r) => Math.min(7, Math.floor((r.durationMs / max) * 8)) === i,
      ).length,
  );
  const peak = Math.max(1, ...buckets);
  return (
    <section className="latency">
      <div className="section-title">
        <h2>Request duration</h2>
        <span className="muted">{measured.length} retained observations</span>
      </div>
      {measured.length ? (
        <>
          <svg
            className="chart"
            viewBox="0 0 440 154"
            role="img"
            aria-label={`Duration histogram, ${measured.length} requests, range zero to ${Math.ceil(max)} milliseconds`}
          >
            {[0, 0.5, 1].map((n) => (
              <g key={n}>
                <line
                  x1="35"
                  x2="425"
                  y1={115 - n * 90}
                  y2={115 - n * 90}
                  className="gridline"
                />
                <text x="27" y={119 - n * 90} textAnchor="end">
                  {Math.round(peak * n)}
                </text>
              </g>
            ))}
            {buckets.map((count, i) => (
              <rect
                key={i}
                x={42 + i * 48}
                y={115 - (count / peak) * 90}
                width="33"
                height={(count / peak) * 90}
                rx="2"
              >
                <title>
                  {Math.round((i * max) / 8)}–{Math.round(((i + 1) * max) / 8)}{" "}
                  ms: {count} requests
                </title>
              </rect>
            ))}
            <text x="35" y="140">
              0 ms
            </text>
            <text x="425" y="140" textAnchor="end">
              {Math.ceil(max)} ms
            </text>
            <text x="35" y="13">
              Requests
            </text>
          </svg>
          <p className="caption">
            End-to-end duration · admission rejections excluded
          </p>
        </>
      ) : (
        <div className="chart-empty">
          <span className="empty-rule" />
          <strong>No latency observations</strong>
          <p>Send a request to measure the gateway.</p>
        </div>
      )}
    </section>
  );
}
function Activity({
  records,
  full,
  nextCursor,
  refresh,
}: {
  records: RequestRecord[];
  full: boolean;
  nextCursor: string | null;
  refresh: () => void;
}) {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("all");
  const [expanded, setExpanded] = useState<string | null>(null);
  const [older, setOlder] = useState<RequestRecord[]>([]);
  const [cursor, setCursor] = useState<string | null | undefined>(undefined);
  const [paging, setPaging] = useState<Operation>({ kind: "idle" });
  const pagingController = useRef<AbortController | null>(null);
  useEffect(() => () => pagingController.current?.abort(), []);
  const combined = [
    ...records,
    ...older.filter((r) => !records.some((current) => current.id === r.id)),
  ];
  const filtered = combined.filter(
    (r) =>
      (status === "all" || r.status === status) &&
      `${r.id} ${r.clientId} ${r.provider} ${r.attempts.map((a) => a.provider).join(" ")} ${r.errorCode}`
        .toLowerCase()
        .includes(query.toLowerCase()),
  );
  const shown = full ? filtered : records.slice(0, 8);
  const loadMore = async () => {
    const before = cursor === undefined ? nextCursor : cursor;
    if (!before) return;
    const controller = new AbortController();
    pagingController.current = controller;
    const timeout = setTimeout(() => controller.abort(), 8000);
    setPaging({ kind: "pending", label: "Loading earlier requests…" });
    try {
      const result = await json(
        `/api/v1/requests?limit=100&before=${encodeURIComponent(before)}`,
        requestsSchema,
        { signal: controller.signal },
      );
      setOlder((items) => [
        ...items,
        ...result.items.filter((r) => !items.some((i) => i.id === r.id)),
      ]);
      setCursor(result.nextCursor);
      setPaging({ kind: "idle" });
    } catch (error) {
      setPaging({ kind: "error", label: message(error) });
    } finally {
      clearTimeout(timeout);
    }
  };
  return (
    <section className="activity">
      <div className="section-title">
        <div>
          <h2>{full ? "All request activity" : "Recent activity"}</h2>
          <p className="caption">
            One row per client request. Expand to inspect provider attempts.
          </p>
        </div>
        <button className="quiet" onClick={refresh}>
          <Icon name="refresh" /> Refresh
        </button>
      </div>
      {full && (
        <div className="filters">
          <label>
            Find request
            <input
              placeholder="Client, provider, request ID…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </label>
          <label>
            Outcome
            <select value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="all">All outcomes</option>
              {["success", "error", "rejected", "canceled"].map((s) => (
                <option key={s}>{s}</option>
              ))}
            </select>
          </label>
          <span className="muted">{filtered.length} matching</span>
        </div>
      )}
      <div
        className="table-scroll"
        role="region"
        aria-label="Request activity, scroll horizontally for all columns"
        tabIndex={0}
      >
        <table>
          <thead>
            <tr>
              <th>Client / request</th>
              <th>Provider path</th>
              <th>Outcome</th>
              <th>Duration</th>
              <th>Est. cost</th>
              <th>Cache</th>
              <th>Started</th>
              <th>
                <span className="sr-only">Details</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {shown.map((r) => (
              <Fragment key={r.id}>
                <tr>
                  <td>
                    <strong>{r.clientId}</strong>
                    <small className="mono" title={r.id}>
                      {r.id.slice(0, 14)}
                    </small>
                  </td>
                  <td>
                    {r.attempts.length
                      ? r.attempts.map((a) => a.provider).join(" → ")
                      : r.provider || "—"}
                  </td>
                  <td>
                    <Badge
                      tone={
                        r.status === "success"
                          ? "good"
                          : r.status === "rejected"
                            ? "warn"
                            : "bad"
                      }
                    >
                      {r.status}
                    </Badge>
                  </td>
                  <td className="mono">{ms(r.durationMs)}</td>
                  <td className="mono">{money(r.costMicros)}</td>
                  <td>
                    {r.cacheHit ? <Badge tone="good">hit</Badge> : "miss"}
                  </td>
                  <td className="mono">{clock(r.startedAt)}</td>
                  <td>
                    <button
                      className={`expand ${expanded === r.id ? "is-open" : ""}`}
                      aria-label={`Attempt details for ${r.id}`}
                      aria-expanded={expanded === r.id}
                      onClick={() =>
                        setExpanded(expanded === r.id ? null : r.id)
                      }
                    >
                      <Icon name="arrow" />
                    </button>
                  </td>
                </tr>
                {expanded === r.id && (
                  <tr className="detail">
                    <td colSpan={8}>
                      <div className="detail-header">
                        <code>{r.id}</code>
                        <span>
                          {r.model} · {r.inputTokens} in / {r.outputTokens} out
                          · {r.usageAuthoritative ? "reported" : "estimated"}{" "}
                          usage · TTFT {ms(r.ttftMs)}
                        </span>
                      </div>
                      {r.errorCode && (
                        <p className="error-text">{r.errorCode}</p>
                      )}
                      {r.attempts.length ? (
                        <ol className="attempts">
                          {r.attempts.map((a, i) => (
                            <li key={i}>
                              <strong>{a.provider}</strong>
                              <Badge
                                tone={a.status === "success" ? "good" : "bad"}
                              >
                                {a.status}
                              </Badge>
                              <span>{a.model}</span>
                              <span className="mono">
                                {ms(a.durationMs)} · TTFT {ms(a.ttftMs)}
                              </span>
                              <span>
                                {a.httpStatus ? `HTTP ${a.httpStatus}` : ""}{" "}
                                {a.errorCode}
                              </span>
                              <span className="mono">
                                {money(a.costMicros)} · reserved{" "}
                                {money(a.reservedMicros)}
                              </span>
                              <span>
                                {a.inputTokens} in / {a.outputTokens} out ·{" "}
                                {a.usageAuthoritative
                                  ? "reported"
                                  : "estimated"}{" "}
                                usage · {clock(a.startedAt)}
                              </span>
                            </li>
                          ))}
                        </ol>
                      ) : (
                        <p className="muted">
                          {r.cacheHit
                            ? "Served from exact cache. No provider attempt."
                            : "No provider attempt; request ended before routing."}
                        </p>
                      )}
                    </td>
                  </tr>
                )}
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>
      {!shown.length && (
        <div className="empty">
          <strong>
            {full && records.length
              ? "No requests match these filters"
              : "No requests yet"}
          </strong>
          <p>
            {full && records.length
              ? "Try another client, provider, or outcome."
              : "Open the playground and send your first request."}
          </p>
        </div>
      )}
      {full && (cursor === undefined ? nextCursor : cursor) && (
        <button
          className="load-more"
          disabled={paging.kind === "pending"}
          onClick={() => void loadMore()}
        >
          Load earlier requests
        </button>
      )}
      {paging.kind === "error" && (
        <p role="alert" className="error-text">
          {paging.label}
        </p>
      )}
    </section>
  );
}
const presets = [
  {
    name: "Healthy",
    status: 200,
    firstChunkDelayMs: 0,
    chunkDelayMs: 35,
    cutAfter: -1,
  },
  {
    name: "Return 503",
    status: 503,
    firstChunkDelayMs: 0,
    chunkDelayMs: 35,
    cutAfter: -1,
  },
  {
    name: "Slow first chunk",
    status: 200,
    firstChunkDelayMs: 5000,
    chunkDelayMs: 35,
    cutAfter: -1,
  },
  {
    name: "Cut after 2",
    status: 200,
    firstChunkDelayMs: 0,
    chunkDelayMs: 35,
    cutAfter: 2,
  },
];
type Preset = (typeof presets)[number];
function Routing({
  snapshot,
  refresh,
}: {
  snapshot: Snapshot;
  refresh: () => void;
}) {
  const [operations, setOperations] = useState<Record<string, Operation>>({});
  const [applied, setApplied] = useState<Record<string, Fake>>({});
  const controllers = useRef(new Set<AbortController>());
  useEffect(() => () => controllers.current.forEach((c) => c.abort()), []);
  const control = async (id: string, preset: Preset) => {
    const controller = new AbortController();
    controllers.current.add(controller);
    const timeout = setTimeout(() => controller.abort(), 8000);
    setOperations((o) => ({
      ...o,
      [id]: {
        kind: "pending",
        label: `Applying ${preset.name.toLowerCase()}…`,
      },
    }));
    try {
      const { status, firstChunkDelayMs, chunkDelayMs, cutAfter } = preset;
      const result = await json(
        `/api/v1/demo/providers/${encodeURIComponent(id)}/control`,
        fakeSchema,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            status,
            firstChunkDelayMs,
            chunkDelayMs,
            cutAfter,
          }),
          signal: controller.signal,
        },
      );
      setApplied((a) => ({ ...a, [id]: result }));
      setOperations((o) => ({
        ...o,
        [id]: {
          kind: "success",
          label: `Applied ${preset.name.toLowerCase()} · revision ${result.revision}`,
        },
      }));
      refresh();
    } catch (error) {
      setOperations((o) => ({
        ...o,
        [id]: { kind: "error", label: message(error) },
      }));
    } finally {
      clearTimeout(timeout);
      controllers.current.delete(controller);
    }
  };
  const ordered = [...snapshot.providers].sort(
    (a, b) =>
      snapshot.demo.routing.indexOf(a.id) - snapshot.demo.routing.indexOf(b.id),
  );
  return (
    <section className="routing">
      <div className="section-title">
        <h2>Provider routing</h2>
        <span className="muted">{snapshot.totals.active} active requests</span>
      </div>
      {ordered.length ? (
        ordered.map((p) => {
          const override = applied[p.id];
          const fake =
            override && (!p.fake || override.revision > p.fake.revision)
              ? override
              : p.fake;
          const op = operations[p.id];
          const rank = snapshot.demo.routing.indexOf(p.id);
          return (
            <div className="provider" key={p.id}>
              <div className="provider-heading">
                <div>
                  <span className="route-rank">
                    {rank === 0 ? "Primary" : rank > 0 ? "Backup" : "Provider"}
                  </span>
                  <h3>{p.name}</h3>
                  <span className="caption">
                    {p.kind} · {p.model}
                  </span>
                </div>
                <Badge tone={p.circuit === "closed" ? "good" : "warn"}>
                  {p.circuit.replace("_", " ")}
                </Badge>
              </div>
              <dl className="provider-stats">
                <div>
                  <dt>Requests</dt>
                  <dd>{p.requests}</dd>
                </div>
                <div>
                  <dt>Failures</dt>
                  <dd>{p.failures}</dd>
                </div>
                <div>
                  <dt>Active</dt>
                  <dd>{p.active}</dd>
                </div>
                <div>
                  <dt>Est. cost</dt>
                  <dd>{money(p.costMicros)}</dd>
                </div>
              </dl>
              {p.retryAt && (
                <p className="notice">
                  Probe eligible after {clock(p.retryAt)}. A subsequent request
                  tests recovery.
                </p>
              )}
              <div className="presets">
                {presets.map((preset) => (
                  <button
                    key={preset.name}
                    disabled={
                      !snapshot.demo.enabled || !fake || op?.kind === "pending"
                    }
                    onClick={() => void control(p.id, preset)}
                  >
                    {preset.name}
                  </button>
                ))}
              </div>
              {fake ? (
                <p className="caption">
                  Applied rev. {fake.revision} · HTTP {fake.status} · first{" "}
                  {fake.firstChunkDelayMs} ms · chunk {fake.chunkDelayMs} ms ·
                  cut {fake.cutAfter === -1 ? "off" : fake.cutAfter} · server
                  work {fake.requests ?? "—"} requests / {fake.completed ?? "—"}{" "}
                  completed / {fake.canceled ?? "—"} canceled
                </p>
              ) : (
                <p className="caption">
                  Real provider · fault controls unavailable
                </p>
              )}
              {op && op.kind !== "idle" && (
                <p
                  role={op.kind === "error" ? "alert" : "status"}
                  className={op.kind === "error" ? "error-text" : "caption"}
                >
                  {op.label}
                </p>
              )}
            </div>
          );
        })
      ) : (
        <div className="empty">No providers configured.</div>
      )}
    </section>
  );
}
function Playground({
  snapshot,
  refresh,
}: {
  snapshot: Snapshot | undefined;
  refresh: () => void;
}) {
  const [key, setKey] = useState("demo-key");
  const [model, setModel] = useState("general");
  const [prompt, setPrompt] = useState(
    "Explain how a gateway keeps requests reliable when a provider fails.",
  );
  const [tokens, setTokens] = useState("256");
  const [cache, setCache] = useState(false);
  const [output, setOutput] = useState<Output>({ kind: "idle" });
  const [demo, setDemo] = useState<Operation>({ kind: "idle" });
  const [results, setResults] = useState<Success[]>([]);
  const controller = useRef<AbortController | null>(null);
  const text = useRef("");
  useEffect(() => () => controller.current?.abort(), []);
  const busy = output.kind === "streaming" || demo.kind === "pending";
  const send = async () => {
    const active = new AbortController();
    controller.current = active;
    text.current = "";
    setOutput({ kind: "streaming", text: "" });
    const timeout = setTimeout(() => active.abort(), 90000);
    try {
      const result = await streamChat(
        {
          messages: [{ role: "user", content: prompt }],
          stream: true,
          model,
          max_output_tokens: Number(tokens),
          temperature: 0,
          cache,
        },
        key,
        active.signal,
        (value) => {
          text.current = value;
          setOutput({ kind: "streaming", text: value });
        },
      );
      setOutput({ kind: "completed", text: text.current, result });
    } catch (error) {
      const detail = message(error);
      setOutput({
        kind:
          active.signal.aborted ||
          text.current.length > 0 ||
          detail.startsWith("Interrupted")
            ? "interrupted"
            : "error",
        text: text.current,
        error: active.signal.aborted
          ? "Stopped or timed out before completion. Partial output is not a successful response."
          : detail,
      });
    } finally {
      clearTimeout(timeout);
      refresh();
      controller.current = null;
    }
  };
  const primary = snapshot?.providers.find(
    (p) => p.id === snapshot.demo.routing[0],
  );
  const runDemo = async (restore: boolean) => {
    if (!primary?.fake) return;
    const active = new AbortController();
    controller.current = active;
    const timeout = setTimeout(() => active.abort(), 90000);
    setResults([]);
    setDemo({
      kind: "pending",
      label: restore ? "Restoring primary…" : "Applying HTTP 503 to primary…",
    });
    try {
      await json(
        `/api/v1/demo/providers/${encodeURIComponent(primary.id)}/control`,
        fakeSchema,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            status: restore ? 200 : 503,
            firstChunkDelayMs: 0,
            chunkDelayMs: 35,
            cutAfter: -1,
          }),
          signal: active.signal,
        },
      );
      if (restore) {
        setDemo({
          kind: "success",
          label:
            "Healthy configuration applied. Circuit recovery is tested by a subsequent request after cooldown.",
        });
      } else {
        for (let i = 0; i < 4; i++) {
          setDemo({ kind: "pending", label: `Sending request ${i + 1} of 4…` });
          const result = await json("/v1/chat", successSchema, {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              Authorization: `Bearer ${key}`,
            },
            body: JSON.stringify({
              messages: [
                {
                  role: "user",
                  content: "Explain provider failover in one sentence.",
                },
              ],
              model,
              max_output_tokens: 64,
              temperature: 0,
              cache: false,
              stream: false,
            }),
            signal: active.signal,
          });
          setResults((previous) => [...previous, result]);
          refresh();
        }
        setDemo({
          kind: "success",
          label:
            "4 requests completed. Inspect activity below for the actual provider attempts. Primary remains set to HTTP 503 until restored.",
        });
      }
    } catch (error) {
      setDemo({
        kind: "error",
        label: active.signal.aborted
          ? "Demo interrupted. Provider settings already applied remain in effect; restore primary when ready."
          : `${message(error)}. Demo stopped; provider configuration may remain applied.`,
      });
    } finally {
      clearTimeout(timeout);
      controller.current = null;
      refresh();
    }
  };
  return (
    <>
      <section className="playground">
        <div className="section-title">
          <div>
            <h2>Streaming playground</h2>
            <p className="caption">
              Send a real authenticated request through the gateway.
            </p>
          </div>
          <Badge>POST /v1/chat</Badge>
        </div>
        <div className="playground-grid">
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void send();
            }}
          >
            <div className="form-pair">
              <label>
                Client key
                <input
                  type="password"
                  autoComplete="off"
                  value={key}
                  onChange={(e) => setKey(e.target.value)}
                  required
                  disabled={busy}
                />
              </label>
              <label>
                Model alias
                <input
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                  required
                  disabled={busy}
                />
              </label>
            </div>
            <label>
              Message
              <textarea
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                rows={5}
                maxLength={16000}
                required
                disabled={busy}
              />
            </label>
            <div className="form-pair">
              <label>
                Maximum output
                <select
                  value={tokens}
                  onChange={(e) => setTokens(e.target.value)}
                  disabled={busy}
                >
                  {[64, 128, 256, 512, 1024].map((n) => (
                    <option key={n} value={n}>
                      {n} tokens
                    </option>
                  ))}
                </select>
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={cache}
                  onChange={(e) => setCache(e.target.checked)}
                  disabled={busy}
                />{" "}
                Use exact cache
              </label>
            </div>
            <div className="actions">
              <button
                className="primary"
                type="submit"
                disabled={
                  busy || !prompt.trim() || !key.trim() || !model.trim()
                }
              >
                <Icon name="playground" /> Send request
              </button>
              <button
                type="button"
                disabled={!busy}
                onClick={() => controller.current?.abort()}
              >
                Stop request
              </button>
            </div>
            <p className="caption">
              Local demo key only. Output display is limited to 32,000
              characters.
            </p>
          </form>
          <div className="response">
            <div className="response-header">
              <h3>Response</h3>
              <Badge
                tone={
                  output.kind === "completed"
                    ? "good"
                    : output.kind === "error"
                      ? "bad"
                      : output.kind === "interrupted"
                        ? "warn"
                        : ""
                }
              >
                {output.kind === "idle" ? "Ready" : output.kind}
              </Badge>
            </div>
            <pre aria-live="polite" aria-busy={output.kind === "streaming"}>
              {output.kind === "idle"
                ? "Your response will appear here."
                : output.text || "Waiting for first chunk…"}
            </pre>
            {(output.kind === "error" || output.kind === "interrupted") && (
              <p role="alert" className="error-text">
                {output.error}
              </p>
            )}
            {output.kind === "completed" && (
              <p className="caption">
                {output.result.provider} ·{" "}
                {output.result.cacheHit ? "cached" : "uncached"} ·{" "}
                {output.result.usage.outputTokens} output tokens ·{" "}
                {money(output.result.costMicros)}
                <br />
                <code>{output.result.requestId}</code>
              </p>
            )}
          </div>
        </div>
      </section>
      <section className="demo-panel">
        <div>
          <h2>See fallback in action</h2>
          <p className="caption">
            Set the primary to HTTP 503, then send four uncached requests. Rate
            or budget errors stop the run.
          </p>
        </div>
        <div className="actions">
          <button
            onClick={() => void runDemo(false)}
            disabled={
              busy || !snapshot?.demo.enabled || !primary?.fake || !key.trim()
            }
          >
            Run failover demo
          </button>
          <button
            onClick={() => void runDemo(true)}
            disabled={busy || !snapshot?.demo.enabled || !primary?.fake}
          >
            Restore primary
          </button>
        </div>
        {!snapshot?.demo.enabled && (
          <p className="caption">
            Fault demonstrations require local demo mode.
          </p>
        )}
        {demo.kind !== "idle" && (
          <p
            role={demo.kind === "error" ? "alert" : "status"}
            className={demo.kind === "error" ? "error-text" : "notice"}
          >
            {demo.label}
          </p>
        )}
        {demo.kind === "pending" && (
          <button onClick={() => controller.current?.abort()}>Stop demo</button>
        )}
        {results.length > 0 && (
          <ol className="demo-results">
            {results.map((r) => (
              <li key={r.requestId}>
                <code>{r.requestId}</code>
                <span>{r.provider}</span>
                <Badge tone="good">completed</Badge>
              </li>
            ))}
          </ol>
        )}
      </section>
    </>
  );
}
function Budgets({ snapshot }: { snapshot: Snapshot }) {
  return (
    <section className="budgets">
      <div className="section-title">
        <h2>Client budgets</h2>
        <span className="muted">Estimated USD · reservations included</span>
      </div>
      <div
        className="table-scroll"
        role="region"
        aria-label="Client budgets, scroll horizontally for all columns"
        tabIndex={0}
      >
        <table>
          <thead>
            <tr>
              <th>Client</th>
              <th>Spent</th>
              <th>Reserved</th>
              <th>Remaining</th>
              <th>Budget</th>
              <th>Rate policy</th>
              <th>Rejected</th>
            </tr>
          </thead>
          <tbody>
            {snapshot.clients.map((c) => (
              <tr key={c.id}>
                <td>
                  <strong>{c.id}</strong>
                </td>
                <td className="mono">{money(c.spentMicros)}</td>
                <td className="mono">{money(c.reservedMicros)}</td>
                <td className="mono">{money(c.remainingMicros)}</td>
                <td className="mono">{money(c.budgetMicros)}</td>
                <td>
                  {c.rpm} / min · burst {c.burst}
                </td>
                <td>{c.rejected}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!snapshot.clients.length && (
        <p className="empty">No clients configured.</p>
      )}
      <div className="cache-summary">
        <strong>Exact cache</strong>
        <span>{snapshot.cache.entries} entries</span>
        <span>
          {snapshot.cache.hits} hits / {snapshot.cache.misses} misses
        </span>
        <span>{Math.round(snapshot.cache.ttlMs / 1000)} s TTL</span>
        <span>
          {snapshot.cache.bytes.toLocaleString()} /{" "}
          {snapshot.cache.maxBytes.toLocaleString()} bytes
        </span>
        <span>{snapshot.cache.evictions} evictions</span>
      </div>
    </section>
  );
}
function App() {
  const { feed, refresh } = useFeed();
  const [page, setPage] = useState<Page>(pageFromPath);
  useEffect(() => {
    const handler = () => setPage(pageFromPath());
    window.addEventListener("popstate", handler);
    return () => window.removeEventListener("popstate", handler);
  }, []);
  const navigate = (next: Page) => {
    history.pushState(null, "", next === "overview" ? "/" : `/${next}`);
    setPage(next);
    window.scrollTo({ top: 0 });
  };
  const data = "data" in feed ? feed.data : undefined;
  const snapshot = data?.snapshot;
  return (
    <div className="app">
      <aside className="sidebar">
        <a
          className="brand"
          href="/"
          onClick={(e) => {
            e.preventDefault();
            navigate("overview");
          }}
        >
          <svg
            width="28"
            height="28"
            viewBox="0 0 28 28"
            fill="none"
            aria-hidden="true"
          >
            <path
              d="M5 23V8l9-4 9 4v15M10 23V11h8v12M3 23h22"
              stroke="currentColor"
              strokeWidth="1.7"
              strokeLinejoin="round"
            />
          </svg>
          <span>Portunus</span>
        </a>
        <nav aria-label="Main navigation">
          {(["overview", "requests", "playground"] satisfies Page[]).map(
            (item) => (
              <a
                href={item === "overview" ? "/" : `/${item}`}
                key={item}
                aria-current={page === item ? "page" : undefined}
                onClick={(e) => {
                  e.preventDefault();
                  navigate(item);
                }}
              >
                <Icon name={item} />
                {item[0]?.toUpperCase()}
                {item.slice(1)}
              </a>
            ),
          )}
        </nav>
        <div className="sidebar-footer">
          <span
            className={`status-dot ${feed.kind === "live" ? "connected" : ""}`}
          />
          <span>
            {snapshot?.demo.enabled ? "Local demo" : "Gateway console"}
            <small>
              {feed.kind === "live"
                ? "Connected"
                : feed.kind === "loading"
                  ? "Connecting…"
                  : "Connection unavailable"}
            </small>
          </span>
        </div>
      </aside>
      <main>
        <header className="page-header">
          <div>
            <h1>
              {page === "overview"
                ? "Gateway overview"
                : page === "requests"
                  ? "Requests"
                  : "Playground"}
            </h1>
            <p className="muted">
              {page === "overview"
                ? "Routing, reliability, and the requests behind them."
                : page === "requests"
                  ? "Follow each request from admission to final outcome."
                  : "Test streaming, caching, and failure recovery."}
            </p>
          </div>
          <div className="live-controls">
            <Badge tone={feed.kind === "live" ? "good" : "warn"}>
              {feed.kind === "live"
                ? "Live"
                : feed.kind === "loading"
                  ? "Connecting"
                  : "Offline"}
            </Badge>
            <span className="caption">
              {snapshot
                ? `Updated ${clock(snapshot.capturedAt)}`
                : "Awaiting snapshot"}
            </span>
            <button
              className="icon-button"
              aria-label="Refresh gateway data"
              onClick={refresh}
            >
              <Icon name="refresh" />
            </button>
          </div>
        </header>
        {(feed.kind === "error" || feed.kind === "stale") && (
          <div className="connection-error" role="alert">
            <strong>
              {feed.kind === "stale"
                ? "Showing last known data"
                : "Gateway data unavailable"}
            </strong>
            <span>{feed.error}</span>
            <button onClick={refresh}>Retry connection</button>
          </div>
        )}
        {page === "overview" && (
          <>
            {snapshot ? (
              <>
                <section className="metrics" aria-label="Gateway metrics">
                  <div>
                    <span>Client requests</span>
                    <strong>{snapshot.totals.requests.toLocaleString()}</strong>
                    <small>
                      {snapshot.totals.active} active ·{" "}
                      {snapshot.totals.rejected} rejected
                    </small>
                  </div>
                  <div>
                    <span>Success rate</span>
                    <strong>
                      {snapshot.totals.requests
                        ? `${((snapshot.totals.succeeded / snapshot.totals.requests) * 100).toFixed(1)}%`
                        : "—"}
                    </strong>
                    <small>
                      {snapshot.totals.succeeded} completed /{" "}
                      {snapshot.totals.requests} received
                    </small>
                  </div>
                  <div>
                    <span>p95 duration</span>
                    <strong>{ms(snapshot.latency.p95Ms)}</strong>
                    <small>{snapshot.latency.count} retained samples</small>
                  </div>
                  <div>
                    <span>Estimated cost</span>
                    <strong>
                      {snapshot.totals.requests
                        ? money(snapshot.totals.costMicros)
                        : "—"}
                    </strong>
                    <small>
                      {snapshot.totals.providerAttempts} attempts ·{" "}
                      {snapshot.totals.fallbacks} fallbacks
                    </small>
                  </div>
                </section>
                <div className="overview-grid">
                  <Routing snapshot={snapshot} refresh={refresh} />
                  <Latency records={data?.requests ?? []} />
                </div>
              </>
            ) : feed.kind === "loading" ? (
              <div className="loading" role="status">
                <span className="status-dot" /> Reading gateway state…
              </div>
            ) : null}
          </>
        )}
        {page === "playground" && (
          <Playground snapshot={snapshot} refresh={refresh} />
        )}
        {page !== "playground" && feed.kind !== "loading" && (
          <Activity
            records={data?.requests ?? []}
            full={page === "requests"}
            nextCursor={data?.nextCursor ?? null}
            refresh={refresh}
          />
        )}
        {page === "overview" && snapshot && (
          <>
            <div className="workspace-divider">
              <h2>Operate the gateway</h2>
              <p className="caption">
                Generate real evidence, then inspect the route above.
              </p>
            </div>
            <Playground snapshot={snapshot} refresh={refresh} />
            <Budgets snapshot={snapshot} />
          </>
        )}
        {page === "playground" && data && (
          <Activity
            records={data.requests}
            full={false}
            nextCursor={data.nextCursor}
            refresh={refresh}
          />
        )}
        <footer className="main-footer">
          <span>Portunus / operator console</span>
          <span>
            {snapshot
              ? `Process uptime ${Math.floor(snapshot.uptimeMs / 60000)} min · sequence ${snapshot.sequence}`
              : "Waiting for gateway"}{" "}
            · costs are estimated
          </span>
        </footer>
      </main>
    </div>
  );
}
const root = document.getElementById("root");
if (!root) throw new Error("Missing root element");
createRoot(root).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
