import { readFileSync, writeFileSync, mkdirSync, renameSync } from 'node:fs';
import { dirname, resolve } from 'node:path';

const input = process.argv[2] ?? 'docs/benchmarks/latest.json';
const output = process.argv[3] ?? 'docs/benchmarks/README.md';
const report = JSON.parse(readFileSync(input, 'utf8'));
if (report.valid !== true || report.rounds < 5 || !Array.isArray(report.phases) || report.failures.length !== 0) {
  throw new Error('A valid report with at least five rounds is required.');
}
const median = (values) => {
  const sorted = [...values].sort((a, b) => a - b);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
};
const spread = (values) => `${median(values).toFixed(3)} (${Math.min(...values).toFixed(3)} to ${Math.max(...values).toFixed(3)})`;
const names = ['direct', 'gateway', 'cache', 'outage'];
const groups = Object.fromEntries(names.map(name => [name, report.phases.filter(p => p.name === name)]));
for (const name of names) {
  if (groups[name].length !== report.rounds || groups[name].some(p => p.errors !== 0 || p.work === null || p.failures.length)) {
    throw new Error(`Incomplete or failed ${name} measurements.`);
  }
}
const paired = groups.gateway.map(g => g.successfulTotalLatencyMs.p50 - groups.direct.find(d => d.round === g.round).successfulTotalLatencyMs.p50);
const pairedTTFT = groups.gateway.map(g => g.successfulTTFTMs.p50 - groups.direct.find(d => d.round === g.round).successfulTTFTMs.p50);
const avoided = groups.cache.reduce((n, p) => n + p.providerCallsSaved, 0);
const synthetic = groups.cache.reduce((n, p) => n + BigInt(p.syntheticEstimatedCostAvoidedMicros), 0n);
const dollars = `${synthetic / 1000000n}.${(synthetic % 1000000n).toString().padStart(6, '0')}`;
const rows = names.map(name => {
  const phases = groups[name];
  const complete = phases.reduce((n, p) => n + p.verifiedCompletions, 0);
  return `| ${name} | ${complete}/${report.requestsPerPhase * report.rounds} | ${spread(phases.map(p => p.successfulTotalLatencyMs.p50))} | ${spread(phases.map(p => p.successfulTotalLatencyMs.p95))} | ${spread(phases.map(p => p.successfulTTFTMs.p50))} |`;
});
const text = `# Recorded local measurements

Generated from [the raw report](latest.json) by node scripts/summarize-benchmark.mjs.

Recorded ${report.timestamp} with ${report.goVersion}, ${report.platform}, ${report.logicalCPUs} logical CPUs. Host load at start was ${report.hostLoad}.

## Workload

${report.rounds} rounds, ${report.requestsPerPhase} requests per phase, ${report.concurrency} concurrent workers. Both baseline paths stream the same deterministic four-chunk answer with a configured 2 ms delay between chunks. Warmup runs at measured concurrency before counters and timers start. Baseline order alternates each round. Every completion must include the expected text and terminal event; actual fake-provider work counters must match the workload.

Values below are the median of per-round percentiles. Parentheses show the minimum and maximum round value, in milliseconds. They are not confidence intervals.

| Phase | Verified completions | p50 total ms | p95 total ms | p50 first text ms |
| --- | --- | --- | --- | --- |
${rows.join('\n')}

All measured phases had zero errors, non-200 responses, incomplete answers, or transport failures. The outage workload opened the primary circuit in every round, with three initial primary failures followed by backup completion. Its wall time includes the circuit observation between the sequential failure prelude and concurrent load, so its throughput is not steady-state failover capacity.

## What the numbers support

The paired gateway-minus-direct p50 total difference was ${spread(paired)} ms. Its spread crosses zero, so this run does not establish a stable isolated total-latency overhead. The paired p50 first-text difference was ${spread(pairedTTFT)} ms. These are local observations, not Internet-provider latency guarantees.

Warm cache requests avoided ${avoided} provider calls during measured hit phases. They performed zero upstream work and incurred zero new configured provider charge. The avoided charge estimate was $${dollars} at illustrative synthetic prices; this is not real billing savings. Cache priming is outside the measured hit workload.

## Limiter evidence and limits

A separate, unreported diagnostic run sampled both gateway and generator with Darwin sample and top. See [the diagnostic notes](diagnostics.md), [CPU observations](profile-cpu.txt), [gateway stacks](profile-gateway.txt), and [generator stacks](profile-generator.txt). Neither process saturated a core in the observed window. The stacks are dominated by parked runtime/network threads, with timer activity. This is consistent with the fixed eight-worker, timer-paced workload; it does not identify an isolated gateway CPU bottleneck or establish maximum capacity.

The host was shared and not idle. Profiling results are excluded from the timings above. Cache phases are short warm-hit bursts, not sustained capacity tests. The workload uses local HTTP fake providers, synthetic byte-unit token usage, and configured estimated prices. No live provider, external network, multi-host failure, or real bill was measured.
`;
mkdirSync(dirname(resolve(output)), { recursive: true });
const temporary = `${output}.tmp`;
writeFileSync(temporary, text);
renameSync(temporary, output);
console.log(`Wrote ${output}`);
