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
