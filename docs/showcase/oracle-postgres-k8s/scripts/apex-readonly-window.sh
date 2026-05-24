#!/bin/sh
set -eu

runtime_dir="${TORQUE_RUNTIME_DIR:-./runtime}"
source_system="${TORQUE_SOURCE_SYSTEM:-apex-prod}"
source_mode="${TORQUE_SOURCE_MODE:-readonly}"
artifact="${runtime_dir}/apex-readonly-window.json"

mkdir -p "$runtime_dir"

cat >"$artifact" <<EOF
{"sourceSystem":"$source_system","mode":"$source_mode","status":"frozen","scope":"source-write-path"}
EOF

cat "$artifact"
