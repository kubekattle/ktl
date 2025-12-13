# KTL Architecture & File Guide


## System Overview
KTL is a Go-based Kubernetes operations toolkit. The CLI entrypoint under `cmd/` wires Cobra commands, shell helpers, and sandbox/streaming utilities. Those commands defer to focused packages under `internal/` for collectors, reporters, BuildKit orchestration, archival formats, and streaming servers. The repo keeps tests beside the code they verify so documentation and enforcement stay in lockstep.

* `cmd/ktl` contains every end-user command plus CLI-only helpers (flag guards, help templates, sandbox re-exec glue, etc.).
* `internal/*` provides reusable libraries: Kubernetes collectors, deploy/report engines, capture machinery, BuildKit/deploy stream broadcasters, UI primitives, and serialization helpers.
* Generated artifacts and fixtures live outside these trees (`testdata/`, `bin/`, `dist/`) and are intentionally not documented here because they are build outputs, not source.

This document maps each Go file in `cmd/` and `internal/` to its responsibility so contributors can quickly find the right place to change or extend behavior.

## Runtime Flow Highlights


1. `main.go` normalizes CLI arguments, builds the root Cobra command, wires loggers (zap via logr), and routes subcommands (logs, diag, build, deploy, etc.).
2. Subcommands configure `internal` packages:
   * Observability commands (`ktl logs`, `ktl analyze-*`, captures) lean on `internal/tailer`, `internal/capture`, `internal/sniff`, and the various `*cast` servers for mirroring.
   * Diagnostics (`ktl diag ...`) call into collectors such as `internal/nodes`, `internal/networkstatus`, `internal/report`, and `internal/resources` to render textual or HTML summaries.
   * Deployment/build tooling (`ktl deploy`, `ktl build`, `ktl compose`, `ktl tag/push`) coordinate BuildKit (`pkg/buildkit`), Helm/deploy helpers (`internal/deploy`, `internal/deploydiff`), and registry/auth utilities.
3. UI mirrors (`--ui`/`--ws-listen`) use `internal/caststream`, `internal/sniffcast`, or `internal/syscallscast` to expose WebSocket feeds, while CLI-only spinners/tables live in `internal/ui`.
4. Supporting packages (`internal/config`, `internal/sqlitewriter`, `internal/apparchive`, etc.) provide configuration plumbing, persistence formats, and signing that higher-level commands reuse.

## CLI Layer (`cmd/`)

### `cmd/ktl` files

| File | Purpose / Usage |
| --- | --- |
| `cmd/ktl/analyze_syscalls.go` | analyze_syscalls.go wires up the 'ktl analyze syscalls' command so responders can spin up helper pods and collect syscall profiles from live workloads. |
| `cmd/ktl/app.go` | app.go defines the 'ktl app' parent command that groups packaging, unpacking, and vendoring actions for ktl archives. |
| `cmd/ktl/app_package.go` | app_package.go implements 'ktl app package', capturing namespaces plus metadata into reproducible .k8s application archives. |
| `cmd/ktl/app_package_verify.go` | app_package_verify.go backs 'ktl app package verify', checking signed .k8s archives and their attestations against user-provided keys. |
| `cmd/ktl/app_unpack.go` | app_unpack.go provides 'ktl app unpack' so teams can extract manifests, templates, and attachments from archived snapshots. |
| `cmd/ktl/app_vendor.go` | app_vendor.go contains the 'ktl app vendor' workflow that mirrors external charts/assets into deterministic vendor directories. |
| `cmd/ktl/app_vendor_test.go` | End-to-end test that shells through `newAppVendorCommand`, syncs the Grafana fixture with vendir, and asserts lock + chart contents. |
| `cmd/ktl/build.go` | build.go exposes the 'ktl build' pipeline, delegating to BuildKit and registry helpers to assemble and ship OCI artifacts from Compose/Build files. |
| `cmd/ktl/build_console_observer.go` | Implements the colorized `tailer.LogObserver` used by `ktl build` to render BuildKit/sandbox logs with timestamps, glyphs, and deterministic color palettes. |
| `cmd/ktl/build_fixtures_integration_test.go` | Linux-only integration that runs `ktl build` against every Dockerfile/Compose fixture in `testdata` to catch regressions in BuildKit wiring. |
| `cmd/ktl/build_flags_test.go` | Stubs BuildKit to confirm every build flag/secret/cache/mirror option is propagated into `buildkit.DockerfileBuildOptions` and that log redirection behaves. |
| `cmd/ktl/build_integration_helpers_test.go` | Shared linux-tagged helpers that validate the integration harness utilities leveraged by the sandbox/build fixture tests. |
| `cmd/ktl/build_login.go` | Defines the `ktl build login/logout` commands, including prompt/TTY helpers, registry ping logic, Docker config loading, and credential-store writes. |
| `cmd/ktl/build_login_test.go` | Unit tests covering credential prompts, password stdin handling, registry normalization, and credential-store erasure for build login/logout. |
| `cmd/ktl/build_sandbox_common.go` | Holds sandbox env-key constants plus bind-mount calculation helpers reused by both linux and stub implementations. |
| `cmd/ktl/build_sandbox_integration_test.go` | Integration test (linux) that re-execs ktl inside nsjail to ensure binds, cache dirs, and log streaming work end-to-end. |
| `cmd/ktl/build_sandbox_linux.go` | Linux implementation of `sandboxInjector`; re-execs the CLI inside nsjail, wires bind mounts, forwards signals, and mirrors sandbox logs. |
| `cmd/ktl/build_sandbox_logs_stub_test.go` | !linux compile-time test confirming sandbox log helpers no-op gracefully on unsupported platforms. |
| `cmd/ktl/build_sandbox_stub.go` | !linux stub returning nil injectors so the build command skips sandboxing where nsjail is unavailable. |
| `cmd/ktl/build_sandbox_test.go` | Pure-Go tests for sandbox bind selection, docker-socket detection, and helper utilities. |
| `cmd/ktl/build_test.go` | Verifies helper parsers (key/value args, cache specs, compose detection, build-mode selection) driving the build CLI. |
| `cmd/ktl/build_webcast_test.go` | Guarantees the BuildKit progress broadcaster emits vertex/log events to observers in order. |
| `cmd/ktl/capture.go` | capture.go adds 'ktl logs capture', wrapping the tailer so investigations can persist multi-pod log sessions plus metadata into portable archives. |
| `cmd/ktl/capture_diff.go` | capture_diff.go implements 'ktl logs capture diff', comparing archived snapshots (or live namespaces) to surface drift between captures. |
| `cmd/ktl/capture_replay.go` | capture_replay.go wires 'ktl logs capture replay', streaming stored log archives back through the formatter for offline triage. |
| `cmd/ktl/cast_helpers.go` | Starts `caststream` servers in goroutines, surfaces startup failures instantly, and logs later crashes while keeping the CLI running. |
| `cmd/ktl/completion.go` | completion.go registers the 'ktl completion' command that emits shell-specific autocompletion scripts. |
| `cmd/ktl/completion_helpers.go` | completion_helpers.go centralizes Cobra flag completion helpers (namespaces today) so every command can reuse them consistently. |
| `cmd/ktl/compose.go` | compose.go declares the 'ktl compose' command group, offering BuildKit-backed compose build/push helpers for multi-service projects. |
| `cmd/ktl/compose_shared.go` | Provides compose-file discovery and absolute-path helpers shared by the compose subcommands. |
| `cmd/ktl/compose_test.go` | Tests compose auto-discovery against the Grafana fixtures to ensure defaults stay correct. |
| `cmd/ktl/cronjobs.go` | cronjobs.go registers 'ktl diag cronjobs', pulling CronJob/Job summaries to highlight stuck schedules and failed job history. |
| `cmd/ktl/db_backup.go` | db_backup.go backs the 'ktl db' subtree, orchestrating pg_dump/restore invocations inside pods for managed PostgreSQL backups. |
| `cmd/ktl/deploy.go` | deploy.go defines the 'ktl deploy' parent command that fronts Helm plan/apply/destroy operations with ktl UX improvements. |
| `cmd/ktl/deploy_plan.go` | deploy_plan.go contains the 'ktl deploy plan/apply' logic, rendering manifests, producing HTML diffs, and teeing the plan into files. |
| `cmd/ktl/deploy_plan_graph_test.go` | Ensures the deploy-plan graph builder emits deterministic nodes/edges for visualization exporters. |
| `cmd/ktl/deploy_plan_html_test.go` | Golden test for the deploy-plan HTML template that powers `ktl deploy plan --visualize`. |
| `cmd/ktl/deploy_plan_test.go` | Covers manifest parsing, diff emission, and file-writing logic in the deploy plan command. |
| `cmd/ktl/deploydiff.go` | deploydiff.go provides 'ktl logs diff-deployments', tailing events for specified Deployments to contrast new vs old ReplicaSet behavior. |
| `cmd/ktl/diag.go` | diag.go glues together the 'ktl diag' umbrella command and registers every diagnostic subcommand (nodes, quotas, reports, etc.). |
| `cmd/ktl/docker_auth.go` | Loads/saves Docker `config.json` files (plus credential helpers) so build/tag/push/login share one auth implementation. |
| `cmd/ktl/drift.go` | drift.go adds the 'ktl drift watch' workflow, periodically snapshotting pods and flagging changes between generations. |
| `cmd/ktl/flag_guard.go` | Rejects ambiguous short flags (like `-nfoo`) and normalizes optional-value flags so Cobra parses `--ui :8080` safely. |
| `cmd/ktl/flag_guard_test.go` | Backstops the short-flag enforcement and optional-value normalization helpers. |
| `cmd/ktl/health.go` | health.go powers 'ktl diag health', a scorecard-driven automation command. |
| `cmd/ktl/health_test.go` | Tests fail-on mode parsing plus score badge/format helpers used by the health report command. |
| `cmd/ktl/help_shim.go` | Detects when namespace flags accidentally capture help tokens (`-n -h`) and reroutes to Cobra's help machinery. |
| `cmd/ktl/help_shim_test.go` | Covers namespace-help detection for both direct and inherited flagsets. |
| `cmd/ktl/help_template.go` | help_template.go customizes Cobra's help/usage templates so ktl commands share concise, branded flag sections. |
| `cmd/ktl/help_template_test.go` | Ensures the custom help template preserves flag metadata such as `NoOptDefVal` after formatting. |
| `cmd/ktl/logs.go` | logs.go defines the top-level 'ktl logs' command, connecting CLI flags to the tailer, capture, drift, and streaming subcommands. |
| `cmd/ktl/logs_test.go` | Guards the `requestedHelp` helper so single-dash invocations trigger help in the right situations. |
| `cmd/ktl/main.go` | main.go bootstraps ktl: it builds the root Cobra command, wires profiling, and executes with signal-aware contexts. |
| `cmd/ktl/manifestutil.go` | manifestutil.go hosts helper types for parsing/rendering Helm manifests when generating deploy plans and diffs. |
| `cmd/ktl/network.go` | network.go introduces 'ktl diag network', summarizing Ingresses, Gateways, and Services to verify endpoint readiness. |
| `cmd/ktl/nodes.go` | nodes.go registers 'ktl diag nodes', collecting allocatable/capacity stats plus pressures across cluster nodes. |
| `cmd/ktl/podsecurity.go` | podsecurity.go adds 'ktl diag podsecurity', explaining namespace PodSecurity admission levels and risky exemptions. |
| `cmd/ktl/priorities.go` | priorities.go exposes 'ktl diag priorities', correlating PriorityClasses with workloads so teams can inspect preemption policies. |
| `cmd/ktl/push.go` | push.go implements 'ktl push', copying container artifacts between registries with progress feedback and retries. |
| `cmd/ktl/push_test.go` | Validates the helper that extracts the repository component from a fully qualified image reference for `ktl push`. |
| `cmd/ktl/quotas.go` | quotas.go defines 'ktl diag quotas', printing ResourceQuota/LimitRange consumption per namespace with warning thresholds. |
| `cmd/ktl/report.go` | report.go powers 'ktl diag report', rendering ASCII or HTML health reports for namespaces and optional drift comparisons. |
| `cmd/ktl/report_live.go` | report_live.go serves the 'ktl diag report --live' HTTP endpoint, streaming refreshed HTML reports over SSE/Web. |
| `cmd/ktl/report_live_test.go` | Covers the SSE/live report server: caching, trim/full toggle, error handling, and stream termination. |
| `cmd/ktl/report_trend.go` | report_trend.go handles 'ktl diag report trend', charting historical score data for recurring runs stored in S3 or disk. |
| `cmd/ktl/report_trend_test.go` | Exercises the rolling-window label helper `windowLabel` used by the trend subcommand. |
| `cmd/ktl/resources.go` | resources.go introduces 'ktl diag resources', summarizing deployments/pods per namespace with readiness and image info. |
| `cmd/ktl/sandbox_default_linux.go` | Embeds the default nsjail config and writes it to `~/.cache/ktl` when Linux users do not supply `--sandbox-config`. |
| `cmd/ktl/sandbox_default_other.go` | Non-Linux stub returning an explicit error since nsjail sandboxing only works on Linux. |
| `cmd/ktl/sandbox_logs.go` | Tail-follows sandbox log files, echoes `[sandbox]` lines to stderr, and optionally feeds observers for the build UI. |
| `cmd/ktl/sandbox_policy_integration_test.go` | Linux integration that proves nsjail blocks unbound host paths yet allows explicitly bound directories. |
| `cmd/ktl/snapshot.go` | snapshot.go provides the 'ktl diag snapshot' family (save/replay/diff) for archiving and comparing namespace state. |
| `cmd/ktl/sniff.go` | sniff.go implements the packet-capture helpers ('ktl analyze traffic'), launching tcpdump sidecars and streaming pcap data. |
| `cmd/ktl/storage.go` | storage.go registers 'ktl diag storage', reviewing PVCs, PVs, and pending claims to surface capacity or access issues. |
| `cmd/ktl/tag.go` | tag.go adds the lightweight 'ktl tag' command, copying image references within/between registries via the distribution API. |
| `cmd/ktl/top.go` | top.go wires the 'ktl diag top' helpers that render top-like CPU/memory tables for pods and namespaces. |

### `cmd/tmp` files

| File | Purpose / Usage |
| --- | --- |
| `cmd/tmp/main.go` | Developer scratch binary that spins up a deploy webcast server and sends fake events for manual UI testing. |

## Internal Packages (`internal/`)

### `internal/apparchive`

| File | Purpose / Usage |
| --- | --- |
| `internal/apparchive/archive.go` | archive.go implements the writer used to create ktl .k8s application archives. |
| `internal/apparchive/archive_test.go` | archive_test.go verifies archive creation and integrity routines. |
| `internal/apparchive/reader.go` | reader.go opens and iterates ktl .k8s archives for downstream commands. |

### `internal/buildinfo`

| File | Purpose / Usage |
| --- | --- |
| `internal/buildinfo/buildinfo.go` | buildinfo.go captures build metadata (version, commit, date) for use in --version outputs. |

### `internal/capture`

| File | Purpose / Usage |
| --- | --- |
| `internal/capture/artifact.go` | artifact.go defines the on-disk capture artifact format produced by 'ktl logs capture'. |
| `internal/capture/context.go` | context.go provides context helpers used to control capture lifecycles. |
| `internal/capture/graph.go` | graph.go maintains the informer graph (pods, nodes, events) referenced during capture sessions. |
| `internal/capture/options.go` | options.go collects and normalizes CLI flags used by capture sessions. |
| `internal/capture/replay.go` | replay.go rehydrates captured logs/events back to stdout or JSON for offline analysis. |
| `internal/capture/session.go` | session.go coordinates live capture sessions by wiring Kubernetes informers to log writers. |
| `internal/capture/types.go` | types.go declares the structs serialized inside capture archives (logs, metadata, and node state). |

### `internal/caststream`

| File | Purpose / Usage |
| --- | --- |
| `internal/caststream/deploy_state.go` | Caches the latest deploy webcast events (summary/phase/diff/log) so late WebSocket subscribers immediately receive the current state. |
| `internal/caststream/deploy_state_test.go` | Verifies deploy state replay ordering and log trimming behavior so cached payloads stay bounded. |
| `internal/caststream/server.go` | Hosts the HTTP/WebSocket server for log and deploy webcasts, including filterable HTML, hub fan-out, and deploy observer plumbing. |
| `internal/caststream/server_test.go` | Exercises the caststream hub, payload encoding defaults, and slow-client eviction logic. |

### `internal/config`

| File | Purpose / Usage |
| --- | --- |
| `internal/config/config.go` | Defines the shared `config.Options` struct plus flag binding/validation logic reused by `ktl logs` and related commands. |
| `internal/config/config_test.go` | config_test.go verifies Options parsing, validation, and template helpers for ktl logs flags. |

### `internal/deploy`

| File | Purpose / Usage |
| --- | --- |
| `internal/deploy/apply.go` | apply.go wraps Helm install/upgrade hooks so 'ktl deploy' can apply releases. |
| `internal/deploy/manifest_targets.go` | Parses rendered Helm manifests into tracked resource targets (group/version/kind/name/labels) for status tracking. |
| `internal/deploy/progress.go` | Defines deploy phase constants and the `ProgressObserver` interface used to emit timeline updates. |
| `internal/deploy/status.go` | Translates concrete Kubernetes workload objects into human-readable `ResourceStatus` rows (deployments, jobs, HPAs, etc.). |
| `internal/deploy/status_tracker.go` | Polls release resources (by manifest or label selectors), computes `ResourceStatus` slices, and feeds them to observers during deploys. |
| `internal/deploy/stream.go` | Declares the structured deploy webcast payloads plus the broadcaster that fans them out to observers/UI servers. |
| `internal/deploy/template.go` | template.go renders Helm manifests and change summaries for deploy plan/install operations. |

### `internal/deploydiff`

| File | Purpose / Usage |
| --- | --- |
| `internal/deploydiff/diff.go` | diff.go compares deployment events across ReplicaSets for 'ktl logs diff-deployments'. |

### `internal/drift`

| File | Purpose / Usage |
| --- | --- |
| `internal/drift/collector.go` | collector.go captures pod state over time for 'ktl logs drift watch'. |
| `internal/drift/collector_test.go` | collector_test.go covers the drift sampler logic and its edge cases. |
| `internal/drift/diff.go` | diff.go computes human-readable drift summaries between pod snapshots. |
| `internal/drift/diff_test.go` | diff_test.go ensures drift diffing covers the expected pod change scenarios. |
| `internal/drift/types.go` | types.go defines the structs used by the drift tracker when sampling pod deltas. |

### `internal/gitinfo`

| File | Purpose / Usage |
| --- | --- |
| `internal/gitinfo/gitinfo.go` | gitinfo.go reads Git metadata to stamp artifacts and version output. |

### `internal/images`

| File | Purpose / Usage |
| --- | --- |
| `internal/images/extract.go` | extract.go unpacks previously saved image tarballs for reuse in ktl workflows. |
| `internal/images/save.go` | save.go snapshots container images into tarballs for packaging/offline workflows. |

### `internal/jobs`

| File | Purpose / Usage |
| --- | --- |
| `internal/jobs/collect.go` | collect.go fetches CronJobs/Jobs and builds the health summary powering 'ktl diag cronjobs'. |
| `internal/jobs/render.go` | render.go formats CronJob + Job data into tables. |

### `internal/kube`

| File | Purpose / Usage |
| --- | --- |
| `internal/kube/client.go` | client.go constructs Kubernetes clientsets/dynamic clients used across ktl. |
| `internal/kube/exec.go` | exec.go shells into pods/containers to run commands for db backup and other helpers. |

### `internal/networkstatus`

| File | Purpose / Usage |
| --- | --- |
| `internal/networkstatus/collect.go` | collect.go queries Kubernetes objects and builds the network readiness summary. |
| `internal/networkstatus/render.go` | render.go formats ingress/gateway/service readiness for 'ktl diag network'. |

### `internal/nodes`

| File | Purpose / Usage |
| --- | --- |
| `internal/nodes/collect.go` | collect.go queries Kubernetes for node stats, taints, and pressures that feed the nodes report. |
| `internal/nodes/render.go` | render.go prints node capacity/allocatable summaries for 'ktl diag nodes'. |

### `internal/pgdump`

| File | Purpose / Usage |
| --- | --- |
| `internal/pgdump/dumper.go` | dumper.go orchestrates pg_dump executions inside pods for 'ktl db backup'. |
| `internal/pgdump/dumper_test.go` | dumper_test.go checks pg_dump command construction and flag handling. |
| `internal/pgdump/restore.go` | restore.go runs pg_restore/psql flows to support 'ktl db restore'. |

### `internal/podsecurity`

| File | Purpose / Usage |
| --- | --- |
| `internal/podsecurity/collect.go` | collect.go queries namespaces and builds the PodSecurity summary consumed by the renderer. |
| `internal/podsecurity/render.go` | render.go formats PodSecurity assessment data into the table output used by 'ktl diag podsecurity'. |

### `internal/priorities`

| File | Purpose / Usage |
| --- | --- |
| `internal/priorities/collect.go` | collect.go inspects PriorityClasses and their consumers for the priorities diagnostic. |
| `internal/priorities/render.go` | render.go prints the priority/preemption tables exposed by 'ktl diag priorities'. |

### `internal/pvcstatus`

| File | Purpose / Usage |
| --- | --- |
| `internal/pvcstatus/collect.go` | collect.go correlates PVCs, pods, and node pressures to feed the storage diagnostics. |
| `internal/pvcstatus/render.go` | render.go prints PVC health rows, including node pressure annotations, for 'ktl diag storage'. |

### `internal/quota`

| File | Purpose / Usage |
| --- | --- |
| `internal/quota/quota.go` | quota.go analyzes ResourceQuota/LimitRange data sets for 'ktl diag quotas'. |
| `internal/quota/render.go` | render.go formats quota results into human-readable tables. |

### `internal/report`

| File | Purpose / Usage |
| --- | --- |
| `internal/report/diff.go` | diff.go renders the HTML diff/plan view for deploy/report output. |
| `internal/report/diff_test.go` | diff_test.go ensures diff rendering highlights changes as intended. |
| `internal/report/report.go` | report.go assembles namespace snapshots into ASCII/HTML posture reports. |
| `internal/report/scorecard.go` | scorecard.go models the metrics/threshold scoring used by 'ktl diag report'. |
| `internal/report/scorecard_test.go` | scorecard_test.go covers score calculations and breach detection logic. |

### `internal/resources`

| File | Purpose / Usage |
| --- | --- |
| `internal/resources/collect.go` | collect.go walks workloads per namespace and builds the resource summary consumed by the renderer. |
| `internal/resources/render.go` | render.go outputs namespace workload tables (deployments, pods, images) for 'ktl diag resources'. |

### `internal/resourceutil`

| File | Purpose / Usage |
| --- | --- |
| `internal/resourceutil/resourceutil.go` | resourceutil.go provides helpers for calculating request/limit utilization ratios across workloads. |

### `internal/signing`

| File | Purpose / Usage |
| --- | --- |
| `internal/signing/signing.go` | signing.go wraps signing and verification helpers for ktl .k8s artifacts and attachments. |
| `internal/signing/signing_test.go` | signing_test.go exercises the signing helpers to guarantee envelopes verify as expected. |

### `internal/snapshot`

| File | Purpose / Usage |
| --- | --- |
| `internal/snapshot/snapshot.go` | snapshot.go powers 'ktl diag snapshot' save/replay/diff flows by orchestrating collectors and writers. |

### `internal/sniff`

| File | Purpose / Usage |
| --- | --- |
| `internal/sniff/injector.go` | injector.go injects temporary capture pods/sidecars used by the sniff package. |
| `internal/sniff/runner.go` | runner.go powers 'ktl analyze traffic' by orchestrating tcpdump helpers and streaming pcap data. |

### `internal/sniffcast`

| File | Purpose / Usage |
| --- | --- |
| `internal/sniffcast/server.go` | Implements the traffic capture mirror (HTML + WebSocket) used by `ktl analyze traffic` to share tcpdump lines. |

### `internal/sqlitewriter`

| File | Purpose / Usage |
| --- | --- |
| `internal/sqlitewriter/context.go` | context.go contains helpers for managing writer lifecycles and context cancellation. |
| `internal/sqlitewriter/writer.go` | writer.go streams log lines into on-disk SQLite databases for captures and replay. |
| `internal/sqlitewriter/writer_test.go` | writer_test.go validates the SQLite writer's schema and durability behavior. |

### `internal/syscalls`

| File | Purpose / Usage |
| --- | --- |
| `internal/syscalls/profile.go` | profile.go collects and aggregates syscall samples for 'ktl analyze syscalls'. |

### `internal/syscallscast`

| File | Purpose / Usage |
| --- | --- |
| `internal/syscallscast/server.go` | Streams syscall profile summaries (HTML + WebSocket) to observers of `ktl analyze syscalls`. |

### `internal/tailer`

| File | Purpose / Usage |
| --- | --- |
| `internal/tailer/node_logs.go` | Adds kubelet/node log streaming to the tailer so `ktl logs` can follow node-level files via the proxy endpoints. |
| `internal/tailer/tailer.go` | Core multi-pod log tailer: builds label selectors, colorizes output, handles highlights, and notifies observers (UI, capture, etc.). |
| `internal/tailer/tailer_test.go` | tailer_test.go covers palette/color helpers and other tailer utilities. |

### `internal/top`

| File | Purpose / Usage |
| --- | --- |
| `internal/top/pods.go` | pods.go renders pod-level CPU and memory usage tables for the 'ktl diag top' command. |

### `internal/ui`

| File | Purpose / Usage |
| --- | --- |
| `internal/ui/spinner.go` | spinner.go implements the CLI spinner displayed while ktl performs background work (captures, remotes, etc.). |
| `internal/ui/status_table.go` | ANSI-aware renderer that live-updates the deploy resource table in-place while builds/deploys run. |
