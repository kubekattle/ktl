# Recipes (golden paths)

Copy/paste workflows that cover the common “happy paths” for `ktl`.

## Zero-conf onboarding

```bash
# Initialize repo defaults and detect your kubecontext
ktl init

# Run the interactive setup wizard
ktl init --interactive

# Scaffold chart/ and values/ plus gitignore entries
ktl init --layout --gitignore

# Use an opinionated preset
ktl init --preset prod

# Scaffold a Vault secrets provider
ktl init --secrets-provider vault

# Preview the config without writing
ktl init --dry-run

# Launch the interactive help UI
ktl help --ui
```

## Apply a chart (with and without the UI)

```bash
# Preview what will change
ktl apply plan --chart ./chart --release foo -n default

# Deploy
ktl apply --chart ./chart --release foo -n default

# Deploy with the viewer UI
ktl apply --chart ./chart --release foo -n default --ui
```

## 5-minute demo (public chart)

Do this:
```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update

ktl apply plan --chart bitnami/nginx --release demo-nginx -n default --visualize
ktl apply --chart bitnami/nginx --release demo-nginx -n default --yes
ktl delete --release demo-nginx -n default --yes
```

## Recommended `.ktl.yaml` layout

Do this:
```yaml
build:
  profile: ci

secrets:
  defaultProvider: local
  providers:
    local:
      type: file
      path: ./secrets.dev.yaml
```

## Apply with secret references

```bash
# Define providers in .ktl.yaml (or pass --secret-config)
cat > .ktl.yaml <<'YAML'
secrets:
  defaultProvider: local
  providers:
    local:
      type: file
      path: ./secrets.dev.yaml
YAML

# Use secret:// references in values
cat > values.dev.yaml <<'YAML'
db:
  password: secret://local/db/password
YAML

ktl apply plan --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider local
ktl apply --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider local
ktl stack apply --config ./stacks/prod --secret-provider local --yes
```

## Vault-backed secrets

```bash
cat > .ktl.yaml <<'YAML'
secrets:
  defaultProvider: vault
  providers:
    vault:
      type: vault
      address: https://vault.example.com
      authMethod: approle
      authMount: approle
      roleId: 00000000-0000-0000-0000-000000000000
      secretId: s.0000000000000000000000
      mount: secret
      kvVersion: 2
      key: value
      # kubernetesRole: ktl
      # kubernetesTokenPath: /var/run/secrets/kubernetes.io/serviceaccount/token
      # awsRole: ktl
      # awsRegion: us-east-1
      # awsHeaderValue: vault.example.com
YAML

cat > values.dev.yaml <<'YAML'
db:
  password: secret://vault/app/db#password
YAML

ktl apply plan --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider vault
ktl apply --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider vault
ktl stack apply --config ./stacks/prod --secret-provider vault --yes
```

Inspect providers and references:
```bash
ktl secrets test --secret-provider vault --ref secret://vault/app/db#password
ktl secrets list --secret-provider vault --path app --format json
```

Minimal CLI workflow (sanity check before apply):
```bash
ktl secrets test --secret-provider vault --ref secret://vault/app/db#password
ktl secrets list --secret-provider vault --path app
```

## Regression-proof plans

Do this:
```bash
ktl apply plan --chart ./chart --release foo -n default --baseline ./plan.json
ktl apply plan --chart ./chart --release foo -n default --compare-to ./plan.json
```

## Regression-proof verify

Do this:
```bash
verify verify.yaml --baseline ./baseline.json
verify verify.yaml --compare-to ./baseline.json
```

## Share an `apply plan` visualization

```bash
ktl apply plan --visualize --chart ./chart --release foo -n default
```

## Stack: minimal-flags workflow (plan → apply)

```bash
export KTL_STACK_ROOT=./stacks/prod

# Read-only plan (default `ktl stack` behaves like `ktl stack plan`)
ktl stack

# Execute (DAG order)
ktl stack apply --yes
```

## Stack: resume / rerun failed

```bash
export KTL_STACK_ROOT=./stacks/prod

# Resume the most recent run (frozen plan unless --replan is set)
ktl stack apply --resume --yes

# Convenience: resume and schedule only failed nodes
ktl stack rerun-failed --yes
```

## Stack: inspect runs

```bash
export KTL_STACK_ROOT=./stacks/prod

ktl stack runs --limit 50
ktl stack status --follow
ktl stack audit --output html > stack-audit.html
```

## Build: share the build stream over WebSocket

```bash
ktl build --context . --tag ghcr.io/acme/app:dev --ws-listen :9085
```

## Verify: validate a chart render in CI

```bash
cat > verify-chart-render.yaml <<'YAML'
version: v1

target:
  kind: chart
  chart:
    chart: ./chart
    release: foo
    namespace: default
    useCluster: false

verify:
  mode: block
  failOn: high

output:
  format: table
  report: "-"
YAML

verify verify-chart-render.yaml

# Package a chart then verify the archive
package ./chart --output dist/chart.sqlite
package --verify dist/chart.sqlite
```
