# Terraform Provider Ecosystem Spec

Status: implementation started. The first local adapter is
`torque terraform-adapter`, which runs Terraform/OpenTofu provider resources as
Torque `module.resource` nodes with saved-plan apply/delete and redacted
receipts. This spec defines the productized shape that connects generated
provider modules, Ops targets, and Fleet execution.

## Goal

Make the Terraform/OpenTofu provider ecosystem available to Torque users without
copying provider implementations into Torque core.

Torque owns:

- stack graph ordering, selection, retry, locks, policy, and audit;
- module lifecycle receipts: `observe -> diff -> plan -> apply/delete -> verify`;
- target/credential provenance through Ops;
- fleet readiness, leases, assignment durability, and evidence export;
- generated module pack schemas, examples, conformance tests, and signatures.

Terraform/OpenTofu owns:

- provider plugin installation and protocol compatibility;
- provider-specific validation, diff, CRUD, waiters, imports, and state
  migrations;
- provider registry and mirror behavior;
- plan and state file formats.

## Architecture

Terraform providers enter Torque through generated module collections:

```text
stack.yaml
  -> typed node kind: aws.s3.bucket.ensure
  -> generated module pack: terraform-aws
  -> torque terraform-adapter
  -> Terraform/OpenTofu CLI
  -> provider plugin: registry.terraform.io/hashicorp/aws
```

The stack node remains domain-specific even though execution delegates to the
Terraform/OpenTofu provider:

```yaml
nodes:
  - name: logs-bucket
    kind: aws.s3.bucket.ensure
    targetSelector:
      type: cloud.account
      env: prod
      provider: aws
    module:
      source: oci://ghcr.io/torque-modules/terraform-aws
      version: 0.1.0
      command: ["torque", "terraform-adapter"]
      input:
        resource:
          type: aws_s3_bucket
          name: this
          values:
            bucket: app-prod-logs
            force_destroy: true
```

## Layer Contracts

### Stack

Stack owns dependency semantics. Terraform-backed nodes are normal nodes that
can depend on Helm releases, DB migrations, approvals, HTTP checks, host
operations, or other cloud resources.

Stack responsibilities:

- compile Terraform-backed modules into the same execution plan as all other
  nodes;
- store node input digests and module digests in the effective input hash;
- run lifecycle phases in DAG order for apply and reverse DAG order for delete;
- record module artifacts in stack audit/export bundles;
- block mutation when the Terraform adapter reports `safeToRun=false`,
  `blocked`, `failed`, or `error`.

### Modules

Modules own resource lifecycle. A generated Terraform provider pack is a normal
Torque module collection:

```text
terraform-aws/
  collection.yaml
  schemas/
  modules/
  examples/
  tests/
  signatures/
```

The generated collection exports kinds such as:

```text
aws.s3.bucket.ensure
aws.instance.ensure
aws.iam.role.ensure
cloudflare.dns.record.ensure
github.repository.ensure
```

The first generated module implementation calls `torque terraform-adapter`.
High-value resources may later get native Torque modules, but native rewrites
are not required for ecosystem coverage.

### Ops

Ops answers: where should this run, with what authority, and under what policy?

Terraform-backed modules should use Ops targets instead of raw credentials in
stack files:

```yaml
targets:
  - id: cloud/aws-prod
    type: cloud.account
    labels:
      provider: aws
      env: prod
      region: us-east-1
    credentialsRef: secret://vault/aws/prod
    policy:
      maxConcurrentMutations: 4
      requireApprovalFor:
        - iam.*
        - route53.zone.*
```

Ops responsibilities:

- resolve `targetSelector` to an explicit cloud account/project/region target;
- materialize credentials only for the worker and phase that needs them;
- record credential references, not secret values;
- enforce provider/account/resource rate limits;
- produce policy decisions for dangerous actions such as IAM, networking,
  destructive replacement, public exposure, and cross-account access;
- acquire locks for cloud account, provider state, and resource identity.

### Fleet

Fleet owns distributed execution. It should not contain provider-specific API
logic; it schedules module phases and enforces leases, readiness, capability,
idempotency, and evidence flow.

Fleet responsibilities:

- select workers with `terraform.provider.<name>` and provider-binary
  capabilities;
- keep provider plugin caches warm near workers;
- run module phases close to private networks or account credentials;
- prevent concurrent mutation of the same Terraform state lineage;
- record assignment IDs, receipt offsets, worker identity, and retry decisions;
- resume safely from saved plans and adapter checkpoints.

Required fleet locks:

```text
terraform-state/<provider>/<account>/<workspace>
cloud-account/<provider>/<account>
cloud-resource/<provider>/<account>/<type>/<id>
```

Only one writer may hold a Terraform state lock. Multiple observe/diff phases
may run when the provider and backend support it, but mutation always needs the
state writer lease.

## Provider Pack Generation

Productized commands should look like:

```bash
torque provider import hashicorp/aws --version ">= 6.0"
torque provider generate hashicorp/aws --out ./modules/terraform-aws
torque provider test ./modules/terraform-aws --resource aws_s3_bucket
torque module publish ./modules/terraform-aws
```

The generator should:

- run `terraform providers schema -json` or the OpenTofu equivalent in an
  isolated workspace;
- map provider/resource schemas to Torque JSON/YAML schemas;
- generate `collection.yaml` with exported kinds and lifecycle metadata;
- generate copy/paste stack examples;
- generate conformance tests for selected resources;
- mark sensitive attributes and computed/provider-private state fields;
- attach provider source, version constraint, protocol metadata, and checksums;
- publish signed OCI collections.

Generated kind names should be stable and readable:

```text
<provider>.<terraform-resource-with-prefix-removed>.ensure
```

Examples:

```text
aws.s3.bucket.ensure      -> aws_s3_bucket
aws.iam.role.ensure       -> aws_iam_role
cloudflare.dns.record.ensure -> cloudflare_dns_record
github.repository.ensure  -> github_repository
```

## State And Plan Model

The adapter uses generated workspaces under `.torque/terraform/<node>/` by
default. Production mode must support explicit backends and remote locks, but
the invariant stays the same:

- `plan` writes a saved plan and `torque-plan-meta.json`;
- `apply`/`delete` execute only the saved plan;
- saved plan metadata includes node ID, command, intent digest, config digest,
  plan digest, provider reference, resource reference, risk, and safety verdict;
- plan and state file bodies are not copied into stack audit artifacts;
- audit stores digests, summaries, and redacted command receipts.

Resume must reject mutation when:

- node ID changed;
- command changed;
- effective input hash changed;
- generated Terraform config digest changed;
- saved plan digest changed;
- provider lock file digest changed when strict mode is enabled;
- state lineage or backend lock is not the expected one.

## Security

Terraform providers often keep sensitive or provider-private values in state and
plan data. Torque must treat those files as sensitive by default.

Rules:

- no raw provider credentials in stack YAML;
- no plan/state file body in audit/export bundles by default;
- command receipts include output digests, not stdout/stderr bodies;
- secrets are materialized only from Ops/secret references at runtime;
- credential reference, provider source, provider version, and redaction counts
  are evidence;
- generated schemas mark Terraform `sensitive` attributes and provider-private
  fields as non-printable;
- real-cloud E2E scripts must destroy resources and verify deletion.

## Conformance Levels

### Level 0: Wrapper

- module invokes `torque terraform-adapter`;
- plan/apply/delete/verify receipts exist;
- destructive apply is blocked by default.

### Level 1: Generated Pack

- provider schema import works;
- kinds, schemas, examples, and docs are generated;
- fixture tests run with a deterministic fake provider CLI.

### Level 2: Certified Resource

- real provider create/update/delete/import/drift tests pass;
- state migration/upgrade test passes;
- no secret leakage test passes;
- rate-limit and retry behavior is documented.

### Level 3: Fleet-Certified Resource

- fleet readiness discovers provider capability;
- state/resource locks prevent duplicate mutation;
- assignment/receipt offsets prove idempotent resume;
- worker loss/retry does not duplicate changes;
- provider cache and backend behavior are measured.

## E2E Matrix

Required scenarios:

| ID | Scenario | Default Provider |
| --- | --- | --- |
| `TF-PROV-001` | Single resource create/verify/delete | AWS S3 opt-in |
| `TF-PROV-002` | 100 generated module nodes create/verify/delete | deterministic fake provider |
| `TF-PROV-003` | destructive apply blocked before mutation | deterministic fake provider |
| `TF-PROV-004` | saved plan digest mismatch blocks apply | deterministic fake provider |
| `TF-PROV-005` | no stdout/stderr/state/plan leakage in audit | deterministic fake provider |
| `TF-PROV-006` | import/adopt existing state | fake provider, then AWS opt-in |
| `TF-PROV-007` | provider upgrade/state migration | fake provider, then selected real provider |
| `TF-PROV-008` | fleet worker loss and idempotent resume | NATS JetStream fake provider |

`scripts/e2e/terraform-provider-100.sh` implements the first deterministic
scale proof: 100 independent Terraform-backed module nodes run through
`observe/diff/plan/apply/verify`, then `observe/diff/plan/delete/verify`, with
audit artifact validation and zero remaining fake state resources.

## Productization Roadmap

1. Keep `torque terraform-adapter` as the universal execution substrate.
2. Add `torque provider schema` to capture provider schemas into stable files.
3. Add `torque provider generate` for module collections and examples.
4. Add `torque provider test` for fake and real-provider conformance.
5. Add Ops cloud-account targets and credential reference materialization.
6. Add provider/state/resource locks to the stack lock planner.
7. Add fleet capability reporting for provider binaries and generated packs.
8. Add signed OCI publishing for generated provider packs.
9. Certify a small provider set: AWS S3/EC2/IAM, Cloudflare DNS, GitHub repo.
10. Add import/adoption and drift repair workflows for brownfield Terraform.
