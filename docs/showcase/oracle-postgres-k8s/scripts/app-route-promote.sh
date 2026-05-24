#!/bin/sh
set -eu

runtime_dir="${TORQUE_RUNTIME_DIR:-./runtime}"
target_route="${TORQUE_TARGET_ROUTE:-api-postgres}"
pooler_service="${TORQUE_POOLER_SERVICE:-pgbouncer-rw.data-platform.svc.cluster.local:5432}"
artifact="${TORQUE_ROUTE_STATE_FILE:-$runtime_dir/app-route-promote.json}"

mkdir -p "$runtime_dir"

cat >"$artifact" <<EOF
{"targetRoute":"$target_route","poolerService":"$pooler_service","status":"promoted","mode":"postgres-primary"}
EOF

cat "$artifact"
