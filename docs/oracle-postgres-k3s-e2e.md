# Oracle/APEX To PostgreSQL On k3s: A Torque End-To-End

On May 23, 2026 I ran the Oracle/APEX to PostgreSQL showcase against a real
single-node k3s lab host. The goal was deliberately more complex than a normal
"run a playbook" demo:

- prove target-side Kubernetes readiness;
- record a change-window approval;
- freeze the source write path;
- export a source receipt;
- create a PostgreSQL restore point;
- expand schema;
- backfill rows with checkpoints;
- verify row parity;
- commit a cutover;
- promote the application route;
- contract the schema;
- run a post-cutover check;
- export a durable audit bundle.

The useful product point is that Torque treated the migration as one governed
change graph. A shell runner can execute these individual commands. Torque can
plan them, order them, checkpoint them, prove them, resume them, and export the
evidence.

## Lab Target

The lab host was running Ubuntu 22.04.5 LTS and k3s:

```text
k3s version v1.35.4+k3s1
node: Ready
container runtime: containerd://2.2.3-k3s1
```

For the run, I created a temporary `data-platform` namespace with:

- a PostgreSQL `postgres:16-alpine` Deployment;
- `pg-rw` and `pgbouncer-rw` services pointing at PostgreSQL;
- a `cnpg-controller-manager` service used as an operator-readiness edge;
- an `api-shadow` Deployment and service used as the shadow route edge.

The Kubernetes readiness receipts used the new real-check mode:

```bash
export TORQUE_K8S_READY_MODE=kubectl
export TORQUE_KUBECTL_BIN='k3s kubectl'
```

That mode checks the namespace, service, and ready endpoints before writing the
readiness artifact. The existing receipt-only behavior remains the default for
local proof runs.

## The Graph

The stack was the `oracle-postgres-k8s` showcase graph:

```text
cnpg-ready
  -> postgres-cluster-ready
  -> pgbouncer-ready
  -> app-shadow-ready
  -> change-window-approved
  -> apex-readonly-window
  -> oracle-export
  -> pg-restore
  -> pg-expand
  -> pg-backfill
  -> pg-verify
  -> pg-cutover
  -> app-route-promote
  -> pg-contract
  -> post-cutover-check
```

The run used the PostgreSQL stack file copied to `stack.yaml` because the
current CLI accepts `stack.yaml` or `release.yaml` as stack config filenames.

```bash
cp stack.postgres.yaml stack.yaml

export TORQUE_ORACLE_PG_DSN='postgres://postgres:***@127.0.0.1:15432/oracle_cutover?sslmode=disable'

./torque stack plan --config ./stack.yaml
./torque stack apply --config ./stack.yaml --yes
./torque stack audit --config ./stack.yaml --include-artifacts --output json > runtime/audit.json
./torque stack export --config ./stack.yaml --out runtime/oracle-postgres-run.tgz
```

PostgreSQL was exposed to Torque through a k3s port-forward from the host:

```bash
k3s kubectl -n data-platform port-forward svc/pg-rw 15432:5432
```

## What Happened

Torque planned 15 nodes. The apply run completed in DAG order and wrote a
SQLite-backed run ledger.

The key PostgreSQL proof after the run:

```text
stage_rows=3
shadow_rows=3
route_flags=true,true,true
migration_audit=contracted
```

The audit/export proof:

```text
run_id=2026-05-23T10-14-10.297189414Z
status=succeeded
event_count=108
artifact_count=30
bundle_kind=StackRunBundle
bundle_state_sha256=a446e6847f0634cfc6058955b73f033e905424a4cd8f931edc685437c93c398f
```

The exported bundle contained the portable stack state plus manifest:

```text
runtime/oracle-postgres-run.tgz
runtime/export-check/manifest.json
runtime/export-check/state.sqlite
```

The audit included the important typed artifacts:

```text
restore-point.json=1
schema-expand.json=1
backfill.json=1
verify.json=1
cutover.json=1
schema-contract.json=1
decision.json=15
```

The evidence directory remained on the host for inspection:

```text
/root/torque-k3s-oracle-postgres-e2e-20260523-101233
```

After the bundle was written, the temporary k3s namespace was deleted.

## Why This Is Hard For Ansible

Ansible can run SQL, call `kubectl`, wait for services, and write JSON files.
That is not the hard part.

The hard part is safely representing a stateful production change:

- the source freeze must produce a durable receipt;
- the export must carry a consistency anchor;
- the backfill must checkpoint progress;
- the cutover must not commit twice after a crash;
- downstream route promotion must wait for database verification;
- destructive contract work must wait until after cutover proof;
- every decision must survive the terminal session;
- the final evidence must be exportable for audit.

In Ansible, those behaviors usually become custom conventions spread across
playbooks, variables, registered results, hand-written state files, and log
parsing.

In Torque, they are the product model:

- the stack compiler builds the graph;
- the runner enforces dependencies;
- database node kinds emit typed artifacts;
- the run ledger preserves event integrity;
- audit and export produce portable proof.

That is the difference between task automation and governed change
orchestration.

## What This Proves

The run was intentionally small in data volume, but not small in operational
shape. It covered Kubernetes readiness, source-system receipts, PostgreSQL
state changes, backfill, cutover, route promotion, schema contract, audit, and
export.

That is the class of workload where Torque can be best in class:

```text
Ansible executes the steps.
Torque owns the change program.
```
