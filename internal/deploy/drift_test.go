package deploy

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestCheckReleaseDrift_IgnoresStatusAndResourceVersion(t *testing.T) {
	manifest := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: default
data:
  a: "1"
`
	liveYAML := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: default
  resourceVersion: "123"
data:
  a: "1"
status:
  ignored: true
`
	get := func(ctx context.Context, target resourceTarget) (*unstructured.Unstructured, error) {
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(liveYAML), &obj); err != nil {
			return nil, err
		}
		return &unstructured.Unstructured{Object: obj}, nil
	}
	report, err := CheckReleaseDrift(context.Background(), "r", manifest, get)
	if err != nil {
		t.Fatalf("CheckReleaseDrift: %v", err)
	}
	if !report.Empty() {
		t.Fatalf("expected no drift, got %+v", report.Items)
	}
}

func TestCheckReleaseDrift_DetectsSpecChange(t *testing.T) {
	manifest := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: default
data:
  a: "1"
`
	liveYAML := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: default
data:
  a: "2"
`
	get := func(ctx context.Context, target resourceTarget) (*unstructured.Unstructured, error) {
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(liveYAML), &obj); err != nil {
			return nil, err
		}
		return &unstructured.Unstructured{Object: obj}, nil
	}
	report, err := CheckReleaseDriftWithOptions(context.Background(), "r", manifest, get, DriftOptions{RequireHelmOwnership: false})
	if err != nil {
		t.Fatalf("CheckReleaseDrift: %v", err)
	}
	if report.Empty() {
		t.Fatalf("expected drift, got none")
	}
	if got := report.Items[0].Reason; got == "" {
		t.Fatalf("expected reason, got empty")
	}
	if report.Items[0].Diff == "" {
		t.Fatalf("expected diff, got empty")
	}
}

func TestCheckReleaseDrift_IgnoresServiceClusterIP(t *testing.T) {
	manifest := `
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: default
spec:
  selector:
    app: x
  ports:
  - port: 80
    targetPort: 80
`
	liveYAML := `
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: default
spec:
  clusterIP: 10.0.0.1
  selector:
    app: x
  ports:
  - port: 80
    targetPort: 80
`
	get := func(ctx context.Context, target resourceTarget) (*unstructured.Unstructured, error) {
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(liveYAML), &obj); err != nil {
			return nil, err
		}
		return &unstructured.Unstructured{Object: obj}, nil
	}
	report, err := CheckReleaseDrift(context.Background(), "r", manifest, get)
	if err != nil {
		t.Fatalf("CheckReleaseDrift: %v", err)
	}
	if !report.Empty() {
		t.Fatalf("expected no drift, got %+v", report.Items)
	}
}

func TestCheckReleaseDrift_IgnoresServiceNodePort(t *testing.T) {
	manifest := `
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: default
spec:
  selector:
    app: x
  ports:
  - port: 80
    targetPort: 80
`
	liveYAML := `
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: default
spec:
  selector:
    app: x
  ports:
  - port: 80
    targetPort: 80
    nodePort: 30080
`
	get := func(ctx context.Context, target resourceTarget) (*unstructured.Unstructured, error) {
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(liveYAML), &obj); err != nil {
			return nil, err
		}
		return &unstructured.Unstructured{Object: obj}, nil
	}
	report, err := CheckReleaseDriftWithOptions(context.Background(), "r", manifest, get, DriftOptions{RequireHelmOwnership: false})
	if err != nil {
		t.Fatalf("CheckReleaseDrift: %v", err)
	}
	if !report.Empty() {
		t.Fatalf("expected no drift, got %+v", report.Items)
	}
}

func TestCheckReleaseDrift_IgnoresLastAppliedAnnotation(t *testing.T) {
	manifest := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: default
data:
  a: "1"
`
	liveYAML := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: default
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: "big"
data:
  a: "1"
`
	get := func(ctx context.Context, target resourceTarget) (*unstructured.Unstructured, error) {
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(liveYAML), &obj); err != nil {
			return nil, err
		}
		return &unstructured.Unstructured{Object: obj}, nil
	}
	report, err := CheckReleaseDriftWithOptions(context.Background(), "r", manifest, get, DriftOptions{RequireHelmOwnership: false})
	if err != nil {
		t.Fatalf("CheckReleaseDrift: %v", err)
	}
	if !report.Empty() {
		t.Fatalf("expected no drift, got %+v", report.Items)
	}
}

func TestCheckReleaseDrift_RequiresHelmOwnershipWhenEnabled(t *testing.T) {
	manifest := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: default
  labels:
    app.kubernetes.io/managed-by: Helm
  annotations:
    meta.helm.sh/release-name: monitoring
data:
  a: "1"
`
	liveYAML := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: default
data:
  a: "2"
`
	get := func(ctx context.Context, target resourceTarget) (*unstructured.Unstructured, error) {
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(liveYAML), &obj); err != nil {
			return nil, err
		}
		return &unstructured.Unstructured{Object: obj}, nil
	}
	report, err := CheckReleaseDriftWithOptions(context.Background(), "monitoring", manifest, get, DriftOptions{RequireHelmOwnership: true})
	if err != nil {
		t.Fatalf("CheckReleaseDrift: %v", err)
	}
	// Live object is missing Helm ownership markers; should be skipped rather than reported as drift.
	if !report.Empty() {
		t.Fatalf("expected no drift, got %+v", report.Items)
	}
}
