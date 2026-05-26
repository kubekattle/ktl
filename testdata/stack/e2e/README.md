# Stack e2e fixtures

These fixtures are used by `scripts/stack-e2e-suite.sh` to exercise `torque stack` end-to-end.

- Success fixtures used by the main suite live under `testdata/stack/e2e/01-...` through `testdata/stack/e2e/10-...` plus `13-mixed-nodes`.
- `13-mixed-nodes` proves generic `nodes:` can mix `host.command.run`, `release.helm`, and the legacy `releases:` alias in one DAG.
- `23-module-resource-demo` proves an external typed resource module can keep
  its domain kind (`demo.counter.ensure`) while Torque owns
  `observe -> diff -> plan -> apply/delete -> verify` receipts and audit
  artifacts.
- `14-firecracker-k8s-stackfile` is a real-lab stackfile that creates a
  Firecracker-backed k3s cluster, applies an HTTP DaemonSet app, verifies
  node-local access, and deletes the lab resources through the stack DAG.
- `22-firecracker-mysql-cluster` is a real-lab stackfile that creates three
  Firecracker VMs on `root@141.105.65.227`, configures a MySQL-compatible
  Galera cluster through SSH-backed stack nodes, verifies replicated writes
  through `mysql.replication.verify`, supports idempotent reapply, and deletes
  the VM resources through the stack DAG. The verifier keeps the same stack
  semantics with `transport: ssh` or `transport: nats-mesh`; the NATS path
  dispatches to an agent subject and expects the same redacted operation receipt
  shape as the SSH path.
- `19-firecracker-gitlab-hybrid` is a real-lab GitLab hybrid stack that
  creates Firecracker VMs for a 3-node k3s service tier and 4-node
  PostgreSQL/Redis/MinIO stateful tier, deploys GitLab with external services,
  verifies the sign-in page and admin evidence, runs generic Kubernetes
  cluster inspect, dynamic `targetsFrom` certificate inspect/renew lifecycle
  nodes, policy-gated cert renewal, optional scoped policy override evidence, a
  cluster verify gate, and compact `k8s-lifecycle-policy-decision.json` /
  `k8s-lifecycle-summary.json` artifacts for audit/export review, and supports
  idempotent reruns.
  `scripts/e2e/ops/STACK-LIFE-008.sh` runs the destructive lifecycle proof for
  this fixture: clean delete, recreate/apply, lifecycle verify, idempotent
  rerun, delete, recreate, and final proof export.
- `STACK-LIFE-011` provider matrix hardening is covered by
  `TestRun_KubernetesLifecycleProviderMatrix`, which runs the same local stack
  lifecycle DAG across kubeadm, k3s, RKE2, and explicit custom certificate
  command fixtures.
- `STACK-LIFE-012` parity is covered by
  `scripts/e2e/ops/STACK-LIFE-012.sh`, which generates matching Firecracker
  stackfiles for k3s and kubeadm/upstream Kubernetes, proves inspect, dynamic
  `targetsFrom`, cert inspect/renew, verify, lifecycle summary export, and
  idempotent rerun evidence on the real SSH lab host.
- Expected-failure fixtures live under `testdata/stack/e2e/x1-...` etc and should fail at `torque stack plan`.

All fixtures target a single namespace (`TORQUE_STACK_E2E_NAMESPACE`, default `torque-stack-e2e`) and are safe (ConfigMaps only).

These fixtures may also include `stack.yaml` `cli:` defaults so the real-cluster suite can exercise the “minimal flags” flow. See `docs/stack-cli-defaults.md`.
