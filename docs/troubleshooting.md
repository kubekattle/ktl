# Troubleshooting

Symptom → why it happens → what to run next.

## Helm apply/plan issues

### Symptom: “release not found” / unexpected install vs upgrade

Why:
- The release name/namespace is different than you expect, or the cluster context is wrong.

What to run:
```bash
torque list -n <namespace>
torque apply plan --chart ./chart --release <name> -n <namespace>
```

Next steps:
- Confirm `--kubeconfig` / `--context` and `-n` match the target cluster/namespace.
- Prefer `torque apply plan` first when uncertain.

### Symptom: diff looks wrong / resources missing

Why:
- Values/`--set` are not what you think, or templating differs across environments.

What to run:
```bash
torque apply plan --chart ./chart --release <name> -n <namespace>
```

Next steps:
- Check which values files are passed and whether any `TORQUE_*` env overrides apply.

## RBAC / Kubernetes auth

### Symptom: “forbidden” / “cannot list … at the cluster scope”

Why:
- The current kube context lacks permissions for the requested operation.

What to run:
```bash
kubectl auth can-i --list -n <namespace>
torque apply plan --chart ./chart --release <name> -n <namespace>
```

Next steps:
- Use the correct `--context`/`--kubeconfig`.
- If you need read-only discovery first, start with `torque apply plan` or `torque stack` (read-only).

## Timeouts / stuck rollouts

### Symptom: apply waits forever / readiness never becomes true

Why:
- The underlying workload is failing to become Ready (image pull, scheduling, probes, etc.).

What to run:
```bash
torque logs '<workload|pod-regex>' -n <namespace> --highlight ERROR
kubectl get pods -n <namespace>
kubectl describe pod -n <namespace> <pod>
```

Next steps:
- Look for Warning events like `FailedScheduling`, `ImagePullBackOff`, `ErrImagePull`.
- If using `torque stack`, follow the run stream:
  - `torque stack status --follow`

## Stack selection surprises

### Symptom: “selection matched 0 nodes”

Why:
- Selector defaults/overrides are too strict.

What to run:
```bash
torque env --match stack
torque stack --config <stack-root>
```

Next steps:
- Temporarily remove filters (`TORQUE_STACK_TAG`, `TORQUE_STACK_RELEASE`, `TORQUE_STACK_FROM_PATH`, `TORQUE_STACK_GIT_RANGE`).
- Use `torque stack explain <name>` to understand why a release is/was selected.

## Build sandbox / BuildKit issues

### Symptom: sandbox denies a path / missing mount

Why:
- The sandbox policy is stricter than your build needs (missing bind mounts, tmpfs too small, etc.).

What to run:
```bash
export TORQUE_SANDBOX_CONFIG="$(pwd)/sandbox/linux-ci.cfg"
torque build . --tag ghcr.io/acme/app:dev --sandbox-logs
```

Next steps:
- Inspect `[sandbox]` diagnostics.
- If you need tighter security, start from `sandbox/linux-strict.cfg` and add only the required mounts.
