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
  semantics with `transport: ssh` or `transport: nats`; the NATS path
  dispatches to an agent subject and expects the same redacted operation receipt
  shape as the SSH path.
- `24-host-file-nats-module` proves the external `torque.host` module
  collection by applying `host.file.ensure` through a NATS assignment worker,
  then verifying the same module receipt shape that SSH-backed execution will
  use.
- `30-firecracker-keycloak-postgres-nats-admin` is a real-lab PostgreSQL day-2
  admin stack for the Keycloak Firecracker VM cluster. It runs the typed
  resources `postgres.role.ensure`, `postgres.database.ensure`,
  `postgres.grant.ensure`, `postgres.schema.ensure`,
  `postgres.extension.ensure`, `postgres.replication.verify`,
  `postgres.backup.run`, `postgres.backup.verify`, `postgres.restore.drill`,
  `postgres.config.ensure`, and `postgres.maintenance.run` through a
  JetStream-backed NATS worker on the primary, then proves backup, verify,
  restore-drill, and receipt-offset resume after controller death.
- `26-firecracker-kafka-nats-cluster` and
  `27-firecracker-rabbitmq-nats-cluster` are real-lab NATS stackfiles for
  side-by-side five-node Firecracker/k3s data-service clusters. All stack
  commands dispatch to lab-host `torque-agent nats worker` subjects; the
  workers bootstrap the Firecracker clusters, apply the Kafka/RabbitMQ
  workloads, deploy continuous traffic generators, verify message flow and
  quorum evidence, and delete the labs through the same NATS path.
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
- `31-firecracker-gitlab-hybrid-nats-day2` is a real-lab durable NATS day-2
  overlay for the same GitLab hybrid lab. It assumes the base SSH GitLab stack
  is running, then executes stateful verification, typed PostgreSQL
  role/grant/extension/maintenance/replication/backup/restore-drill resources,
  GitLab runner re-preparation, runner service restart, runner pipeline proof,
  and final GitLab verification through one approved JetStream-backed
  `torque-agent` on host `141`. `scripts/e2e/ops/STACK-GITLAB-NATS-001.sh`
  automates the full flow: base lab apply, private lab-host NATS broker,
  signed agent enrollment, durable `ops exec` proof, NATS day-2 stack apply,
  audit/export, and evidence bundle generation. If the base lab has already
  converged, the same script supports `--skip-base-apply --base-run-id <run>`
  so reruns can jump straight to the durable NATS portion.
- `32-firecracker-jira-postgres-lab` contains the Jira values fixture used by
  `scripts/e2e/ops/STACK-FC-PG-JIRA-001.sh`. That harness generates a
  7-node/2Gi Firecracker PostgreSQL stack variant from
  `15-firecracker-postgres-cluster`, keeps six PostgreSQL pods on `fc-00..05`,
  pins Jira to spare node `fc-06`, deploys the official Atlassian Jira chart
  through `torque apply`, and verifies the setup UI over a live port-forward
  before audit/export and optional teardown.
- `33-firecracker-jenkins-postgres-backup` is a Jenkins-oriented backup stack
  that assumes the direct Firecracker PostgreSQL lab is already running. It
  opens a short-lived SSH tunnel from the Jenkins worker to host `141`,
  executes `postgres.backup.run` and `postgres.backup.verify` locally in native
  mode, and leaves the dump, manifest, catalog, and `pg_restore --list`
  evidence in the Jenkins workspace for artifact capture.
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
