import os
import time

def process_payment():
    print("Initializing payment processor...")
    time.sleep(1)
    # Simulate a bug where env var is missing
    # This will raise KeyError: 'PAYMENT_GATEWAY_KEY'
    api_key = os.environ["PAYMENT_GATEWAY_KEY"]
    print(f"Processing with {api_key}")

if __name__ == "__main__":
    print("Starting application...")
    process_payment()
