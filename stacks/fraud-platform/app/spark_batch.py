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
run_id = os.getenv("BACKFILL_RUN_ID", run_id)
clickhouse_host = os.getenv("CLICKHOUSE_HOST", "signoz-clickhouse.observability.svc.cluster.local")
clickhouse_password = os.getenv("CLICKHOUSE_PASSWORD", "torque-clickhouse")
iceberg_enabled = os.getenv("ICEBERG_ENABLED", "true").lower() not in {"0", "false", "no"}
iceberg_catalog = os.getenv("ICEBERG_CATALOG", "iceberg")
iceberg_rest_uri = os.getenv("ICEBERG_REST_URI", "http://iceberg-rest.data.svc.cluster.local:8181")
iceberg_warehouse = os.getenv("ICEBERG_WAREHOUSE", f"s3://{bucket}/iceberg/warehouse")

s3 = boto3.client("s3", region_name=region)
prefix = os.getenv("RAW_PREFIX", "raw/payments/")
decision_prefix = os.getenv("DECISION_PREFIX", "decisions/payments/")
raw_object_limit = int(os.getenv("RAW_OBJECT_LIMIT", "300"))
decision_object_limit = int(os.getenv("DECISION_OBJECT_LIMIT", str(raw_object_limit)))


def load_json_lines(path_prefix: str, limit: int) -> list[str]:
    paginator = s3.get_paginator("list_objects_v2")
    lines = []
    pages = paginator.paginate(
        Bucket=bucket,
        Prefix=path_prefix,
        PaginationConfig={"MaxItems": limit},
    )
    for page in pages:
        for item in page.get("Contents", []):
            if not item["Key"].endswith(".json"):
                continue
            body = s3.get_object(Bucket=bucket, Key=item["Key"])["Body"].read()
            lines.append(body.decode().strip())
    return lines


raw_lines = load_json_lines(prefix, raw_object_limit)
decision_lines = load_json_lines(decision_prefix, decision_object_limit)

if not raw_lines:
    raise SystemExit(f"no raw payment objects found in s3://{bucket}/{prefix}")
print(f"downloaded {len(raw_lines)} raw payment objects from s3://{bucket}/{prefix}", flush=True)
print(f"downloaded {len(decision_lines)} decision objects from s3://{bucket}/{decision_prefix}", flush=True)

spark_builder = SparkSession.builder.appName("fraud-feature-batch")
if iceberg_enabled:
    spark_builder = (
        spark_builder.config("spark.sql.extensions", "org.apache.iceberg.spark.extensions.IcebergSparkSessionExtensions")
        .config(f"spark.sql.catalog.{iceberg_catalog}", "org.apache.iceberg.spark.SparkCatalog")
        .config(f"spark.sql.catalog.{iceberg_catalog}.catalog-impl", "org.apache.iceberg.rest.RESTCatalog")
        .config(f"spark.sql.catalog.{iceberg_catalog}.uri", iceberg_rest_uri)
        .config(f"spark.sql.catalog.{iceberg_catalog}.warehouse", iceberg_warehouse)
        .config(f"spark.sql.catalog.{iceberg_catalog}.io-impl", "org.apache.iceberg.aws.s3.S3FileIO")
    )
spark = spark_builder.getOrCreate()
df = spark.read.json(spark.sparkContext.parallelize(raw_lines, min(4, len(raw_lines))))
risk_df = None
if decision_lines:
    risk_df = spark.read.json(spark.sparkContext.parallelize(decision_lines, min(4, len(decision_lines))))
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


def write_iceberg() -> dict[str, int]:
    if not iceberg_enabled:
        return {}
    spark.sql(f"CREATE NAMESPACE IF NOT EXISTS {iceberg_catalog}.fraud")
    spark.sql(
        f"""
        CREATE TABLE IF NOT EXISTS {iceberg_catalog}.fraud.raw_payments
        (
          event_ts STRING,
          event_id STRING,
          user_id STRING,
          account_id STRING,
          amount DOUBLE,
          currency STRING,
          merchant_id STRING,
          merchant_name STRING,
          merchant_category STRING,
          country STRING,
          billing_country STRING,
          device_type STRING,
          device_id STRING,
          velocity_5m INT,
          is_fraud_label INT,
          ingested_at TIMESTAMP
        )
        USING iceberg
        PARTITIONED BY (merchant_category)
        """
    )
    raw_out = df.select(
        F.col("event_ts").cast("string"),
        F.col("event_id").cast("string"),
        F.col("user_id").cast("string"),
        F.col("account_id").cast("string"),
        F.col("amount").cast("double"),
        F.col("currency").cast("string"),
        F.col("merchant_id").cast("string"),
        F.col("merchant_name").cast("string"),
        F.col("merchant_category").cast("string"),
        F.col("country").cast("string"),
        F.col("billing_country").cast("string"),
        F.col("device_type").cast("string"),
        F.col("device_id").cast("string"),
        F.col("velocity_5m").cast("int"),
        F.col("is_fraud_label").cast("int"),
        F.current_timestamp().alias("ingested_at"),
    )
    raw_out.writeTo(f"{iceberg_catalog}.fraud.raw_payments").append()

    risk_count = 0
    if risk_df is not None:
        spark.sql(
            f"""
            CREATE TABLE IF NOT EXISTS {iceberg_catalog}.fraud.risk_events
            (
              event_ts STRING,
              decision_ts STRING,
              event_id STRING,
              user_id STRING,
              account_id STRING,
              amount DOUBLE,
              merchant_category STRING,
              country STRING,
              billing_country STRING,
              velocity_5m INT,
              rule_score DOUBLE,
              ml_score DOUBLE,
              decision STRING,
              ingested_at TIMESTAMP
            )
            USING iceberg
            PARTITIONED BY (decision)
            """
        )
        risk_out = risk_df.select(
            F.col("event_ts").cast("string"),
            F.col("decision_ts").cast("string"),
            F.col("event_id").cast("string"),
            F.col("user_id").cast("string"),
            F.col("account_id").cast("string"),
            F.col("amount").cast("double"),
            F.col("merchant_category").cast("string"),
            F.col("country").cast("string"),
            F.col("billing_country").cast("string"),
            F.col("velocity_5m").cast("int"),
            F.col("rule_score").cast("double"),
            F.col("ml_score").cast("double"),
            F.col("decision").cast("string"),
            F.current_timestamp().alias("ingested_at"),
        )
        risk_out.writeTo(f"{iceberg_catalog}.fraud.risk_events").append()
        risk_count = risk_out.count()

    spark.sql(
        f"""
        CREATE TABLE IF NOT EXISTS {iceberg_catalog}.fraud.batch_feature_summary
        (
          run_id STRING,
          computed_at TIMESTAMP,
          merchant_category STRING,
          country STRING,
          event_count BIGINT,
          avg_amount DOUBLE,
          max_amount DOUBLE,
          avg_velocity_5m DOUBLE,
          fraud_label_count BIGINT
        )
        USING iceberg
        PARTITIONED BY (merchant_category)
        """
    )
    summary.writeTo(f"{iceberg_catalog}.fraud.batch_feature_summary").append()
    return {
        "iceberg_raw_rows": raw_out.count(),
        "iceberg_risk_rows": risk_count,
        "iceberg_batch_rows": len(rows),
    }


result = {"downloaded": len(raw_lines), "groups": len(rows), "s3_output": f"s3://{bucket}/{out_key}"}
result.update(write_iceberg())
print(json.dumps(result, sort_keys=True))
spark.stop()
