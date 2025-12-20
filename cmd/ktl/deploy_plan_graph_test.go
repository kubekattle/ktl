package main

import (
	"strings"
	"testing"
)

func TestBuildDependencyGraph_FindsConfigMapsAndSecrets(t *testing.T) {
	manifest := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: prod
spec:
  template:
    spec:
      containers:
      - name: app
        env:
        - name: CFG
          valueFrom:
            configMapKeyRef:
              name: web-env
        envFrom:
        - secretRef:
            name: api-creds
      volumes:
      - name: cfg
        configMap:
          name: web-env
`
	docs := parseManifestDocs(manifest)
	desired := docsToMap(docs)
	nodes, edges := buildDependencyGraph(desired, nil)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes (deployment + configmap + secret), got %d", len(nodes))
	}
	if len(edges) != 3 {
		t.Fatalf("expected 3 edges, got %d", len(edges))
	}
	summaries := summarizeGraphEdges(nodes, edges)
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(summaries))
	}
	foundConfigMap := false
	foundSecret := false
	for _, line := range summaries {
		if containsAll(line, []string{"Deployment", "ConfigMap", "web-env"}) {
			foundConfigMap = true
		}
		if containsAll(line, []string{"Deployment", "Secret", "api-creds"}) {
			foundSecret = true
		}
	}
	if !foundConfigMap || !foundSecret {
		t.Fatalf("missing expected dependency summaries: %v", summaries)
	}
}

func containsAll(line string, want []string) bool {
	for _, token := range want {
		if !strings.Contains(line, token) {
			return false
		}
	}
	return true
}
