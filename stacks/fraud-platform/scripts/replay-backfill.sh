#!/usr/bin/env bash
# Generated from the original fraud-platform Torque stack node: replay-backfill.
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

    redpanda_pod="$(k -n data get pod -l app=redpanda -o jsonpath='{.items[0].metadata.name}')"
    k -n data exec "${redpanda_pod}" -- rpk topic consume payments.raw -n 1 --offset start -X brokers=redpanda.data.svc.cluster.local:9092 >/tmp/torque-fraud-replay-raw.json
    test -s /tmp/torque-fraud-replay-raw.json

    run_id="replay-$(date -u +%Y%m%d%H%M%S)"
    wf="$(
      k -n argo create -f - -o jsonpath='{.metadata.name}' <<YAML
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: fraud-replay-backfill-
spec:
  entrypoint: spark-backfill
  serviceAccountName: argo
  volumes:
    - name: spark-batch-code
      configMap:
        name: spark-batch-code
  templates:
    - name: spark-backfill
      container:
        image: apache/spark:4.1.2-scala2.13-java17-python3-ubuntu
        command: ["/bin/bash", "-lc"]
        args:
          - |
            export HOME=/tmp
            mkdir -p /tmp/.ivy2
            python3 -m pip install --no-cache-dir boto3 clickhouse-connect
            aws_region="\${AWS_REGION:-\${AWS_DEFAULT_REGION:-us-east-1}}"
            spark_extra=(
              --conf "spark.jars.ivy=/tmp/.ivy2"
              --conf "spark.driver.extraJavaOptions=-Daws.region=\${aws_region}"
              --conf "spark.executor.extraJavaOptions=-Daws.region=\${aws_region}"
              --conf "spark.executorEnv.AWS_REGION=\${aws_region}"
              --conf "spark.executorEnv.AWS_DEFAULT_REGION=\${aws_region}"
              --conf "spark.executorEnv.AWS_ACCESS_KEY_ID=\${AWS_ACCESS_KEY_ID}"
              --conf "spark.executorEnv.AWS_SECRET_ACCESS_KEY=\${AWS_SECRET_ACCESS_KEY}"
            )
            if [[ -n "\${AWS_SESSION_TOKEN:-}" ]]; then
              spark_extra+=(--conf "spark.executorEnv.AWS_SESSION_TOKEN=\${AWS_SESSION_TOKEN}")
            fi
            /opt/spark/bin/spark-submit \
              --packages org.apache.iceberg:iceberg-spark-runtime-4.0_2.13:1.10.1,org.apache.iceberg:iceberg-aws-bundle:1.10.1 \
              --master spark://spark-master.ml.svc.cluster.local:7077 \
              "\${spark_extra[@]}" \
              --conf spark.driver.bindAddress=0.0.0.0 \
              --conf spark.driver.host=\$(hostname -i) \
              --conf spark.executor.memory=512m \
              --conf spark.driver.memory=512m \
              /opt/app/spark_batch.py
        envFrom:
          - secretRef:
              name: aws-s3
        env:
          - name: BACKFILL_RUN_ID
            value: ${run_id}
          - name: RAW_OBJECT_LIMIT
            value: "500"
          - name: DECISION_OBJECT_LIMIT
            value: "500"
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

    trino_pod="$(k -n data get pod -l app=trino -o jsonpath='{.items[0].metadata.name}')"
    backfill_rows="$(
      k -n data exec "${trino_pod}" -- trino \
        --server http://localhost:8080 \
        --catalog iceberg \
        --schema fraud \
        --output-format CSV \
        --execute "SELECT count(*) FROM batch_feature_summary WHERE run_id = '${run_id}'" |
        tr -d '\r"' |
        awk -F, '/^[0-9]+/{value=$1} END{print value+0}'
    )"
    [[ "${backfill_rows:-0}" -gt 0 ]] || { echo "no Iceberg backfill rows for run_id=${run_id}" >&2; exit 1; }
    echo "replay_raw_sample=$(head -c 300 /tmp/torque-fraud-replay-raw.json)"
    echo "backfill_run_id=${run_id}"
    echo "iceberg_backfill_batch_features=${backfill_rows}"
    ;;
  delete)
    true
    ;;
  *)
    echo "unknown mode: ${mode}" >&2
    exit 2
    ;;
esac
