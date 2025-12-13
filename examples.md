# ktl Usage Examples

The following scenarios illustrate how to combine `ktl` commands and flags when exploring cluster activity or running diagnostics. Adjust namespaces, selectors, and resource names to match your environment.

## Example 1 - Tail every namespace quickly
```bash
ktl logs . --all-namespaces --tail=10
```

## Example 2 - Focus on prod namespace with highlighting
```bash
ktl logs 'checkout-.*' --namespace prod-payments --highlight ERROR --highlight timeout
```

## Example 3 - Filter by label and container while streaming events
```bash
ktl logs . \
  --namespace canary \
  --selector app=checkout \
  --container 'proxy.*' \
  --events
```

## Example 4 - View only Kubernetes events for rollout pods
```bash
ktl logs 'rollout-.*' --namespace blue --events-only --tail=0
```

## Example 5 - Follow pods created by a deployment
```bash
ktl logs deployment/nginx --namespace edge
```

## Example 6 - Stream kube-system without historical logs
```bash
ktl logs . --namespace kube-system --tail=0
```

## Example 7 - Fetch the full history for a crashing pod
```bash
ktl logs crashloop-api --namespace staging --tail=-1 --follow=false
```

## Example 8 - Show logs newer than 15 minutes
```bash
ktl logs auth --namespace corp-sec --since=15m
```

## Example 9 - Snapshot the last 5 minutes and sort by time
```bash
ktl logs . --all-namespaces --since=5m --no-follow --only-log-lines | sort -k4
```

## Example 10 - Focus on pods scheduled to a specific node
```bash
ktl logs . --all-namespaces --field-selector spec.nodeName=kind-control-plane
```

## Example 11 - Show pods that are not ready
```bash
ktl logs . --namespace qa --condition ready=false --tail=0
```

## Example 12 - Inspect unscheduled pods cluster-wide
```bash
ktl logs . --all-namespaces --condition scheduled=false --tail=0
```

## Example 13 - Filter by label selector across namespaces
```bash
ktl logs . --all-namespaces --selector app=frontend
```

## Example 14 - Exclude Istio sidecars from a namespace
```bash
ktl logs . --namespace staging --exclude-container istio-proxy
```

## Example 15 - Skip noisy kube-apiserver pods
```bash
ktl logs . --namespace kube-system --exclude-pod kube-apiserver
```

## Example 16 - Match only proxy containers via regex
```bash
ktl logs 'cart-.*' --namespace canary --container 'proxy.*'
```

## Example 17 - Filter by owner references while ignoring metrics sidecars
```bash
ktl logs statefulset/cache --namespace edge --exclude-container metrics
```

## Example 18 - Use an alternate kubeconfig
```bash
ktl logs dashboard --namespace kube-system --kubeconfig ~/.kube/nonprod
```

## Example 19 - Switch kubeconfig context without changing files
```bash
ktl logs payments-api --context minikube
```

## Example 20 - Mirror a session via HTML/WebSocket viewers
```bash
ktl logs checkout \
  --namespace prod-payments \
  --ui :8080 \
  --ws-listen :9080 \
  --log-level debug
```

## Example 21 - Highlight warnings in-line
```bash
ktl logs checkout --namespace prod --highlight WARN --highlight Timeout
```

## Example 22 - Exclude heartbeat lines with a regex
```bash
ktl logs 'api-.*' --namespace prod --exclude 'healthz OK'
```

## Example 23 - Color containers differently per pod
```bash
ktl logs cart --namespace shop --diff-container
```

## Example 24 - Apply 24-bit pod colors (Monokai palette)
```bash
podColors="38;2;255;97;136,38;2;169;220;118,38;2;255;216;102,38;2;120;220;232,38;2;171;157;242"
ktl logs deploy/checkout --pod-colors "$podColors"
```

## Example 25 - Underline container names for emphasis
```bash
ktl logs frontend --container-colors "32;4,33;4,34;4,35;4"
```

## Example 26 - Disable color for CI jobs
```bash
ktl logs api --namespace prod --color=never > prod-api.log
```

## Example 27 - Hide timestamps entirely
```bash
ktl logs ops --namespace platform --timestamps=false
```

## Example 28 - Render timestamps in Tokyo
```bash
ktl logs auth --namespace corp-sec --timezone Asia/Tokyo
```

## Example 29 - Customize the timestamp format
```bash
ktl logs auth --namespace corp-sec --timestamp-format '2006-01-02 15:04:05'
```

## Example 30 - Strip prefixes while keeping colors
```bash
ktl logs frontend --no-prefix --color=always
```

## Example 31 - Show only log message bodies
```bash
ktl logs jobs --namespace batch --only-log-lines
```

## Example 32 - Emit JSON for downstream tooling
```bash
ktl logs ingress --namespace edge --json | jq .
```

## Example 33 - Produce raw log lines without prefixes
```bash
ktl logs worker --namespace batch --output raw --color=never
```

## Example 34 - Extended JSON with raw payloads
```bash
ktl logs billing --namespace finance --output extjson | jq .
```

## Example 35 - Pretty-print extended JSON
```bash
ktl logs audit --namespace corp-sec --output ppextjson | jq .
```

## Example 36 - Customize output with an inline template
```bash
ktl logs auth --namespace corp-sec --template '{{printf "%s (%s/%s/%s/%s)\\n" .Message .NodeName .Namespace .PodName .ContainerName}}'
```

## Example 37 - Load a template from disk
```bash
ktl logs backend --template-file ~/.config/ktl/templates/minimal.tpl
```

## Example 38 - Capture a fixed number of lines without following
```bash
ktl logs ingestion --namespace analytics --tail=50 --follow=false
```

## Example 39 - Increase history for sporadic CronJobs
```bash
ktl logs cronjob/emailer --namespace ops --tail=500
```

## Example 40 - Replay logs from STDIN through ktl formatting
```bash
ktl logs --stdin --template '{{.Timestamp}} stdin/{{.Message}}' < service.log
```

## Example 41 - Combine events with label selectors
```bash
ktl logs . --all-namespaces --selector app=frontend --events --since=10m
```

## Example 42 - Compare rollout events between ReplicaSets
```bash
ktl logs diff-deployments checkout-api payments-edge --namespace prod-payments
```

## Example 43 - Watch namespace drift every 15 seconds
```bash
ktl logs drift watch --namespace prod-payments --interval 15s
```

## Example 44 - Monitor cluster-wide drift with a bounded run
```bash
ktl logs drift watch --all-namespaces --interval 1m --iterations 5
```

## Example 45 - Capture a 5-minute incident window with metadata
```bash
ktl logs capture 'checkout-.*' \
  --namespace prod-payments \
  --duration 5m \
  --events \
  --attach-describe \
  --capture-sqlite \
  --capture-output dist/checkout-incident.tar.gz
```

## Example 46 - Archive every namespace into a named capture
```bash
ktl logs capture . \
  --all-namespaces \
  --duration 10m \
  --session-name cluster-audit \
  --capture-output dist/cluster-audit.tar.gz
```

## Example 47 - Replay a capture for one pod and emit JSON
```bash
ktl logs capture replay dist/checkout-incident.tar.gz \
  --namespace prod-payments \
  --pod checkout-api-7fb4d8b4bb-nbqzl \
  --grep timeout \
  --since 2025-11-30T12:00:00Z \
  --prefer-json \
  --json
```

## Example 48 - Use the SQLite index to template replayed logs
```bash
ktl logs capture replay dist/cluster-audit.tar.gz \
  --container api \
  --limit 200 \
  --desc \
  --template '{{.Timestamp}} {{.Namespace}}/{{.Pod}} {{.Rendered}}'
```

## Example 49 - Compare two capture artifacts
```bash
ktl logs capture diff dist/before.tar.gz dist/after.tar.gz
```

## Example 50 - Diff a capture against the live cluster
```bash
ktl logs capture diff dist/incident.tar.gz --live --namespace prod-payments --pod-query 'checkout-.*'
```

## Example 51 - Inject a sniffer and filter HTTPS traffic
```bash
ktl analyze traffic \
  --namespace roedk-2 \
  --target roedk-nginx-pko-86bc555bb-nlcw4:nginx-pko \
  --filter "port 443" \
  --interface any
```

## Example 52 - Capture only DNS and TLS handshakes between two pods
```bash
ktl analyze traffic \
  --target payments/api-0 \
  --target payments/api-1 \
  --between \
  --bpf dns \
  --bpf handshake \
  --count 200 \
  --snaplen 256
```

## Example 53 - Use a hardened tcpdump image and relative timestamps
```bash
ktl analyze traffic \
  --target video/encoder-0:encoder \
  --image ghcr.io/company/tcpdump:distroless \
  --image-pull-policy Always \
  --absolute-time=false \
  --snaplen 256 \
  --startup-timeout 1m
```

## Example 54 - Broadcast syscall profiles with a hardened helper image
First, build and push the helper so it carries your preferred CA bundle and strace version:

```bash
cd images/syscalls-helper
IMAGE=ghcr.io/avkcode/syscalls-helper:latest make push
```

Authorize the workload namespace to pull from your registry (patch the real service account your pod uses):

```bash
kubectl create secret docker-registry ktl-syscalls-regcred \
  --docker-server=ghcr.io \
  --docker-username "$GITHUB_USER" \
  --docker-password "$GHCR_TOKEN" \
  --namespace energy-lab

kubectl patch serviceaccount default \
  --namespace energy-lab \
  --type merge \
  --patch '{"imagePullSecrets":[{"name":"ktl-syscalls-regcred"}]}'
```

Then stream the syscall summary to both your terminal and a browser using `--ui`:

```bash
ktl analyze syscalls \
  --target energy-lab/energy-lab-6f75ffc5f8-7gsrf \
  --profile-duration 25s \
  --match open,connect,execve \
  --top 15 \
  --image ghcr.io/avkcode/syscalls-helper:latest \
  --ui :8081
```

Open `http://127.0.0.1:8081` (or whichever address you bound) so teammates can watch the hottest syscalls without re-running ktl.

## Example 55 - Package a chart's images for an air-gapped install
```bash
ktl package images \
  --chart ./helm-roedk \
  --release roedk \
  --values values-dev.yaml \
  --output dist/roedk-images.tar
```

## Example 56 - Bundle manifests and images into a .k8s app archive
```bash
ktl app package \
  --chart ./deploy/checkout \
  --release checkout \
  --namespace prod-payments \
  --values values-prod.yaml \
  --notes docs/runbook.md \
  --snapshot blue-20251201 \
  --parent-snapshot blue-base \
  --archive-file dist/checkout.k8s
```

## Example 57 - Unpack an app archive snapshot with attachments
```bash
ktl app unpack \
  --archive-file dist/checkout.k8s \
  --snapshot blue-20251201 \
  --output-dir dist/checkout-unpacked \
  --include-attachments
```

## Example 60 - Capture a PostgreSQL backup from a pod
```bash
ktl db backup postgresql-0 \
  --namespace roedk-2 \
  --output backups/ \
  --database app \
  --database analytics
```

## Example 61 - Restore a backup into a different namespace (drop existing DBs)
```bash
ktl db restore postgresql-0 \
  --namespace sandbox \
  --archive backups/db_backup_20251128_161103.tar.gz \
  --drop-db \
  --yes
```

## Example 62 - Review ResourceQuota and LimitRange pressure
```bash
ktl diag quotas --all-namespaces
```

## Example 63 - Inspect allocatable vs capacity per worker node
```bash
ktl diag nodes --selector 'node-role.kubernetes.io/worker'
```

## Example 64 - Correlate PVC health with node pressure
```bash
ktl diag storage --all-namespaces
```

## Example 65 - Show the top memory-hungry containers across the cluster
```bash
ktl diag resources --all-namespaces --top 25
```

## Example 66 - Audit CronJobs plus their most recent Jobs
```bash
ktl diag cronjobs --all-namespaces --show-jobs --job-limit 10
```

## Example 67 - Check Ingress and Services for the edge namespace
```bash
ktl diag network --namespace edge --show-services=false
```

## Example 68 - Summarize PodSecurity labels and violations everywhere
```bash
ktl diag podsecurity --all-namespaces
```

## Example 69 - Inspect PriorityClasses and preemption risks
```bash
ktl diag priorities --all-namespaces
```

## Example 70 - Show CPU and memory usage for pods in roedk-2
```bash
ktl diag top --namespace roedk-2 --sort-cpu
```

## Example 71 - Print a namespace health table
```bash
ktl diag report --namespace prod-payments
```

## Example 72 - Render the HTML posture report to disk
```bash
ktl diag report \
  --namespace prod-payments \
  --html \
  --output dist/prod-report.html
```

## Example 73 - Fail the build if any score drops below 85%
```bash
ktl diag report --all-namespaces --threshold 85 --notify stdout
```

## Example 74 - Serve a continuously updating HTML report
```bash
ktl diag report --all-namespaces --live --listen :8090
```

## Example 75 - Review scorecard trends for the past month
```bash
ktl diag report trend --days 30
```

## Example 76 - Vendor upstream charts with vendir specs
```bash
ktl app vendor sync \
  --chdir deploy \
  --file vendir.yml \
  --lock-file vendir.lock.yml
```

## Example 77 - Stream node/system logs alongside pods
```bash
ktl logs 'checkout-.*' \
  --namespace prod-payments \
  --node-logs \
  --node-log /var/log/kubelet.log \
  --node-log /var/log/syslog
```

## Example 78 - Show only kubelet output across every node
```bash
ktl logs . --node-log-all --node-log-only --node-log /var/log/kubelet.log
```
