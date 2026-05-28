# PostgreSQL S3 to RDS Restore Drill

This showcase is the database day-2 "killer demo":

1. seed a small Keycloak-like PostgreSQL source database;
2. run `postgres.backup.run` through Torque's native adapter;
3. upload the dump, manifest, and catalog to S3 with multipart evidence;
4. restore the backup into a disposable public RDS PostgreSQL instance;
5. verify the restored `torque` realm exists;
6. audit the stack receipts;
7. delete every AWS object created by the harness.

Run the full AWS E2E:

```bash
TORQUE_AWS_RDS_E2E_CONFIRM=1 \
AWS_REGION=ap-south-1 \
scripts/e2e/postgres-s3-rds-drill.sh
```

The stackfile is [stack.yaml](./stack.yaml). It is intentionally env-backed so
the same desired state works with disposable buckets and RDS endpoints:

- `TORQUE_DEMO_SOURCE_PGHOST`
- `TORQUE_DEMO_SOURCE_PGPORT`
- `TORQUE_DEMO_SOURCE_PGPASSWORD`
- `TORQUE_DEMO_S3_BUCKET`
- `TORQUE_DEMO_S3_PREFIX`
- `TORQUE_DEMO_RDS_ENDPOINT`
- `TORQUE_DEMO_RDS_PASSWORD`
- `AWS_REGION`

The harness writes proof under `docs/showcase/postgres-s3-rds-drill/runtime/`
and removes AWS resources during cleanup. Local runtime files are ignored by
Git.

For a step-by-step manual run against existing resources, see
[MANUAL.md](./MANUAL.md). Run it from the repository root with
`torque stack apply --config docs/showcase/postgres-s3-rds-drill --yes`;
plain `torque apply` is the Helm apply surface and expects `--chart` and
`--release`.
