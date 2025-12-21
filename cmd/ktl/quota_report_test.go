package main

import "testing"

func TestComputeDesiredQuotaTotals(t *testing.T) {
	manifest := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: demo
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: example.com/web:1
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: demo
spec:
  selector:
    app: web
  ports:
    - port: 80
      targetPort: 8080
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data
  namespace: demo
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 10Gi
`
	docs := docsToMap(parseManifestDocs(manifest))
	totals, warnings := computeDesiredQuotaTotals(docs, "demo")
	if totals == nil {
		t.Fatalf("expected totals")
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if totals.Pods != 2 {
		t.Fatalf("expected pods=2, got %d", totals.Pods)
	}
	if totals.Services != 1 {
		t.Fatalf("expected services=1, got %d", totals.Services)
	}
	if totals.PVCs != 1 {
		t.Fatalf("expected pvcs=1, got %d", totals.PVCs)
	}
	if totals.CPURequests.Value != "200m" {
		t.Fatalf("expected cpu requests 200m, got %q", totals.CPURequests.Value)
	}
	if totals.CPULimits.Value != "400m" {
		t.Fatalf("expected cpu limits 400m, got %q", totals.CPULimits.Value)
	}
	if totals.MemoryRequests.Value != "128Mi" {
		t.Fatalf("expected memory requests 128Mi, got %q", totals.MemoryRequests.Value)
	}
	if totals.MemoryLimits.Value != "256Mi" {
		t.Fatalf("expected memory limits 256Mi, got %q", totals.MemoryLimits.Value)
	}
	if totals.Storage.Value != "10Gi" {
		t.Fatalf("expected storage 10Gi, got %q", totals.Storage.Value)
	}
}
