import json
import os
from datetime import datetime, timezone

import boto3
import clickhouse_connect
from pyspark.sql import SparkSession
from pyspark.sql import functions as F


bucket = os.environ["S3_BUCKET"]
region = os.getenv("AWS_DEFAULT_REGION", os.getenv("AWS_REGION", "us-east-1"))
run_id = os.getenv("ARGO_WORKFLOW_NAME", f"manual-{datetime.now(timezone.utc):%Y%m%d%H%M%S}")
clickhouse_host = os.getenv("CLICKHOUSE_HOST", "signoz-clickhouse.observability.svc.cluster.local")
clickhouse_password = os.getenv("CLICKHOUSE_PASSWORD", "torque-clickhouse")

s3 = boto3.client("s3", region_name=region)
prefix = os.getenv("RAW_PREFIX", "raw/payments/")
raw_object_limit = int(os.getenv("RAW_OBJECT_LIMIT", "300"))

paginator = s3.get_paginator("list_objects_v2")
raw_lines = []
pages = paginator.paginate(
    Bucket=bucket,
    Prefix=prefix,
    PaginationConfig={"MaxItems": raw_object_limit},
)
for page in pages:
    for item in page.get("Contents", []):
        if not item["Key"].endswith(".json"):
            continue
        body = s3.get_object(Bucket=bucket, Key=item["Key"])["Body"].read()
        raw_lines.append(body.decode().strip())

if not raw_lines:
    raise SystemExit(f"no raw payment objects found in s3://{bucket}/{prefix}")
print(f"downloaded {len(raw_lines)} raw payment objects from s3://{bucket}/{prefix}", flush=True)

spark = SparkSession.builder.appName("fraud-feature-batch").getOrCreate()
df = spark.read.json(spark.sparkContext.parallelize(raw_lines, min(4, len(raw_lines))))
summary = (
    df.groupBy("merchant_category", "country")
    .agg(
        F.count("*").alias("event_count"),
        F.round(F.avg("amount"), 2).alias("avg_amount"),
        F.round(F.max("amount"), 2).alias("max_amount"),
        F.round(F.avg("velocity_5m"), 2).alias("avg_velocity_5m"),
        F.sum("is_fraud_label").alias("fraud_label_count"),
    )
    .withColumn("run_id", F.lit(run_id))
    .withColumn("computed_at", F.current_timestamp())
)

rows = [row.asDict(recursive=True) for row in summary.collect()]
out_key = f"curated/fraud_features/run_id={run_id}/summary.json"
s3.put_object(
    Bucket=bucket,
    Key=out_key,
    Body=json.dumps(rows, default=str, sort_keys=True).encode() + b"\n",
    ContentType="application/json",
)

client = clickhouse_connect.get_client(
    host=clickhouse_host,
    port=8123,
    username="admin",
    password=clickhouse_password,
)
client.command("CREATE DATABASE IF NOT EXISTS fraud")
client.command(
    """
    CREATE TABLE IF NOT EXISTS fraud.batch_feature_summary
    (
      run_id String,
      computed_at DateTime,
      merchant_category String,
      country String,
      event_count UInt64,
      avg_amount Float64,
      max_amount Float64,
      avg_velocity_5m Float64,
      fraud_label_count UInt64
    )
    ENGINE = MergeTree
    ORDER BY (run_id, merchant_category, country)
    """
)
client.insert(
    "fraud.batch_feature_summary",
    [
        [
            run_id,
            row["computed_at"],
            row["merchant_category"],
            row["country"],
            row["event_count"],
            row["avg_amount"],
            row["max_amount"],
            row["avg_velocity_5m"],
            row["fraud_label_count"],
        ]
        for row in rows
    ],
    column_names=[
        "run_id",
        "computed_at",
        "merchant_category",
        "country",
        "event_count",
        "avg_amount",
        "max_amount",
        "avg_velocity_5m",
        "fraud_label_count",
    ],
)
print(json.dumps({"downloaded": len(raw_lines), "groups": len(rows), "s3_output": f"s3://{bucket}/{out_key}"}, sort_keys=True))
spark.stop()
