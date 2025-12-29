# Verify showcase namespace

This folder contains a small Kubernetes namespace and a custom `ktl verify` ruleset
intended to demonstrate the scanner output (including a CRITICAL severity rule).

## Apply to a cluster

```bash
kubectl --kubeconfig ~/.kube/archimedes.yaml apply -f testdata/verify/showcase/namespace.yaml
kubectl --kubeconfig ~/.kube/archimedes.yaml apply -f testdata/verify/showcase/resources.yaml
```

## Run verify

Use the custom ruleset (includes a CRITICAL rule):

```bash
./bin/ktl verify namespace ktl-verify-showcase \
  --kubeconfig ~/.kube/archimedes.yaml \
  --rules-dir testdata/verify/showcase/rules
```

To remove:

```bash
kubectl --kubeconfig ~/.kube/archimedes.yaml delete ns ktl-verify-showcase
```

