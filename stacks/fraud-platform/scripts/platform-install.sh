#!/usr/bin/env bash
# Generated from the original fraud-platform Torque stack node: platform-install.
# Keep runtime differences in environment/profile values, not by editing evidence output.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STACK_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
export TORQUE_STACK_ROOT="${TORQUE_STACK_ROOT:-${STACK_DIR}}"
export TORQUE_FRAUD_PROFILE="${TORQUE_FRAUD_PROFILE:-${TORQUE_STACK_PROFILE:-lab}}"
mode="${1:-apply}"
      case "${mode}" in
        apply)
set -euo pipefail
KUBECONFIG_PATH="${TORQUE_FRAUD_KUBECONFIG:-/tmp/torque-fraud-platform.kubeconfig}"
k() { kubectl --kubeconfig "${KUBECONFIG_PATH}" "$@"; }
k label node fc-00 fraud.torque.dev/workload=control --overwrite
k label node fc-01 fraud.torque.dev/workload=observability --overwrite
k label node fc-02 fraud.torque.dev/workload=events --overwrite
k label node fc-03 fraud.torque.dev/workload=processing --overwrite
k label node fc-04 fraud.torque.dev/workload=mlbatch --overwrite

k apply -n argo -f https://github.com/argoproj/argo-workflows/releases/download/v3.6.19/install.yaml
k -n argo apply -f - <<'YAML'
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: argo-workflow-taskresults
rules:
  - apiGroups: ["argoproj.io"]
    resources: ["workflowtaskresults"]
    verbs: ["create", "get", "list", "patch", "update", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: argo-workflow-taskresults
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: argo-workflow-taskresults
subjects:
  - kind: ServiceAccount
    name: argo
    namespace: argo
YAML
k -n argo patch deployment argo-server --type='json' -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--auth-mode=server"}]' >/dev/null 2>&1 || true
k -n argo patch service argo-server -p '{"spec":{"type":"NodePort","ports":[{"name":"web","port":2746,"targetPort":2746,"nodePort":32746}]}}'

tmp_values="$(mktemp)"
trap 'rm -f "${tmp_values}"' EXIT
cat >"${tmp_values}" <<'YAML'
global:
  storageClass: local-path
  clusterName: firecracker-fraud-platform
clickhouse:
  password: torque-clickhouse
  persistence:
    enabled: false
  nodeSelector:
    fraud.torque.dev/workload: observability
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      cpu: 1000m
      memory: 900Mi
  zookeeper:
    replicaCount: 1
    heapSize: 256
    persistence:
      enabled: false
    nodeSelector:
      fraud.torque.dev/workload: observability
    resources:
      requests:
        cpu: 50m
        memory: 128Mi
      limits:
        cpu: 500m
        memory: 384Mi
signoz:
  persistence:
    enabled: false
  service:
    type: NodePort
    nodePort: 32301
  nodeSelector:
    fraud.torque.dev/workload: observability
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 750m
      memory: 768Mi
otelCollector:
  nodeSelector:
    fraud.torque.dev/workload: observability
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi
telemetryStoreMigrator:
  nodeSelector:
    fraud.torque.dev/workload: observability
YAML
helm repo add signoz https://charts.signoz.io >/dev/null 2>&1 || true
helm repo update signoz >/dev/null
if ! helm upgrade --install signoz signoz/signoz -n observability --kubeconfig "${KUBECONFIG_PATH}" -f "${tmp_values}" --timeout 25m; then
  helm status signoz -n observability --kubeconfig "${KUBECONFIG_PATH}" | grep -q 'STATUS: deployed'
fi

for attempt in $(seq 1 180); do
  not_ready="$(k get pods -n observability --no-headers 2>/dev/null | awk '{split($2,ready,"/")} $3!="Running" && $3!="Completed"{print} $3=="Running" && ready[1]!=ready[2]{print}' || true)"
  if [[ -z "${not_ready}" ]]; then
    break
  fi
  if (( attempt % 12 == 0 )); then
    echo "waiting-observability attempt=${attempt}" >&2
    k get pods -n observability -o wide >&2 || true
  fi
  sleep 5
done
k get pods -n observability -o wide
          ;;
        delete)
set +e
KUBECONFIG_PATH="${TORQUE_FRAUD_KUBECONFIG:-/tmp/torque-fraud-platform.kubeconfig}"
helm uninstall signoz -n observability --kubeconfig "${KUBECONFIG_PATH}" >/dev/null 2>&1
kubectl --kubeconfig "${KUBECONFIG_PATH}" delete namespace argo observability --ignore-not-found --timeout=120s
          ;;
        *) echo "unknown mode: ${mode}" >&2; exit 2 ;;
      esac
