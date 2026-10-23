import assert from "node:assert/strict";
import { test } from "node:test";
import { requestSchema } from "../src/api.ts";
import {
  budgetUsage,
  deriveTelemetry,
  mergeRequestRecords,
} from "../src/telemetry.ts";

function request(overrides = {}) {
  return requestSchema.parse({
    id: "request-1",
    startedAt: "2026-10-23T12:00:00Z",
    clientId: "demo",
    model: "general",
    status: "success",
    errorCode: "",
    provider: "backup",
    cacheHit: false,
    durationMs: 100,
    ttftMs: 10,
    inputTokens: 1,
    outputTokens: 2,
    usageAuthoritative: false,
    costMicros: "10",
    attempts: [],
    ...overrides,
  });
}
function attempt(provider, status) {
  return {
    provider,
    model: "general",
    startedAt: "2026-10-23T12:00:00Z",
    durationMs: 50,
    ttftMs: null,
    status,
    errorCode: "",
    httpStatus: status === "success" ? 200 : 503,
    inputTokens: 1,
    outputTokens: 2,
    usageAuthoritative: false,
    reservedMicros: "10",
    costMicros: "5",
  };
}

test("fallback attempts, final routes, cache hits, and rejected requests stay distinct", () => {
  const records = [
    request({
      id: "fallback",
      attempts: [attempt("primary", "error"), attempt("backup", "success")],
    }),
    request({
      id: "cached",
      cacheHit: true,
      durationMs: 0,
      startedAt: "2026-10-23T12:00:02Z",
    }),
    request({
      id: "rejected",
      status: "rejected",
      provider: "",
      durationMs: 0,
    }),
    request({
      id: "canceled",
      status: "canceled",
      provider: "primary",
      durationMs: 800,
      startedAt: "2026-10-23T11:59:00Z",
      attempts: [attempt("primary", "canceled")],
    }),
    request({
      id: "error",
      status: "error",
      provider: "primary",
      durationMs: 200,
      attempts: [attempt("primary", "error")],
    }),
  ];
  const originalOrder = records.map((record) => record.id);
  const result = deriveTelemetry(records);
  assert.deepEqual(result.counts, {
    success: 2,
    error: 1,
    rejected: 1,
    canceled: 1,
  });
  assert.equal(result.cacheHits, 1);
  assert.equal(result.multiAttemptRequests, 1);
  assert.deepEqual(result.providers, [
    { id: "primary", routes: 2, attempts: 3, failures: 3 },
    { id: "backup", routes: 1, attempts: 1, failures: 0 },
  ]);
  assert.deepEqual(
    result.measured.map((record) => record.id),
    ["canceled", "fallback", "error", "cached"],
  );
  assert.deepEqual(
    records.map((record) => record.id),
    originalOrder,
  );
  assert.equal(
    result.buckets.reduce((sum, bucket) => sum + bucket.count, 0),
    4,
  );
  assert.equal(result.buckets.at(-1).count, 1);
  assert.equal(result.maxDuration, 800);
});

test("empty and zero-duration observations produce finite histogram scales", () => {
  const empty = deriveTelemetry([]);
  assert.equal(empty.measured.length, 0);
  assert.equal(empty.providers.length, 0);
  assert.equal(
    empty.buckets.reduce((sum, bucket) => sum + bucket.count, 0),
    0,
  );
  const zero = deriveTelemetry([request({ durationMs: 0 })]);
  assert.equal(zero.buckets[0].count, 1);
  assert.equal(zero.maxDuration, 0);
  assert.equal(zero.durationCeiling, 1);
  assert.ok(
    zero.buckets.every(
      (bucket) =>
        Number.isFinite(bucket.startMs) && Number.isFinite(bucket.endMs),
    ),
  );
});

test("budget reservations use exact microdollar arithmetic even above Number precision", () => {
  const usage = budgetUsage({
    budgetMicros: "100000000000000000000",
    spentMicros: "25000000000000000000",
    reservedMicros: "10000000000000000000",
  });
  assert.equal(usage.spentPercent, 25);
  assert.equal(usage.reservedPercent, 10);
  assert.equal(usage.spentWidth, 25);
  assert.equal(usage.reservedWidth, 10);
  assert.equal(usage.exhausted, false);
});

test("zero and exhausted budgets cannot overfill a meter or imply available capacity", () => {
  const zero = budgetUsage({
    budgetMicros: "0",
    spentMicros: "0",
    reservedMicros: "0",
  });
  assert.equal(zero.zeroBudget, true);
  assert.equal(zero.exhausted, true);
  assert.equal(zero.spentWidth, 0);
  assert.equal(zero.reservedWidth, 0);
  const over = budgetUsage({
    budgetMicros: "100",
    spentMicros: "90",
    reservedMicros: "30",
  });
  assert.equal(over.exhausted, true);
  assert.equal(over.reservedPercent, 30);
  assert.equal(over.spentWidth + over.reservedWidth, 100);
  const overspent = budgetUsage({
    budgetMicros: "100",
    spentMicros: "120",
    reservedMicros: "30",
  });
  assert.equal(overspent.spentWidth, 100);
  assert.equal(overspent.reservedWidth, 0);
});

test("polling preserves loaded records between the latest page and earlier history", () => {
  const initial = Array.from({ length: 100 }, (_, index) =>
    request({ id: String(101 - index).padStart(20, "0") }),
  );
  const previous = mergeRequestRecords(
    initial,
    [request({ id: "00000000000000000001" })],
    1000,
  );
  const next = [
    request({ id: "00000000000000000102" }),
    ...initial.slice(0, 99),
  ];
  const merged = mergeRequestRecords(next, previous, 1000);
  assert.equal(merged.length, 102);
  assert(merged.some((record) => record.id === "00000000000000000002"));
  assert.equal(mergeRequestRecords(next, previous, 100).length, 100);
});
