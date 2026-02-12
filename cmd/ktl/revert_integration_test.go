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
)

func TestRevertSelectsLastKnownGood(t *testing.T) {
	kubeconfig := strings.TrimSpace(os.Getenv("KUBECONFIG"))
	if kubeconfig == "" {
		t.Skip("KUBECONFIG not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	ns := fmt.Sprintf("ktl-revert-%d", time.Now().UnixNano())
	release := fmt.Sprintf("revert-%d", time.Now().UnixNano())

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
	t.Setenv("KTL_YES", "1")

	apply := func(value string) error {
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
			"--set", "data.value=" + value,
			"--yes",
		})
		err := root.ExecuteContext(ctx)
		if err != nil {
			return fmt.Errorf("apply failed: %v\nstderr:\n%s", err, errOut.String())
		}
		return nil
	}

	if err := apply("1"); err != nil {
		t.Fatal(err)
	}
	if err := apply("2"); err != nil {
		t.Fatal(err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{
		"revert",
		"--release", release,
		"--namespace", ns,
		"--kubeconfig", kubeconfig,
		"--yes",
	})
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("revert: %v\nstderr:\n%s", err, errOut.String())
	}

	cm, err := client.Clientset.CoreV1().ConfigMaps(ns).Get(ctx, release+"-cm", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	if got := cm.Data["value"]; got != "1" {
		t.Fatalf("expected value to revert to 1, got %q", got)
	}
	if !strings.Contains(errOut.String(), "selected=") {
		t.Fatalf("expected selection rationale in stderr, got:\n%s", errOut.String())
	}
}
