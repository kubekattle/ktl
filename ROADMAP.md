# ktl Roadmap

## Secrets Provider Abstraction

Pulumi’s secret-provider model lets users wire AWS Secrets Manager, HashiCorp Vault, and other backends into their infrastructure definitions without sprinkling credentials through code. ktl should adopt a similar abstraction so Helm values (or entire manifest fragments) can be hydrated directly from a secrets backend at deploy time.

### Goals

- Allow `ktl apply` (and companions like `ktl plan`/`ktl plan --visualize`) to reference secret placeholders (e.g., `secret://aws-secrets-manager/my-app/db-password`) that are resolved right before templating.
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

- Offer a typed gRPC API for core workflows (`build`, `ktl plan`, `ktl apply`, `logs`, `analyze`) to unlock remote execution, richer automation, and future language bindings.
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

## `ktl deploy`: Build → Lock → Apply (Release Units)

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
