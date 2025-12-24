# ktl Roadmap

## Secrets Provider Abstraction

Pulumi’s secret-provider model lets users wire AWS Secrets Manager, HashiCorp Vault, and other backends into their infrastructure definitions without sprinkling credentials through code. ktl should adopt a similar abstraction so Helm values (or entire manifest fragments) can be hydrated directly from a secrets backend at deploy time.

### Goals

- Allow `ktl apply` (and companions like `ktl apply plan`/`ktl apply plan --visualize`) to reference secret placeholders (e.g., `secret://aws-secrets-manager/my-app/db-password`) that are resolved right before templating.
- Support pluggable providers: AWS Secrets Manager, AWS Parameter Store, HashiCorp Vault, GCP Secret Manager, Azure Key Vault, plus a local `.env`/JSON fallback for dev.
- Keep secrets out of values files, plan artefacts, and `--reuse-plan` metadata; resolution should happen in-memory with audit logs indicating which provider/secret was accessed.
- Integrate with `--ui` and plan outputs by masking resolved secrets while still signalling where they came from (“value supplied by Vault path …”).

### Implementation Sketch

1. **Config schema**  
   - Introduce `secrets.yaml` / `ktl.yaml` defining named secret providers (type + connection details).  
   - Allow provider selection via flags (`--secret-provider vault`) and environment variables (`KTL_SECRET_PROVIDER`).

2. **Placeholder syntax**  
   - Support `secret://<provider>/<path>` tokens inside values files and `--set/--set-string`.  
   - Provide helper commands (`ktl secrets test`, `ktl secrets list --provider vault --path ...`) to validate access before deploy.

3. **Resolution layer**  
   - Add a pre-render hook in `executeDeployPlan` / `ktl apply` that walks values, resolves secret URIs, and replaces them with the fetched material.  
   - Cache secrets per run to avoid repeat network calls and to enable eventual token revocation.

4. **Provider implementations**  
   - Ship first-party providers for Vault (token + AWS IAM auth), AWS Secrets Manager, AWS SSM, and a local file provider.  
   - Expose an interface so users can drop in custom providers via Go plugins or exec hooks.

5. **Security posture**  
   - Ensure secrets never hit disk (no logging, no plan artifacts).  
   - Provide auditing hooks (e.g., emit which secret URIs were resolved) without leaking values.  
   - Consider optional envelope encryption for in-memory caching (using age or KMS data keys).

6. **Documentation & UX**  
   - Update README/AGENTS with “Bring your own secret backend” instructions.  
   - Add recipes: `ktl apply --secret-provider vault --set db.password=secret://vault/app/db-password`.

### Milestones

1. MVP: Local file + AWS Secrets Manager provider, placeholder syntax, and deploy/apply integration.  
2. Vault/GCP/Azure providers with auth helpers + `ktl secrets login`.  
3. UI integration (plan/viz) that displays masked secret origins and lints for unresolved placeholders.  
4. Policy hooks (“deny deploy if secret comes from unapproved provider”).

## gRPC Control Plane Parity (Terraform-style)

Terraform exposes most of its provider/plugin functionality over gRPC so the CLI, remote services, and third-party tooling share a single typed contract. ktl should adopt a similar transport so builds, deploys, and diagnostics can run through a long-lived daemon or remote control plane, while keeping the existing single-binary UX untouched by default.

### Goals / Non-Goals

- Offer a typed gRPC API for core workflows (`build`, `ktl apply plan`, `ktl apply`, `logs`, `analyze`) to unlock remote execution, richer automation, and future language bindings.
- Mirror Terraform’s handshake/capabilities negotiation so old/new clients can coexist and roll independently.
- Provide a **default-off** path: fresh `ktl` invocations continue to execute in-process unless a flag/env/daemon opt-in is supplied.
- Avoid introducing extra background processes unless the user runs `ktl daemon` (or similar) explicitly.
- Non-goal: replacing Cobra commands with a REST service; the CLI remains primary.

### Implementation Sketch

1. **Proto Design & Versioning**  
   - Define `proto/ktl/v1/*.proto` covering lifecycle RPCs plus shared types (contexts, artifact refs, UI streaming events).  
   - Copy Terraform’s plugin pattern: `HandshakeResponse` with magic cookie + protocol version + capability list, so mismatches fail fast.  
   - Generate Go + TypeScript stubs for CLI/server and future UI consumers.

2. **In-Process Server (Opt-In)**  
   - Embed a lightweight gRPC server that binds to `unix://$TMP/ktl-daemon.sock` (macOS/Linux) or named pipes (Windows) when `ktl daemon` runs.  
   - Gate client usage behind `KTL_RPC_ENDPOINT` or `--rpc-endpoint`. If unset, commands call existing Go functions directly so default behavior is unchanged.  
   - Share `context.Context` propagation, auth tokens, and log streaming channels to keep parity with today’s UX.

3. **Remote Endpoint & Auth**  
   - Add TLS + mTLS support, optional OIDC bearer tokens, and per-RPC ACLs.  
   - Support Terraform-style `credentials.tfrc.json` equivalent (`~/.config/ktl/credentials.json`) to manage remote endpoints.  
   - Document how managed runners (e.g., CI) expose the daemon publicly while desktops stick to loopback sockets.

4. **CLI UX & Fallbacks**  
   - Introduce discovery commands (`ktl rpc status`, `ktl rpc login`) that explain whether the CLI is using local execution or RPC.  
   - Ensure every RPC-capable command has a deterministic fallback path; if the remote endpoint is unreachable, drop back to local execution unless `--rpc-required` is passed.  
   - Maintain existing flags/outputs; streaming UI (`--ui`, `--ws-listen`) should just mirror remote events fed over gRPC.

5. **Observability & Extensibility**  
   - Emit structured metrics/traces from both client/server so we can compare performance vs. today’s direct calls.  
   - Allow future plugins (policy engines, custom reporters) to subscribe via additional gRPC services without embedding into the CLI binary.

### Milestones

1. Research + RFC: document Terraform’s provider handshake, env contract, and upgrade path; decide on ktl proto package layout.
2. Ship alpha proto + shared Go client, gated behind `KTL_RPC_ENDPOINT`. Implement `ktl build` over loopback to validate parity.
3. Expand coverage to deploy workflows + stream-based outputs; add `ktl daemon` command and status tooling.
4. Harden remote auth/mTLS, add CI runner story, and publish official API docs/examples.
5. Optional: expose beta TypeScript/Python SDKs that speak the same gRPC endpoints for automation without shelling out to the CLI.

## `ktl stack` (Hierarchical Multi-Release Orchestration)

Goal: make `ktl apply`/`ktl delete` scale from “one chart” to “hundreds of releases” in a monorepo, without turning ktl into Helmfile/Argo CD. `ktl stack` should be a thin orchestration layer: selection + inheritance + DAG ordering + concurrency + great UX, while reusing the existing apply/delete engine, streaming observers, and UI mirroring.

This section is a concrete spec proposal (schema + CLI + merge rules + examples).

### Design Principles

- **ktl remains the executor**: `ktl stack` compiles to a set of per-release apply/delete operations and runs them; it does not introduce a second templating/hook engine.
- **Hierarchical ownership**: configuration lives next to charts/services and inherits defaults from parent directories; avoid one mega-file for 300+ releases.
- **Selection is the product**: folder/tag/git selection is first-class and explainable; no one should hand-type 300 releases.
- **Deterministic DAG**: explicit `needs` edges; stable toposort; reverse-toposort for deletes.
- **Safe multi-cluster**: multi-cluster runs are supported, but dependency edges are scoped per-cluster by default (cross-cluster edges are a v2 feature).

### Config File Discovery

Stack configs are discovered from the filesystem. The root stack directory is either the current working directory or `--root`.

- A directory may contain `stack.yaml` defining defaults and optionally local releases.
- A release may be declared in either:
  - `release.yaml` (recommended leaf form), or
  - `stack.yaml` under a `releases:` list (handy for small subtrees).
- A Stack is the union of all releases discovered under `--root`.

Conventions:
- Keep `release.yaml` close to the chart/service it deploys.
- Use directory names to reflect ownership boundaries (team/app/domain), not environment.

### Schema (v1)

#### `stack.yaml`

```yaml
apiVersion: ktl.dev/v1
kind: Stack
name: roedk

# Optional. If present, selects a profile by default unless overridden by --profile.
defaultProfile: dev

# Profiles are overlays applied after base defaults, before leaf overrides.
profiles:
  dev:
    defaults:
      values: [values-dev.yaml]
  prod:
    defaults:
      values: [values-prod.yaml]

# Defaults inherited by all descendants (unless overridden below).
defaults:
  cluster:
    name: roedk2
    kubeconfig: ~/.kube/roedk2.yaml
    context: roedk2
  namespace: roedk-2
  values: [values-common.yaml]
  set:
    global.cluster: roedk2

  apply:
    atomic: true
    timeout: 10m
    wait: true
  delete:
    timeout: 10m

  tags: [team-platform] # tags inherited by descendants (additive)

# Optional local releases (for small directories).
releases:
  - name: redis
    chart: ./redis/chart
    tags: [core, cache]
    needs: []
```

#### `release.yaml`

```yaml
apiVersion: ktl.dev/v1
kind: Release

name: frontend
chart: ./chart

# Optional per-release overrides. If omitted, inherited from parent stack.yaml.
cluster:
  name: roedk2
namespace: roedk-2

values:
  - values-frontend.yaml
set:
  image.tag: "123"

tags: [app]
needs: [redis] # same-cluster dependency names (v1)
```

#### Supported Fields (v1)

- Common:
  - `name` (required, string): release identifier.
  - `tags` (optional, []string): additive labels for selection and reporting.
  - `needs` (optional, []string): DAG edges to other releases in the same cluster (by `name`).
- Targeting:
  - `cluster.name` (optional, string): logical cluster ID used for selection and reporting.
  - `cluster.kubeconfig` (optional, string): path; may contain `~` expansion.
  - `cluster.context` (optional, string): kube context override.
  - `namespace` (optional, string): default namespace for that release.
- Helm inputs:
  - `chart` (required, string): local path or chart reference supported by existing `ktl apply`.
  - `values` (optional, []string): file paths; additive (parent first).
  - `set` (optional, map[string]string): additive (child overrides same key).
- Execution options (subset; keep surface small in v1):
  - `apply.atomic`, `apply.timeout`, `apply.wait`
  - `delete.timeout`

Non-goals for v1 schema:
- Hooks, templating, and arbitrary scripting.
- Cross-cluster `needs` edges.
- Full parity with every `ktl apply` flag (start with the 20% that covers 80% use).

### Inheritance & Merge Rules (v1)

Resolution happens per release by walking from the root stack to the leaf directory containing the release definition.

Precedence (low → high):
1. Root `stack.yaml` `defaults`
2. Intermediate directory `stack.yaml` `defaults` (each level)
3. Selected `profiles.<name>.defaults` overlays (root-to-leaf, when present)
4. `release.yaml` (or in-file release entry) overrides

Merge behavior:
- Scalars (`namespace`, `cluster.kubeconfig`, `cluster.context`, timeouts, booleans): last writer wins.
- Lists (`values`, `tags`): concatenate in precedence order; duplicates are kept (v1) to preserve intent.
- Maps (`set`): merge by key; child overrides same key.

### Identity & Dependency Rules (v1)

Node identity is:
- `id = <cluster.name>/<namespace>/<release.name>` (for logs, summary tables, and stable output)

Dependency lookup:
- `needs` entries are resolved within the same `cluster.name` only (v1).
- `needs` entries refer to `release.name` (not full IDs) for ergonomics.
- Compiler errors:
  - missing dependency
  - cycle
  - dependency resolves to a different cluster

### Selection (v1)

Selection composes; users can provide multiple selectors that intersect/union (v1 should default to union for “target more”, with an explicit `--select-mode` if needed).

#### Reasoned Selection (v1)

At 100s of releases, selection must be auditable. `ktl stack` should record **why** each release was included and surface that in `plan` output and in the stack UI.

Definitions:
- A **selector** is a user-specified filter/input (`--git-range`, `--tag`, `--from-path`, `--release`, `--cluster`).
- An **expansion** is a compiler-added inclusion (e.g., include DAG dependencies/dependents).
- A **reason** is a normalized string (and optional structured payload) explaining inclusion.

Reason kinds (v1):
- `explicit:release:<name>`: included via `--release`.
- `explicit:tag:<tag>`: included via `--tag`.
- `explicit:path:<path>`: included via `--from-path`.
- `explicit:git:<path>`: included because a changed file mapped to this release (path is the changed file or mapped owner directory).
- `expand:dep-of:<node-id>`: included because it is a dependency of a selected node.
- `expand:dependent-of:<node-id>`: included because it is a dependent of a selected node.

Rules:
- Each release has `selectedBy[]` (deduplicated) in the resolved plan.
- Every expanded node must include at least one `expand:*` reason that references the node that caused the expansion.
- Plan/UI should be able to explain “minimal set”: show the initial selected set and the expanded set separately.

Selectors:
- `--tag <t>` (repeatable): include releases that have any of the tags.
- `--from-path <path>` (repeatable): include releases defined under that directory subtree.
- `--release <name>[,<name>...]`: explicit names (scoped by cluster; error on ambiguity).
- `--cluster <name>[,<name>...]`: filter the universe first (useful for multi-cluster trees).
- `--git-range <a>...<b>`: include releases whose subtree contains changed files in the given git diff.
  - Mapping rule: each changed file is mapped to the closest ancestor directory that contains `release.yaml` or a `stack.yaml` with `releases:`.
  - Provide `--git-include-deps` and `--git-include-dependents` to expand selection along the DAG after the initial mapping.

### Execution (v1)

Commands:
- `ktl stack apply` runs `ktl apply` for the resolved set in DAG order.
- `ktl stack delete` runs `ktl delete` for the resolved set in **reverse** DAG order.

Scheduling:
- Topological sort within each cluster.
- Execute ready nodes with `--concurrency N` (default 1).
- Failure behavior:
  - Default `--fail-fast`: stop scheduling new nodes on first error; keep already-running ones; exit non-zero.
  - Optional `--continue-on-error`: continue scheduling independent nodes; exit non-zero with aggregated summary.

Observers/UI:
- Reuse existing deploy stream observers; prefix each line/event with stack release identity.
- Optional: `--ui/--ws-listen` should expose a “stack run” view that links to per-release deploy viewers (v1 can start with terminal-only + event prefixes).

State/safety:
- After every run, write a local record: `.ktl/stack/<stack-name>/<profile>/last-run.json` containing:
  - selected releases + resolved IDs (cluster/ns/name)
  - resolved inputs (chart path, values list, set keys) for audit
  - start/end timestamps and per-release status
- `delete` should prompt when the resolved selection is large, unless `--yes`.

#### Run Artifacts (JSON-only, no SQLite)

`ktl stack` should support durable `--resume`/`--rerun-failed` without any external services by persisting a run artifact as files under `.ktl/stack/runs/<run-id>/` using atomic writes and an append-only event log.

Directory layout:

- `.ktl/stack/runs/<run-id>/plan.json` (immutable after creation)
- `.ktl/stack/runs/<run-id>/events.jsonl` (append-only WAL)
- `.ktl/stack/runs/<run-id>/summary.json` (periodic snapshot; safe to delete)
- `.ktl/stack/runs/<run-id>/meta.json` (who/where/git; safe to delete)

**Write rules**
- `plan.json` is written before any apply/delete begins.
- `events.jsonl` is append-only; each line is a single JSON object (one event).
- All non-append writes use `write tmp + fsync + rename` so crashes/power loss do not corrupt files.

##### `plan.json` (schema sketch)

Purpose: freeze the resolved universe + selection + effective inputs so resuming does not accidentally pick up config drift.

```json
{
  "apiVersion": "ktl.dev/stack-run/v1",
  "runId": "2025-12-24T13-30-00Z_3f2c9a1",
  "stackRoot": "/private/tmp/ktl",
  "stackName": "roedk",
  "command": "apply",
  "profile": "dev",
  "selector": {
    "clusters": ["roedk2"],
    "tags": ["core"],
    "fromPaths": [],
    "releases": [],
    "gitRange": "origin/main...HEAD",
    "gitIncludeDeps": true,
    "gitIncludeDependents": true
  },
  "concurrency": 4,
  "failMode": "fail-fast",
  "nodes": [
    {
      "id": "roedk2/roedk-2/redis",
      "name": "redis",
      "cluster": { "name": "roedk2", "kubeconfig": "~/.kube/roedk2.yaml", "context": "roedk2" },
      "namespace": "roedk-2",
      "chart": "./helm-roedk/redis",
      "values": ["values-common.yaml", "values-dev.yaml", "values-redis.yaml"],
      "set": { "global.cluster": "roedk2" },
      "tags": ["core", "cache"],
      "needs": [],
      "selectedBy": ["explicit:git:helm-roedk/redis/Chart.yaml", "expand:dep-of:roedk2/roedk-2/frontend"],
      "effectiveInputHash": "sha256:…",
      "executionGroup": 0
    }
  ]
}
```

Notes:
- `effectiveInputHash` must cover the effective inputs: chart reference, values file contents (not just names), set map, cluster identity, namespace, and ktl version (plus Helm version if applicable).
- `executionGroup` is the DAG “wave” number (useful for UX/plan printing); it is informational only.
- `selectedBy` should be stable and human-readable; keep the vocabulary small and normalized to enable policy checks in CI.

##### `events.jsonl` (schema sketch)

Purpose: durable progress tracking for resume/retry with minimal complexity.

Event fields:
- `ts` (RFC3339Nano), `runId`, `nodeId`, `type`, `attempt`, and optional `message`/`error`.

Example lines:
```json
{"ts":"2025-12-24T13:30:01.123Z","runId":"…","nodeId":"roedk2/roedk-2/redis","type":"NODE_QUEUED","attempt":0}
{"ts":"2025-12-24T13:30:02.456Z","runId":"…","nodeId":"roedk2/roedk-2/redis","type":"NODE_RUNNING","attempt":1}
{"ts":"2025-12-24T13:31:10.000Z","runId":"…","nodeId":"roedk2/roedk-2/redis","type":"NODE_FAILED","attempt":1,"error":{"class":"RATE_LIMIT","message":"429 Too Many Requests","digest":"sha256:…"}}
{"ts":"2025-12-24T13:31:12.000Z","runId":"…","nodeId":"roedk2/roedk-2/redis","type":"NODE_RETRY_SCHEDULED","attempt":2,"message":"backoff=2.4s"}
{"ts":"2025-12-24T13:31:40.000Z","runId":"…","nodeId":"roedk2/roedk-2/redis","type":"NODE_SUCCEEDED","attempt":2}
```

##### `summary.json` (schema sketch)

Purpose: quick UX without scanning the full WAL (can be rebuilt by replaying `events.jsonl`).

```json
{
  "apiVersion": "ktl.dev/stack-run/v1",
  "runId": "…",
  "status": "running",
  "startedAt": "…",
  "updatedAt": "…",
  "totals": { "planned": 120, "succeeded": 37, "failed": 1, "skipped": 0, "running": 2 },
  "nodes": {
    "roedk2/roedk-2/redis": { "status": "failed", "attempt": 1, "lastErrorClass": "RATE_LIMIT" }
  }
}
```

##### Resume & Drift Rules (v1)

- `ktl stack apply --resume` resumes the most recent run (or `--run-id <id>`).
- Resume **must** use `plan.json` as the source of truth (no re-discovery/re-merge) unless `--replan` is explicitly passed.
- If any node’s `effectiveInputHash` differs from the current computed hash, resume should:
  - fail by default with a clear “inputs changed” report, and
  - allow override via `--allow-drift` (explicit footgun) or `--replan`.

##### Retries (v1)

- Retries are bounded and recorded in the WAL (`attempt` increments; never resets on `--resume`).
- Default retryable classes: API rate limits (429), transient 5xx, transport errors; default non-retryable: invalid input, forbidden, immutable field errors.
- `--retry <n>` sets a global max attempts; per-release override can be introduced later.
- `--rerun-failed` schedules only failed nodes (plus optional dependents via `--include-dependents`).

### CLI (v1)

Global:
- `--root <dir>`: stack root directory (default `.`)
- `--profile <name>`: profile overlay (default `stack.yaml.defaultProfile` if set)
- `--cluster <name>[,<name>...]`

Selection:
- `--tag <tag>` (repeatable)
- `--from-path <path>` (repeatable)
- `--release <name>[,<name>...]`
- `--git-range <a>...<b>`
- `--git-include-deps`
- `--git-include-dependents`

Execution:
- `--concurrency <n>`
- `--fail-fast` / `--continue-on-error`
- `--yes` (skip confirmation prompts)

Output:
- `--output table|json` (default `table`)
- `--plan-only` (for apply/delete: compile + print plan, no execution)

### Example Output (v1)

`ktl stack plan --git-range origin/main...HEAD --output table`

- Header: stack name, profile, selected clusters, release count.
- Plan groups: per cluster, list execution waves (DAG levels).
- For each release: `cluster/ns/name`, chart, status=PLANNED.
- Include a selection summary: counts by reason kind (explicit vs expanded), and (optionally) `--explain <release>` printing `selectedBy[]`.

`ktl stack apply --git-range origin/main...HEAD --concurrency 4`

- Live stream: prefixed by `roedk2/roedk-2/frontend` (or shorter display name) to keep multiplexed output readable.
- Final summary table (always printed):
  - `ID | RESULT | DURATION | CHART | NOTES (first error line)`

### Example Stack Layout (Single Cluster, 5 Services + `helm-roedk`)

This example matches a common repo structure where `services/` contains one chart per service and `helm-roedk/` is a “bundle chart” that deploys the main application and depends on the services.

Repository layout:

- `stack.yaml`
- `services/stack.yaml`
- `helm-roedk/stack.yaml`
- `services/<svc>/Chart.yaml` (e.g., `services/postgresql/Chart.yaml`)
- `services/<svc>/values-dev.yaml`
- `helm-roedk/Chart.yaml`
- `helm-roedk/values-dev-roedk2.yaml`

`stack.yaml` (repo root)

```yaml
apiVersion: ktl.dev/v1
kind: Stack
name: roedk2

defaultProfile: dev

defaults:
  cluster:
    name: roedk2
    kubeconfig: ~/.kube/roedk2.yaml
    context: roedk2
  namespace: roedk-2
  apply: { atomic: true, timeout: 10m, wait: true }
  delete: { timeout: 10m }
```

`services/stack.yaml`

```yaml
defaults:
  tags: [svc]

releases:
  - name: postgresql
    chart: ./postgresql
    values: [values-dev.yaml]
    tags: [db]

  - name: redis
    chart: ./redis
    values: [values-dev.yaml]
    tags: [cache]

  - name: minio
    chart: ./minio
    values: [values-dev.yaml]
    tags: [storage]

  - name: keycloak
    chart: ./keycloak
    values: [values-dev.yaml]
    needs: [postgresql]
    tags: [auth]

  - name: pgadmin
    chart: ./pgadmin
    values: [values-dev.yaml]
    needs: [postgresql]
    tags: [ops]
```

`helm-roedk/stack.yaml` (single release)

```yaml
defaults:
  tags: [app]

releases:
  - name: roedk
    chart: .
    values: [values-dev-roedk2.yaml]
    needs: [postgresql, redis, minio, keycloak]
    tags: [bundle]
```

Example commands:

- Full deploy: `ktl stack apply`
- Deploy only dependencies: `ktl stack apply --from-path services/`
- Deploy the app chart (and, if requested, expand deps): `ktl stack apply --from-path helm-roedk/ --git-include-deps`

### Milestones

1. **Compiler + plan**: filesystem discovery, inheritance merge, DAG validation, and `ktl stack plan/graph`.
2. **Apply/delete orchestration**: concurrency scheduler + fail-fast + reverse delete order; reuse existing deploy engine.
3. **Selection**: tags/from-path/release; add git-range mapping and `--git-include-{deps,dependents}`.
4. **UX hardening**: stable IDs, crisp summaries, JSON output, last-run record + delete confirmations.
5. **UI integration (optional)**: stack-level UI run view that groups per-release viewers and renders the DAG timeline.

## `ktl deploy` (future): Build → Lock → Apply (Release Units)

Note: `ktl deploy` is not part of the current CLI (it was removed in favor of `ktl apply plan`/`ktl apply`/`ktl delete`). This section describes a possible future “release unit” workflow.

Argo CD and Helmfile are strong at GitOps-style, continuously-reconciled Helm orchestration. `ktl deploy` should win on a different axis: a single, developer-to-prod workflow that produces a reproducible “release unit” (build outputs + chart inputs + manifests + verification) that can be reviewed, applied, captured, and promoted across environments.

### Goals

- Combine `ktl build` + `ktl apply` into one workflow with shared streaming UX (`--ui`, `--ws-listen`) and a single `--capture` session.
- Make image updates automatic and safe by default: build produces digests; deploy injects digests/tags into Helm in a deterministic way.
- Treat every deploy as an immutable, promotable artifact (“release unit”), not just a one-off apply.
- Provide fast, rich `plan` output developers actually run (images changed, risky diffs, deletions, immutable-field warnings).
- Close the loop with verification and diagnostics collection on failure.

### Design Sketch

1. **Command Shape (Bundle-First)**
   - `ktl deploy bundle --chart <chart> --release <name> [build flags…] [apply-like flags…] -o <release-unit.sqlite>`
   - `ktl deploy plan <release-unit.sqlite>` (render + diff for the bundle, no apply)
   - `ktl deploy apply <release-unit.sqlite>` (apply exactly what was bundled)
   - `ktl deploy promote <release-unit.sqlite> --to <env/cluster/ns>` (apply the same unit to another target)
   - `ktl deploy verify <release-unit.sqlite|--release ...>` (post-apply checks)

2. **Release Units (First-Class Artifacts)**
   - Bundle the exact inputs/outputs into a single artifact (SQLite, like capture/archive patterns):
     - chart reference + chart package digest
     - resolved Helm values (post-merge)
     - image digests (and optionally SBOM/provenance references)
     - rendered manifest digest + per-object hashes
     - apply release manifest + status/notes
     - verification results and collected diagnostics
   - Support “replay” (render/view UI from bundle) and “promote” (apply same bits to another target).

3. **Image Injection Contract (Chart Convention Auto-Detect)**
   - Prefer digest pinning. Record digests even when using tags for dev.
   - Default to chart convention auto-detect (common `image.repository`/`image.tag` keys, per-service `images.*`, `global.image.*`, etc.).
   - Allow override mapping when auto-detect is insufficient or ambiguous:
     - values overlay (`--set ...`) driven by a mapping file.
     - optional post-render rewriting for charts without clean values hooks.

4. **Progressive Delivery (Opt-In)**
   - Strategies that work with vanilla Helm/Kubernetes where possible:
     - rolling (default)
     - canary (weight steps + pauses)
     - blue/green (service switch)
   - Tie steps to readiness signals and optional checks; capture results in the release unit.

5. **Verification + Safety Rails**
   - Verify workloads are running expected digest/tag; hooks/jobs succeeded; readiness achieved; no new crashloops/events regressions.
   - Policy file support (`--policy`) to gate risky operations (forbidden kinds, cluster-scoped resources, digest-required in prod, etc.).

6. **GitOps Interop (Complement, Don’t Replace)**
   - Emit artifacts suitable for GitOps repos (values overlays + chart package refs) and optionally generate a “promotion PR”.

### Milestones

1. **MVP**: `ktl deploy bundle` produces a release unit; `ktl deploy apply` applies it with unified streaming/capture.
2. **Release Unit**: add a bundle format + `ktl deploy promote` for cross-environment reuse; add “replay UI from bundle”.
3. **Verification**: post-apply verification + auto-diagnostics capture; fail with actionable summary and artifacts.
4. **Policy + Guardrails**: policy engine hooks + default safety checks; document recommended patterns in `docs/agent-playbook.md`.
5. **Progressive Delivery**: canary/blue-green options integrated with existing UI and event stream.
