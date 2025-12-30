# Troubleshooting

Symptom → why it happens → what to run next.

## Helm apply/plan issues

### Symptom: “release not found” / unexpected install vs upgrade

Why:
- The release name/namespace is different than you expect, or the cluster context is wrong.

What to run:
```bash
ktl list -n <namespace>
ktl apply plan --chart ./chart --release <name> -n <namespace>
```

Next steps:
- Confirm `--kubeconfig` / `--context` and `-n` match the target cluster/namespace.
- Prefer `ktl apply plan` first when uncertain.

### Symptom: diff looks wrong / resources missing

Why:
- Values/`--set` are not what you think, or templating differs across environments.

What to run:
```bash
ktl apply plan --chart ./chart --release <name> -n <namespace>
```

Next steps:
- Check which values files are passed and whether any `KTL_*` env overrides apply.

## RBAC / Kubernetes auth

### Symptom: “forbidden” / “cannot list … at the cluster scope”

Why:
- The current kube context lacks permissions for the requested operation.

What to run:
```bash
kubectl auth can-i --list -n <namespace>
ktl apply plan --chart ./chart --release <name> -n <namespace>
```

Next steps:
- Use the correct `--context`/`--kubeconfig`.
- If you need read-only discovery first, start with `ktl apply plan` or `ktl stack` (read-only).

## Timeouts / stuck rollouts

### Symptom: apply waits forever / readiness never becomes true

Why:
- The underlying workload is failing to become Ready (image pull, scheduling, probes, etc.).

What to run:
```bash
ktl logs '<workload|pod-regex>' -n <namespace> --highlight ERROR
kubectl get pods -n <namespace>
kubectl describe pod -n <namespace> <pod>
```

Next steps:
- Look for Warning events like `FailedScheduling`, `ImagePullBackOff`, `ErrImagePull`.
- If using `ktl stack`, follow the run stream:
  - `ktl stack status --follow`

## Stack selection surprises

### Symptom: “selection matched 0 releases”

Why:
- Selector defaults/overrides are too strict.

What to run:
```bash
ktl env --match stack
ktl stack --config <stack-root>
```

Next steps:
- Temporarily remove filters (`KTL_STACK_TAG`, `KTL_STACK_RELEASE`, `KTL_STACK_FROM_PATH`, `KTL_STACK_GIT_RANGE`).
- Use `ktl stack explain <name>` to understand why a release is/was selected.

## Build sandbox / BuildKit issues

### Symptom: sandbox denies a path / missing mount

Why:
- The sandbox policy is stricter than your build needs (missing bind mounts, tmpfs too small, etc.).

What to run:
```bash
export KTL_SANDBOX_CONFIG="$(pwd)/testdata/sandbox/linux-ci.cfg"
ktl build --context . --tag ghcr.io/acme/app:dev --sandbox-logs
```

Next steps:
- Inspect `[sandbox]` diagnostics.
- If you need tighter security, start from `testdata/sandbox/linux-strict.cfg` and add only the required mounts.

