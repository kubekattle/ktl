import os
import time

import requests


API_URL = os.getenv("PAYMENTS_API_URL", "http://payments-api.apps.svc.cluster.local:8080")
COUNT = int(os.getenv("GENERATOR_BATCH_SIZE", "5"))
FRAUD_RATE = float(os.getenv("GENERATOR_FRAUD_RATE", "0.12"))
INTERVAL = float(os.getenv("GENERATOR_INTERVAL_SECONDS", "3"))


while True:
    try:
        response = requests.post(
            f"{API_URL}/generate",
            params={"count": COUNT, "fraud_rate": FRAUD_RATE},
            timeout=60,
        )
        print(response.status_code, response.text, flush=True)
    except Exception as exc:
        print(f"generator_error={exc}", flush=True)
    time.sleep(INTERVAL)
