# ktl Usage Examples

## Logs
```bash
# Tail every namespace quickly
ktl logs . --all-namespaces --tail=10

# Focus on prod namespace with highlighting
ktl logs 'checkout-.*' --namespace prod-payments --highlight ERROR --highlight timeout

# Filter by label and container while streaming events
ktl logs . \
  --namespace canary \
  --selector app=checkout \
  --container 'proxy.*' \
  --events

# View only Kubernetes events for rollout pods
ktl logs 'rollout-.*' --namespace blue --events-only --tail=0

# Stream node/system logs alongside pods
ktl logs 'checkout-.*' \
  --namespace prod-payments \
  --node-logs \
  --node-log /var/log/kubelet.log \
  --node-log /var/log/syslog
```

## Plan / Apply / Delete (Helm)
```bash
# Preview chart changes
ktl apply plan \
  --chart ./deploy/checkout \
  --release checkout \
  --namespace prod-payments \
  --values values/prod.yaml

# Write a shareable HTML plan
ktl apply plan \
  --chart ./deploy/checkout \
  --release checkout \
  --namespace prod-payments \
  --values values/prod.yaml \
  --format=html \
  --output dist/checkout-plan.html

# Apply with a live viewer + WebSocket event stream
ktl apply \
  --chart ./deploy/checkout \
  --release checkout \
  --namespace prod-payments \
  --values values/prod.yaml \
  --ui :8080 \
  --ws-listen :9086

# Delete with a live viewer + WebSocket event stream
ktl delete \
  --release checkout \
  --namespace prod-payments \
  --ui :8080 \
  --ws-listen :9087
```

## Build (BuildKit)
```bash
ktl build . --tag ghcr.io/example/checkout:latest --ws-listen :9085
ktl build login ghcr.io -u "$GITHUB_USER" --password-stdin
ktl build logout ghcr.io

# Cache intelligence as JSON (for CI tooling)
ktl build . --cache-intel-format json --cache-intel-output dist/cache-intel.json
```
