# Oracle Or APEX To PostgreSQL In Kubernetes

This showcase turns a realistic migration story into a runnable Torque graph:

- the source system is a standalone Oracle or Oracle APEX deployment;
- the target system is a PostgreSQL stack in Kubernetes;
- Torque owns the durable PostgreSQL-side program;
- agents own the source-system and app-routing edges through `action.script`.

The important product point is that the risky change is not "run a migration
script". It is a reviewable run graph with durable checkpoints, approval
receipts, source export evidence, PostgreSQL backfill and cutover artifacts, and
an exportable run bundle.

## Why this use case

This is the kind of migration that is routinely handled with a brittle mix of:

- Oracle export scripts on a VM;
- change-window notes in tickets;
- ad hoc target-database SQL;
- hand-switched application routes;
- no durable evidence after the terminal session closes.

Torque is stronger when it owns the whole operational program:

- target-stack readiness;
- approval receipt;
- source read-only window;
- source export receipt;
- typed PostgreSQL restore/expand/backfill/verify/cutover/contract steps;
- app-route promotion;
- final verification and bundle export.

## Product API Shape

This is the product-level request shape the CLI, MCP bridge, or future control
plane API should accept for this workflow:

```json
{
  "goal": "migrate apex-prod from standalone Oracle to PostgreSQL in Kubernetes",
  "stackConfig": "./docs/showcase/oracle-postgres-k8s/stack.postgres.yaml",
  "approvalMode": "cab-required",
  "exportBundle": true,
  "inputs": {
    "sourceSystem": "apex-prod",
    "changeRequest": "CRQ-4242",
    "targetCluster": "prod-eu1",
    "targetNamespace": "data-platform",
    "targetPoolerService": "pgbouncer-rw.data-platform.svc.cluster.local:5432",
    "targetAppRoute": "api-shadow"
  }
}
```

The runnable demo below uses stack YAML directly because that is what Torque can
execute today.

## Run Graph

The demo graph is intentionally linear because this class of migration is
usually gated by maintenance-window safety, not by raw parallelism:

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

The target-stack nodes are action receipts in this demo. In a production stack
they can sit beside real Helm or operator-managed releases without changing the
rest of the graph.

## Agent Capabilities

The use case needs a small set of typed agent capabilities:

- `agent.k8s.stack.inspect`: confirm CNPG, the PostgreSQL cluster, the pooler,
  and the shadow app route are ready.
- `agent.change.approval.record`: persist the CAB or change-window approval
  receipt.
- `agent.oracle.apex.readonly`: freeze the Oracle or APEX source workload.
- `agent.oracle.export`: export a consistent Oracle manifest and attach it to
  the run.
- `agent.app.route.promote`: switch the application route to the PostgreSQL
  target stack.
- `agent.app.verify`: verify the promoted route after cutover.

Torque itself then owns the typed PostgreSQL program through:

- `db.restore-point`
- `db.schema-expand`
- `db.backfill`
- `db.verify`
- `db.cutover`
- `db.schema-contract`

## Evidence Bundle

After a successful run, `torque stack audit --include-artifacts` and
`torque stack export` should contain:

- readiness receipts for CNPG, PostgreSQL, PgBouncer, and the shadow app;
- the change-window approval receipt;
- the Oracle export manifest;
- `restore-point.json`
- `schema-expand.json`
- `backfill.json`
- `verify.json`
- `cutover.json`
- `schema-contract.json`
- the app-route promotion receipt;
- the post-cutover verification receipt;
- the portable `state.sqlite` ledger and bundle `manifest.json`.

The current runtime now records `action.script` output as durable artifacts, so
the source-system and routing phases survive export instead of living only in
stdout.

## Approval Flow

The runnable demo uses an `action.script` node named
`change-window-approved` to persist an approval receipt today.

That is deliberate:

- it makes the flow executable right now;
- it keeps the artifact in the run bundle;
- it can later be replaced by a typed `approval.manual` node without changing
  the rest of the migration graph.

## Demo Files

- [stack.postgres.yaml](./stack.postgres.yaml): realistic target shape for a
  PostgreSQL migration.
- [stack.sqlite.yaml](./stack.sqlite.yaml): local proof harness with the same
  run graph but SQLite as the target database.
- [scripts](./scripts): source-system, approval, readiness, and app-routing
  receipts used by the runnable demo.

## Run The Local Proof Harness

```bash
cd docs/showcase/oracle-postgres-k8s

export TORQUE_ORACLE_PG_DSN="$PWD/runtime/oracle-pg.sqlite"
cp stack.sqlite.yaml stack.yaml

torque stack plan --config ./stack.yaml
torque stack apply --config ./stack.yaml --yes
torque stack audit --config ./stack.yaml --include-artifacts --output json > ./runtime/audit.json
torque stack export --config ./stack.yaml --out ./runtime/oracle-postgres-run.tgz
```

## Run Against PostgreSQL

```bash
cd docs/showcase/oracle-postgres-k8s

export TORQUE_ORACLE_PG_DSN='postgres://postgres:postgres@127.0.0.1:5432/oracle_cutover?sslmode=disable'
cp stack.postgres.yaml stack.yaml

torque stack plan --config ./stack.yaml
torque stack apply --config ./stack.yaml --yes
torque stack audit --config ./stack.yaml --include-artifacts --output json > ./runtime/audit.json
torque stack export --config ./stack.yaml --out ./runtime/oracle-postgres-run.tgz
```

## Realistic Next Step

The next implementation step after this showcase is not a new YAML primitive. It
is a typed source-system edge such as `db.export.oracle` or
`db.stage-load.postgres`, so the Oracle side becomes as first-class as the
PostgreSQL side.
