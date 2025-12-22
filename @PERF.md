# Performance Notes (5 Iterations)

This file captures iterative thoughts (5 passes) on how to improve `ktl` performance. It focuses on common hot paths: CLI startup, config loading, Kubernetes client initialization, log streaming, UI streaming, and Helm/build workflows.

## Iteration 1 — Measure first, then shave

- Add **repeatable benchmarks** for end-to-end CLI startup latency:
  - `ktl --help` (no kubeconfig access)
  - `ktl env` (config + flag wiring)
  - `ktl logs --help` (subcommand help)
  - `ktl logs <query> --namespace <ns> --since 5m` (kube init + watch)
- Make profiling easy:
  - Keep `--cpuprofile/--memprofile` (if present) working on every command.
  - Add a `KTL_PROFILE=1` / `--profile-out` option for lightweight `pprof` dumps around known expensive blocks.
- Identify the largest sources of overhead:
  - Module init (`init()`), Cobra command construction, Viper/env scanning.
  - Kubeconfig parsing / client-go rest config creation.
  - JSON/YAML parsing in Helm/plan codepaths.
- Ensure measuring is cheap:
  - Prefer `testing.B` + `go test -bench` for microbenchmarks.
  - For E2E, write a `scripts/perf_smoke.sh` that runs `hyperfine` (optional) but works without it.

## Iteration 2 — Reduce startup work on cold path

- **Lazy-init expensive dependencies**:
  - Avoid creating kube clients, loading kubeconfig, or dialing gRPC unless the chosen command needs it.
  - For help/usage flows, skip any cluster or filesystem discovery beyond what Cobra requires.
- **Trim Cobra/Viper overhead**:
  - Bind only flags that need config/env integration; avoid binding entire flag sets if unused.
  - Avoid scanning environment variables repeatedly; compute once and pass down.
- **Avoid dynamic wrapping / formatting** in help and output:
  - Keep deterministic wrapping width for flag usages to avoid extra work and keep script stability.
- **Minimize allocations** in hot CLI glue:
  - Reuse buffers in help/formatting paths where possible.
  - Avoid repeated `strings.TrimSpace`/`Sprintf` in loops; precompute when constructing commands.

## Iteration 3 — Make log tailing scale with pods and throughput

- **Batch and bound work per tick**:
  - If there is a resync/refresh loop, cap how much work happens per iteration.
  - Avoid O(N pods × M filters) re-evaluations; precompile filters and apply incremental updates.
- **Streaming pipeline**:
  - Use a single fan-in queue with backpressure and bounded memory.
  - Consider a ring buffer for recent lines (for UI / WS consumers) instead of unbounded slices.
- **Reduce per-line overhead**:
  - Avoid regex matching if simple substring/prefix checks suffice; pre-detect filter types.
  - Normalize colorization and highlighting so it can be disabled without branches in the inner loop.
- **Concurrency choices**:
  - Prefer per-stream goroutines with minimal shared locks; aggregate with channels.
  - If ordering doesn’t matter across pods, don’t pay for global ordering.

## Iteration 4 — Cache and reuse Kubernetes client resources

- **Cache rest.Config/clientset** by `(kubeconfigPath, context)` within a single run:
  - Many subcommands may create similar clients; reuse when safe.
- **Avoid repeated discovery calls**:
  - Cache API discovery / preferred resources if used by deploy/apply/plan viewers.
  - If watching multiple namespaces, avoid repeated namespace list calls.
- **Tune client-go**:
  - Use sensible QPS/Burst defaults for high-throughput log watching (but keep conservative defaults).
  - Reuse HTTP transports; ensure idle connections remain available.
- **Avoid blocking on cluster calls** during UI rendering:
  - Render initial UI skeleton immediately; stream updates asynchronously.
  - Defer expensive details (events, diffs) behind “load more” or behind a threshold.

## Iteration 5 — Make performance improvements safer to ship

- **Add regression gates**:
  - A small CI check that runs a startup benchmark and fails on big regressions (with a generous threshold).
  - Optional: track `go test -run Test... -bench .` results in artifacts.
- **Expose lightweight tracing**:
  - Add `KTL_TRACE=1` to log durations of major phases (config load, kube init, watch start, first line).
  - Emit structured timing events so they can be graphed later.
- **Keep defaults fast**:
  - Default `--log-level info` should not do debug-only formatting.
  - Keep `--help` paths free of kubeconfig reads and network calls.
- **Document hot paths and invariants**:
  - Which operations are allowed during command construction vs during RunE.
  - Which packages must not do work in `init()`.

