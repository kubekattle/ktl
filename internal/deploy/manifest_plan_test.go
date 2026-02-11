// File: internal/deploy/manifest_plan_test.go
// Brief: Tests for manifest plan summarization.

package deploy

import "testing"

func TestSummarizeManifestPlanCounts(t *testing.T) {
	prev := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: a
  namespace: ns
data:
  x: "1"
---
apiVersion: v1
kind: Service
metadata:
  name: b
  namespace: ns
spec:
  selector:
    app: b
`
	next := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: a
  namespace: ns
data:
  x: "2"
---
apiVersion: v1
kind: Secret
metadata:
  name: c
  namespace: ns
type: Opaque
data:
  k: YQ==
`

	summary, err := SummarizeManifestPlan(prev, next)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.Add != 1 || summary.Change != 1 || summary.Destroy != 1 {
		t.Fatalf("unexpected counts: %#v", summary)
	}
}

func TestSummarizeManifestPlanDetectsReplaceOnAPIVersionChange(t *testing.T) {
	prev := `
apiVersion: autoscaling/v1
kind: HorizontalPodAutoscaler
metadata:
  name: app
  namespace: ns
spec:
  maxReplicas: 3
  minReplicas: 1
`
	next := `
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: app
  namespace: ns
spec:
  maxReplicas: 3
  minReplicas: 1
`
	summary, err := SummarizeManifestPlan(prev, next)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.Replace != 1 || summary.Add != 0 || summary.Destroy != 0 {
		t.Fatalf("unexpected counts: %#v", summary)
	}
}

func TestSummarizeManifestPlanServiceClusterIPEmptyDoesNotReplace(t *testing.T) {
	prev := `
apiVersion: v1
kind: Service
metadata:
  name: api
  namespace: ns
spec:
  clusterIP: ""
  ports:
    - name: http
      port: 80
      targetPort: 8080
`
	next := `
apiVersion: v1
kind: Service
metadata:
  name: api
  namespace: ns
spec:
  clusterIP: 10.0.0.2
  ports:
    - name: http
      port: 80
      targetPort: 8080
`
	summary, err := SummarizeManifestPlan(prev, next)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.Replace != 0 {
		t.Fatalf("expected no replace, got %#v", summary)
	}
}
