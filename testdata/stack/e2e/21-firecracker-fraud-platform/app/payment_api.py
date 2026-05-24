import json
import math
import os
import random
import threading
import time
import uuid
from datetime import datetime, timezone
from typing import Any

import boto3
import clickhouse_connect
import requests
from fastapi import FastAPI, HTTPException
from kafka import KafkaProducer
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from pydantic import BaseModel, Field


SERVICE_NAME = os.getenv("OTEL_SERVICE_NAME", "payments-api")
KAFKA_BOOTSTRAP = os.getenv("KAFKA_BOOTSTRAP", "redpanda.data.svc.cluster.local:9092")
RAW_TOPIC = os.getenv("RAW_TOPIC", "payments.raw")
DECISION_TOPIC = os.getenv("DECISION_TOPIC", "payments.decisions")
RAY_SCORE_URL = os.getenv("RAY_SCORE_URL", "http://ray-head.ml.svc.cluster.local:8000/score")
S3_BUCKET = os.environ["S3_BUCKET"]
AWS_REGION = os.getenv("AWS_DEFAULT_REGION", os.getenv("AWS_REGION", "us-east-1"))
CLICKHOUSE_HOST = os.getenv("CLICKHOUSE_HOST", "signoz-clickhouse.observability.svc.cluster.local")
CLICKHOUSE_PORT = int(os.getenv("CLICKHOUSE_PORT", "8123"))
CLICKHOUSE_USER = os.getenv("CLICKHOUSE_USER", "admin")
CLICKHOUSE_PASSWORD = os.getenv("CLICKHOUSE_PASSWORD", "torque-clickhouse")

resource = Resource.create({"service.name": SERVICE_NAME, "deployment.environment": "firecracker-lab"})
provider = TracerProvider(resource=resource)
endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
if endpoint:
    provider.add_span_processor(
        BatchSpanProcessor(OTLPSpanExporter(endpoint=endpoint.rstrip("/") + "/v1/traces"))
    )
trace.set_tracer_provider(provider)
tracer = trace.get_tracer(__name__)

app = FastAPI(title="Firecracker Fraud Payments API", version="1.0.0")
s3 = boto3.client("s3", region_name=AWS_REGION)
producer: KafkaProducer | None = None
clickhouse = None
clickhouse_lock = threading.Lock()

MERCHANTS = [
    ("m-101", "Northstar Grocery", "grocery"),
    ("m-204", "Metro Fuel", "fuel"),
    ("m-309", "Cloud Phone Store", "electronics"),
    ("m-412", "FlyNow Travel", "travel"),
    ("m-515", "GameStack Digital", "digital"),
    ("m-618", "CrossBorder Market", "marketplace"),
]
COUNTRIES = ["US", "US", "US", "CA", "GB", "DE", "BR", "SG"]
DEVICES = ["ios", "android", "web", "pos"]


class PaymentEvent(BaseModel):
    event_id: str = Field(default_factory=lambda: str(uuid.uuid4()))
    event_ts: str | None = None
    user_id: str
    account_id: str
    amount: float
    currency: str = "USD"
    merchant_id: str
    merchant_name: str
    merchant_category: str
    country: str
    billing_country: str = "US"
    device_type: str
    device_id: str
    velocity_5m: int
    is_fraud_label: int = 0


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds")


def json_bytes(payload: dict[str, Any]) -> bytes:
    return json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()


def make_producer() -> KafkaProducer:
    return KafkaProducer(
        bootstrap_servers=KAFKA_BOOTSTRAP,
        value_serializer=json_bytes,
        key_serializer=lambda value: value.encode(),
        linger_ms=20,
        retries=10,
    )


def make_clickhouse_client():
    return clickhouse_connect.get_client(
        host=CLICKHOUSE_HOST,
        port=CLICKHOUSE_PORT,
        username=CLICKHOUSE_USER,
        password=CLICKHOUSE_PASSWORD,
        connect_timeout=5,
        send_receive_timeout=10,
    )


def init_clickhouse() -> None:
    global clickhouse
    clickhouse = make_clickhouse_client()
    clickhouse.command("CREATE DATABASE IF NOT EXISTS fraud")
    clickhouse.command(
        """
        CREATE TABLE IF NOT EXISTS fraud.payment_decisions
        (
          event_ts DateTime64(3, 'UTC'),
          event_id String,
          user_id String,
          account_id String,
          amount Float64,
          merchant_id String,
          merchant_category String,
          country LowCardinality(String),
          billing_country LowCardinality(String),
          velocity_5m UInt16,
          rule_score Float64,
          ml_score Float64,
          decision LowCardinality(String),
          raw_json String
        )
        ENGINE = MergeTree
        ORDER BY (event_ts, event_id)
        """
    )


def event_to_s3(prefix: str, payload: dict[str, Any]) -> None:
    dt = datetime.fromisoformat(payload["event_ts"].replace("Z", "+00:00"))
    key = (
        f"{prefix}/dt={dt:%Y-%m-%d}/hour={dt:%H}/"
        f"{payload['event_id']}.json"
    )
    s3.put_object(
        Bucket=S3_BUCKET,
        Key=key,
        Body=json.dumps(payload, sort_keys=True).encode() + b"\n",
        ContentType="application/json",
    )


def synthetic_event(fraud_rate: float) -> PaymentEvent:
    merchant_id, merchant_name, category = random.choice(MERCHANTS)
    user_num = random.randint(1, 250)
    billing_country = random.choice(["US", "US", "US", "CA", "GB"])
    country = random.choice(COUNTRIES)
    amount = round(random.lognormvariate(3.5, 0.9), 2)
    velocity = random.randint(0, 4)
    fraud = random.random() < fraud_rate
    if fraud:
        amount = round(random.uniform(650, 2400), 2)
        velocity = random.randint(5, 14)
        country = random.choice(["BR", "SG", "DE", "GB"])
    return PaymentEvent(
        event_ts=now_iso(),
        user_id=f"user-{user_num:04d}",
        account_id=f"acct-{user_num // 2:04d}",
        amount=amount,
        merchant_id=merchant_id,
        merchant_name=merchant_name,
        merchant_category=category,
        country=country,
        billing_country=billing_country,
        device_type=random.choice(DEVICES),
        device_id=f"dev-{random.randint(1, 900):04d}",
        velocity_5m=velocity,
        is_fraud_label=1 if fraud else 0,
    )


def rule_score(event: dict[str, Any]) -> float:
    amount_component = min(float(event["amount"]) / 1200.0, 1.0)
    velocity_component = min(float(event["velocity_5m"]) / 10.0, 1.0)
    country_component = 1.0 if event["country"] != event["billing_country"] else 0.0
    risky_category = 1.0 if event["merchant_category"] in {"electronics", "travel", "digital"} else 0.0
    return round(
        0.42 * amount_component
        + 0.28 * velocity_component
        + 0.20 * country_component
        + 0.10 * risky_category,
        4,
    )


def model_score(event: dict[str, Any]) -> float:
    try:
        response = requests.post(RAY_SCORE_URL, json=event, timeout=2.5)
        response.raise_for_status()
        return float(response.json()["fraud_probability"])
    except Exception:
        score = rule_score(event)
        return round(1.0 / (1.0 + math.exp(-6.0 * (score - 0.55))), 4)


@app.on_event("startup")
def startup() -> None:
    global producer
    producer = make_producer()
    init_clickhouse()


@app.get("/healthz")
def healthz() -> dict[str, Any]:
    return {
        "ok": True,
        "kafka": KAFKA_BOOTSTRAP,
        "s3_bucket": S3_BUCKET,
        "ray_score_url": RAY_SCORE_URL,
        "clickhouse": CLICKHOUSE_HOST,
    }


@app.post("/events")
def ingest(event: PaymentEvent) -> dict[str, Any]:
    if producer is None or clickhouse is None:
        raise HTTPException(status_code=503, detail="dependencies are not ready")
    with tracer.start_as_current_span("payment.ingest") as span:
        payload = event.model_dump()
        payload["event_ts"] = payload["event_ts"] or now_iso()
        score_rule = rule_score(payload)
        score_ml = model_score(payload)
        decision = "decline" if max(score_rule, score_ml) >= 0.82 else "review" if max(score_rule, score_ml) >= 0.55 else "approve"
        decision_payload = {
            **payload,
            "rule_score": score_rule,
            "ml_score": round(score_ml, 4),
            "decision": decision,
            "decision_ts": now_iso(),
        }
        producer.send(RAW_TOPIC, key=payload["event_id"], value=payload).get(timeout=10)
        producer.send(DECISION_TOPIC, key=payload["event_id"], value=decision_payload).get(timeout=10)
        event_to_s3("raw/payments", payload)
        event_to_s3("decisions/payments", decision_payload)
        with clickhouse_lock:
            clickhouse.insert(
                "fraud.payment_decisions",
                [[
                    payload["event_ts"],
                    payload["event_id"],
                    payload["user_id"],
                    payload["account_id"],
                    payload["amount"],
                    payload["merchant_id"],
                    payload["merchant_category"],
                    payload["country"],
                    payload["billing_country"],
                    payload["velocity_5m"],
                    score_rule,
                    round(score_ml, 4),
                    decision,
                    json.dumps(decision_payload, sort_keys=True),
                ]],
                column_names=[
                    "event_ts",
                    "event_id",
                    "user_id",
                    "account_id",
                    "amount",
                    "merchant_id",
                    "merchant_category",
                    "country",
                    "billing_country",
                    "velocity_5m",
                    "rule_score",
                    "ml_score",
                    "decision",
                    "raw_json",
                ],
            )
        span.set_attribute("payment.event_id", payload["event_id"])
        span.set_attribute("payment.decision", decision)
        span.set_attribute("payment.amount", payload["amount"])
        return {"event_id": payload["event_id"], "decision": decision, "rule_score": score_rule, "ml_score": round(score_ml, 4)}


@app.post("/generate")
def generate(count: int = 25, fraud_rate: float = 0.12) -> dict[str, Any]:
    count = max(1, min(count, 1000))
    decisions: dict[str, int] = {"approve": 0, "review": 0, "decline": 0}
    started = time.time()
    for _ in range(count):
        result = ingest(synthetic_event(fraud_rate))
        decisions[result["decision"]] += 1
    return {"generated": count, "decisions": decisions, "seconds": round(time.time() - started, 3)}
