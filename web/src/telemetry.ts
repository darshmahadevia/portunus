import type { RequestRecord, Snapshot } from "./api.ts";

export const outcomes = [
  "success",
  "error",
  "rejected",
  "canceled",
] satisfies RequestRecord["status"][];

export function deriveTelemetry(records: readonly RequestRecord[]) {
  const counts: Record<RequestRecord["status"], number> = {
    success: 0,
    error: 0,
    rejected: 0,
    canceled: 0,
  };
  const measured = records
    .filter((record) => record.status !== "rejected")
    .sort((a, b) => Date.parse(a.startedAt) - Date.parse(b.startedAt));
  const maxDuration = Math.max(
    0,
    ...measured.map((record) => record.durationMs),
  );
  const durationCeiling = Math.max(1, maxDuration);
  const buckets = Array.from({ length: 8 }, (_, index) => ({
    startMs: (index * durationCeiling) / 8,
    endMs: ((index + 1) * durationCeiling) / 8,
    count: 0,
  }));
  const providers = new Map<
    string,
    { id: string; routes: number; attempts: number; failures: number }
  >();
  const provider = (id: string) => {
    const entry = providers.get(id) ?? {
      id,
      routes: 0,
      attempts: 0,
      failures: 0,
    };
    providers.set(id, entry);
    return entry;
  };
  for (const record of records) {
    counts[record.status]++;
    if (record.provider && !record.cacheHit) provider(record.provider).routes++;
    for (const attempt of record.attempts) {
      const entry = provider(attempt.provider);
      entry.attempts++;
      if (attempt.status !== "success") entry.failures++;
    }
    if (record.status !== "rejected") {
      const bucket =
        buckets[
          Math.min(7, Math.floor((record.durationMs / durationCeiling) * 8))
        ];
      if (bucket) bucket.count++;
    }
  }
  return {
    counts,
    measured,
    maxDuration,
    durationCeiling,
    buckets,
    providers: [...providers.values()].sort(
      (a, b) => b.attempts - a.attempts || a.id.localeCompare(b.id),
    ),
    cacheHits: records.filter((record) => record.cacheHit).length,
    multiAttemptRequests: records.filter((record) => record.attempts.length > 1)
      .length,
  };
}

export function budgetUsage(
  client: Pick<
    Snapshot["clients"][number],
    "budgetMicros" | "spentMicros" | "reservedMicros"
  >,
) {
  const budget = BigInt(client.budgetMicros);
  const spent = BigInt(client.spentMicros);
  const reserved = BigInt(client.reservedMicros);
  const percent = (value: bigint) =>
    budget === 0n ? 0 : Number((value * 10000n) / budget) / 100;
  const spentPercent = percent(spent);
  const reservedPercent = percent(reserved);
  return {
    spentPercent,
    reservedPercent,
    spentWidth: Math.min(100, spentPercent),
    reservedWidth: Math.min(Math.max(0, 100 - spentPercent), reservedPercent),
    exhausted: spent + reserved >= budget,
    zeroBudget: budget === 0n,
  };
}

export function mergeRequestRecords(
  recent: readonly RequestRecord[],
  loaded: readonly RequestRecord[],
  limit: number,
) {
  const records = new Map(loaded.map((record) => [record.id, record]));
  for (const record of recent) records.set(record.id, record);
  return [...records.values()]
    .sort((a, b) => b.id.localeCompare(a.id))
    .slice(0, limit);
}
