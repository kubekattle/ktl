#!/bin/sh
set -eu

runtime_dir="${TORQUE_RUNTIME_DIR:-./runtime}"
component="${TORQUE_COMPONENT:?TORQUE_COMPONENT is required}"
namespace="${TORQUE_NAMESPACE:-data-platform}"
service="${TORQUE_SERVICE:-$component}"
endpoint="${TORQUE_ENDPOINT:-$service.$namespace.svc.cluster.local}"
capability="${TORQUE_CAPABILITY:-ready}"
artifact="${runtime_dir}/${component}.json"
mode="${TORQUE_K8S_READY_MODE:-receipt}"

mkdir -p "$runtime_dir"

if [ "$mode" = "kubectl" ]; then
  kubectl_bin="${TORQUE_KUBECTL_BIN:-kubectl}"
  if ! command -v "$kubectl_bin" >/dev/null 2>&1; then
    if command -v k3s >/dev/null 2>&1; then
      kubectl_bin="k3s kubectl"
    else
      echo "kubectl mode requested but kubectl/k3s is unavailable" >&2
      exit 1
    fi
  fi
  if ! $kubectl_bin get namespace "$namespace" >/dev/null 2>&1; then
    echo "namespace $namespace is not ready" >&2
    exit 1
  fi
  if ! $kubectl_bin -n "$namespace" get service "$service" >/dev/null 2>&1; then
    echo "service $namespace/$service is not ready" >&2
    exit 1
  fi
  ready_addresses="$($kubectl_bin -n "$namespace" get endpoints "$service" -o jsonpath='{range .subsets[*].addresses[*]}{.ip}{" "}{end}' 2>/dev/null || true)"
  if [ -z "$ready_addresses" ]; then
    echo "service $namespace/$service has no ready endpoints" >&2
    exit 1
  fi
  endpoint_count="$(printf '%s\n' "$ready_addresses" | wc -w | tr -d ' ')"
  cat >"$artifact" <<EOF
{"component":"$component","namespace":"$namespace","service":"$service","endpoint":"$endpoint","capability":"$capability","status":"ready","mode":"kubectl","readyEndpoints":$endpoint_count}
EOF
  cat "$artifact"
  exit 0
fi

cat >"$artifact" <<EOF
{"component":"$component","namespace":"$namespace","service":"$service","endpoint":"$endpoint","capability":"$capability","status":"ready","mode":"receipt"}
EOF

cat "$artifact"
