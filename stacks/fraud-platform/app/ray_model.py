import math
import os

import ray
from ray import serve


MODEL_NAME = os.getenv("MODEL_NAME", "ray-serve-logistic-risk")
MODEL_VERSION = os.getenv("MODEL_VERSION", "v1")

ray.init(address="auto", ignore_reinit_error=True)
serve.start(http_options={"host": "0.0.0.0", "port": 8000})


@serve.deployment(ray_actor_options={"num_cpus": 0.1})
class FraudScorer:
    async def __call__(self, request):
        event = await request.json()
        amount = min(float(event.get("amount", 0.0)) / 1200.0, 1.0)
        velocity = min(float(event.get("velocity_5m", 0)) / 10.0, 1.0)
        country = 1.0 if event.get("country") != event.get("billing_country") else 0.0
        category = 1.0 if event.get("merchant_category") in {"electronics", "travel", "digital"} else 0.0
        linear_score = 2.8 * amount + 2.0 * velocity + 1.7 * country + 0.9 * category - 2.2
        probability = 1.0 / (1.0 + math.exp(-linear_score))
        return {
            "fraud_probability": round(probability, 4),
            "model": f"{MODEL_NAME}-{MODEL_VERSION}",
            "model_name": MODEL_NAME,
            "model_version": MODEL_VERSION,
            "features": {
                "amount": amount,
                "velocity_5m": velocity,
                "country_mismatch": country,
                "risky_category": category,
            },
        }


serve.run(FraudScorer.bind(), name="fraud-scorer", route_prefix="/score")
print("fraud scorer deployed", flush=True)
