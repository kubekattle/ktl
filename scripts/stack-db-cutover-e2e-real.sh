#!/usr/bin/env bash
set -euo pipefail

KUBECONFIG_PATH="${KUBECONFIG_PATH:-}"
KUBE_CONTEXT="${KUBE_CONTEXT:-}"
NAMESPACE="${TORQUE_STACK_DB_CUTOVER_E2E_NAMESPACE:-torque-stack-db-cutover-e2e}"

if [[ "${TORQUE_STACK_DB_CUTOVER_E2E_CONFIRM:-}" != "1" ]]; then
  echo "Refusing to run without TORQUE_STACK_DB_CUTOVER_E2E_CONFIRM=1" >&2
  exit 2
fi
if [[ -z "${KUBECONFIG_PATH}" ]]; then
  echo "missing KUBECONFIG_PATH" >&2
  exit 2
fi
if [[ ! -f "${KUBECONFIG_PATH}" ]]; then
  echo "missing kubeconfig: ${KUBECONFIG_PATH}" >&2
  exit 2
fi
if ! command -v kubectl >/dev/null 2>&1; then
  echo "missing kubectl in PATH" >&2
  exit 2
fi

make -s build

kubectl_args=(--kubeconfig "${KUBECONFIG_PATH}")
torque_args=(--kubeconfig "${KUBECONFIG_PATH}")
if [[ -n "${KUBE_CONTEXT}" ]]; then
  kubectl_args+=(--context "${KUBE_CONTEXT}")
  torque_args+=(--context "${KUBE_CONTEXT}")
fi

tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-stack-db-cutover-e2e.XXXXXX")"
port_forward_postgres_pid=""
port_forward_mariadb_pid=""
cleanup() {
  if [[ -n "${port_forward_postgres_pid}" ]]; then
    kill "${port_forward_postgres_pid}" >/dev/null 2>&1 || true
    wait "${port_forward_postgres_pid}" 2>/dev/null || true
  fi
  if [[ -n "${port_forward_mariadb_pid}" ]]; then
    kill "${port_forward_mariadb_pid}" >/dev/null 2>&1 || true
    wait "${port_forward_mariadb_pid}" 2>/dev/null || true
  fi
  kubectl "${kubectl_args[@]}" delete ns "${NAMESPACE}" --ignore-not-found >/dev/null 2>&1 || true
  rm -rf "${tmp_root}"
}
trap cleanup EXIT

kubectl "${kubectl_args[@]}" delete ns "${NAMESPACE}" --ignore-not-found >/dev/null 2>&1 || true
kubectl "${kubectl_args[@]}" create ns "${NAMESPACE}" >/dev/null

cat <<'YAML' | kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: db-auth
type: Opaque
stringData:
  POSTGRES_PASSWORD: postgres
  MARIADB_ROOT_PASSWORD: mysql
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
spec:
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
        - name: postgres
          image: postgres:16
          env:
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: db-auth
                  key: POSTGRES_PASSWORD
          ports:
            - containerPort: 5432
          readinessProbe:
            exec:
              command:
                - sh
                - -c
                - pg_isready -U postgres
            initialDelaySeconds: 5
            periodSeconds: 3
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
spec:
  selector:
    app: postgres
  ports:
    - port: 5432
      targetPort: 5432
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mariadb
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mariadb
  template:
    metadata:
      labels:
        app: mariadb
    spec:
      containers:
        - name: mariadb
          image: mariadb:11
          env:
            - name: MARIADB_ROOT_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: db-auth
                  key: MARIADB_ROOT_PASSWORD
          ports:
            - containerPort: 3306
          readinessProbe:
            exec:
              command:
                - sh
                - -c
                - mariadb-admin ping -h 127.0.0.1 -uroot -p"$MARIADB_ROOT_PASSWORD"
            initialDelaySeconds: 10
            periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: mariadb
spec:
  selector:
    app: mariadb
  ports:
    - port: 3306
      targetPort: 3306
YAML

kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" rollout status deploy/postgres --timeout=180s >/dev/null
kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" rollout status deploy/mariadb --timeout=240s >/dev/null

kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" port-forward svc/postgres 15432:5432 >/dev/null 2>&1 &
port_forward_postgres_pid="$!"
kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" port-forward svc/mariadb 13306:3306 >/dev/null 2>&1 &
port_forward_mariadb_pid="$!"
sleep 5

export TORQUE_DB_DSN_POSTGRES='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable'
export TORQUE_DB_DSN_MYSQL='root:mysql@tcp(127.0.0.1:13306)/mysql'

stack_root="${tmp_root}/stack"
mkdir -p "${stack_root}"
cat >"${stack_root}/stack.yaml" <<'YAML'
apiVersion: torque.dev/v1
kind: Stack
name: db-cutover-e2e
defaults:
  cluster:
    name: local
releases:
  - name: precheck-postgres
    kind: action.script
    action:
      idempotent: true
      apply:
        command: ["sh", "-c", "echo postgres > ./precheck-postgres.txt"]

  - name: restore-postgres
    kind: db.restore-point
    needs: [precheck-postgres]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN_POSTGRES
      restorePointSQL: "CREATE TABLE IF NOT EXISTS restore_points (marker TEXT PRIMARY KEY, created_at TEXT NOT NULL); INSERT INTO restore_points(marker, created_at) VALUES ('before-program', 'now') ON CONFLICT (marker) DO NOTHING;"
      verifySQL: "SELECT COUNT(*) > 0, (SELECT COUNT(*) FROM restore_points) FROM restore_points WHERE marker = 'before-program';"

  - name: expand-postgres
    kind: db.schema-expand
    needs: [restore-postgres]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN_POSTGRES
      expandSQL: "CREATE TABLE IF NOT EXISTS source_users (id INTEGER PRIMARY KEY, name TEXT NOT NULL); CREATE TABLE IF NOT EXISTS shadow_users (id INTEGER PRIMARY KEY, name TEXT NOT NULL); CREATE TABLE IF NOT EXISTS cutover_flags (name TEXT PRIMARY KEY, live BOOLEAN NOT NULL DEFAULT FALSE, verified BOOLEAN NOT NULL DEFAULT FALSE, contracted BOOLEAN NOT NULL DEFAULT FALSE); INSERT INTO source_users(id, name) VALUES (1, 'a'), (2, 'b'), (3, 'c'), (4, 'd'), (5, 'e') ON CONFLICT (id) DO NOTHING; INSERT INTO cutover_flags(name, live, verified, contracted) VALUES ('api', FALSE, FALSE, FALSE) ON CONFLICT (name) DO NOTHING;"
      verifySQL: "SELECT EXISTS(SELECT 1 FROM source_users WHERE id = 5), (SELECT COUNT(*) FROM source_users);"

  - name: backfill-postgres
    kind: db.backfill
    needs: [expand-postgres]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN_POSTGRES
      verifySQL: "SELECT (SELECT COUNT(*) FROM shadow_users) = (SELECT COUNT(*) FROM source_users), (SELECT COUNT(*) FROM shadow_users);"
      backfill:
        checkpointTable: torque_backfill_state
        checkpointKey: postgres-program
        startSQL: "SELECT COALESCE(MIN(id), 1) - 1 FROM source_users;"
        endSQL: "SELECT COALESCE(MAX(id), 0) FROM source_users;"
        batchSQL: "INSERT INTO shadow_users(id, name) SELECT id, name FROM source_users WHERE id > {{.cursor_start}} AND id <= {{.cursor_end}} ON CONFLICT(id) DO NOTHING;"
        batchSize: 2

  - name: verify-postgres
    kind: db.verify
    needs: [backfill-postgres]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN_POSTGRES
      verifySQL: "SELECT (SELECT COUNT(*) FROM shadow_users) = (SELECT COUNT(*) FROM source_users), (SELECT COUNT(*) FROM shadow_users);"

  - name: cutover-postgres
    kind: db.cutover
    needs: [verify-postgres]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN_POSTGRES
      metadataTable: torque_cutover_state
      prepareSQL: "UPDATE cutover_flags SET live = FALSE, verified = FALSE WHERE name = 'api';"
      commitSQL: "UPDATE cutover_flags SET live = TRUE WHERE name = 'api';"
      verifySQL: "SELECT live, verified FROM cutover_flags WHERE name = 'api';"
      finalizeSQL: "UPDATE cutover_flags SET verified = TRUE WHERE name = 'api';"

  - name: contract-postgres
    kind: db.schema-contract
    needs: [cutover-postgres]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN_POSTGRES
      contractSQL: "UPDATE cutover_flags SET contracted = TRUE WHERE name = 'api'; CREATE TABLE IF NOT EXISTS contract_log(entry TEXT PRIMARY KEY); INSERT INTO contract_log(entry) VALUES ('contract-complete') ON CONFLICT (entry) DO NOTHING;"
      verifySQL: "SELECT contracted, live, verified FROM cutover_flags WHERE name = 'api';"

  - name: precheck-mariadb
    kind: action.script
    action:
      idempotent: true
      apply:
        command: ["sh", "-c", "echo mariadb > ./precheck-mariadb.txt"]

  - name: restore-mariadb
    kind: db.restore-point
    needs: [precheck-mariadb]
    database:
      driver: mariadb
      dsnEnv: TORQUE_DB_DSN_MYSQL
      restorePointSQL: "CREATE TABLE IF NOT EXISTS restore_points (marker VARCHAR(64) PRIMARY KEY, created_at VARCHAR(64) NOT NULL); INSERT INTO restore_points(marker, created_at) VALUES ('before-program', 'now') ON DUPLICATE KEY UPDATE marker = VALUES(marker);"
      verifySQL: "SELECT COUNT(*) > 0, (SELECT COUNT(*) FROM restore_points) FROM restore_points WHERE marker = 'before-program';"

  - name: expand-mariadb
    kind: db.schema-expand
    needs: [restore-mariadb]
    database:
      driver: mariadb
      dsnEnv: TORQUE_DB_DSN_MYSQL
      expandSQL: "CREATE TABLE IF NOT EXISTS source_users (id INTEGER PRIMARY KEY, name VARCHAR(255) NOT NULL); CREATE TABLE IF NOT EXISTS shadow_users (id INTEGER PRIMARY KEY, name VARCHAR(255) NOT NULL); CREATE TABLE IF NOT EXISTS cutover_flags (name VARCHAR(64) PRIMARY KEY, live BOOLEAN NOT NULL DEFAULT FALSE, verified BOOLEAN NOT NULL DEFAULT FALSE, contracted BOOLEAN NOT NULL DEFAULT FALSE); INSERT INTO source_users(id, name) VALUES (1, 'a'), (2, 'b'), (3, 'c'), (4, 'd'), (5, 'e') ON DUPLICATE KEY UPDATE name = VALUES(name); INSERT INTO cutover_flags(name, live, verified, contracted) VALUES ('api', FALSE, FALSE, FALSE) ON DUPLICATE KEY UPDATE name = VALUES(name);"
      verifySQL: "SELECT EXISTS(SELECT 1 FROM source_users WHERE id = 5), (SELECT COUNT(*) FROM source_users);"

  - name: backfill-mariadb
    kind: db.backfill
    needs: [expand-mariadb]
    database:
      driver: mariadb
      dsnEnv: TORQUE_DB_DSN_MYSQL
      verifySQL: "SELECT (SELECT COUNT(*) FROM shadow_users) = (SELECT COUNT(*) FROM source_users), (SELECT COUNT(*) FROM shadow_users);"
      backfill:
        checkpointTable: torque_backfill_state
        checkpointKey: mariadb-program
        startSQL: "SELECT COALESCE(MIN(id), 1) - 1 FROM source_users;"
        endSQL: "SELECT COALESCE(MAX(id), 0) FROM source_users;"
        batchSQL: "INSERT INTO shadow_users(id, name) SELECT id, name FROM source_users WHERE id > {{.cursor_start}} AND id <= {{.cursor_end}} ON DUPLICATE KEY UPDATE name = VALUES(name);"
        batchSize: 2

  - name: verify-mariadb
    kind: db.verify
    needs: [backfill-mariadb]
    database:
      driver: mariadb
      dsnEnv: TORQUE_DB_DSN_MYSQL
      verifySQL: "SELECT (SELECT COUNT(*) FROM shadow_users) = (SELECT COUNT(*) FROM source_users), (SELECT COUNT(*) FROM shadow_users);"

  - name: cutover-mariadb
    kind: db.cutover
    needs: [verify-mariadb]
    database:
      driver: mariadb
      dsnEnv: TORQUE_DB_DSN_MYSQL
      metadataTable: torque_cutover_state
      prepareSQL: "UPDATE cutover_flags SET live = FALSE, verified = FALSE WHERE name = 'api';"
      commitSQL: "UPDATE cutover_flags SET live = TRUE WHERE name = 'api';"
      verifySQL: "SELECT live, verified FROM cutover_flags WHERE name = 'api';"
      finalizeSQL: "UPDATE cutover_flags SET verified = TRUE WHERE name = 'api';"

  - name: contract-mariadb
    kind: db.schema-contract
    needs: [cutover-mariadb]
    database:
      driver: mariadb
      dsnEnv: TORQUE_DB_DSN_MYSQL
      contractSQL: "UPDATE cutover_flags SET contracted = TRUE WHERE name = 'api'; CREATE TABLE IF NOT EXISTS contract_log(entry VARCHAR(64) PRIMARY KEY); INSERT INTO contract_log(entry) VALUES ('contract-complete') ON DUPLICATE KEY UPDATE entry = VALUES(entry);"
      verifySQL: "SELECT contracted, live, verified FROM cutover_flags WHERE name = 'api';"
YAML

./bin/torque "${torque_args[@]}" stack plan --config "${stack_root}" --output json >/dev/null
./bin/torque "${torque_args[@]}" stack apply --config "${stack_root}" --yes >/dev/null
./bin/torque "${torque_args[@]}" stack apply --config "${stack_root}" --yes >/dev/null
./bin/torque "${torque_args[@]}" stack audit --config "${stack_root}" --output json >"${tmp_root}/audit.json"
./bin/torque "${torque_args[@]}" stack export --config "${stack_root}" --out "${tmp_root}/stack-export.tgz" >/dev/null

if [[ ! -f "${stack_root}/precheck-postgres.txt" ]]; then
  echo "missing precheck-postgres.txt after action.script node" >&2
  exit 1
fi
if [[ ! -f "${stack_root}/precheck-mariadb.txt" ]]; then
  echo "missing precheck-mariadb.txt after action.script node" >&2
  exit 1
fi

postgres_pod="$(kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" get pods -l app=postgres -o jsonpath='{.items[0].metadata.name}')"
postgres_state="$(kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" exec "${postgres_pod}" -- sh -c "PGPASSWORD=postgres psql -U postgres -d postgres -tAc \"SELECT live::text || ',' || verified::text || ',' || contracted::text FROM cutover_flags WHERE name = 'api';\"")"
postgres_state="$(echo "${postgres_state}" | tr -d '[:space:]')"
if [[ "${postgres_state}" != "true,true,true" ]]; then
  echo "unexpected postgres cutover_flags state: ${postgres_state}" >&2
  exit 1
fi

postgres_meta="$(kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" exec "${postgres_pod}" -- sh -c "PGPASSWORD=postgres psql -U postgres -d postgres -tAc \"SELECT phase || ',' || phase_status FROM torque_cutover_state WHERE object_id = 'local/default/cutover-postgres';\"")"
postgres_meta="$(echo "${postgres_meta}" | tr -d '[:space:]')"
if [[ "${postgres_meta}" != "finalize,success" ]]; then
  echo "unexpected postgres torque_cutover_state row: ${postgres_meta}" >&2
  exit 1
fi
postgres_backfill="$(kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" exec "${postgres_pod}" -- sh -c "PGPASSWORD=postgres psql -U postgres -d postgres -tAc \"SELECT current_cursor::text || ',' || phase_status FROM torque_backfill_state WHERE object_id = 'local/default/backfill-postgres';\"")"
postgres_backfill="$(echo "${postgres_backfill}" | tr -d '[:space:]')"
if [[ "${postgres_backfill}" != "5,success" ]]; then
  echo "unexpected postgres backfill state: ${postgres_backfill}" >&2
  exit 1
fi
postgres_shadow_count="$(kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" exec "${postgres_pod}" -- sh -c "PGPASSWORD=postgres psql -U postgres -d postgres -tAc \"SELECT COUNT(*) FROM shadow_users;\"")"
postgres_shadow_count="$(echo "${postgres_shadow_count}" | tr -d '[:space:]')"
if [[ "${postgres_shadow_count}" != "5" ]]; then
  echo "unexpected postgres shadow_users count: ${postgres_shadow_count}" >&2
  exit 1
fi

mariadb_pod="$(kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" get pods -l app=mariadb -o jsonpath='{.items[0].metadata.name}')"
mariadb_state="$(kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" exec "${mariadb_pod}" -- sh -c "mariadb -uroot -pmysql -Nse \"SELECT live, verified, contracted FROM cutover_flags WHERE name = 'api';\" mysql | tr '\t' ','")"
mariadb_state="$(echo "${mariadb_state}" | tr -d '[:space:]')"
if [[ "${mariadb_state}" != "1,1,1" ]]; then
  echo "unexpected mariadb cutover_flags state: ${mariadb_state}" >&2
  exit 1
fi

mariadb_meta="$(kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" exec "${mariadb_pod}" -- sh -c "mariadb -uroot -pmysql -Nse \"SELECT phase, phase_status FROM torque_cutover_state WHERE object_id = 'local/default/cutover-mariadb';\" mysql | tr '\t' ','")"
mariadb_meta="$(echo "${mariadb_meta}" | tr -d '[:space:]')"
if [[ "${mariadb_meta}" != "finalize,success" ]]; then
  echo "unexpected mariadb torque_cutover_state row: ${mariadb_meta}" >&2
  exit 1
fi
mariadb_backfill="$(kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" exec "${mariadb_pod}" -- sh -c "mariadb -uroot -pmysql -Nse \"SELECT current_cursor, phase_status FROM torque_backfill_state WHERE object_id = 'local/default/backfill-mariadb';\" mysql | tr '\t' ','")"
mariadb_backfill="$(echo "${mariadb_backfill}" | tr -d '[:space:]')"
if [[ "${mariadb_backfill}" != "5,success" ]]; then
  echo "unexpected mariadb backfill state: ${mariadb_backfill}" >&2
  exit 1
fi
mariadb_shadow_count="$(kubectl "${kubectl_args[@]}" -n "${NAMESPACE}" exec "${mariadb_pod}" -- sh -c "mariadb -uroot -pmysql -Nse \"SELECT COUNT(*) FROM shadow_users;\" mysql")"
mariadb_shadow_count="$(echo "${mariadb_shadow_count}" | tr -d '[:space:]')"
if [[ "${mariadb_shadow_count}" != "5" ]]; then
  echo "unexpected mariadb shadow_users count: ${mariadb_shadow_count}" >&2
  exit 1
fi

if ! grep -q '"backfill.json"' "${tmp_root}/audit.json"; then
  echo "missing backfill.json artifact in stack audit output" >&2
  exit 1
fi
if ! grep -q '"cutover.json"' "${tmp_root}/audit.json"; then
  echo "missing cutover.json artifact in stack audit output" >&2
  exit 1
fi
if [[ ! -f "${tmp_root}/stack-export.tgz" ]]; then
  echo "missing stack export bundle" >&2
  exit 1
fi

echo "stack db program e2e passed (postgres + mariadb)"
