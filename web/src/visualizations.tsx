import type { ReactNode } from "react";
import type { RequestRecord, Snapshot } from "./api";
import { deriveTelemetry, outcomes } from "./telemetry";

type Telemetry = ReturnType<typeof deriveTelemetry>;
const time = (value: string) =>
  new Date(value).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
const duration = (value: number) =>
  value < 10
    ? Number(value.toFixed(1)).toLocaleString()
    : Math.round(value).toLocaleString();
const labels = {
  success: "Success",
  error: "Error",
  rejected: "Rejected",
  canceled: "Canceled",
};

function ChartPanel({
  title,
  scope,
  children,
  className = "",
}: {
  title: string;
  scope: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`chart-panel ${className}`}>
      <div className="section-title">
        <h2>{title}</h2>
        <span className="scope">{scope}</span>
      </div>
      {children}
    </section>
  );
}
function EmptyChart({ children }: { children: ReactNode }) {
  return (
    <div className="chart-empty">
      <strong>No observations yet</strong>
      <p>{children}</p>
    </div>
  );
}

export function Timeline({ telemetry }: { telemetry: Telemetry }) {
  const { measured, maxDuration, durationCeiling } = telemetry;
  const first = measured[0];
  const last = measured.at(-1);
  const start = first ? Date.parse(first.startedAt) : 0;
  const end = last ? Date.parse(last.startedAt) : 0;
  const x = (record: RequestRecord) =>
    start === end
      ? 281
      : 70 + ((Date.parse(record.startedAt) - start) / (end - start)) * 422;
  const y = (value: number) => 204 - (value / durationCeiling) * 152;
  return (
    <ChartPanel
      title="Latency over time"
      scope="Latest 100 requests"
      className="timeline-panel"
    >
      <p className="caption">End-to-end duration by request start time</p>
      {first && last ? (
        <>
          <svg
            className="timeline-chart"
            viewBox="0 0 520 264"
            role="img"
            aria-label={`${measured.length} non-rejected requests from ${time(first.startedAt)} to ${time(last.startedAt)}. Maximum duration ${duration(maxDuration)} milliseconds. Each point is a request.`}
          >
            <text x="70" y="23">
              Duration · ms
            </text>
            {[0, 0.5, 1].map((fraction) => (
              <g key={fraction}>
                <line
                  className="gridline"
                  x1="70"
                  x2="492"
                  y1={y(fraction * durationCeiling)}
                  y2={y(fraction * durationCeiling)}
                />
                <text
                  x="58"
                  y={y(fraction * durationCeiling) + 4}
                  textAnchor="end"
                >
                  {duration(fraction * durationCeiling)}
                </text>
              </g>
            ))}
            {measured.map((record) => (
              <circle
                key={record.id}
                cx={x(record)}
                cy={y(record.durationMs)}
                r="4"
                className={`plot-point outcome-${record.status}`}
              >
                <title>{`${time(record.startedAt)} · ${record.clientId} · ${labels[record.status]} · ${duration(record.durationMs)} ms · ${record.id}`}</title>
              </circle>
            ))}
            <text
              x={start === end ? 281 : 70}
              y="230"
              textAnchor={start === end ? "middle" : "start"}
            >
              {time(first.startedAt)}
            </text>
            {start !== end && (
              <text x="492" y="230" textAnchor="end">
                {time(last.startedAt)}
              </text>
            )}
            <text x="281" y="253" textAnchor="middle">
              Request start · local time
            </text>
          </svg>
          <div className="chart-foot">
            <span>{measured.length} observations · rejections excluded</span>
            <div className="legend">
              <span>
                <i className="swatch outcome-success" />
                Success
              </span>
              <span>
                <i className="swatch outcome-error" />
                Error
              </span>
              <span>
                <i className="swatch outcome-canceled" />
                Canceled
              </span>
            </div>
          </div>
        </>
      ) : (
        <EmptyChart>
          Send a request in Playground to measure its duration. Admission
          rejections are excluded.
        </EmptyChart>
      )}
    </ChartPanel>
  );
}

export function OutcomeChart({
  telemetry,
  total,
}: {
  telemetry: Telemetry;
  total: number;
}) {
  const peak = Math.max(1, ...Object.values(telemetry.counts));
  return (
    <ChartPanel title="Request outcomes" scope="Latest 100 requests">
      <p className="caption">{total} observed client requests</p>
      {total ? (
        <>
          <svg
            className="outcome-chart"
            viewBox="0 0 350 240"
            role="img"
            aria-label={outcomes
              .map(
                (status) =>
                  `${telemetry.counts[status]} ${labels[status].toLowerCase()}`,
              )
              .join(", ")}
          >
            {outcomes.map((status, index) => (
              <g key={status}>
                <text x="0" y={36 + index * 45}>
                  {labels[status]}
                </text>
                <rect
                  className="bar-track"
                  x="90"
                  y={23 + index * 45}
                  width="210"
                  height="20"
                  rx="3"
                />
                <rect
                  className={`outcome-${status}`}
                  x="90"
                  y={23 + index * 45}
                  width={(telemetry.counts[status] / peak) * 210}
                  height="20"
                  rx="3"
                />
                <text x="340" y={37 + index * 45} textAnchor="end">
                  {telemetry.counts[status]}
                </text>
              </g>
            ))}
            <text x="90" y="225">
              0
            </text>
            <text x="300" y="225" textAnchor="end">
              {peak} requests
            </text>
          </svg>
          <div className="chart-foot">
            <span>{telemetry.cacheHits} cache hits</span>
            <span>
              {telemetry.multiAttemptRequests} requests with multiple attempts
            </span>
          </div>
        </>
      ) : (
        <EmptyChart>
          Outcomes appear when requests complete, fail, or are rejected.
        </EmptyChart>
      )}
    </ChartPanel>
  );
}

export function Histogram({ telemetry }: { telemetry: Telemetry }) {
  const peak = Math.max(1, ...telemetry.buckets.map((bucket) => bucket.count));
  return (
    <ChartPanel title="Duration distribution" scope="Latest 100 requests">
      <p className="caption">
        {telemetry.measured.length} observations · rejections excluded
      </p>
      {telemetry.measured.length ? (
        <>
          <svg
            className="histogram-chart"
            viewBox="0 0 440 220"
            role="img"
            aria-label={`Eight duration buckets between zero and ${duration(telemetry.durationCeiling)} milliseconds. Counts ${telemetry.buckets.map((bucket) => bucket.count).join(", ")}.`}
          >
            <text x="38" y="22">
              Requests
            </text>
            {[0, 0.5, 1].map((fraction) => (
              <g key={fraction}>
                <line
                  x1="38"
                  x2="422"
                  y1={170 - fraction * 126}
                  y2={170 - fraction * 126}
                  className="gridline"
                />
                <text x="28" y={174 - fraction * 126} textAnchor="end">
                  {Number((peak * fraction).toFixed(1))}
                </text>
              </g>
            ))}
            {telemetry.buckets.map((bucket, index) => (
              <rect
                key={index}
                className="histogram-bar"
                x={44 + index * 47}
                y={170 - (bucket.count / peak) * 126}
                width="34"
                height={(bucket.count / peak) * 126}
                rx="3"
              >
                <title>{`${duration(bucket.startMs)} to ${duration(bucket.endMs)} ms · ${bucket.count} requests`}</title>
              </rect>
            ))}
            <text x="38" y="194">
              0 ms
            </text>
            <text x="422" y="194" textAnchor="end">
              {duration(telemetry.durationCeiling)} ms
            </text>
            <text x="230" y="216" textAnchor="middle">
              End-to-end duration · ms
            </text>
          </svg>
        </>
      ) : (
        <EmptyChart>
          Duration buckets will show the spread of measured request latency.
        </EmptyChart>
      )}
    </ChartPanel>
  );
}

export function ProviderVolumes({
  snapshot,
  telemetry,
}: {
  snapshot: Snapshot;
  telemetry: Telemetry;
}) {
  const providers = [
    ...snapshot.providers.map((provider) => ({
      id: provider.id,
      name: provider.name,
      circuit: provider.circuit,
      retryAt: provider.retryAt,
      ...telemetry.providers.find((entry) => entry.id === provider.id),
    })),
    ...telemetry.providers
      .filter(
        (entry) =>
          !snapshot.providers.some((provider) => provider.id === entry.id),
      )
      .map((entry) => ({
        ...entry,
        name: entry.id,
        circuit: null,
        retryAt: null,
      })),
  ];
  const peak = Math.max(
    1,
    ...providers.map((provider) =>
      Math.max(provider.attempts ?? 0, provider.routes ?? 0),
    ),
  );
  return (
    <ChartPanel title="Provider traffic & health" scope="Latest 100 requests">
      <p className="caption">
        Final uncached routes compared with all provider attempts
      </p>
      <div className="legend provider-legend">
        <span>
          <i className="swatch route-swatch" />
          Final routes
        </span>
        <span>
          <i className="swatch attempt-swatch" />
          Attempts
        </span>
      </div>
      {providers.length ? (
        <div className="volume-list">
          {providers.map((provider) => (
            <div className="volume-provider" key={provider.id}>
              <div className="volume-heading">
                <strong>{provider.name}</strong>
                {provider.circuit && (
                  <span
                    className={`badge ${provider.circuit === "closed" ? "good" : "warn"}`}
                  >
                    {provider.circuit.replace("_", " ")}
                  </span>
                )}
              </div>
              <svg
                viewBox="0 0 480 65"
                className="volume-chart"
                role="img"
                aria-label={`${provider.name}, ${provider.routes ?? 0} final uncached routes, ${provider.attempts ?? 0} attempts, ${provider.failures ?? 0} unsuccessful attempts. Scale zero to ${peak} requests or attempts.`}
              >
                <rect
                  className="bar-track"
                  x="0"
                  y="5"
                  width="424"
                  height="10"
                  rx="2"
                />
                <rect
                  className="route-bar"
                  x="0"
                  y="5"
                  width={((provider.routes ?? 0) / peak) * 424}
                  height="10"
                  rx="2"
                />
                <text x="480" y="15" textAnchor="end">
                  {provider.routes ?? 0}
                </text>
                <rect
                  className="bar-track"
                  x="0"
                  y="24"
                  width="424"
                  height="10"
                  rx="2"
                />
                <rect
                  className="attempt-bar"
                  x="0"
                  y="24"
                  width={((provider.attempts ?? 0) / peak) * 424}
                  height="10"
                  rx="2"
                />
                <text x="480" y="34" textAnchor="end">
                  {provider.attempts ?? 0}
                </text>
                <text x="0" y="57">
                  0
                </text>
                <text x="424" y="57" textAnchor="end">
                  {peak} requests / attempts
                </text>
              </svg>
              <p className="caption">
                {provider.failures ?? 0} unsuccessful attempts
                {provider.retryAt
                  ? ` · probe eligible after ${time(provider.retryAt)}`
                  : ""}
              </p>
            </div>
          ))}
        </div>
      ) : (
        <EmptyChart>No providers are configured.</EmptyChart>
      )}
      <p className="chart-note">
        Circuit state is current. Cached responses have no provider attempt.
        Multiple attempts may belong to one request.
      </p>
    </ChartPanel>
  );
}
