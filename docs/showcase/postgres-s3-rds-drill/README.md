# PostgreSQL S3 to RDS Restore Drill

This showcase is the database day-2 "killer demo":

1. create a disposable Keycloak-like PostgreSQL source container;
2. create a disposable S3 bucket and public RDS PostgreSQL instance;
3. run `postgres.backup.run` through Torque's native adapter;
4. upload the dump, manifest, and catalog to S3 with multipart evidence;
5. restore the backup into RDS;
6. verify the restored `torque` realm exists;
7. delete every AWS object through `torque stack delete`.

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

The stack writes proof under `docs/showcase/postgres-s3-rds-drill/runtime/`.
Local runtime files are ignored by Git.

For a step-by-step manual run, see [MANUAL.md](./MANUAL.md). Run it from the
repository root with
`torque stack apply --config docs/showcase/postgres-s3-rds-drill --yes`;
plain `torque apply` is the Helm apply surface and expects `--chart` and
`--release`. Cleanup is now stack-owned:
`torque stack delete --config docs/showcase/postgres-s3-rds-drill --yes`.
