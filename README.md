# ktl

`ktl` is a kubectl-friendly observability Swiss Army knife. It starts as a log tailer, then layers on diagnostics for quotas, PVC health, rollouts, service readiness, packaging, database workflows, and more—without leaving your terminal.

## Why ktl?
- **Instant pod logs** – informer-backed discovery keeps startup under a second, even in large clusters.
- **Human-friendly output** – colors for namespaces/containers, Go-template rendering, JSON passthrough, and regex highlighting.
- **Multi-source visibility** – attach node/system logs alongside pods with `--node-logs`, `--node-log <path>`, `--node-log-all`, or `--node-log-only`.
- **Zero re-rollout downtime** – automatic reattachment when pods restart plus event streaming via `--events`.
- **Shareable live mirrors** – broadcast `ktl logs` or `ktl build` sessions via `--ui` or `--ws-listen` so teammates can follow along without re-running ktl.
- **Operational insight** – built-in commands for quotas, node capacity, PVC vs node pressure, CronJobs, rollout diffs, and more.
- **Air-gap ready** – package an entire chart’s image set into a single tarball for offline clusters with `ktl package images`.
- **Database aware** – capture or restore PostgreSQL dumps directly from pods via `ktl db backup` / `ktl db restore --drop-db`.
- **Inline net forensics** – run `ktl analyze traffic` to inject temporary tcpdump helpers into any pod (or multiple pods) without leaving your terminal, complete with host-to-host filtering.
- **Syscall profiler** – `ktl analyze syscalls` attaches strace/bcc helpers to noisy pods, summarizes the busiest syscalls, and emits either tables or JSON for downstream automation.

### UI mirror defaults
Any command that supports `--ui` lets you omit the host/port. Unless noted, the mirror binds to `:8080` when you pass just `--ui`. Override the port (or host) by supplying an explicit value such as `--ui 0.0.0.0:9000`.

| Command | Default `--ui` bind |
| --- | --- |
| `ktl logs`, `ktl report live`, and other log mirrors | `:8080` |
| `ktl analyze traffic` | `:8081` |
| `ktl analyze syscalls` | `:8081` |
| `ktl build` | `:8085` |
| `ktl deploy apply` | `:8080` |
| `ktl deploy destroy` | `:8080` |

## Install
```bash
# From source (Go 1.25+)
go install ./cmd/ktl

# Via Makefile helper
make build     # writes ./bin/ktl
make install   # equivalent to go install ./cmd/ktl
make build-darwin-arm64  # cross-builds ./bin/ktl-darwin-arm64 (Apple Silicon)
make build-windows-386  # cross-builds ./bin/ktl-windows-386.exe (Windows x86)
make release   # cross-build archives into ./dist

# Build macOS binaries without the Makefile
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o bin/ktl-darwin-arm64 ./cmd/ktl  # Apple Silicon
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o bin/ktl-darwin-amd64 ./cmd/ktl  # Intel
GOOS=windows GOARCH=386  CGO_ENABLED=0 go build -o bin/ktl-windows-386.exe ./cmd/ktl  # Windows x86
```

`ktl build` also exposes BuildKit's gateway debugging tools: add `-i` / `--interactive` to pause on the first failing `RUN` instruction and open a shell inside the container with the same mounts, environment, and working directory. Use `--interactive-shell "bash -l"` (default: `/bin/sh`) to pick a different entrypoint. Interactive mode requires a TTY (local terminals or `ssh -tt`) and is meant for local debugging—CI pipelines should keep it disabled.

Working on a Compose workspace? `ktl build` now auto-detects `docker-compose.yml`/`compose.yaml` when no Dockerfile exists in the context and runs a multi-service build automatically. Force a specific mode with `--mode compose` or `--mode dockerfile`, list compose inputs via `--compose-file`, and narrow the build to particular services with `--compose-service checkout,worker`.

Need registry credentials for pushes? Use `ktl build login ghcr.io -u <user> --password-stdin` to store a PAT in the Docker credential store (mirrors `docker login`) and `ktl build logout ghcr.io` to remove it. Point all build subcommands at a custom auth file with `--authfile /path/to/config.json` if you keep credentials outside `~/.docker`.

### Sandboxed builds

On Linux, `ktl build` automatically re-execs itself inside the configured sandbox runtime as soon as the runtime binary referenced by `--sandbox-bin` is on `$PATH`. The embedded default policy (see `cmd/ktl/sandbox/ktl-default.cfg`) caps CPU/memory, wires `/workspace` plus BuildKit caches, keeps Docker’s `unix:///var/run/docker.sock` reachable for Buildx fallbacks, and preserves TLS/registry environment variables. Bring your own policy with `--sandbox-config`, extend mounts via `--sandbox-bind host:guest`, change the working directory with `--sandbox-workdir`, or point to a different binary using `--sandbox-bin`. Set `KTL_SANDBOX_DISABLE=1` if you need to opt out temporarily. The CLI prints `Running ktl build inside the default sandbox` once the re-exec succeeds so automated tests can assert that the sandbox is active.

Need to see exactly what the sandbox runtime is doing? Add `--sandbox-logs` to mirror the sandbox’s stdout/stderr into your terminal (prefixed with `[sandbox]`) and into any `--ui/--ws-listen` sessions so remote viewers can watch the same diagnostics.

Need real-world fixtures? `testdata/build/dockerfiles/*` and `testdata/build/compose/docker-compose*.yml` carry five Dockerfile contexts and five Compose stacks (with matching `services/*` Dockerfiles) used by our integration suite. Run `go test -tags=integration ./cmd/ktl -run TestBuildDockerfileFixtures` (and the Compose variant) on a host with Docker plus the sandbox runtime to make sure regressions don’t sneak in.

Need a quick repro? `testdata/build/interactive/Dockerfile` intentionally exits with code 42; run `ktl build testdata/build/interactive -i --interactive-shell "bash -l"` to verify the workflow.

## Feature flags

Some ktl changes ship behind feature flags so you can opt in before a behavior becomes default.

- Register new flags (and their descriptions) in `internal/featureflags/featureflags.go`, keep names kebab-cased, and add docs to `AGENTS.md`.
- Enable a flag per-invocation with `ktl --feature <name>` (repeatable), add it to your config file (`feature: ["<name>"]`), or export `KTL_FEATURE_<FLAG>=1` (uppercase with `_` instead of `-`), e.g. `KTL_FEATURE_DEPLOY_PLAN_HTML_V3=1`.
- Resolved flags propagate through `context.Context`, so packages can read them via `featureflags.FromContext(ctx)`.

| Flag | Stage | Description |
| --- | --- | --- |
| `deploy-plan-html-v3` | experimental | Switch `ktl deploy plan --visualize` HTML output to the new v3 components. |

### Mirror build progress

Add `--ui :8085` (and optionally `--ws-listen :9085`) to any `ktl build` invocation to mirror BuildKit progress, cache hits, and RUN output inside the same frosted-glass UI that `ktl logs` uses. Teammates can follow retries or pushes in real time without screen sharing:

```bash
ktl build . \
  --tag ghcr.io/example/checkout:latest \
  --ui :8085 \
  --ws-listen :9085
```

The HTML mirror stays read-only: the terminal that launched `ktl build` keeps control of flags, filters, and prompts, while observers simply watch the shared stream.

When the build runs in Compose mode, the UI now renders a “compose heatmap” panel above the log feed. Each service gets a score card showing cache hits vs misses, how many layers were executed, the slowest BuildKit vertices (hotspots), and any failing step. The cards reuse the DESIGN.md score-card patterns, highlight failures/warnings automatically, and refresh the moment each service finishes so incident rooms can spot the outlier service without scrolling the log.

## Tail logs in seconds
Run `ktl logs <query>` (or the backward-compatible `ktl <query>`) to access the tailer.
```bash
# Tail all checkout pods in prod and highlight failures
ktl logs 'checkout-.*' \
  --namespace prod-payments \
  --highlight ERROR --highlight timeout

# Follow only proxy sidecars and stream events alongside logs
ktl logs '.*' \
  --namespace canary \
  --selector app=checkout \
  --container 'proxy.*' \
  --events

# Render structured output for downstream tools
ktl logs 'ingress.*' --namespace edge --output json | jq .
```

Useful toggles:
- `-n/--namespace`, `-A/--all-namespaces`
- `-c/--container`, `--exclude-container`, `--exclude-pod`
- `--tail`, `--since`, `--follow=false`
- `--field-selector spec.nodeName=kind-control-plane`
- `--template`, `--template-file`, `--json`, `--output {default,raw,json,extjson,ppextjson}`
- `--plain` to shortcut `--only-log-lines --no-prefix`
- `--no-follow`, `--only-log-lines`, `--timezone Asia/Tokyo`
- `--pod-colors`, `--container-colors`
- `--condition ready=false` to restrict pods by condition state
- `--highlight/-H`, `--color {auto|always|never}`, `--diff-container`
- `--events`, `--events-only`
- `--stdin` to read plain log streams from files/pipes
- `--log-level {debug,info,warn,error}` to tune controller-runtime logging (use `debug` when diagnosing freezes)
- `--node-logs`, `--node-log /var/log/kubelet.log`, `--node-log-all`, `--node-log-only` to merge kubelet/syslog output with pods (or show only node entries)
- `--ui :8080`, `--ws-listen :9080` to mirror the same session over HTML or a raw WebSocket feed for other responders
Environment overrides follow `KTL_<FLAG>` (e.g. `KTL_ALL_NAMESPACES=true`).

### Aggregate pods and node/system logs
Bring kubelet/syslog context into the same view by turning on node log streaming:

```bash
# Stream checkout pods plus kubelet + syslog entries from the nodes hosting them
ktl logs 'checkout-.*' \
  --namespace prod-payments \
  --node-logs \
  --node-log /var/log/kubelet.log \
  --node-log /var/log/syslog

# Focus exclusively on node/system logs across the entire cluster
ktl logs . --node-log-all --node-log-only --node-log /var/log/kubelet.log
```

`--node-logs` automatically tails `kubelet.log`. Add more files with `--node-log <path>` (paths are relative to `/var/log`). `--node-log-all` enables node streaming even when no matching pods are found, and `--node-log-only` suppresses pod lines so you can zero-in on kubelet/systemd chatter.

### Mirror sessions to teammates
Keep everyone on the same page without copy/pasting terminals. `ktl logs` **and** `ktl build` can broadcast live output in two ways:

- `--ui :8080` – serves a polished HTML viewer that mirrors your stdout stream (including highlights and glyphs) plus cluster context from your kubeconfig.
- `--ws-listen :9080` – emits the same payloads over a raw WebSocket so lightweight dashboards can subscribe.

```bash
# Tail prod pods while sharing the same session with browsers and dashboards
ktl logs 'checkout-.*' \
  --namespace prod-payments \
  --highlight ERROR \
  --ui :8080 \
  --ws-listen :9080
```

The viewers are read-only mirrors of your terminal, so you control the filters/highlights while teammates (or CI bots) simply observe.

```bash
# Stream BuildKit progress for teammates while images are compiling
ktl build . \
  --tag ghcr.io/example/payments:$(git rev-parse --short HEAD) \
  --ui :8085 \
  --ws-listen :9085
```

The build mirror shows step transitions, cache hits, RUN output, and push progress with the same frosted-glass UI as the log viewer.

## Operations toolkit
Use `ktl diag <command>` for read-only diagnostics; other subcommands remain top-level.
| Command | Description |
| --- | --- |
| `ktl diag quotas` | ResourceQuotas & LimitRanges per namespace (pods/CPU/memory/PVCs). |
| `ktl diag nodes` | Allocatable vs capacity vs actual usage; taints and pressure hints. |
| `ktl diag storage` | PVC phase cross-referenced with node Memory/Disk/PID pressure. |
| `ktl diag resources` | Per-container requests/limits plus live metrics usage (top offenders). |
| `ktl diag cronjobs` | CronJob + Job summary (active/succeeded/failed, last schedule). |
| `ktl diag network` | Ingress/Gateway/Service readiness, LB IPs, TLS secrets, EndpointSlices. |
| `ktl diag podsecurity` | Namespace PodSecurity labels with detected violations. |
| `ktl diag priorities` | PriorityClasses plus pod priority/preemption context. |
| `ktl logs diff-deployments` | Live event diff (new vs old ReplicaSets) during rollouts. |
| `ktl logs capture` | Record logs, events, and workload state into a replayable tarball. |
| `ktl logs capture replay` | Rehydrate capture artifacts offline (JSON or SQLite-backed). |
| `ktl logs capture diff` | Compare metadata (window, namespaces, pod counts, flags) across two captures. |
| `ktl analyze traffic --target pod[:container]` | Inject ephemeral tcpdump helpers into pods and stream their packets, optionally filtering traffic between targets. |
| `ktl diag top --all-namespaces` | Display CPU/memory usage for pods (kubectl top pods equivalent). |
| `ktl deploy plan --chart ./chart --release foo` | Render charts and summarize creates/updates/deletes before running deploy apply. |
| `ktl deploy plan --visualize --chart ./chart --release foo --kubeconfig ~/.kube/archimedes.yaml` | Emit the tree-based dependency browser with dependency callouts + YAML/diff viewer (`--output` writes the static HTML). |
| `ktl package images --chart ./chart` | Render the chart, pull every referenced container image, and save them into a tar archive. |
| `ktl package manifests --chart ./chart` | Render manifests plus metadata (chart version, git SHA, checksums) for change control or GitOps pipelines. |
| `ktl app package --chart ./chart --release foo` | Produce a single `.k8s` SQLite archive containing manifests, images, metadata, and attestations for offline installs. |
| `ktl app package verify --archive-file dist/foo.k8s` | Verify the Ed25519 signature plus embedded SBOM/provenance/license attachments before rollout. |
| `ktl db backup/restore` | Create or restore PostgreSQL dumps directly inside pods. |
| `ktl logs drift watch` | Continuously snapshot namespaces and highlight pod/container drift. |
| `ktl diag snapshot save` | Capture namespace manifests, pods, logs, and metrics into a portable archive. |
| `ktl diag report [--html]` | Print an ASCII namespace table or render the HTML posture report with scorecards. |
| `ktl diag health` | Run the scorecard checks headlessly, emit JSON, and fail CI when checks degrade. |

Each command respects the global `--kubeconfig/-k` and `--context/-K` flags.

## Preview Helm deploys before applying
`ktl deploy plan` renders the same chart/values you pass to `deploy apply`, fetches the live objects in your cluster, and summarizes the resulting creates/updates/deletes. The command highlights pod-impacting resources (Deployments, StatefulSets, Jobs, etc.) plus PodDisruptionBudget removals so SREs can double-check disruption windows before running the real upgrade.

```bash
ktl deploy plan \
  --chart ./deploy/checkout \
  --release checkout \
  --namespace prod-payments \
  --values values/prod.yaml \
  --kubeconfig ~/.kube/archimedes.yaml

# Polish the summary into a shareable artifact
ktl deploy plan \
  --chart ./deploy/checkout \
  --release checkout \
  --namespace prod-payments \
  --values values/prod.yaml \
  --html --output dist/checkout-plan.html
```

Typical output:

```
Release checkout @ prod-payments
Chart version: 1.2.3
Creates: 1, Updates: 2, Deletes: 0, Unchanged: 7

Planned changes:
- Create prod-payments/cache ConfigMap (core)
- Update prod-payments/checkout Deployment (apps)
    --- live
    +++ desired
    @@
    -  image: checkout:v1
    +  image: checkout:v2

Warnings:
- Updating prod-payments/checkout Deployment (apps) will restart pods; ensure PodDisruptionBudgets allow the rollout.
```

Run the plan repeatedly as you tweak values, then hand the resulting summary (or HTML report) to teammates for impact review before calling `ktl deploy apply`.

Already generated a plan artifact? Reuse it directly during rollout:

```bash
ktl deploy apply --reuse-plan dist/checkout-plan.html --watch 2m
```

`--reuse-plan` parses either the JSON (`--format json`) or HTML report and pre-fills the chart, release, namespace, values, and set flags so you can iterate on the plan and apply steps separately without retyping long flag lists.

Deploy plans also ship a **resource dependency graph**: ktl inspects pod specs for ConfigMap, Secret, PVC, imagePullSecret, and service account references, then renders those relationships in both the CLI output and HTML sidebar (plus embeds them in the plan JSON for downstream tooling). Use this graph to see which configs gate a workload before you touch the cluster.

### Visualize chart topology

Need something richer than raw YAML when onboarding teammates? Append `--visualize` to `deploy plan`:

```bash
ktl deploy plan --visualize \
  --chart ./deploy/checkout \
  --release checkout \
  --namespace prod-payments \
  --values values/prod.yaml \
  --kubeconfig ~/.kube/archimedes.yaml \
  --output dist/checkout-visualize.html
```

If you skip `--output`, ktl writes `./ktl-deploy-visualize-<release>-<timestamp>.html` automatically. Override the path with `--output /tmp/visualize.html`, or emit the raw HTML to stdout with `--output -`. Share it by copying the file to your wiki/chat, or host it with any static file server (`python3 -m http.server`, `npx serve dist`, etc.).

The refreshed HTML viewer mirrors ktl’s frosted-glass design and includes:
- A collapsible tree grouped by namespace → kind → resource, plus expand/collapse-all shortcuts that auto-fold huge namespaces for faster navigation.
- Inline filtering with both text search and resource-type chips (workloads, networking, config, storage) and a “Show changed only” toggle that hides everything except manifests with diffs.
- Dependency callouts that summarize what each workload depends on, why (envFrom, PVC, imagePullSecret, etc.), and which resources point back to it, using the same graph as `deploy plan`.
- Deep-linkable selections (URL hashes + “Copy link” button) so reviewers can drop teammates directly onto `ns/prod · kind/Secret · name/payments-env`, and filter/panel choices persist between reloads.
- Download buttons for rendered YAML and unified diffs, plus a YAML panel with a “Diff vs live” toggle. When live manifests were captured, the panel switches to syntax-colored diffs; otherwise it explains why the diff isn’t available (offline fallback, no live object, etc.).
- Optional comparison mode: upload any saved visualize artifact to view its manifests beside the current release, automatically flagging divergent resources and glowing their upstream/downstream dependencies so the blast radius is obvious.
- Chunked/lazy rendering so even four-digit release sizes stay responsive in the browser without freezing the page.

Share the HTML artifact in design reviews or ship it with release notes so SREs, PMs, and auditors can drill down into the release topology, inspect live-vs-rendered differences, and understand dependencies in one place.

### Share deploy progress in a browser

Rollouts rarely happen alone. Add webcast flags to mirror a live HTML dashboard (and optional raw WebSocket stream) anywhere you can open a browser:

```
ktl deploy apply \
  --chart ./deploy/checkout \
  --release checkout-prod \
  --namespace prod \
  --values values/prod.yaml \
  --ui :8080 \
  --ws-listen :9086
```

Everyone watching `http://localhost:8080` sees the same phase timeline (render → diff → upgrade/apply → wait → post-hooks), resource readiness table, manifest diff (when `--diff` is enabled), event/log feed, and health cards. The viewer also highlights Helm notes/errors immediately and mirrors the in-terminal status table so SREs, incident channels, and approvers can watch the rollout without ssh-ing into the box. Use `--ws-listen` if you want to archive the JSON event stream or plug it into a custom dashboard.

Need similar visibility without spinning up the UI? When stderr is a TTY, `ktl deploy apply` now auto-renders the deploy console: a metadata banner (release, namespace, chart/files), inline phase badges with color-coded states, a sticky warning rail, and an adaptive resource table that collapses gracefully on narrow panes (`--console-wide` keeps the full four-column grid when you have ≥100 columns). Non-TTY runs fall back to the spinner + warning lines so CI logs stay compact.

## Bundle everything into a SQLite app archive
Need a single file that encapsulates rendered manifests, every referenced container image, and deployment metadata? `ktl app package` emits a **signed** `.k8s` SQLite database that stores:
- `metadata` table (chart version, release name, git commit, kube version target, ktl version, render timestamp).
- `manifests` table (API version/kind/namespace/name/body/checksum per object).
- `images` table (docker-archive blobs for each referenced image).
- `attachments` such as:
  - `provenance.slsa.json` – SLSA v1 provenance describing the builder, invocation parameters, and materials (chart + git SHA + image digests).
  - `sbom.spdx.json` – SPDX 2.3 package list derived from the packaged images (including license declarations).
  - `license-summary.json` – quick license/category counts for auditors (also surfaced in archive metadata).
- A detached Ed25519 signature (`<archive>.sig`). If `--signing-key` is omitted, ktl generates one under `~/.config/ktl/signing_ed25519` and reuses it for future packages; share the `.pub` alongside the archive so downstream verifiers can trust it.

```bash
ktl app package \
  --chart ./deploy/checkout \
  --release checkout \
  --namespace prod-payments \
  --values values-prod.yaml \
  --notes docs/runbook.md \
  --archive-file dist/checkout.k8s
```

Feed the `.k8s` archive to air-gapped clusters, stash it as a golden artifact, or later extract manifests/images with simple SQL tools. Combining `ktl app package` with `ktl deploy --from-archive` (coming soon) will deliver a turnkey offline install path. Use `ktl app package verify --archive-file dist/checkout.k8s --signature-file dist/checkout.k8s.sig --public-key ~/.config/ktl/signing_ed25519.pub` to double-check the signature and embedded attestations before applying.

Need to inspect a snapshot? Unpack the manifests (and attachments) locally:

```bash
ktl app unpack \
  --archive-file dist/checkout.k8s \
  --snapshot base \
  --output-dir ./dist/checkout-manifests
```

### Why ktl's SQLite app archive architecture matters
ktl treats the `.k8s` archive as the single source of truth for everything a cluster rollout may ever need: manifests, container blobs, provenance, SBOMs, license metadata, and runbook attachments. Because the container is just SQLite, all of that data is queryable with stock tooling—`sqlite3`, Python, Rust, Go, even spreadsheet connectors—and works on any OS without an agent.

#### Unified asset graph in one file
- **Tables mirroring live Kubernetes objects.** Each manifest row captures API version, kind, namespace, name, checksum, and body. It is trivial to run `SELECT kind,name FROM manifests WHERE namespace='prod' ORDER BY kind;` to audit what would hit the cluster.
- **Image blobs stored as docker-archive payloads.** Air-gapped installs simply `sqlite3 dist/app.k8s "SELECT writefile('checkout.tar', blob) FROM images WHERE name='ghcr.io/acme/checkout';"` and load them into the local registry.
- **Attachments table for provenance, SBOM, and license sheets.** No bespoke format; every attachment is a row with media type metadata so consumers can pick JSON, SPDX, or Markdown runbooks.

#### Why SQLite beats Google/Microsoft/VMware style bundles
| Vendor pattern | Limitation | ktl advantage |
| --- | --- | --- |
| Google Anthos Config Controller ZIPs with YAML + scripts | Requires Cloud KMS/CSR plus Anthos-specific tooling; no offline schema | ktl archives validate with a pure `sqlite3` binary and Ed25519 keys, no control-plane dependency |
| Microsoft Azure Arc export tarballs | Layout varies per release, images shipped separately in ACR | ktl stores manifests/images/SBOM in the *same* file, so CI promotes a single checksum |
| VMware Tanzu packages (.imgpkg bundles) | Image relocations require vendir/imgpkg CLIs; SBOMs optional | ktl embeds vendir-powered syncs but emits a deterministic `.k8s` artifact consumers mount with only SQLite |

#### Operational superpowers
1. **Deterministic diffs.** Because manifests live inside a relational DB, operators diff archives with `SELECT body FROM manifests WHERE name='payment';` and feed the result to standard diff tooling. Other vendors rely on tarball ordering, which is non-deterministic.
2. **Policy enforcement via SQL.** SREs can assert “no image older than 30 days” with a single query and wire it into Gatekeeper or CI. Closed formats often need custom parsers.
3. **Built-in attestations.** ktl signs the entire database and records provenance + SBOM + license manifests next to the data, so verifiers check a single signature. Competing tools sign image indexes separately, forcing multi-step validation.
4. **Offline-first distribution.** Copy the `.k8s` file to USB, S3 Glacier, or artifact storage; nothing breaks because SQLite uses a portable page format with 100% backward compatibility.
5. **Snapshot diff UX.** `ktl diag report --html --compare-left baseline.k8s --compare-right release.k8s` reads the *same* schema and paints a drift view without writing converters.

#### Example: attesting a rollout pipeline
```sql
-- Inspect every image destined for prod-payments
SELECT name, digest, size_bytes
FROM images
WHERE snapshot = 'prod-payments'
ORDER BY name;

-- Validate provenance chain
SELECT json_extract(data, '$.subject[0].digest.sha256') AS sha
FROM attachments
WHERE name = 'provenance.slsa.json';
```
Those queries run on macOS, Linux, Windows, or inside CI containers with no extra binaries because SQLite ships inside Go’s stdlib bindings.

#### Extensibility story
- **Schema migrations baked into ktl.** When a new attachment type (e.g., runtime SBOM) appears, ktl bumps the DB schema and migrates older archives automatically, so consumers read a forward-compatible view.
- **Streaming writer optimized for BuildKit.** Internal `sqlitewriter` uses WAL mode to ingest large image blobs without locking, allowing multi-gigabyte archives while keeping the final file compact.
- **Namespace-level snapshots.** Each `.k8s` archive can carry multiple snapshots (base, canary, rollback). ktl references them via snapshot names, so a single file powers progressive delivery and instant rollback.

#### Compliance and audit readiness
- **License manifesting.** ktl auto-categorizes LICENSE fields from container layers and writes a JSON summary (permissive vs copyleft vs restricted). Auditors just query the `attachments` table instead of running custom scanners.
- **SBOM anchoring.** SPDX v2.3 docs in the archive reference the exact digest shipped, not “latest” tags, closing a common gap in vendor bundles.
- **SLSA provenance.** The provenance JSON records the Git SHA, chart checksum, BuildKit version, and ktl CLI version; downstream clusters can reject archives missing the chain.

#### Developer ergonomics
- **One-file promotion.** CI copies `dist/checkout.k8s` from dev → staging → prod, verifying a single SHA-256 at each gate. Google/Microsoft/VMware approaches typically juggle separate tarballs for manifests and registry exports.
- **Easy embedding.** Teams embed the archive inside Go tests or Terraform modules because it is just bytes; no need to rehydrate OCI registries in CI.
- **Tooling-friendly.** Data scientists can open the archive in Datasette or DuckDB, product managers can inspect attachments in any SQLite browser, and GitHub Actions can extract manifests without Helm installed.

#### Net: sqlite is the great equalizer
Vendors chase bespoke packaging formats tied to their control planes. ktl deliberately uses the most pervasive embedded database on earth to keep the supply chain boring, inspectable, scriptable, and resilient. If you can copy a file, you can move a ktl app archive; if you can run SQL, you can interrogate its contents. That is the power of shipping Kubernetes releases as a self-contained SQLite universe.

## Capture startup profiles
Trying to speed up ktl itself? Export `KTL_PROFILE=startup` before running a command and ktl will write CPU and heap profiles (e.g. `ktl-startup-20251203-231410.cpu.pprof`) into the current directory. Feed those `.pprof` files into `go tool pprof` to spot hot paths during initialization.

## Vendor upstream charts & manifests without installing vendir
Need to keep third-party Helm charts or git directories in sync, but don't want to ship another binary? `ktl app vendor` embeds VMware's vendir engine so you can point at any `vendir.yml` and pull the declared sources plus their lock file in one step.

```bash
ktl app vendor sync \
  --chdir deploy \
  --file vendir.yml \
  --lock-file vendir.lock.yml \
  --directory charts/grafana
```

Highlights:
- Supports every vendir source (git, helm charts, GitHub releases, inline files, OCI images) plus lazy/partial sync semantics—because it is the vendir CLI.
- Understands multi-config workflows (`-f vendir.yml -f overrides.yml`) and lock enforcement via `--locked`.
- Allows targeted refreshes with `--directory <path>` and local overrides (`dir=subst/path`).
- Exposes the rest of the vendir toolbox (`ktl app vendor version`, `ktl app vendor tools sort-semver`, etc.) without installing another binary.
- Honors the standard vendir environment variables such as `VENDIR_CACHE_DIR`, `VENDIR_MAX_CACHE_SIZE`, `VENDIR_GITHUB_API_TOKEN`, and `VENDIR_HELM_BINARY`.

Ship vendored bundles into `testdata/charts/`, bundle them into `.k8s` archives, or hand the synchronized tree to your CI/CD pipelines—no extra tooling required.

## PostgreSQL backups & restores
Create compressed, per-database dumps without installing tooling on your workstation:

```bash
ktl db backup postgresql-0 -n roedk-2 --output backups/
```

When `--database` is omitted, ktl introspects `pg_database` to dump every non-template DB. Restoring is just as simple, and you can wipe target pods before replaying the archive:

```bash
ktl db restore postgresql-0 -n sandbox \
  --archive backups/db_backup_20251128_161103.tar.gz \
  --drop-db        # prompts unless --yes is supplied
```

Progress spinners keep you informed database-by-database, and ktl automatically drops/recreates each DB before piping the SQL dump through `psql`.

## Inline traffic analysis
Need packet captures without shelling into pods or baking tcpdump into every image? `ktl analyze traffic` injects a short-lived, privileged ephemeral container that shares the target pod’s network namespace, launches tcpdump from that helper image, and streams the output in real time:

```bash
ktl analyze traffic \
  --target roedk-2/roedk-nginx-pko-86bc555bb-nlcw4:nginx-pko \
  --filter "port 443" \
  --interface any
```

Add `--between` with exactly two `--target` values to auto-capture conversations between them, or point multiple `--target` values at workloads on different namespaces to watch cross-service chatter from a single command. You can also override the helper image with `--image` if you maintain a hardened tcpdump build.

Need quick filters? Stack the new preset flag to avoid maintaining raw BPF strings:

```bash
ktl analyze traffic --target payments/api-0 --bpf dns --bpf handshake
```

Available presets include `dns`, `service-mesh`, `handshake`, `http`, `https`, `grpc`, `postgres`, `mysql`, `redis`, `kafka`, `ssh`, `kube-api`, `node-metrics`, `health-checks`, `ingress`, `nodeport`, `control-plane`, `etcd`, `istio-mtls`, `otel-collector`, and `prometheus-scrape`. Combine them (and raw `--filter` expressions) for precise captures.

## Syscall profiling
Need to understand which syscalls are blocking a pod without shelling into it? `ktl analyze syscalls` injects a privileged helper that shares the target pod's PID namespace, attaches `strace -c`, and streams the hottest syscalls back to your terminal:

```bash
ktl analyze syscalls \
  --target payments/api-0 \
  --profile-duration 20s \
  --top 12 \
  --match open,connect,execve
```

The CLI prints a tabular summary (percentage of time, total seconds blocked, average usec per call, call count, and error count) for each syscall. Pass `--format json` to integrate with incident bots or to feed scorecards. The helper honors the same `--image`, `--image-pull-policy`, `--privileged`, and `--startup-timeout` flags as `analyze traffic`, plus syscall-specific knobs:

- `--profile-duration` (default 30s) controls how long strace runs before emitting the aggregate table.
- `--match` filters the capture to a subset of syscalls or strace groups (for example `file`, `network`, `desc`, or explicit syscall names like `openat`).
- `--top` trims the output to the busiest syscalls; set it to `0` to print everything.
- `--target-pid` selects which PID inside the container should be traced (defaults to PID 1).

Because the helper relies on `ptrace`/eBPF, it defaults to privileged mode; kube RBAC must include `pods/ephemeralcontainers` plus `create`/`update` on the target pods—mirroring the requirements documented for traffic analysis.

### Stream profiles to teammates

Add `--ui :8081` (and optionally `--ws-listen :9081`) to mirror the syscall summary over HTML or a raw WebSocket feed. Anyone who can reach your workstation can open `http://127.0.0.1:8081` (or the bound IP) and watch the same rolling tables as your terminal without re-running ktl. The stream inherits the same highlights, filters, and multi-target ordering you see locally, making it easy to keep an incident channel in sync.

### Bring your own helper image

If your organization requires hardened bases or additional tooling, build and publish a custom helper from `images/syscalls-helper/` and point `--image` at it:

```bash
cd images/syscalls-helper
IMAGE=ghcr.io/your-org/syscalls-helper:$(git rev-parse --short HEAD)

# Build for your workstation architecture (override PLATFORM=linux/arm64 if needed)
make build

# Push once you have "docker login" credentials for the registry
make push
```

The `Dockerfile` installs only `strace`, networking basics, and a tiny shell loop, so you can extend it with extra packages or CA bundles before rebuilding.

### Authorize the cluster to pull the image

Because `ktl analyze syscalls` injects an ephemeral container into the live pod, Kubernetes reuses that pod's `imagePullSecrets`. Create (or update) a registry secret in the workload namespace and attach it to the service account or pod spec that backs the target:

```bash
kubectl create secret docker-registry ktl-syscalls-regcred \
  --docker-server=ghcr.io \
  --docker-username "$GITHUB_USER" \
  --docker-password "$GHCR_TOKEN" \
  --namespace energy-lab

kubectl patch serviceaccount default \
  --namespace energy-lab \
  --type merge \
  --patch '{"imagePullSecrets":[{"name":"ktl-syscalls-regcred"}]}'
```

If your pods run under a dedicated service account, patch that account instead; for one-off pods, you can also `kubectl patch pod <name> --type merge --patch '{"spec":{"imagePullSecrets":[{"name":"ktl-syscalls-regcred"}]}}'` before launching ktl. Once the secret is wired, run ktl with your published image and optional web stream:

```bash
ktl analyze syscalls \
  --target energy-lab/energy-lab-6f75ffc5f8-7gsrf \
  --image ghcr.io/avkcode/syscalls-helper:latest \
  --profile-duration 30s \
  --ui :8081 \
  --match file,network \
  --top 15
```

The terminal and browser share the same summary, so remote responders can inspect syscall hotspots while you keep the CLI focused on remediation.

## Quick pod resource stats
`ktl top` mirrors `kubectl top pods` but works wherever ktl does (including contexts configured via `--kubeconfig`):

```bash
ktl top -n roedk-2 --sort-cpu
```

Use `-A/--all-namespaces` to get a cluster-wide view, or add `-l` selectors to drill into workloads by label.

### Flag scopes

- **Global flags** (declared on `ktl` itself) always come last on the CLI line and apply to every subcommand: `--kubeconfig/-k`, `--context/-K`, `--log-level`, and the core tailer knobs from `ktl [POD_QUERY]`.
- **Command flags** belong to a specific subcommand (for example, `ktl logs capture` owns `--duration`, `--capture-output`, `--session-name`, `--capture-sqlite`).
- Run `ktl <command> --help` to see the sections separately—each command surfaces its own "[command] Flags" block followed by "Global Flags (inherited from ktl)" so you always know which knobs travel with you when switching subcommands.

### RBAC-aware modes

ktl automatically relies on whatever permissions your kubeconfig context grants. If the API server denies a call (for example, streaming events), ktl surfaces the error so you can adjust your RBAC grants without juggling extra CLI flags.

Want a turnkey config starter? Copy `docs/config.all-options.yaml` into `~/.config/ktl/config.yaml` (or point `KTL_CONFIG` at it) to see every flag expressed with example values.

## Color configuration

`ktl` will load additional defaults from `$XDG_CONFIG_HOME/ktl/config.yaml` (falling back to `~/.config/ktl/config.yaml` or `~/.ktl/config.yaml`). You can point to a different path with `KTL_CONFIG=/path/to/config.yaml`. Pod and container highlight palettes accept comma-separated SGR sequences, making it easy to mix attributes, underline, or even 24-bit colors:

## Scorecards, Alerts, and Trends

`ktl diag report` now layers a posture scorecard on top of the namespace inventory. Each HTML report renders Pod Security, quota headroom, rollout drift, and SLO burn scores with animated budget bars plus quick links to reproduce the diagnostics (`ktl diag podsecurity`, `ktl diag quotas`, `ktl logs drift watch`, etc.).

- Score widgets now include delta arrows, inline sparklines built from the local history DB, and collapsible drill-down panes so execs can see the top offenders without rerunning CLI commands.
- Append `?print` to the report URL for a PDF-friendly skin, and use the built-in filter chips (“Under 80% Ready”, “Restarts ≥5”, “PodSecurity Violations”) next to the search bar to spotlight problem pods instantly.
- The right-side insight stack includes the alert timeline, quota budget donuts (click to filter hot namespaces), and contextual runbook cards with copy-to-clipboard commands so responders can jump directly into follow-up diagnostics.
- Each score exposes contextual call-to-action buttons—click “Copy command” to grab the exact `ktl diag ...` invocation, or jump straight into drill-down sections that reuse the pod/ingress data already collected.
- `ktl diag report --threshold 85 --notify json` emits a JSON payload to stdout whenever any score dips below the threshold. Use `--notify stdout` for human-readable summaries or `--notify none` to suppress alerts.
- Score snapshots are persisted locally (under `~/.config/ktl/scorecard/history.db`). Inspect deltas with `ktl diag report trend --days 7`, which prints the daily averages and worst offenders over the selected window.
- Need automation? `ktl diag health -A --json --fail-on warn` reuses the exact same scorecard logic but prints machine-readable JSON and returns a non-zero exit code when checks degrade, making it ideal for CI or cron-based cluster monitors.
- Drift scoring uses the state captured via `ktl logs drift watch`, so subsequent report runs highlight rollout/hash changes without reconfiguring collectors.

```yaml
# ~/.config/ktl/config.yaml
# Green, Yellow, Blue, Magenta, Cyan, White
pod-colors: "32,33,34,35,36,37"

# Colors with underline (SGR 4); leave empty to reuse pod colors
container-colors: "32;4,33;4,34;4,35;4,36;4,37;4"
```

### Example config file

```yaml
# ~/.config/ktl/config.yaml
namespace: prod-payments
tail: 200
follow: true
color: always
pod-colors: "38;5;81,38;5;214,38;5;161"
container-colors: ""
highlight:
  - ERROR
  - timeout
timestamps: true
timestamp-format: youtube
template: "[{{.Timestamp}}] {{.PodDisplay}} {{.ContainerTag}} {{.Message}}"
```

The same values can be passed at runtime via `--pod-colors/-g` and `--container-colors`—because `ktl` forwards raw SGR codes, 24-bit palettes Just Work when your terminal supports them:

`.PodDisplay` mirrors the raw pod name, while `.ContainerTag` renders the container name wrapped in square brackets (e.g. `[coredns]`) so colorization can always target the prefix without accidentally recoloring parts of the pod name or log body.

```bash
# Monokai-inspired pod colors (24-bit)
podColors="38;2;255;97;136,38;2;169;220;118,38;2;255;216;102,38;2;120;220;232,38;2;171;157;242"
ktl deploy/payment --pod-colors "$podColors"
```

Templates can still be overridden inline or via files:

```bash
ktl backend --template '{{printf "%s (%s/%s/%s/%s)\\n" .Message .NodeName .Namespace .PodName .ContainerName}}'
ktl backend --template-file ~/.config/ktl/templates/minimal.tpl
```

### Capture SQLite option

Add `--capture-sqlite` when running `ktl logs capture` to bundle a ready-to-query SQLite database (`logs.sqlite`) inside the incident artifact. Every log line is mirrored into the `logs` table with `collected_at`, `log_timestamp`, `namespace`, `pod`, `container`, `raw`, and `rendered` columns plus handy indexes so you can later run ad-hoc queries such as:

```sql
SELECT namespace, pod, COUNT(*)
FROM logs
WHERE container = 'proxy'
  AND log_timestamp >= '2025-01-01T00:00:00Z'
GROUP BY namespace, pod
ORDER BY COUNT(*) DESC;
```

This feature is experimental: the schema may evolve as we iterate on capture replay tooling.

### Incident flight recorder

Use `ktl logs capture` when you need a bounded, shareable artifact for on-call handoffs or offline analysis:

```bash
# Capture checkout pods for 3 minutes, enrich with workload state, and write incident tarball
ktl logs capture 'checkout-.*' \
  --namespace prod-payments \
  --duration 3m \
  --capture-output dist/checkout-incident.tar.gz
```

Use `--duration` to pick the capture length that matches your investigation, whether you need a quick 30-second snapshot or a 15-minute soak test.

Pass `--attach-describe` to snapshot `kubectl describe`-style summaries for every observed pod; ktl drops them under `describes/<namespace>/<pod>.txt` inside the capture so you can scan pod annotations, container states, and correlated events without a cluster.

What ends up in the archive:
- `logs.jsonl` – every log line plus pod conditions, node pressure, container restarts, and rollout hashes resolved from cached informers (ReplicaSets/Deployments).
- `metadata.json` – capture window, namespaces, ktl flags, kube context, and observed pod counts.
- `logs.sqlite` – included when you pass `--capture-sqlite`, so you can query with SQL after the fact. ktl now injects correlated Kubernetes context (Events, Deployments, ConfigMaps) into dedicated tables, giving you pod history, rollout metadata, and configuration snapshots even when you’re offline.

Replaying locally is as simple as unpacking the tarball and piping through your favorite tools:

```bash
tar -xzf dist/checkout-incident.tar.gz -C /tmp/incident
cat /tmp/incident/logs.jsonl | jq '.namespace + "/" + .pod + " " + .rendered'

# Or replay directly from the artifact (uses logs.sqlite when present)
ktl logs capture replay dist/checkout-incident.tar.gz \
  --namespace prod-payments \
  --grep ERROR \
  --since 2025-11-25T08:00:00Z
```

Pass `--prefer-json` if you want replay to read `logs.jsonl`; otherwise ktl automatically streams from `logs.sqlite`, letting `--namespace`, `--pod`, `--since`, `--limit`, and `--grep` execute as SQL predicates for faster slicing.

Need to inspect a capture that’s still running? Point replay at the capture’s working directory and add `--follow` to stream new lines as they land (this mode currently reads from `logs.jsonl`).

### Compare capture metadata

See what changed between two captures (different windows, namespaces, pod queries, pod counts, etc.) without unpacking them manually:

```bash
ktl logs capture diff dist/checkout-incident-before.tar.gz dist/checkout-incident-after.tar.gz
```

The command prints a side-by-side summary for each artifact, then highlights namespace differences and any metadata deltas (duration, pod queries, tail settings, event/follow flags, SQLite inclusion, and more).

Add `--live` to compare a capture against the cluster you’re currently pointed at:

```bash
ktl logs capture diff dist/checkout-incident.tar.gz --live --namespace prod-payments --pod-query 'checkout-.*'
```

ktl will recreate capture metadata from the live API server (respecting `--namespace`, `-A`, and `--pod-query`) and surface what drifted—new namespaces, pod counts, follow settings, etc.—since the incident archive was taken.

### Drift watch (text-based)

Need to know *what changed* since the last snapshot? `ktl logs drift watch` samples pod metadata on a fixed cadence (default 30 seconds) and prints textual diffs showing pods that were added, removed, or mutated (phase changes, node moves, rollout hash swaps, container restarts/readiness flips):

```bash
# Watch drift in prod-payments every 15 seconds
ktl logs drift watch -n prod-payments --interval 15s

# Monitor the entire cluster, but only for 10 iterations
ktl logs drift watch -A --iterations 10
```

Each iteration prints sections for added (`+`), removed (`-`), and changed (`~`) pods with human-readable reasons (e.g., `rollout hash abc -> def`, `container api restarts 2 -> 5`). Use this to catch rogue rollouts, scheduling flaps, or pods that silently became unready between captures.

### Generate an HTML namespace report

Need a handoff-friendly snapshot of what’s running in a namespace? `ktl diag report` prints a concise ASCII table by default, add `--html` to emit the polished Johnny-Ive-style report HTML, or run `--live` to serve the same UI over HTTP for continuous refresh:

```bash
ktl diag report -n prod-payments                     # terminal-friendly table
ktl diag report -n prod-payments --html --output dist/payments-report.html
ktl diag report -A --live --listen :8080              # live server at http://127.0.0.1:8080/
ktl diag report --html \
  --compare-left dist/releases/app-v1.k8s@blue \
  --compare-right dist/releases/app-v2.k8s@green \
  --output dist/drift.html                            # render snapshot diff UI (added/removed/changed manifests + rollback snippets)

When both `--compare-left` and `--compare-right` point to `.k8s` archives (use `@snapshot` to pin layers), the HTML report surfaces added/removed/changed manifests, field-level highlights (env vars, RBAC rules, CRD versions), and one-click rollback snippets alongside the usual scorecards.
```

The live server streams the same HTML, auto-refreshes when new report hashes arrive over the `/events/live` SSE feed, and serves compressed responses with a short cache so multiple viewers share the same snapshot. Append `?full=1` to the live URL if you need ingress tables and other heavier sections.

### Namespace snapshots

Use `ktl diag snapshot` to capture incident-ready bundles:

```shell
# Capture everything running in prod-foo into a tar.gz archive
ktl diag snapshot save --kubeconfig ~/.kube/archimedes.yaml --namespace prod-foo --output incidents/prod-foo-$(date +%s).tgz

# Rehydrate the snapshot into a staging namespace
ktl diag snapshot replay incidents/prod-foo-1700000000.tgz --namespace foo-sim --create-namespace

# Compare two captures to see what changed
ktl diag snapshot diff incidents/prod-foo-1699999900.tgz incidents/prod-foo-1700000000.tgz
```

Each archive contains namespaced resources (Deployments, Services, ConfigMaps, etc.), full Pod specs, tail logs for every container, Events, and pod metrics so you can hand the file to another engineer (or CI job) and instantly replay or diff the workload without cluster access.

The HTML variant includes summary cards (pod counts, ready pods, restart totals) and per-pod tables showing container readiness, images, restart counts, and resource requests/limits. Each namespace renders as a collapsible panel (“slider”) so large multi-namespace reports stay tidy—click a namespace to drill into its pods and ingress definitions. The default ASCII view surfaces the same pod info plus an ingress table per namespace. Use `-A/--all-namespaces` to cover the entire cluster, or provide multiple `--namespace` flags to compare environments side-by-side.

## Examples

```bash
# Tail all logs from all namespaces
ktl logs . --all-namespaces

# Tail kube-system without backfilling historical logs
ktl logs . -n kube-system --tail=0

# Focus on the gateway container in the envvars pod using a custom context
ktl logs envvars --context=staging --container=gateway

# Skip istio-proxy containers in the staging namespace
ktl logs . -n staging --exclude-container istio-proxy

# Skip kube-apiserver pods in kube-system
ktl logs . -n kube-system --exclude-pod kube-apiserver

# Show auth activity from the last 15 minutes with timestamps
ktl logs auth -t --since=15m

# Snapshot everything from the last 5 minutes, suppress prefixes, then sort by time
ktl logs . --since=5m --no-follow --only-log-lines -A --tail=-1 | sort -k4

# Render timestamps in a different timezone
ktl auth -t --timezone Asia/Tokyo


# Inspect pods created by kubernetes-dashboard in kube-system
ktl kubernetes-dashboard --namespace kube-system

# Match all pods with run=nginx across namespaces
ktl . --all-namespaces -l run=nginx

# Follow frontend pods marked as canary
ktl frontend --selector release=canary

# Restrict by node
ktl . --all-namespaces --field-selector spec.nodeName=kind-control-plane

# Target pods via owner references such as deployment/nginx
ktl deployment/nginx

# Pipe structured output into jq
ktl backend -o json | jq .

# Highlight matches inline
ktl auth --highlight timeout --highlight ERROR

# Feed ktl from STDIN instead of Kubernetes
ktl --stdin < service.log

# Only display logs for pods that are not ready
ktl . --condition=ready=false --tail=0

# Merge pod logs with kubelet output but suppress the pod lines
ktl logs 'energy-.*' --namespace prod-lab --node-logs --node-log /var/log/kubelet.log --node-log-only

# Mirror a troubleshooting session to teammates in real time
ktl logs 'checkout-.*' --namespace prod-payments --ui :8080 --ws-listen :9080
```

## Development
```bash
make test   # go test ./...
make fmt    # gofmt
make lint   # go vet ./...
```

ktl uses controller-runtime logging, client-go informers, the dynamic client, Cobra, and Viper. Contributions are welcome—see `AGENTS.md` for contributor guidance.
