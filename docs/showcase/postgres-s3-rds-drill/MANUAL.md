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

## Manual Apply Against Existing Resources

Build the local Torque binary first:

```bash
cd /Users/antonvkrylov/work/torque
make build
```

Export the resources the stackfile references:

```bash
export AWS_REGION=ap-south-1
export TORQUE_DEMO_SOURCE_PGHOST=127.0.0.1
export TORQUE_DEMO_SOURCE_PGPORT=5432
export TORQUE_DEMO_SOURCE_PGPASSWORD='<source-postgres-password>'
export TORQUE_DEMO_S3_BUCKET='<existing-s3-bucket>'
export TORQUE_DEMO_S3_PREFIX='postgres-rds-drill/manual'
export TORQUE_DEMO_RDS_ENDPOINT='<rds-endpoint>'
export TORQUE_DEMO_RDS_PASSWORD='<rds-master-password>'
```

The source database must contain database `keycloak` with table `realm`, and
the RDS target must be reachable from this machine.

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

1. `postgres.backup.run`: native `pg_dump` backup from source Postgres.
2. Uploads dump, manifest, catalog, and resumable upload session evidence to S3.
3. `postgres.backup.verify`: validates the backup artifact.
4. Removes the local dump so restore must fetch from S3.
5. `postgres.restore.drill`: restores into RDS database `keycloak_restore_drill`.
6. Runs SQL proof: `select count(*) from realm where name = 'torque'`.
