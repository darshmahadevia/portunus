# Diagnostic evidence

These observations come from a separate run excluded from the published timing report. The diagnostic workload used five rounds, 1,200 requests per phase, eight workers, and the same four-chunk fake response with a 2 ms inter-chunk delay. All 20 diagnostic phases completed without errors; [the phase log](profile-run.log) retains their work counts.

The diagnostic used the same gateway implementation and connection-pool settings as the published run. It preceded the benchmark-only correction that moves cache-prime cost validation before measured work. That correction does not alter gateway execution or the valid workload's measured path.

## Commands

The benchmark binary started its own compiled gateway. Process IDs were obtained from that parent-child relationship rather than sampling an unrelated service.

```sh
/usr/bin/sample <gateway-pid> 3 10 -fullPaths -file gateway.sample.txt
/usr/bin/sample <generator-pid> 3 10 -fullPaths -file generator.sample.txt
/usr/bin/top -l 4 -s 1 -pid <generator-pid> -pid <gateway-pid> -stats pid,command,cpu,time,threads,mem
```

The sampled processes were generator 43413 and gateway 43439. Their original reports are retained as [generator stacks](profile-generator.txt), [gateway stacks](profile-gateway.txt), and [CPU observations](profile-cpu.txt). Sampling ran while the early baseline phases were active. The reports include startup and runtime scheduler frames, so they must not be treated as a Go goroutine profile or a precise function-level allocation of wall time.

## Findings

After the initial CPU observation, the generator used 11.6%, 14.1%, and 23.1% CPU. The gateway process, which also hosts the fake providers, used 10.2%, 42.9%, and 54.5%. These Darwin percentages are relative to one logical CPU. Neither process saturated a core in that window. The shared host had substantial other activity and was not an isolated benchmark machine.

The native samples predominantly show condition waits, network polling, and runtime scheduling. They also include timer processing, provider parsing, and HTTP connection-pool activity. The fake's interruptible timer wait is in `internal/fakeprovider/fakeprovider.go`; the workload intentionally inserts three 2 ms gaps in each answer.

The observations are consistent with a fixed-concurrency workload whose completion rate is paced largely by provider delays and scheduling. The native sampler cannot attribute every parked Go goroutine to a specific wait. It therefore does not establish an isolated gateway bottleneck, maximum throughput, or how much faster a different synchronization strategy would be.

The published report is descriptive. Its paired total-latency difference crosses zero across rounds, so no stable isolated total-overhead claim is made. Cache phases establish verified provider work avoided, not sustained cache capacity. Outage phases establish completed requests during the configured HTTP outage, not a production availability guarantee.
