#!/usr/bin/env bash
# Generated from the original fraud-platform Torque stack node: workloads-install.
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
ROOT="${TORQUE_STACK_ROOT}"
k() { kubectl --kubeconfig "${KUBECONFIG_PATH}" "$@"; }
secret_value() {
  k -n data get secret aws-s3 -o "jsonpath={.data.${1}}" |
    python3 -c 'import base64, sys; sys.stdout.write(base64.b64decode(sys.stdin.read()).decode())'
}
secret_value_or_empty() {
  k -n data get secret aws-s3 -o "jsonpath={.data.${1}}" 2>/dev/null |
    python3 -c 'import base64, sys; data=sys.stdin.read(); sys.stdout.write(base64.b64decode(data).decode() if data else "")'
}

iceberg_bucket="$(secret_value S3_BUCKET)"
iceberg_region="$(secret_value_or_empty AWS_REGION)"
if [[ -z "${iceberg_region}" ]]; then
  iceberg_region="$(secret_value AWS_DEFAULT_REGION)"
fi
tmp_iceberg_catalog="$(mktemp)"
cat >"${tmp_iceberg_catalog}" <<EOF
connector.name=iceberg
iceberg.catalog.type=rest
iceberg.rest-catalog.uri=http://iceberg-rest.data.svc.cluster.local:8181
iceberg.rest-catalog.warehouse=s3://${iceberg_bucket}/iceberg/warehouse
iceberg.file-format=PARQUET
fs.s3.enabled=true
s3.region=${iceberg_region}
EOF
k -n data create configmap trino-iceberg-catalog --from-file=iceberg.properties="${tmp_iceberg_catalog}" --dry-run=client -o yaml | k apply -f -
rm -f "${tmp_iceberg_catalog}"

k -n apps create configmap payments-api-code --from-file=payment_api.py="${ROOT}/app/payment_api.py" --dry-run=client -o yaml | k apply -f -
k -n apps create configmap payments-generator-code --from-file=generator.py="${ROOT}/app/generator.py" --dry-run=client -o yaml | k apply -f -
k -n ml create configmap ray-model-code --from-file=ray_model.py="${ROOT}/app/ray_model.py" --dry-run=client -o yaml | k apply -f -
k -n argo create configmap spark-batch-code --from-file=spark_batch.py="${ROOT}/app/spark_batch.py" --dry-run=client -o yaml | k apply -f -
k -n data delete job redpanda-topic-init --ignore-not-found --wait=false >/dev/null 2>&1 || true
k -n data delete job fraud-data-contracts --ignore-not-found --wait=false >/dev/null 2>&1 || true
k -n ml delete job ray-serve-deploy --ignore-not-found --wait=false >/dev/null 2>&1 || true

k apply -f - <<'YAML'
apiVersion: v1
kind: Service
metadata:
  name: redpanda
  namespace: data
spec:
  selector:
    app: redpanda
  ports:
    - name: kafka
      port: 9092
      targetPort: 9092
    - name: admin
      port: 9644
      targetPort: 9644
    - name: registry
      port: 8081
      targetPort: 8081
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redpanda
  namespace: data
spec:
  serviceName: redpanda
  replicas: 1
  selector:
    matchLabels:
      app: redpanda
  template:
    metadata:
      labels:
        app: redpanda
    spec:
      nodeSelector:
        fraud.torque.dev/workload: events
      containers:
        - name: redpanda
          image: redpandadata/redpanda:v26.1.9
          args:
            - redpanda
            - start
            - --smp=1
            - --memory=768M
            - --reserve-memory=0M
            - --overprovisioned
            - --node-id=0
            - --check=false
            - --kafka-addr=PLAINTEXT://0.0.0.0:9092
            - --advertise-kafka-addr=PLAINTEXT://redpanda.data.svc.cluster.local:9092
            - --schema-registry-addr=0.0.0.0:8081
            - --rpc-addr=0.0.0.0:33145
            - --advertise-rpc-addr=redpanda-0.redpanda.data.svc.cluster.local:33145
          ports:
            - containerPort: 9092
              name: kafka
            - containerPort: 9644
              name: admin
            - containerPort: 8081
              name: registry
          resources:
            requests:
              cpu: 200m
              memory: 512Mi
            limits:
              cpu: "1"
              memory: 1000Mi
          readinessProbe:
            httpGet:
              path: /v1/status/ready
              port: 9644
            initialDelaySeconds: 20
            periodSeconds: 5
---
apiVersion: batch/v1
kind: Job
metadata:
  name: redpanda-topic-init
  namespace: data
spec:
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: topic-init
          image: redpandadata/redpanda:v26.1.9
          command: ["/bin/bash", "-lc"]
          args:
            - |
              for topic in payments.raw payments.risk payments.decisions payments.raw.replay; do
                rpk topic create "${topic}" -X brokers=redpanda.data.svc.cluster.local:9092 || true
              done
---
apiVersion: batch/v1
kind: Job
metadata:
  name: fraud-data-contracts
  namespace: data
spec:
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: register-contracts
          image: python:3.12-slim
          command: ["/bin/bash", "-lc"]
          args:
            - |
              python - <<'PY'
              import json
              import time
              import urllib.error
              import urllib.request

              base = "http://redpanda.data.svc.cluster.local:8081"
              headers = {
                  "content-type": "application/vnd.schemaregistry.v1+json",
                  "accept": "application/vnd.schemaregistry.v1+json",
              }

              def request(method, path, payload=None):
                  data = None if payload is None else json.dumps(payload).encode()
                  req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
                  with urllib.request.urlopen(req, timeout=5) as resp:
                      body = resp.read().decode()
                      return json.loads(body) if body else {}

              for _ in range(120):
                  try:
                      print(request("GET", "/schemas/types"), flush=True)
                      break
                  except Exception as exc:
                      last = exc
                      time.sleep(2)
              else:
                  raise RuntimeError(f"schema registry not ready: {last}")

              common = {
                  "$schema": "http://json-schema.org/draft-07/schema#",
                  "type": "object",
                  "additionalProperties": True,
                  "required": [
                      "event_id",
                      "event_ts",
                      "user_id",
                      "account_id",
                      "amount",
                      "merchant_id",
                      "merchant_category",
                      "country",
                      "billing_country",
                      "velocity_5m",
                  ],
                  "properties": {
                      "event_id": {"type": "string"},
                      "event_ts": {"type": "string"},
                      "user_id": {"type": "string"},
                      "account_id": {"type": "string"},
                      "amount": {"type": "number"},
                      "currency": {"type": "string"},
                      "merchant_id": {"type": "string"},
                      "merchant_name": {"type": "string"},
                      "merchant_category": {"type": "string"},
                      "country": {"type": "string"},
                      "billing_country": {"type": "string"},
                      "device_type": {"type": "string"},
                      "device_id": {"type": "string"},
                      "velocity_5m": {"type": "integer"},
                      "is_fraud_label": {"type": "integer"},
                  },
              }
              risk = json.loads(json.dumps(common))
              risk["required"] = common["required"] + ["rule_risk", "rule_decision"]
              risk["properties"].update(
                  {
                      "rule_risk": {"type": "number"},
                      "rule_decision": {"type": "string", "enum": ["approve", "review", "decline"]},
                  }
              )
              decisions = json.loads(json.dumps(common))
              decisions["required"] = common["required"] + ["rule_score", "ml_score", "decision", "decision_ts"]
              decisions["properties"].update(
                  {
                      "rule_score": {"type": "number"},
                      "ml_score": {"type": "number"},
                      "decision": {"type": "string", "enum": ["approve", "review", "decline"]},
                      "decision_ts": {"type": "string"},
                  }
              )
              subjects = {
                  "payments.raw-value": common,
                  "payments.risk-value": risk,
                  "payments.decisions-value": decisions,
              }
              for subject, schema in subjects.items():
                  request("PUT", f"/config/{subject}", {"compatibility": "BACKWARD"})
                  response = request(
                      "POST",
                      f"/subjects/{subject}/versions",
                      {
                          "schemaType": "JSON",
                          "schema": json.dumps(schema, sort_keys=True),
                          "metadata": {
                              "properties": {
                                  "owner": "fraud-platform",
                                  "domain": "payments-risk",
                                  "tier": "lab-contract",
                              }
                          },
                      },
                  )
                  print({subject: response}, flush=True)
              PY
---
apiVersion: v1
kind: Service
metadata:
  name: iceberg-rest
  namespace: data
spec:
  selector:
    app: iceberg-rest
  ports:
    - name: http
      port: 8181
      targetPort: 8181
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: iceberg-rest
  namespace: data
spec:
  replicas: 1
  selector:
    matchLabels:
      app: iceberg-rest
  template:
    metadata:
      labels:
        app: iceberg-rest
    spec:
      nodeSelector:
        fraud.torque.dev/workload: analytics
      containers:
        - name: catalog
          image: tabulario/iceberg-rest:1.6.0
          envFrom:
            - secretRef:
                name: aws-s3
          env:
            - name: CATALOG_WAREHOUSE
              value: s3://$(S3_BUCKET)/iceberg/warehouse
            - name: CATALOG_IO__IMPL
              value: org.apache.iceberg.aws.s3.S3FileIO
            - name: CATALOG_S3_REGION
              value: $(AWS_REGION)
          ports:
            - containerPort: 8181
              name: http
          resources:
            requests:
              cpu: 100m
              memory: 384Mi
            limits:
              cpu: "1"
              memory: 768Mi
          readinessProbe:
            httpGet:
              path: /v1/config
              port: 8181
            initialDelaySeconds: 15
            periodSeconds: 5
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: trino-config
  namespace: data
data:
  config.properties: |
    coordinator=true
    node-scheduler.include-coordinator=true
    http-server.http.port=8080
    discovery.uri=http://localhost:8080
    query.max-memory=384MB
    query.max-memory-per-node=256MB
    memory.heap-headroom-per-node=128MB
  node.properties: |
    node.environment=fraud_lab
    node.id=trino-fraud-lab
    node.data-dir=/tmp/trino
  jvm.config: |
    -server
    -Xmx768M
    -XX:+ExplicitGCInvokesConcurrent
    -XX:+ExitOnOutOfMemoryError
    -Dfile.encoding=UTF-8
    --add-modules=jdk.incubator.vector
  log.properties: |
    io.trino=INFO
  clickhouse.properties: |
    connector.name=clickhouse
    connection-url=jdbc:clickhouse://signoz-clickhouse.observability.svc.cluster.local:8123/
    connection-user=admin
    connection-password=torque-clickhouse
---
apiVersion: v1
kind: Service
metadata:
  name: trino
  namespace: data
spec:
  type: NodePort
  selector:
    app: trino
  ports:
    - name: http
      port: 8080
      targetPort: 8080
      nodePort: 32082
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: trino
  namespace: data
spec:
  replicas: 1
  selector:
    matchLabels:
      app: trino
  template:
    metadata:
      labels:
        app: trino
    spec:
      nodeSelector:
        fraud.torque.dev/workload: analytics
      volumes:
        - name: trino-config
          configMap:
            name: trino-config
        - name: trino-clickhouse-catalog
          configMap:
            name: trino-config
            items:
              - key: clickhouse.properties
                path: clickhouse.properties
        - name: trino-iceberg-catalog
          configMap:
            name: trino-iceberg-catalog
      containers:
        - name: trino
          image: trinodb/trino:481
          envFrom:
            - secretRef:
                name: aws-s3
          ports:
            - containerPort: 8080
              name: http
          volumeMounts:
            - name: trino-config
              mountPath: /etc/trino/config.properties
              subPath: config.properties
            - name: trino-config
              mountPath: /etc/trino/node.properties
              subPath: node.properties
            - name: trino-config
              mountPath: /etc/trino/jvm.config
              subPath: jvm.config
            - name: trino-config
              mountPath: /etc/trino/log.properties
              subPath: log.properties
            - name: trino-clickhouse-catalog
              mountPath: /etc/trino/catalog/clickhouse.properties
              subPath: clickhouse.properties
            - name: trino-iceberg-catalog
              mountPath: /etc/trino/catalog/iceberg.properties
              subPath: iceberg.properties
          resources:
            requests:
              cpu: 250m
              memory: 768Mi
            limits:
              cpu: "1"
              memory: 1200Mi
          readinessProbe:
            httpGet:
              path: /v1/info
              port: 8080
            initialDelaySeconds: 30
            periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: ray-head
  namespace: ml
spec:
  type: NodePort
  selector:
    app: ray-head
  ports:
    - name: gcs
      port: 6379
      targetPort: 6379
    - name: dashboard
      port: 8265
      targetPort: 8265
      nodePort: 32665
    - name: client
      port: 10001
      targetPort: 10001
      nodePort: 32001
    - name: serve
      port: 8000
      targetPort: 8000
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ray-head
  namespace: ml
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: ray-head
  template:
    metadata:
      labels:
        app: ray-head
    spec:
      nodeSelector:
        fraud.torque.dev/workload: mlbatch
      volumes:
        - name: ray-model-code
          configMap:
            name: ray-model-code
      containers:
        - name: ray
          image: rayproject/ray:2.55.1-py312-cpu
          command: ["/bin/bash", "-lc"]
          args:
            - |
              ray start --head --node-ip-address=$(hostname -i) --port=6379 --dashboard-host=0.0.0.0 --dashboard-port=8265 --ray-client-server-port=10001 --num-cpus=1 --object-store-memory=100000000 --disable-usage-stats
              python /opt/app/ray_model.py
              exec tail -f /dev/null
          env:
            - name: RAY_memory_usage_threshold
              value: "0.99"
            - name: MODEL_NAME
              value: ray-serve-logistic-risk
            - name: MODEL_VERSION
              value: v1
          ports:
            - containerPort: 6379
            - containerPort: 8265
            - containerPort: 10001
            - containerPort: 8000
          resources:
            requests:
              cpu: 300m
              memory: 900Mi
            limits:
              cpu: "1"
              memory: 1700Mi
          volumeMounts:
            - name: ray-model-code
              mountPath: /opt/app
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ray-worker
  namespace: ml
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ray-worker
  template:
    metadata:
      labels:
        app: ray-worker
    spec:
      nodeSelector:
        fraud.torque.dev/workload: control
      containers:
        - name: ray
          image: rayproject/ray:2.55.1-py312-cpu
          command: ["/bin/bash", "-lc"]
          args:
            - until getent hosts ray-head.ml.svc.cluster.local; do sleep 2; done; ray start --address=ray-head.ml.svc.cluster.local:6379 --num-cpus=1 --object-store-memory=100000000 --disable-usage-stats --block
          resources:
            requests:
              cpu: 300m
              memory: 500Mi
            limits:
              cpu: "1"
              memory: 900Mi
---
apiVersion: batch/v1
kind: Job
metadata:
  name: ray-serve-deploy
  namespace: ml
spec:
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: deploy
          image: python:3.12-slim
          command: ["/bin/bash", "-lc"]
          args:
            - |
              python - <<'PY'
              import json
              import time
              import urllib.request

              payload = json.dumps({
                  "amount": 900,
                  "velocity_5m": 6,
                  "country": "US",
                  "billing_country": "CA",
                  "merchant_category": "travel",
              }).encode()
              for _ in range(120):
                  req = urllib.request.Request(
                      "http://ray-head.ml.svc.cluster.local:8000/score",
                      data=payload,
                      headers={"content-type": "application/json"},
                      method="POST",
                  )
                  try:
                      with urllib.request.urlopen(req, timeout=3) as resp:
                          body = resp.read().decode()
                          if resp.status == 200 and "fraud_probability" in body and "model_version" in body:
                              print(body, flush=True)
                              raise SystemExit(0)
                  except Exception as exc:
                      last = exc
                  time.sleep(2)
              raise RuntimeError(f"ray serve score endpoint not ready: {last}")
              PY
---
apiVersion: v1
kind: Service
metadata:
  name: spark-master
  namespace: ml
spec:
  type: NodePort
  selector:
    app: spark-master
  ports:
    - name: master
      port: 7077
      targetPort: 7077
    - name: ui
      port: 8080
      targetPort: 8080
      nodePort: 32080
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spark-master
  namespace: ml
spec:
  replicas: 1
  selector:
    matchLabels:
      app: spark-master
  template:
    metadata:
      labels:
        app: spark-master
    spec:
      hostname: spark-master
      nodeSelector:
        fraud.torque.dev/workload: mlbatch
      containers:
        - name: spark-master
          image: apache/spark:4.1.2-scala2.13-java17-python3-ubuntu
          command: ["/opt/spark/bin/spark-class"]
          args: ["org.apache.spark.deploy.master.Master", "--host", "spark-master", "--port", "7077", "--webui-port", "8080"]
          env:
            - name: SPARK_DAEMON_MEMORY
              value: 384m
            - name: SPARK_MASTER_HOST
              value: spark-master
            - name: SPARK_MASTER_PORT
              value: "7077"
          ports:
            - containerPort: 7077
            - containerPort: 8080
          resources:
            requests:
              cpu: 200m
              memory: 384Mi
            limits:
              cpu: "1"
              memory: 768Mi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spark-worker
  namespace: ml
spec:
  replicas: 1
  selector:
    matchLabels:
      app: spark-worker
  template:
    metadata:
      labels:
        app: spark-worker
    spec:
      nodeSelector:
        fraud.torque.dev/workload: processing
      containers:
        - name: spark-worker
          image: apache/spark:4.1.2-scala2.13-java17-python3-ubuntu
          command: ["/opt/spark/bin/spark-class"]
          args: ["org.apache.spark.deploy.worker.Worker", "spark://spark-master.ml.svc.cluster.local:7077", "--cores", "1", "--memory", "512m", "--webui-port", "8081"]
          env:
            - name: SPARK_DAEMON_MEMORY
              value: 256m
          resources:
            requests:
              cpu: 200m
              memory: 384Mi
            limits:
              cpu: "1"
              memory: 768Mi
---
apiVersion: v1
kind: Service
metadata:
  name: flink-jobmanager
  namespace: stream
spec:
  type: NodePort
  selector:
    app: flink
    component: jobmanager
  ports:
    - name: rpc
      port: 6123
      targetPort: 6123
    - name: blob
      port: 6124
      targetPort: 6124
    - name: ui
      port: 8081
      targetPort: 8081
      nodePort: 32081
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: flink-jobmanager
  namespace: stream
spec:
  replicas: 1
  selector:
    matchLabels:
      app: flink
      component: jobmanager
  template:
    metadata:
      labels:
        app: flink
        component: jobmanager
    spec:
      nodeSelector:
        fraud.torque.dev/workload: control
      containers:
        - name: jobmanager
          image: flink:1.20.1-scala_2.12-java17
          securityContext:
            runAsUser: 0
          command: ["/bin/bash", "-lc"]
          args:
            - wget -q -O /opt/flink/lib/flink-sql-connector-kafka-3.3.0-1.20.jar https://repo1.maven.org/maven2/org/apache/flink/flink-sql-connector-kafka/3.3.0-1.20/flink-sql-connector-kafka-3.3.0-1.20.jar && exec /docker-entrypoint.sh jobmanager
          env:
            - name: JOB_MANAGER_RPC_ADDRESS
              value: flink-jobmanager
            - name: FLINK_PROPERTIES
              value: |
                jobmanager.rpc.address: flink-jobmanager
                jobmanager.memory.process.size: 1024m
                taskmanager.memory.process.size: 1200m
                taskmanager.memory.task.heap.size: 256m
                taskmanager.memory.managed.size: 128m
                taskmanager.numberOfTaskSlots: 1
                parallelism.default: 1
          ports:
            - containerPort: 6123
            - containerPort: 8081
          resources:
            requests:
              cpu: 200m
              memory: 512Mi
            limits:
              cpu: "1"
              memory: 1200Mi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: flink-taskmanager
  namespace: stream
spec:
  replicas: 1
  selector:
    matchLabels:
      app: flink
      component: taskmanager
  template:
    metadata:
      labels:
        app: flink
        component: taskmanager
    spec:
      nodeSelector:
        fraud.torque.dev/workload: processing
      containers:
        - name: taskmanager
          image: flink:1.20.1-scala_2.12-java17
          securityContext:
            runAsUser: 0
          command: ["/bin/bash", "-lc"]
          args:
            - wget -q -O /opt/flink/lib/flink-sql-connector-kafka-3.3.0-1.20.jar https://repo1.maven.org/maven2/org/apache/flink/flink-sql-connector-kafka/3.3.0-1.20/flink-sql-connector-kafka-3.3.0-1.20.jar && exec /docker-entrypoint.sh taskmanager
          env:
            - name: JOB_MANAGER_RPC_ADDRESS
              value: flink-jobmanager
            - name: FLINK_PROPERTIES
              value: |
                jobmanager.rpc.address: flink-jobmanager
                jobmanager.memory.process.size: 1024m
                taskmanager.memory.process.size: 1200m
                taskmanager.memory.task.heap.size: 256m
                taskmanager.memory.managed.size: 128m
                taskmanager.numberOfTaskSlots: 1
                parallelism.default: 1
          resources:
            requests:
              cpu: 200m
              memory: 900Mi
            limits:
              cpu: "1"
              memory: 1400Mi
---
apiVersion: v1
kind: Service
metadata:
  name: payments-api
  namespace: apps
spec:
  type: NodePort
  selector:
    app: payments-api
  ports:
    - name: http
      port: 8080
      targetPort: 8080
      nodePort: 30080
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payments-api
  namespace: apps
spec:
  replicas: 1
  selector:
    matchLabels:
      app: payments-api
  template:
    metadata:
      labels:
        app: payments-api
    spec:
      nodeSelector:
        fraud.torque.dev/workload: events
      volumes:
        - name: payments-api-code
          configMap:
            name: payments-api-code
      containers:
        - name: api
          image: python:3.12-slim
          command: ["/bin/bash", "-lc"]
          args:
            - python -m pip install --no-cache-dir fastapi uvicorn boto3 kafka-python requests clickhouse-connect opentelemetry-sdk opentelemetry-exporter-otlp-proto-http >/tmp/pip.log 2>&1 && exec uvicorn payment_api:app --host 0.0.0.0 --port 8080 --app-dir /opt/app
          envFrom:
            - secretRef:
                name: aws-s3
          env:
            - name: KAFKA_BOOTSTRAP
              value: redpanda.data.svc.cluster.local:9092
            - name: RAY_SCORE_URL
              value: http://ray-head.ml.svc.cluster.local:8000/score
            - name: CLICKHOUSE_HOST
              value: signoz-clickhouse.observability.svc.cluster.local
            - name: CLICKHOUSE_PASSWORD
              value: torque-clickhouse
            - name: OTEL_SERVICE_NAME
              value: payments-api
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: http://signoz-otel-collector.observability.svc.cluster.local:4318
          ports:
            - containerPort: 8080
          volumeMounts:
            - name: payments-api-code
              mountPath: /opt/app
          resources:
            requests:
              cpu: 150m
              memory: 256Mi
            limits:
              cpu: "1"
              memory: 768Mi
          readinessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 30
            periodSeconds: 5
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payments-generator
  namespace: apps
spec:
  replicas: 1
  selector:
    matchLabels:
      app: payments-generator
  template:
    metadata:
      labels:
        app: payments-generator
    spec:
      nodeSelector:
        fraud.torque.dev/workload: events
      volumes:
        - name: payments-generator-code
          configMap:
            name: payments-generator-code
      containers:
        - name: generator
          image: python:3.12-slim
          command: ["/bin/bash", "-lc"]
          args:
            - python -m pip install --no-cache-dir requests >/tmp/pip.log 2>&1 && exec python /opt/app/generator.py
          env:
            - name: PAYMENTS_API_URL
              value: http://payments-api.apps.svc.cluster.local:8080
            - name: GENERATOR_BATCH_SIZE
              value: "3"
            - name: GENERATOR_INTERVAL_SECONDS
              value: "10"
          volumeMounts:
            - name: payments-generator-code
              mountPath: /opt/app
          resources:
            requests:
              cpu: 50m
              memory: 96Mi
            limits:
              cpu: 300m
              memory: 256Mi
YAML

k -n apps rollout restart deployment/payments-api deployment/payments-generator
k -n ml rollout restart deployment/ray-head

k -n apps rollout status deployment/payments-api --timeout=10m
k -n apps rollout status deployment/payments-generator --timeout=10m
k -n data wait --for=condition=Ready pod/redpanda-0 --timeout=10m
k -n data wait --for=condition=complete job/redpanda-topic-init --timeout=5m
k -n data wait --for=condition=complete job/fraud-data-contracts --timeout=5m
k -n data rollout status deployment/iceberg-rest --timeout=10m
k -n data rollout status deployment/trino --timeout=10m
k -n ml rollout status deployment/ray-head --timeout=10m
k -n ml rollout status deployment/ray-worker --timeout=10m
k -n ml wait --for=condition=complete job/ray-serve-deploy --timeout=10m
k -n ml rollout status deployment/spark-master --timeout=10m
k -n ml rollout status deployment/spark-worker --timeout=10m
k -n stream rollout status deployment/flink-jobmanager --timeout=10m
k -n stream rollout status deployment/flink-taskmanager --timeout=10m

jm="$(k -n stream get pod -l app=flink,component=jobmanager -o jsonpath='{.items[0].metadata.name}')"
sql="$(mktemp)"
trap 'rm -f "${sql}"' EXIT
cat >"${sql}" <<'SQL'
CREATE TABLE raw_payments (
  event_id STRING,
  event_ts STRING,
  user_id STRING,
  account_id STRING,
  amount DOUBLE,
  merchant_id STRING,
  merchant_name STRING,
  merchant_category STRING,
  country STRING,
  billing_country STRING,
  device_type STRING,
  device_id STRING,
  velocity_5m INT,
  is_fraud_label INT
) WITH (
  'connector' = 'kafka',
  'topic' = 'payments.raw',
  'properties.bootstrap.servers' = 'redpanda.data.svc.cluster.local:9092',
  'properties.group.id' = 'flink-risk-rules',
  'scan.startup.mode' = 'earliest-offset',
  'scan.bounded.mode' = 'latest-offset',
  'format' = 'json',
  'json.ignore-parse-errors' = 'true'
);
CREATE TABLE risk_events (
  event_id STRING,
  event_ts STRING,
  user_id STRING,
  amount DOUBLE,
  merchant_category STRING,
  country STRING,
  billing_country STRING,
  velocity_5m INT,
  rule_risk DOUBLE,
  rule_decision STRING
) WITH (
  'connector' = 'kafka',
  'topic' = 'payments.risk',
  'properties.bootstrap.servers' = 'redpanda.data.svc.cluster.local:9092',
  'format' = 'json'
);
INSERT INTO risk_events
SELECT
  event_id,
  event_ts,
  user_id,
  amount,
  merchant_category,
  country,
  billing_country,
  velocity_5m,
  CASE WHEN amount >= 800 OR velocity_5m >= 5 OR country <> billing_country THEN 1.0 ELSE 0.0 END,
  CASE WHEN amount >= 800 OR velocity_5m >= 5 OR country <> billing_country THEN 'review' ELSE 'approve' END
FROM raw_payments;
SQL
k -n stream exec -i "${jm}" -- /bin/bash -lc 'cat >/tmp/risk.sql' <"${sql}"
k -n stream exec "${jm}" -- /opt/flink/bin/sql-client.sh -f /tmp/risk.sql
k -n apps rollout status deployment/payments-api --timeout=15m
k -n apps rollout status deployment/payments-generator --timeout=5m
k get pods -A -o wide
          ;;
        delete)
set +e
KUBECONFIG_PATH="${TORQUE_FRAUD_KUBECONFIG:-/tmp/torque-fraud-platform.kubeconfig}"
kubectl --kubeconfig "${KUBECONFIG_PATH}" delete namespace apps data stream ml --ignore-not-found --timeout=180s
          ;;
        *) echo "unknown mode: ${mode}" >&2; exit 2 ;;
      esac
