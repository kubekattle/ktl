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
