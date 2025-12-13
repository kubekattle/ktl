# ktl Roadmap

## Secrets Provider Abstraction

Pulumi’s secret-provider model lets users wire AWS Secrets Manager, HashiCorp Vault, and other backends into their infrastructure definitions without sprinkling credentials through code. ktl should adopt a similar abstraction so Helm values (or entire manifest fragments) can be hydrated directly from a secrets backend at deploy time.

### Goals

- Allow `ktl deploy apply` (and companions like `deploy plan`/`deploy plan --visualize`) to reference secret placeholders (e.g., `secret://aws-secrets-manager/my-app/db-password`) that are resolved right before templating.
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
   - Add a pre-render hook in `executeDeployPlan` / `deploy apply` that walks values, resolves secret URIs, and replaces them with the fetched material.  
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
   - Add recipes: `ktl deploy apply --secret-provider vault --set db.password=secret://vault/app/db-password`.

### Milestones

1. MVP: Local file + AWS Secrets Manager provider, placeholder syntax, and deploy/apply integration.  
2. Vault/GCP/Azure providers with auth helpers + `ktl secrets login`.  
3. UI integration (plan/viz) that displays masked secret origins and lints for unresolved placeholders.  
4. Policy hooks (“deny deploy if secret comes from unapproved provider”).

## gRPC Control Plane Parity (Terraform-style)

Terraform exposes most of its provider/plugin functionality over gRPC so the CLI, remote services, and third-party tooling share a single typed contract. ktl should adopt a similar transport so builds, deploys, and diagnostics can run through a long-lived daemon or remote control plane, while keeping the existing single-binary UX untouched by default.

### Goals / Non-Goals

- Offer a typed gRPC API for core workflows (`build`, `deploy plan`, `deploy apply`, `logs`, `analyze`) to unlock remote execution, richer automation, and future language bindings.
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
