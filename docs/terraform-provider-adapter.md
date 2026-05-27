# Terraform Provider Adapter

Torque can run Terraform/OpenTofu providers as typed stack modules through the
`torque terraform-adapter` module command. This is the compatibility path for
the Terraform ecosystem: Torque owns graph execution, policy, receipts, audit,
and replay while Terraform/OpenTofu owns provider installation, planning, state
migration, and resource CRUD.

The adapter is intentionally lifecycle-shaped:

```text
observe -> diff -> plan -> apply/delete -> verify
```

`plan` writes a saved plan plus metadata under `.torque/terraform/<node>/`.
`apply` and `delete` execute only that saved plan after checking the node ID,
command, intent digest, generated config digest, and plan digest. Destructive
actions during `stack apply` are blocked unless `module.input.allowDestroy` is
set. `stack delete` is the preferred destroy path.

## Stack Example

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: terraform-aws-s3
nodes:
  - name: logs-bucket
    kind: aws.s3.bucket.ensure
    module:
      source: oci://ghcr.io/torque-modules/terraform-aws
      version: 0.1.0
      command: ["torque", "terraform-adapter"]
      timeout: 20m
      input:
        provider:
          source: hashicorp/aws
          version: ">= 5.0"
          localName: aws
          config:
            region: us-east-1
        resource:
          type: aws_s3_bucket
          name: this
          values:
            bucket: my-company-prod-logs
            force_destroy: true
            tags:
              managed-by: torque
```

Run it like any other stack:

```bash
torque stack plan --config ./stacks/terraform-aws-s3 --output json
torque stack apply --config ./stacks/terraform-aws-s3 --yes
torque stack audit --config ./stacks/terraform-aws-s3 --output json --include-artifacts
torque stack delete --config ./stacks/terraform-aws-s3 --yes
```

Set `TORQUE_TERRAFORM_BIN` when the binary is not named `tofu` or `terraform`.
The adapter prefers OpenTofu (`tofu`) and then Terraform when no binary is
configured.

## Input Contract

`module.input.provider`:

- `source`: Terraform provider source, for example `hashicorp/aws`.
- `version`: optional provider version constraint.
- `localName`: optional local provider name. When omitted, Torque infers it
  from the resource type prefix or provider source.
- `config`: provider block attributes.
- `blocks`: provider nested blocks.
- `rawHCL`: advanced escape hatch appended inside the provider block.

`module.input.resource`:

- `type`: Terraform resource type, for example `aws_s3_bucket`.
- `name`: Terraform resource local name. Defaults to `this`.
- `values`: resource attributes.
- `blocks`: resource nested blocks.
- `rawHCL`: advanced escape hatch appended inside the resource block.

Other fields:

- `terraformBin`: optional Terraform/OpenTofu executable path or name.
- `workspaceDir`: optional generated workspace directory. Relative paths are
  resolved under the stack root.
- `lockTimeout`: Terraform state lock timeout. Defaults to `5m`.
- `allowDestroy`: allow destructive actions during `stack apply`; use
  `stack delete` for ordinary destruction.

Use `{"__hcl": "expression"}` for a single raw expression value when a provider
requires references that should not be quoted.

## Evidence And Secret Handling

The stack audit contains `module-observe.json`, `module-diff.json`,
`module-plan.json`, `module-apply.json` or `module-delete.json`,
`module-verify.json`, `module-resource.json`, `decision.json`, and adapter
artifacts such as Terraform CLI receipts, plan summaries, config digests, plan
digests, state digests, provider references, and resource action summaries.

Torque does not store Terraform plan or state contents in the stack audit
because those files can contain sensitive provider-private data. They stay in
the local `.torque/terraform/<node>/` workspace, which is already ignored by
Git. Put credentials in the normal provider environment, CLI config, or secret
materialization path; do not put raw secrets in `module.input`.

## AWS Smoke Test

The repository includes an opt-in S3 smoke harness:

```bash
TORQUE_AWS_E2E_CONFIRM=1 \
AWS_REGION=us-east-1 \
scripts/e2e/terraform-aws-s3.sh
```

It creates a unique S3 bucket through the adapter, verifies the Torque audit
receipts, runs `torque stack delete`, and fails if delete/verify evidence is
missing.

## 100-Node Provider Harness

The deterministic scale harness does not touch a real cloud account. It creates
100 Terraform-backed module nodes, runs apply and delete through the adapter,
and verifies one state resource per node after apply plus zero resources after
delete:

```bash
scripts/e2e/terraform-provider-100.sh
```

Use `--count` and `--concurrency` to change the scale and runner pressure:

```bash
scripts/e2e/terraform-provider-100.sh --count 100 --concurrency 20
```
