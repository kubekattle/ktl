//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kubekattle/ktl/internal/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestDriftGuardDetectsManualChange(t *testing.T) {
	kubeconfig := strings.TrimSpace(os.Getenv("KUBECONFIG"))
	if kubeconfig == "" {
		t.Skip("KUBECONFIG not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	ns := fmt.Sprintf("ktl-drift-guard-%d", time.Now().UnixNano())
	release := fmt.Sprintf("drift-%d", time.Now().UnixNano())

	client, err := kube.New(ctx, kubeconfig, "")
	if err != nil {
		t.Fatalf("kube.New: %v", err)
	}
	if _, err := client.Clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
	})

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{
		"apply",
		"--chart", repoTestdata("charts", "drift-guard"),
		"--release", release,
		"--namespace", ns,
		"--kubeconfig", kubeconfig,
		"--yes",
	})
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("initial apply: %v\nstderr:\n%s", err, errOut.String())
	}

	patch := []byte(`{"data":{"value":"999"}}`)
	if _, err := client.Clientset.CoreV1().ConfigMaps(ns).Patch(ctx, release+"-cm", types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		t.Fatalf("patch configmap: %v", err)
	}

	// Verify patch
	cm, err := client.Clientset.CoreV1().ConfigMaps(ns).Get(ctx, release+"-cm", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	t.Logf("Patched ConfigMap data: %v", cm.Data)

	root = newRootCommand()
	out.Reset()
	errOut.Reset()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{
		"apply",
		"--chart", repoTestdata("charts", "drift-guard"),
		"--release", release,
		"--namespace", ns,
		"--kubeconfig", kubeconfig,
		"--drift-guard",
		"--drift-guard-mode", "last-applied",
		"--yes",
	})
	err = root.ExecuteContext(ctx)
	if err == nil {
		t.Logf("stderr:\n%s", errOut.String())
		t.Logf("stdout:\n%s", out.String())
		t.Fatalf("expected drift guard to fail, got nil error")
	}
	got := err.Error()
	if !strings.Contains(got, "drift detected") {
		t.Fatalf("expected drift message in error, got:\n%s", got)
	}
	if !strings.Contains(got, "ConfigMap") || !strings.Contains(got, "changed") {
		t.Fatalf("expected drift item mention in error, got:\n%s", got)
	}
}
