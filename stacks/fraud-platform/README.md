# Fraud Platform Stack Package

This package is the production-shaped version of the Firecracker fraud platform
demo. The live lab stack proved the graph end to end; this layout makes the
same scenario reviewable as a stack package instead of one large YAML file.

## Layout

- `stack.yaml` declares the Torque graph, profiles, runner defaults, and node
  ordering.
- `scripts/` contains the host, cluster, workload, batch, public-access, and
  verification phases.
- `app/` contains the payment API, generator, Ray model, and Spark batch code.
- `images/` contains Dockerfiles for production image builds.
- `values/` captures environment differences for review and release packaging.

## Profiles

`lab` is the default profile. It expects `TORQUE_LAB_SSH` or
`TORQUE_LAB_PUBLIC_IP`, creates/reuses the Firecracker k3s lab, exposes NodePort
DNAT rules, enables the event generator, and verifies public endpoints.

`stage` expects an existing kubeconfig through `TORQUE_FRAUD_KUBECONFIG` or
`KUBECONFIG`, uses the same verification graph, and keeps public access outside
the Firecracker host scripts.

`prod` also expects an existing kubeconfig. It skips the Firecracker and DNAT
lab nodes, expects pre-created `aws-s3` secrets or workload identity, and should
consume digest-pinned application images from `values/prod.yaml`.

## Packaging Flow

```bash
export TORQUE_STACK_ROOT=stacks/fraud-platform
export TORQUE_STACK_PROFILE=prod
export TORQUE_FRAUD_KUBECONFIG=/path/to/prod.kubeconfig
export TORQUE_FRAUD_AWS_SECRET_MODE=existing

./bin/torque stack plan --config "${TORQUE_STACK_ROOT}" \
  --bundle dist/fraud-platform-plan.tgz \
  --bundle-diff-summary

./bin/torque stack seal --config "${TORQUE_STACK_ROOT}" \
  --profile "${TORQUE_STACK_PROFILE}" \
  --out dist/fraud-platform-prod \
  --concurrency 2

./bin/torque stack apply --sealed-dir dist/fraud-platform-prod \
  --capture dist/fraud-platform-prod.sqlite \
  --capture-tag env=prod \
  --capture-tag app=fraud-platform \
  --yes

./bin/torque stack audit --config "${TORQUE_STACK_ROOT}" \
  --output html > dist/fraud-platform-audit.html

./bin/torque stack export --config "${TORQUE_STACK_ROOT}" \
  --out dist/fraud-platform-run.tgz
```

Current Torque seals host command strings and Helm inputs. When scripts are
externalized like this package, keep the source package alongside the sealed
plan or promote the scripts as declared stack inputs before relying on a sealed
bundle as a fully standalone runtime artifact.

## Production Rules

- Keep cloud credentials out of stack output. Use workload identity or
  pre-created Kubernetes secrets resolved from `secret://` providers.
- Build the app sources in `app/` into images with Torque build capture,
  SBOM/provenance, and digest-pinned image references.
- Treat `values/prod.yaml` as the production contract: pinned image digests,
  generator disabled, durable S3 and ClickHouse settings, and strict
  verification gates.
- Verification is part of the graph: API health, Ray Serve response, Flink job
  state, Argo/Spark workflow success, S3 object counts, ClickHouse row counts,
  Redpanda offsets, public endpoint checks, and no unhealthy pods.
