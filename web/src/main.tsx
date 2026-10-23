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
import { budgetUsage, deriveTelemetry, mergeRequestRecords } from "./telemetry";
import {
  Histogram,
  OutcomeChart,
  ProviderVolumes,
  Timeline,
} from "./visualizations";
import "./style.css";

type WorkspaceOperation = "idle" | "request" | "demo" | "provider";
type Data = {
  epoch: number;
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
          setFeed((previous) => ({
            kind: "live",
            data: {
              epoch:
                "data" in previous
                  ? previous.data.epoch +
                    (snapshot.uptimeMs < previous.data.snapshot.uptimeMs ||
                    snapshot.sequence < previous.data.snapshot.sequence
                      ? 1
                      : 0)
                  : 0,
              snapshot,
              requests: records.items,
              nextCursor: records.nextCursor,
            },
          }));
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
function Activity({
  records,
  full,
  nextCursor,
  refresh,
  maxRecords,
}: {
  records: RequestRecord[];
  full: boolean;
  nextCursor: string | null;
  refresh: () => void;
  maxRecords: number;
}) {
  const requestedId = full
    ? new URLSearchParams(location.search).get("request")
    : null;
  const [query, setQuery] = useState(requestedId ?? "");
  const [status, setStatus] = useState("all");
  const [expanded, setExpanded] = useState<string | null>(requestedId);
  const [older, setOlder] = useState<RequestRecord[]>([]);
  const [cursor, setCursor] = useState<string | null | undefined>(undefined);
  const [paging, setPaging] = useState<Operation>({ kind: "idle" });
  const [lookup, setLookup] = useState<Operation>({ kind: "idle" });
  useEffect(() => {
    if (!requestedId || records.some((record) => record.id === requestedId))
      return;
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 8000);
    setLookup({
      kind: "pending",
      label: "Finding selected request in retained history…",
    });
    const find = async () => {
      try {
        let before: string | null = null;
        let examined = 0;
        do {
          const result: ReturnType<typeof requestsSchema.parse> = await json(
            `/api/v1/requests?limit=1000${before ? `&before=${encodeURIComponent(before)}` : ""}`,
            requestsSchema,
            { signal: controller.signal },
          );
          if (controller.signal.aborted) return;
          setCursor(result.nextCursor);
          setOlder((previous) =>
            mergeRequestRecords(result.items, previous, maxRecords),
          );
          if (result.items.some((record) => record.id === requestedId)) {
            setLookup({ kind: "idle" });
            return;
          }
          examined += result.items.length;
          before = result.nextCursor;
        } while (before && examined < maxRecords);
        setLookup({
          kind: "error",
          label:
            "The selected request is no longer retained. The gateway may have restarted or reached its history limit. Clear the search to inspect current requests.",
        });
      } catch (error) {
        if (!controller.signal.aborted)
          setLookup({
            kind: "error",
            label: `Could not find the selected request: ${message(error)}. Refresh this page to retry.`,
          });
        else
          setLookup({
            kind: "error",
            label:
              "Searching request history timed out. Refresh this page to retry.",
          });
      } finally {
        clearTimeout(timeout);
      }
    };
    void find();
    return () => {
      clearTimeout(timeout);
      controller.abort();
    };
  }, [requestedId, maxRecords]);
  const pagingController = useRef<AbortController | null>(null);
  useEffect(() => () => pagingController.current?.abort(), []);
  useEffect(() => {
    setOlder((previous) => mergeRequestRecords(records, previous, maxRecords));
  }, [records, maxRecords]);
  const combined = mergeRequestRecords(records, older, maxRecords);
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
      setOlder((items) => mergeRequestRecords(result.items, items, maxRecords));
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
          <h2>{full ? "Request explorer" : "Recent activity"}</h2>
          <p className="caption">
            One row per client request. Open details to follow every provider
            attempt.
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
          <span className="filter-count">
            {filtered.length} matching / {combined.length} loaded
          </span>
          {(query || status !== "all") && (
            <button
              className="quiet"
              onClick={() => {
                setQuery("");
                setStatus("all");
              }}
            >
              Clear filters
            </button>
          )}
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
                      {r.id.length > 12 ? `…${r.id.slice(-10)}` : r.id}
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
      {lookup.kind !== "idle" && (
        <p
          className={
            lookup.kind === "error"
              ? "error-text lookup-notice"
              : "caption lookup-notice"
          }
          role={lookup.kind === "error" ? "alert" : "status"}
        >
          {lookup.label}
        </p>
      )}
      {!shown.length && lookup.kind !== "pending" && (
        <div className="empty">
          <strong>
            {full && combined.length
              ? "No requests match these filters"
              : "No requests yet"}
          </strong>
          <p>
            {full && combined.length
              ? "Try another client, provider, or outcome."
              : "Open the playground and send your first request."}
          </p>
        </div>
      )}
      <div className="activity-footer">
        <span className="caption">
          {full
            ? `${combined.length} loaded records. Filters apply to loaded records only.`
            : "Latest 8 requests. Open Requests for search and earlier records."}
        </span>
      </div>
      {full && (cursor === undefined ? nextCursor : cursor) && (
        <button
          className="load-more"
          disabled={paging.kind === "pending"}
          onClick={() => void loadMore()}
        >
          {paging.kind === "pending" ? paging.label : "Load earlier requests"}
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
  workspaceOperation,
  setWorkspaceOperation,
}: {
  snapshot: Snapshot;
  refresh: () => void;
  workspaceOperation: WorkspaceOperation;
  setWorkspaceOperation: (operation: WorkspaceOperation) => void;
}) {
  const [operations, setOperations] = useState<Record<string, Operation>>({});
  const [applied, setApplied] = useState<Record<string, Fake>>({});
  const controllers = useRef(new Set<AbortController>());
  useEffect(() => () => controllers.current.forEach((c) => c.abort()), []);
  const control = async (id: string, preset: Preset) => {
    setWorkspaceOperation("provider");
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
      setWorkspaceOperation("idle");
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
        <span className="scope">Attempts / failures / cost · lifetime</span>
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
                  <dt>Attempts</dt>
                  <dd>{p.requests}</dd>
                </div>
                <div>
                  <dt>Failures</dt>
                  <dd>{p.failures}</dd>
                </div>
                <div>
                  <dt>Active now</dt>
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
              {snapshot.demo.enabled && fake && (
                <p className="control-label">Set provider behavior</p>
              )}
              <div className="presets">
                {presets.map((preset) => (
                  <button
                    key={preset.name}
                    disabled={
                      !snapshot.demo.enabled ||
                      !fake ||
                      workspaceOperation !== "idle" ||
                      op?.kind === "pending"
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
  inspectRequest,
  workspaceOperation,
  setWorkspaceOperation,
}: {
  snapshot: Snapshot | undefined;
  refresh: () => void;
  inspectRequest: (id: string) => void;
  workspaceOperation: WorkspaceOperation;
  setWorkspaceOperation: (operation: WorkspaceOperation) => void;
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
  const busy = workspaceOperation !== "idle";
  const send = async () => {
    setWorkspaceOperation("request");
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
      setWorkspaceOperation("idle");
    }
  };
  const primary = snapshot?.providers.find(
    (p) => p.id === snapshot.demo.routing[0],
  );
  const runDemo = async (restore: boolean) => {
    if (!primary?.fake) return;
    setWorkspaceOperation("demo");
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
            "4 requests completed. Open Requests for the actual provider attempts. Primary remains set to HTTP 503 until restored.",
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
      setWorkspaceOperation("idle");
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
                disabled={
                  output.kind !== "streaming" && demo.kind !== "pending"
                }
                onClick={() => controller.current?.abort()}
              >
                Stop request
              </button>
            </div>
            <p className="caption">
              Use a configured client key. Local demo uses demo-key. Output
              display is limited to 32,000 characters.
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
                : output.text ||
                  (output.kind === "streaming"
                    ? "Waiting for first chunk…"
                    : "No output received.")}
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
                <button
                  type="button"
                  className="request-link"
                  onClick={() => inspectRequest(output.result.requestId)}
                >
                  Inspect request <code>{output.result.requestId}</code>
                  <Icon name="arrow" />
                </button>
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
                <button
                  type="button"
                  className="request-link"
                  onClick={() => inspectRequest(r.requestId)}
                  aria-label={`Inspect request ${r.requestId}`}
                >
                  <code>{r.requestId}</code>
                  <Icon name="arrow" />
                </button>
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
        <div>
          <h2>Client budgets</h2>
          <p className="caption">
            Estimated USD · lifetime spend plus active reservations
          </p>
        </div>
        <div className="legend">
          <span>
            <i className="swatch route-swatch" />
            Spent
          </span>
          <span>
            <i className="swatch attempt-swatch" />
            Reserved
          </span>
        </div>
      </div>
      <div className="budget-list">
        {snapshot.clients.map((client) => {
          const usage = budgetUsage(client);
          return (
            <div className="budget-row" key={client.id}>
              <div className="budget-identity">
                <strong>{client.id}</strong>
                <span className="caption">
                  {client.rpm} requests / min · burst {client.burst}
                </span>
              </div>
              <div className="budget-visual">
                <div className="budget-values">
                  <span>
                    {money(client.spentMicros)}{" "}
                    <span className="muted">spent</span>
                  </span>
                  <span className="muted">of {money(client.budgetMicros)}</span>
                </div>
                <svg
                  className="budget-meter"
                  viewBox="0 0 400 8"
                  preserveAspectRatio="none"
                  role="img"
                  aria-label={`${client.id} budget. ${money(client.spentMicros)} spent, ${money(client.reservedMicros)} reserved, ${money(client.remainingMicros)} remaining of ${money(client.budgetMicros)}. ${usage.zeroBudget ? "Zero budget configured." : `${(usage.spentPercent + usage.reservedPercent).toFixed(1)} percent committed.`}`}
                >
                  <rect className="bar-track" width="400" height="8" rx="4" />
                  <rect
                    className={
                      usage.exhausted ? "outcome-rejected" : "route-bar"
                    }
                    width={usage.spentWidth * 4}
                    height="8"
                    rx="4"
                  />
                  <rect
                    className="attempt-bar"
                    x={usage.spentWidth * 4}
                    width={usage.reservedWidth * 4}
                    height="8"
                  />
                </svg>
                <p className="caption">
                  {money(client.reservedMicros)} reserved
                  {usage.zeroBudget
                    ? " · zero budget configured"
                    : usage.exhausted
                      ? " · budget committed"
                      : ""}
                </p>
              </div>
              <div className="budget-remaining">
                <strong>{money(client.remainingMicros)}</strong>
                <span className="caption">
                  remaining · {client.rejected} rejected
                </span>
              </div>
            </div>
          );
        })}
      </div>
      {!snapshot.clients.length && (
        <p className="empty">No clients configured.</p>
      )}
      <div className="cache-summary">
        <strong>Exact cache</strong>
        <span>
          {snapshot.cache.entries} / {snapshot.cache.maxEntries} entries
        </span>
        <span>
          {snapshot.cache.hits} hits / {snapshot.cache.misses} misses · lifetime
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
const pages = {
  overview: {
    title: "Gateway overview",
    description: "A clear view of request health, routing, and spend.",
    task: "Monitor",
  },
  requests: {
    title: "Requests",
    description: "Find a request. Follow its route. Understand the outcome.",
    task: "Investigate",
  },
  playground: {
    title: "Playground",
    description: "Send real requests and test how the gateway recovers.",
    task: "Operate",
  },
} satisfies Record<Page, { title: string; description: string; task: string }>;
function App() {
  const { feed, refresh } = useFeed();
  const [workspaceOperation, setWorkspaceOperation] =
    useState<WorkspaceOperation>("idle");
  const [page, setPage] = useState<Page>(pageFromPath);
  const [selectedRequest, setSelectedRequest] = useState(() =>
    new URLSearchParams(location.search).get("request"),
  );
  useEffect(() => {
    const handler = () => {
      setPage(pageFromPath());
      setSelectedRequest(new URLSearchParams(location.search).get("request"));
    };
    window.addEventListener("popstate", handler);
    return () => window.removeEventListener("popstate", handler);
  }, []);
  const navigate = (next: Page) => {
    history.pushState(null, "", next === "overview" ? "/" : `/${next}`);
    setPage(next);
    setSelectedRequest(null);
    window.scrollTo({ top: 0 });
  };
  const inspectRequest = (id: string) => {
    history.pushState(null, "", `/requests?request=${encodeURIComponent(id)}`);
    setPage("requests");
    setSelectedRequest(id);
    refresh();
    window.scrollTo({ top: 0 });
  };
  const data = "data" in feed ? feed.data : undefined;
  const snapshot = data?.snapshot;
  const telemetry = deriveTelemetry(data?.requests ?? []);
  const link = (
    next: Page,
    children: React.ReactNode,
    className = "text-link",
  ) => (
    <a
      href={next === "overview" ? "/" : `/${next}`}
      className={className}
      onClick={(event) => {
        if (
          event.button === 0 &&
          !event.metaKey &&
          !event.ctrlKey &&
          !event.shiftKey &&
          !event.altKey
        ) {
          event.preventDefault();
          navigate(next);
        }
      }}
    >
      {children}
    </a>
  );
  return (
    <div className="app">
      <a className="skip-link" href="#main-content">
        Skip to content
      </a>
      <aside className="sidebar">
        {link(
          "overview",
          <>
            <svg
              width="30"
              height="30"
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
          </>,
          "brand",
        )}
        <div className="workspace-label">
          <span className="workspace-dot" />
          Gateway console
        </div>
        <nav aria-label="Main navigation">
          {(["overview", "requests", "playground"] satisfies Page[]).map(
            (item) => (
              <a
                href={item === "overview" ? "/" : `/${item}`}
                key={item}
                aria-current={page === item ? "page" : undefined}
                onClick={(event) => {
                  if (
                    event.button === 0 &&
                    !event.metaKey &&
                    !event.ctrlKey &&
                    !event.shiftKey &&
                    !event.altKey
                  ) {
                    event.preventDefault();
                    navigate(item);
                  }
                }}
              >
                <Icon name={item} />
                <span>
                  {item[0]?.toUpperCase()}
                  {item.slice(1)}
                  <small>{pages[item].task}</small>
                </span>
                {page === item && <span className="nav-marker" />}
              </a>
            ),
          )}
        </nav>
        <div className="sidebar-note">
          <p>
            One request.
            <br />
            Every attempt.
          </p>
          <span>
            Observe the gateway.
            <br />
            Understand the decisions.
          </span>
        </div>
        <div className="sidebar-footer">
          <span
            className={`status-dot ${feed.kind === "live" ? "connected" : ""}`}
          />
          <span>
            {snapshot?.demo.enabled ? "Local demo" : "Gateway console"}
            <small>
              {feed.kind === "live"
                ? "Connected · polls every 2 s"
                : feed.kind === "loading"
                  ? "Connecting…"
                  : "Connection unavailable"}
            </small>
          </span>
        </div>
      </aside>
      <main id="main-content">
        <div className="topbar">
          <span>
            Workspace <span className="breadcrumb-divider">/</span>{" "}
            {pages[page].task}
          </span>
          <span>
            {snapshot?.demo.enabled
              ? "Local demo · synthetic providers"
              : "Gateway telemetry"}
          </span>
        </div>
        <header className="page-header">
          <div>
            <h1>{pages[page].title}</h1>
            <p className="muted">{pages[page].description}</p>
          </div>
          <div className="live-controls">
            <div>
              <Badge tone={feed.kind === "live" ? "good" : "warn"}>
                <span
                  className={`status-dot ${feed.kind === "live" ? "connected" : ""}`}
                />
                {feed.kind === "live"
                  ? "Live"
                  : feed.kind === "loading"
                    ? "Connecting"
                    : feed.kind === "stale"
                      ? "Stale"
                      : "Offline"}
              </Badge>
              <span className="caption">
                {snapshot
                  ? `Updated ${clock(snapshot.capturedAt)}`
                  : "Awaiting snapshot"}
              </span>
            </div>
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
            <div>
              <strong>
                {feed.kind === "stale"
                  ? "Showing last known data"
                  : "Gateway data unavailable"}
              </strong>
              <p>{feed.error}</p>
              <p className="caption">
                Automatic retries continue every 2 seconds while this tab is
                visible.
              </p>
            </div>
            <button onClick={refresh}>Retry connection</button>
          </div>
        )}
        {feed.kind === "loading" && page !== "playground" && (
          <div className="loading" role="status">
            <span className="status-dot" />
            Reading gateway state…
            <p className="caption">
              Connecting to request records and the latest snapshot.
            </p>
          </div>
        )}
        {page === "overview" && snapshot && data && (
          <>
            <div className="overview-summary">
              <h2>At a glance</h2>
              <span className="scope">
                Lifetime totals · since process start
              </span>
            </div>
            <section
              className="metrics"
              aria-label="Gateway metrics and measurement scopes"
            >
              <div>
                <span>Client requests</span>
                <strong>{snapshot.totals.requests.toLocaleString()}</strong>
                <small>
                  {snapshot.totals.active} active · {snapshot.totals.rejected}{" "}
                  rejected
                </small>
              </div>
              <div>
                <span>Successful / received</span>
                <strong>
                  {snapshot.totals.requests
                    ? `${((snapshot.totals.succeeded / snapshot.totals.requests) * 100).toFixed(1)}%`
                    : "No requests"}
                </strong>
                <small>
                  {snapshot.totals.succeeded} successful /{" "}
                  {snapshot.totals.requests} received · lifetime
                </small>
              </div>
              <div>
                <span>
                  p95 duration <span className="scope-inline">Retained</span>
                </span>
                <strong>{ms(snapshot.latency.p95Ms)}</strong>
                <small>
                  {snapshot.latency.count} retained latency samples · p50{" "}
                  {ms(snapshot.latency.p50Ms)}
                </small>
              </div>
              <div>
                <span>Estimated spend</span>
                <strong>{money(snapshot.totals.costMicros)}</strong>
                <small>
                  {snapshot.totals.providerAttempts} attempts ·{" "}
                  {snapshot.totals.fallbacks} fallbacks · lifetime
                </small>
              </div>
            </section>
            <div className="guidance">
              <div>
                <strong>
                  {data.requests.length
                    ? "Put the gateway to the test"
                    : "Your gateway is ready for its first request"}
                </strong>
                <p className="caption">
                  {data.requests.length
                    ? "Send a prompt or simulate a provider failure. Follow the resulting attempts in Requests."
                    : "Send a prompt in Playground. These charts will fill with real request observations."}
                </p>
              </div>
              {link(
                "playground",
                <>
                  Open playground <Icon name="arrow" />
                </>,
                "button-link primary",
              )}
            </div>
            <div className="chart-grid primary-charts">
              <Timeline telemetry={telemetry} />
              <OutcomeChart
                telemetry={telemetry}
                total={data.requests.length}
              />
            </div>
            <div className="chart-grid secondary-charts">
              <ProviderVolumes snapshot={snapshot} telemetry={telemetry} />
              <Histogram telemetry={telemetry} />
            </div>
            <Budgets snapshot={snapshot} />
            <div className="activity-link">
              {link(
                "requests",
                <>
                  Explore all requests <Icon name="arrow" />
                </>,
              )}
            </div>
            <Activity
              key={`overview-${data.epoch}`}
              records={data.requests}
              full={false}
              nextCursor={data.nextCursor}
              maxRecords={data.snapshot.retention.maxRequests}
              refresh={refresh}
            />
          </>
        )}
        {page === "requests" && data && (
          <>
            <div className="request-context">
              <p>
                <strong>
                  {snapshot?.retention.maxRequests.toLocaleString()}
                </strong>{" "}
                request records retained at most. Search the latest 100, then
                load earlier records.
              </p>
              <span className="caption">
                Details include timing, usage provenance, cost, and provider
                attempts.
              </span>
            </div>
            <Activity
              key={`requests-${data.epoch}-${selectedRequest ?? "all"}`}
              records={data.requests}
              full
              nextCursor={data.nextCursor}
              maxRecords={data.snapshot.retention.maxRequests}
              refresh={refresh}
            />
          </>
        )}
        <div hidden={page !== "playground"}>
          <Playground
            snapshot={snapshot}
            refresh={refresh}
            inspectRequest={inspectRequest}
            workspaceOperation={workspaceOperation}
            setWorkspaceOperation={setWorkspaceOperation}
          />
          {snapshot && (
            <Routing
              snapshot={snapshot}
              refresh={refresh}
              workspaceOperation={workspaceOperation}
              setWorkspaceOperation={setWorkspaceOperation}
            />
          )}
          <div className="investigate-link">
            <div>
              <h2>Follow the result</h2>
              <p className="caption">
                Inspect request outcomes, provider attempts, and reported or
                estimated usage.
              </p>
            </div>
            {link(
              "requests",
              <>
                Open requests <Icon name="arrow" />
              </>,
              "button-link",
            )}
          </div>
        </div>
        <footer className="main-footer">
          <span>
            Portunus <span className="footer-divider">/</span> operator console
          </span>
          <span>
            {snapshot
              ? `Uptime ${Math.floor(snapshot.uptimeMs / 60000)} min · sequence ${snapshot.sequence} · retention up to ${snapshot.retention.maxRequests.toLocaleString()} requests`
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
