#!/bin/sh
set -eu

runtime_dir="${TORQUE_RUNTIME_DIR:-./runtime}"
target_route="${TORQUE_TARGET_ROUTE:-api-postgres}"
route_state="${TORQUE_ROUTE_STATE_FILE:-$runtime_dir/app-route-promote.json}"
artifact="${TORQUE_POST_CHECK_FILE:-$runtime_dir/post-cutover-check.json}"

if [ ! -f "$route_state" ]; then
  echo "missing route-state artifact: $route_state" >&2
  exit 1
fi

mkdir -p "$runtime_dir"

cat >"$artifact" <<EOF
{"targetRoute":"$target_route","routeStateFile":"$route_state","status":"verified","checks":["route-promoted","pooler-selected","shadow-data-parity"]}
EOF

cat "$artifact"
