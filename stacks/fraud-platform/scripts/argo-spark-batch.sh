#!/usr/bin/env bash
# Generated from the original fraud-platform Torque stack node: argo-spark-batch.
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
HOST_PUBLIC_IP="${TORQUE_LAB_PUBLIC_IP:?set TORQUE_LAB_PUBLIC_IP to the public host IP}"
k() { kubectl --kubeconfig "${KUBECONFIG_PATH}" "$@"; }
curl -fsS -X POST "http://${HOST_PUBLIC_IP}:30080/generate?count=50&fraud_rate=0.18"
wf="$(k -n argo create -f - -o jsonpath='{.metadata.name}' <<'YAML'
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: fraud-spark-batch-
spec:
  entrypoint: spark-batch
  serviceAccountName: argo
  volumes:
    - name: spark-batch-code
      configMap:
        name: spark-batch-code
  templates:
    - name: spark-batch
      container:
        image: apache/spark:4.1.2-scala2.13-java17-python3-ubuntu
        command: ["/bin/bash", "-lc"]
        args:
          - export HOME=/tmp && mkdir -p /tmp/.ivy2 && python3 -m pip install --no-cache-dir boto3 clickhouse-connect && /opt/spark/bin/spark-submit --packages org.apache.iceberg:iceberg-spark-runtime-4.0_2.13:1.10.1,org.apache.iceberg:iceberg-aws-bundle:1.10.1 --master spark://spark-master.ml.svc.cluster.local:7077 --conf spark.jars.ivy=/tmp/.ivy2 --conf spark.driver.bindAddress=0.0.0.0 --conf spark.driver.host=$(hostname -i) --conf spark.executor.memory=512m --conf spark.driver.memory=512m /opt/app/spark_batch.py
        envFrom:
          - secretRef:
              name: aws-s3
        env:
          - name: CLICKHOUSE_HOST
            value: signoz-clickhouse.observability.svc.cluster.local
          - name: CLICKHOUSE_PASSWORD
            value: torque-clickhouse
          - name: ICEBERG_REST_URI
            value: http://iceberg-rest.data.svc.cluster.local:8181
        volumeMounts:
          - name: spark-batch-code
            mountPath: /opt/app
        resources:
          requests:
            cpu: 300m
            memory: 768Mi
          limits:
            cpu: "1"
            memory: 1400Mi
YAML
)"
for attempt in $(seq 1 180); do
  phase="$(k -n argo get workflow "${wf}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  echo "workflow=${wf} phase=${phase}"
  [[ "${phase}" == "Succeeded" ]] && break
  [[ "${phase}" == "Failed" || "${phase}" == "Error" ]] && { k -n argo get workflow "${wf}" -o yaml; exit 1; }
  sleep 5
done
k -n argo get workflow "${wf}" -o wide
          ;;
        delete)
true          ;;
        *) echo "unknown mode: ${mode}" >&2; exit 2 ;;
      esac
