# Recorded local measurements

Generated from [the raw report](latest.json) by node scripts/summarize-benchmark.mjs.

Recorded 2026-10-23T17:52:47.624291Z with go1.26.5, darwin/arm64, 10 logical CPUs. Host load at start was 13:52  up 1 day,  1:47, 1 user, load averages: 3.08 3.47 3.52.

## Workload

5 rounds, 100 requests per phase, 8 concurrent workers. Both baseline paths stream the same deterministic four-chunk answer with a configured 2 ms delay between chunks. Warmup runs at measured concurrency before counters and timers start. Baseline order alternates each round. Every completion must include the expected text and terminal event; actual fake-provider work counters must match the workload.

Values below are the median of per-round percentiles. Parentheses show the minimum and maximum round value, in milliseconds. They are not confidence intervals.

| Phase | Verified completions | p50 total ms | p95 total ms | p50 first text ms |
| --- | --- | --- | --- | --- |
| direct | 500/500 | 6.986 (6.961 to 7.260) | 7.108 (7.097 to 8.018) | 0.159 (0.128 to 0.248) |
| gateway | 500/500 | 7.276 (7.052 to 7.351) | 7.656 (7.499 to 8.613) | 0.513 (0.433 to 0.601) |
| cache | 500/500 | 0.134 (0.122 to 0.155) | 0.476 (0.396 to 0.647) | 0.130 (0.119 to 0.150) |
| outage | 500/500 | 7.231 (7.205 to 7.244) | 7.560 (7.507 to 7.713) | 0.443 (0.414 to 0.607) |

All measured phases had zero errors, non-200 responses, incomplete answers, or transport failures. The outage workload opened the primary circuit in every round, with three initial primary failures followed by backup completion. Its wall time includes the circuit observation between the sequential failure prelude and concurrent load, so its throughput is not steady-state failover capacity.

## What the numbers support

The paired gateway-minus-direct p50 total difference was 0.290 (-0.208 to 0.315) ms. Its spread crosses zero, so this run does not establish a stable isolated total-latency overhead. The paired p50 first-text difference was 0.354 (0.205 to 0.408) ms. These are local observations, not Internet-provider latency guarantees.

Warm cache requests avoided 500 provider calls during measured hit phases. They performed zero upstream work and incurred zero new configured provider charge. The avoided charge estimate was $0.043500 at illustrative synthetic prices; this is not real billing savings. Cache priming is outside the measured hit workload.

## Limiter evidence and limits

A separate, unreported diagnostic run sampled both gateway and generator with Darwin sample and top. See [the diagnostic notes](diagnostics.md), [CPU observations](profile-cpu.txt), [gateway stacks](profile-gateway.txt), and [generator stacks](profile-generator.txt). Neither process saturated a core in the observed window. The stacks are dominated by parked runtime/network threads, with timer activity. This is consistent with the fixed eight-worker, timer-paced workload; it does not identify an isolated gateway CPU bottleneck or establish maximum capacity.

The host was shared and not idle. Profiling results are excluded from the timings above. Cache phases are short warm-hit bursts, not sustained capacity tests. The workload uses local HTTP fake providers, synthetic byte-unit token usage, and configured estimated prices. No live provider, external network, multi-host failure, or real bill was measured.
