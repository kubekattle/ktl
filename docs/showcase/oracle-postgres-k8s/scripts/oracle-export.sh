#!/bin/sh
set -eu

runtime_dir="${TORQUE_RUNTIME_DIR:-./runtime}"
source_system="${TORQUE_SOURCE_SYSTEM:-apex-prod}"
schema_name="${TORQUE_EXPORT_SCHEMA:-APEX_CUSTOMERS}"
rows="${TORQUE_EXPORT_ROWS:-3}"
manifest="${TORQUE_ORACLE_EXPORT_FILE:-$runtime_dir/oracle-export.json}"
csv="${TORQUE_ORACLE_EXPORT_CSV:-$runtime_dir/oracle-export.csv}"

mkdir -p "$runtime_dir"

cat >"$csv" <<'EOF'
101,apex-admin@example.com,enterprise
102,billing@example.com,team
103,ops@example.com,starter
EOF

cat >"$manifest" <<EOF
{"sourceSystem":"$source_system","schema":"$schema_name","rows":$rows,"csv":"$csv","status":"consistent-export"}
EOF

cat "$manifest"
