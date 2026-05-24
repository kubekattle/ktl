#!/usr/bin/env bash
# Generated from the original fraud-platform Torque stack node: verify-e2e.
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
. /tmp/torque-fraud-s3.env
curl -fsS "http://${HOST_PUBLIC_IP}:30080/healthz"
curl -fsS "http://${HOST_PUBLIC_IP}:3301/api/v1/health?live=1" || curl -fsS "http://${HOST_PUBLIC_IP}:3301/"
curl -fsS "http://${HOST_PUBLIC_IP}:8265/api/version"
curl -fsS "http://${HOST_PUBLIC_IP}:8080/json/"
curl -fsS "http://${HOST_PUBLIC_IP}:8081/overview"
curl -fsS "http://${HOST_PUBLIC_IP}:8082/v1/info"
api_pod="$(k -n apps get pod -l app=payments-api -o jsonpath='{.items[0].metadata.name}')"
s3_counts="$(k -n apps exec -i "${api_pod}" -- python - <<'PY'
import os
import boto3

bucket = os.environ["S3_BUCKET"]
s3 = boto3.client("s3", region_name=os.getenv("AWS_DEFAULT_REGION", os.getenv("AWS_REGION", "us-east-1")))

def count(prefix):
    total = 0
    paginator = s3.get_paginator("list_objects_v2")
    for page in paginator.paginate(Bucket=bucket, Prefix=prefix):
        total += len(page.get("Contents", []))
    return total

print(count("raw/payments/"), count("curated/fraud_features/"))
PY
)"
raw_count="$(awk '{print $1}' <<<"${s3_counts}")"
curated_count="$(awk '{print $2}' <<<"${s3_counts}")"
[[ "${raw_count}" -gt 0 ]] || { echo "no raw S3 payment objects" >&2; exit 1; }
[[ "${curated_count}" -gt 0 ]] || { echo "no curated Spark feature objects" >&2; exit 1; }
k -n apps exec -i "${api_pod}" -- python - <<'PY'
import os
import clickhouse_connect
client = clickhouse_connect.get_client(
  host=os.getenv("CLICKHOUSE_HOST", "signoz-clickhouse.observability.svc.cluster.local"),
  port=8123,
  username="admin",
  password=os.getenv("CLICKHOUSE_PASSWORD", "torque-clickhouse"),
)
decisions = client.query("SELECT count() FROM fraud.payment_decisions").result_rows[0][0]
batches = client.query("SELECT count() FROM fraud.batch_feature_summary").result_rows[0][0]
print({"clickhouse_decisions": decisions, "clickhouse_batch_rows": batches})
if decisions <= 0 or batches <= 0:
    raise SystemExit(1)
PY
k -n apps exec -i "${api_pod}" -- python - <<'PY'
import json
import urllib.request

base = "http://redpanda.data.svc.cluster.local:8081"
subjects = set(json.loads(urllib.request.urlopen(f"{base}/subjects", timeout=5).read()))
required = {"payments.raw-value", "payments.risk-value", "payments.decisions-value"}
missing = required - subjects
if missing:
    raise SystemExit(f"missing schema subjects: {sorted(missing)}")
compat = {}
for subject in sorted(required):
    payload = json.loads(urllib.request.urlopen(f"{base}/config/{subject}", timeout=5).read())
    compat[subject] = payload.get("compatibilityLevel", payload.get("compatibility"))
print({"schema_subjects": sorted(required), "compatibility": compat})
PY
trino_pod="$(k -n data get pod -l app=trino -o jsonpath='{.items[0].metadata.name}')"
trino_count() {
  local catalog="$1"
  local query="$2"
  k -n data exec "${trino_pod}" -- trino \
    --server http://localhost:8080 \
    --catalog "${catalog}" \
    --schema fraud \
    --output-format CSV \
    --execute "${query}" |
    tr -d '\r"' |
    awk -F, '/^[0-9]+/{value=$1} END{print value+0}'
}
trino_decisions="$(trino_count clickhouse "SELECT count(*) FROM payment_decisions")"
[[ "${trino_decisions:-0}" -gt 0 ]] || { echo "Trino could not query ClickHouse payment decisions" >&2; exit 1; }
iceberg_raw="$(trino_count iceberg "SELECT count(*) FROM raw_payments")"
iceberg_risk="$(trino_count iceberg "SELECT count(*) FROM risk_events")"
iceberg_batches="$(trino_count iceberg "SELECT count(*) FROM batch_feature_summary")"
[[ "${iceberg_raw:-0}" -gt 0 ]] || { echo "Trino could not query Iceberg raw payments" >&2; exit 1; }
[[ "${iceberg_risk:-0}" -gt 0 ]] || { echo "Trino could not query Iceberg risk events" >&2; exit 1; }
[[ "${iceberg_batches:-0}" -gt 0 ]] || { echo "Trino could not query Iceberg batch features" >&2; exit 1; }
redpanda_pod="$(k -n data get pod -l app=redpanda -o jsonpath='{.items[0].metadata.name}')"
risk_high_watermark=0
for attempt in $(seq 1 30); do
  risk_high_watermark="$(k -n data exec "${redpanda_pod}" -- rpk topic describe payments.risk -p -X brokers=redpanda.data.svc.cluster.local:9092 | awk '$1=="0"{print $6}')"
  [[ "${risk_high_watermark:-0}" -gt 0 ]] && break
  sleep 2
done
[[ "${risk_high_watermark:-0}" -gt 0 ]] || { echo "no Flink risk messages produced" >&2; exit 1; }
k -n data exec "${redpanda_pod}" -- rpk topic consume payments.risk -n 1 -X brokers=redpanda.data.svc.cluster.local:9092 >/tmp/torque-fraud-risk-message.json
test -s /tmp/torque-fraud-risk-message.json
echo "s3_raw_objects=${raw_count}"
echo "s3_curated_objects=${curated_count}"
echo "trino_payment_decisions=${trino_decisions}"
echo "iceberg_raw_payments=${iceberg_raw}"
echo "iceberg_risk_events=${iceberg_risk}"
echo "iceberg_batch_features=${iceberg_batches}"
echo "risk_high_watermark=${risk_high_watermark}"
echo "risk_message_sample=$(head -c 300 /tmp/torque-fraud-risk-message.json)"
          ;;
        delete)
true          ;;
        *) echo "unknown mode: ${mode}" >&2; exit 2 ;;
      esac
