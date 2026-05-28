# Manual Run: PostgreSQL S3 to RDS Restore Drill

This directory contains a Torque stack, not a Helm chart. Use `torque stack
apply`, not `torque apply`.

`torque apply` is the Kubernetes/Helm apply command and requires `--chart` and
`--release`. That is why this fails:

```bash
torque apply -f stack.yaml
```

Run this stack from the repository root with:

```bash
cd /Users/antonvkrylov/work/torque
torque stack apply --config docs/showcase/postgres-s3-rds-drill --yes
```

This creates the disposable source container, S3 bucket, RDS instance, backup,
restore drill, and local `runtime/manual.env`. Cleanup is also stack-owned:

```bash
torque stack delete --config docs/showcase/postgres-s3-rds-drill --yes
```

If you are already inside `docs/showcase/postgres-s3-rds-drill`, go back to the
repository root first because the stack's backup paths are root-relative:

```bash
cd ../../..
torque stack apply --config docs/showcase/postgres-s3-rds-drill --yes
```

## Fast Full E2E

The easiest path is the harness. It creates a disposable source PostgreSQL
container, S3 bucket, public RDS PostgreSQL instance, runs the stack, verifies
the restore, and then deletes the AWS resources.

```bash
cd /Users/antonvkrylov/work/torque

TORQUE_AWS_RDS_E2E_CONFIRM=1 \
AWS_REGION=ap-south-1 \
scripts/e2e/postgres-s3-rds-drill.sh
```

## Keep Resources For Manual Checks

The harness deletes resources by default after proof. To keep the stack-owned
resources for manual checks:

```bash
cd /Users/antonvkrylov/work/torque

TORQUE_AWS_RDS_E2E_CONFIRM=1 \
TORQUE_AWS_RDS_E2E_KEEP_RESOURCES=1 \
AWS_REGION=ap-south-1 \
scripts/e2e/postgres-s3-rds-drill.sh
```

Or run the stack directly and simply do not delete yet:

```bash
./bin/torque stack apply --config docs/showcase/postgres-s3-rds-drill --yes
```

The stack writes:

```text
docs/showcase/postgres-s3-rds-drill/runtime/manual.env
```

Load it only for manual AWS/psql inspection commands:

```bash
set -a
source docs/showcase/postgres-s3-rds-drill/runtime/manual.env
set +a
```

When done, destroy the stack-owned AWS resources and source container:

```bash
./bin/torque stack delete --config docs/showcase/postgres-s3-rds-drill --yes
```

## Manual Apply

Build the local Torque binary first:

```bash
cd /Users/antonvkrylov/work/torque
make build
```

Plan first:

```bash
./bin/torque stack plan --config docs/showcase/postgres-s3-rds-drill
```

Apply:

```bash
./bin/torque stack apply \
  --config docs/showcase/postgres-s3-rds-drill \
  --yes \
  --capture docs/showcase/postgres-s3-rds-drill/runtime/manual-run.sqlite
```

For live task-level progress in the TTY `DETAILS` panel, run the same command
with debug logging:

```bash
./bin/torque --log-level debug stack apply \
  --config docs/showcase/postgres-s3-rds-drill \
  --yes
```

Delete:

```bash
./bin/torque stack delete \
  --config docs/showcase/postgres-s3-rds-drill \
  --yes
```

Export audit evidence:

```bash
./bin/torque stack audit \
  --config docs/showcase/postgres-s3-rds-drill \
  --output json \
  --include-artifacts \
  > docs/showcase/postgres-s3-rds-drill/runtime/manual-audit.json
```

## Verify By Hand

Check that the backup object exists in S3:

```bash
aws s3api head-object \
  --bucket "$TORQUE_DEMO_S3_BUCKET" \
  --key "$TORQUE_DEMO_S3_PREFIX/base/keycloak/rds-drill/keycloak.dump" \
  --region "$AWS_REGION"
```

Check that the restore drill created the expected row in RDS:

```bash
PGPASSWORD="$TORQUE_DEMO_RDS_PASSWORD" PGSSLMODE=require psql \
  -h "$TORQUE_DEMO_RDS_ENDPOINT" \
  -p 5432 \
  -U torque_demo \
  -d keycloak_restore_drill \
  -At \
  -c "select count(*) from realm where name = 'torque';"
```

Expected output:

```text
1
```

## What The Stack Runs

The DAG in `stack.yaml` does this:

1. `host.command.run/demo-resources`: create source Postgres, S3, RDS, and `manual.env`.
2. `postgres.backup.run`: native `pg_dump` backup from source Postgres.
3. Uploads dump, manifest, catalog, and resumable upload session evidence to S3.
4. `postgres.backup.verify`: validates the backup artifact.
5. Removes the local dump so restore must fetch from S3.
6. `postgres.restore.drill`: restores into RDS database `keycloak_restore_drill`.
7. Runs SQL proof: `select count(*) from realm where name = 'torque'`.
8. `torque stack delete`: runs `demo-resources.deleteCommand` and proves AWS/Docker cleanup.
