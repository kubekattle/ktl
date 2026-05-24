#!/bin/sh
set -eu

runtime_dir="${TORQUE_RUNTIME_DIR:-./runtime}"
change_request="${TORQUE_CHANGE_REQUEST:-CRQ-0000}"
window="${TORQUE_CHANGE_WINDOW:-sat-2200z}"
artifact="${runtime_dir}/change-window-approved.json"

mkdir -p "$runtime_dir"

cat >"$artifact" <<EOF
{"changeRequest":"$change_request","window":"$window","status":"approved","mode":"cab-recorded"}
EOF

cat "$artifact"
