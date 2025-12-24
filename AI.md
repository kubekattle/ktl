# AI Operator Guide for `ktl`

Reference for autonomous agents running the latest `ktl` toolchain. Follow these patterns to operate safely across modern kubernetes clusters, whether you are tailing logs, rendering HTML reports, or assembling offline app bundles.

---

## 1. Core Principles

1. **Confirm target context.** Resolve kubeconfig and context explicitly (`ktl --kubeconfig <path> --context <name>`). Use `ktl version --client --context ...` to verify connectivity before any destructive flag is considered.
2. **Default to read-only.** `ktl logs`, `ktl diag *`, `ktl package *`, and `ktl app package` are safe; anything that injects helpers (`ktl analyze traffic`, `ktl analyze syscalls`, `ktl db restore`, future deploy flows) requires explicit operator approval.
3. **Make configuration explicit.** Pin namespaces (`-n`, `-A`), releases, and output directories. Prefer environment overrides (`KTL_ALL_NAMESPACES`, `KTL_NAMESPACE`, etc.) only when the human requested enduring defaults.
4. **Report constraints immediately.** Surface RBAC errors, missing metrics APIs, or unsupported features (ephemeral containers, CSI snapshots) verbatim so humans can remediate.
5. **Keep runs reproducible.** Reuse the same query strings and flags when comparing runs, and timestamp any saved outputs under `dist/` for traceability.

---

## 2. Essential Workflows

### 2.1 Log Streaming & Events
- Base command: `ktl logs <pod-regex>` (or legacy `ktl <query>`). Always add `--namespace <ns>` unless instructed to scan cluster-wide.
- Enhance readability with `--highlight`, `--exclude`, `--container`, and `--diff-container` for rollout comparisons.
- Add `--events` for correlated Kubernetes events, or `--events-only` when specifically asked.
- Disable color in pipelines: `--color never`. For downstream tooling, `--output json` or `--template <file>` keeps parsing deterministic.

### 2.3 Diagnostics & Reporting
- Run scoped diagnostics (`ktl diag nodes`, `ktl diag quotas`, `ktl diag storage`, `ktl diag priorities`, etc.) with `--namespace`/`--all-namespaces` according to the request.
- Generate posture summaries with `ktl diag report --html --output dist/report.html` (ensuring the directory exists). Mention if metrics APIs were unavailable so readers understand missing charts.

### 2.4 Packaging & Offline Delivery
- Full app archive: `ktl app package --chart <chart> --release <name> --namespace <ns> --values values.yaml --archive-file dist/<name>.k8s` and, when asked, unpack via `ktl app unpack --archive-file <file> --output-dir dist/<name>-unpacked`.
- Verify each output path exists before reporting success.

### 2.5 Network & Database Operations
- Traffic analysis: `ktl analyze traffic --target ns/pod[:container] [--between podA,podB]`. Requires `pods/ephemeralcontainers` permissions and often privileged nodes; confirm access before injecting helpers.
- Syscall profiling: `ktl analyze syscalls --target ns/pod[:container] [--match open,connect] --profile-duration 30s`. Same RBAC/privilege requirements as traffic. Mention `--format json` when humans want machine-readable outputs, and remind them it attaches strace to PID 1 by default.
- Database workflows: `ktl db backup ns/pod:/var/lib/postgres --output dist/db.dump` and `ktl db restore --drop-db` only when explicitly requested. Provide size estimates beforehand.
- Packaging plus diagnostics often cover operator needs; fall back to raw `kubectl` only if ktl lacks a feature.

### 2.6 Profiling & Performance
- Profile startup locally with `KTL_PROFILE=startup ktl logs checkout -n prod`. ktl drops CPU/heap `.pprof` files in the working directory so you can open them with `go tool pprof` and optimize hot paths.

### 2.7 Deploy Planning
- Use `ktl apply plan --chart <path> --release <name> --namespace <ns> --kubeconfig ~/.kube/archimedes.yaml [-f values.yaml]` to render Helm manifests and diff them against the live cluster before mutating anything.
- The plan is read-only but still talks to the cluster; report RBAC or discovery gaps (e.g., CRDs missing) as warnings in your summary.
- Highlight the creates/updates/deletes plus any pod-affecting changes (Deployments/StatefulSets/Jobs) and PodDisruptionBudget removals so humans understand disruption risk before running `ktl apply`.
- Need an executive-friendly artifact? add `--html --output dist/<name>-plan.html` to emit the same frosted-glass UI as `ktl diag report`, complete with copy-to-clipboard deploy commands for reviewers.

---

## 3. Safety Checklist Before Execution

1. **Authentication:** Validate kubeconfig path and context, especially for CI agents. Never hardcode credentials inside scripts.
2. **RBAC & Feature Gates:** Ask whether the cluster supports ephemeral containers, metrics APIs, and PVC inspect APIs before running commands that rely on them.
3. **Filesystem Prep:** Create/verify directories referenced by `--output` or `--archive-file`. Fail fast if the path is unwritable.
4. **Cluster Impact:** Communicate potential load (namespace-wide scans, multi-target traffic analysis, `ktl app package` Helm renders) and wait for approval when the action could spike API server usage.

---

## 4. Troubleshooting Patterns

| Symptom | Likely Cause | Agent Response |
| --- | --- | --- |
| `Error: resolve default namespace` | kubeconfig lacks namespace | Add `--namespace` or patch context before retrying. |
| `pods/ephemeralcontainers is forbidden` | RBAC missing for traffic analysis | Report forbidden verb/namespace and suggest alternatives (pcap from node, `kubectl sniff`). |
| `strace: ptrace(SETOPTIONS)` / `Operation not permitted` | Node security policy blocks ptrace/bpf; helper not privileged | Confirm `--privileged` stayed true, suggest running on nodes that allow `CAP_SYS_PTRACE`, or fall back to `kubectl debug`. |
| `metrics API unavailable` | Metrics server disabled | Explain charts/usage fields will be blank; proceed with manifest-only data. |
| `sniffer image pull failed` | Registry access denied | Request mirror image (`--image registry/internal/tcpdump:tag`). |
| `helm template` errors during packaging/app builds | Chart or values invalid | Surface Helm stderr verbatim; do not guess overrides. |
| output write failures | output directory unwritable or disk full | Free space / change `--output`/`--archive-file` and re-run after confirmation. |

---

## 5. Logging & Reporting Expectations

- After each command, summarize actionable outcomes: “Exported 42 manifests to dist/ns-manifests; HTML report at dist/report.html”.
- Preserve ktl stdout/stderr when humans request full provenance; redact secrets in pasted excerpts.
- On failure, echo the exact ktl invocation (minus secrets) and the exit error to enable quick reproduction. Include cluster/context info if safe to share.

---

## 6. When Not to Use `ktl`

- Desired data already exists in source control or another artifact (no need to hit the cluster).
- Cluster policies forbid privileged helpers and a non-ktl alternative (kubectl logs, vendor observability) is available.
- Requested action involves mutating live workloads beyond ktl’s scope (e.g., apply manifests, scale deployments); escalate to the deployment pipeline instead.

Adhering to this updated guide keeps ktl operations predictable for the newest feature set while protecting clusters from unintended side effects. When in doubt, stop and ask for human confirmation.
