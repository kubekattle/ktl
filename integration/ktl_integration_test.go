package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	dashboardNamespace = "kubernetes-dashboard"
	dashboardLabel     = "k8s-app=kubernetes-dashboard"
	testNamespace      = "ktl-test"
	testPodName        = "ktl-logger"
	testKubeconfigEnv  = "KTL_TEST_KUBECONFIG"
)

var (
	repoRoot                    string
	ktlBin                      string
	effectiveDashboardNamespace = dashboardNamespace
)

func TestMain(m *testing.M) {
	if err := bootstrapEnvironment(); err != nil {
		fmt.Fprintf(os.Stderr, "test bootstrap failed: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	os.Exit(code)
}

func bootstrapEnvironment() error {
	var err error
	repoRoot, err = resolveRepoRoot()
	if err != nil {
		return err
	}
	if err := ensureKubeconfig(); err != nil {
		return err
	}
	if err := buildKtlBinary(); err != nil {
		return err
	}
	if err := applyTestFixtures(); err != nil {
		return err
	}
	return nil
}

func resolveRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("unable to resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..")), nil
}

func ensureKubeconfig() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	kubeDir := filepath.Join(homeDir, ".kube")
	if err := os.MkdirAll(kubeDir, 0o700); err != nil {
		return err
	}
	dest := filepath.Join(kubeDir, "config")
	if override := strings.TrimSpace(os.Getenv(testKubeconfigEnv)); override != "" {
		contents, err := os.ReadFile(override)
		if err != nil {
			return fmt.Errorf("read kubeconfig override %q: %w", override, err)
		}
		return os.WriteFile(dest, contents, 0o600)
	}
	if _, err := os.Stat(dest); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no default kubeconfig at %s; set %s or configure kubectl", dest, testKubeconfigEnv)
		}
		return fmt.Errorf("stat default kubeconfig %s: %w", dest, err)
	}
	return nil
}

func buildKtlBinary() error {
	binDir := filepath.Join(repoRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	ktlBin = filepath.Join(binDir, "ktl.test")
	cmd := exec.Command("go", "build", "-o", ktlBin, "./cmd/ktl")
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build ktl: %w", err)
	}
	return nil
}

func applyTestFixtures() error {
	manifest := filepath.Join(repoRoot, "testdata", "ktl-logger.yaml")
	cmd := exec.Command("kubectl", "apply", "-f", manifest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply fixtures: %w", err)
	}
	wait := exec.Command("kubectl", "wait", "--for=condition=Ready", fmt.Sprintf("pod/%s", testPodName), "-n", testNamespace, "--timeout=90s")
	wait.Stdout = os.Stdout
	wait.Stderr = os.Stderr
	if err := wait.Run(); err != nil {
		return fmt.Errorf("kubectl wait pod ready: %w", err)
	}
	return nil
}

func TestKtlMatchesKubectlLogs(t *testing.T) {
	pod := dashboardPod(t)
	if effectiveDashboardNamespace != dashboardNamespace {
		t.Skip("kubernetes-dashboard namespace unavailable; skipping dashboard comparison")
	}
	ns := effectiveDashboardNamespace
	expected := toLines(runKubectl(t, "logs", "-n", ns, pod, "--tail=5"))
	output := toLines(runKtl(t, 30*time.Second, pod, "--namespace", ns, "--tail=5", "--follow=false", "--no-prefix", "--color=never", "--timestamps=false"))
	if !reflect.DeepEqual(expected, output) {
		t.Fatalf("ktl output mismatch\nexpected: %v\nactual:   %v", expected, output)
	}
}

func TestKtlJSONOutput(t *testing.T) {
	pod := dashboardPod(t)
	if effectiveDashboardNamespace != dashboardNamespace {
		t.Skip("kubernetes-dashboard namespace unavailable; skipping dashboard comparison")
	}
	ns := effectiveDashboardNamespace
	expected := toLines(runKubectl(t, "logs", "-n", ns, pod, "--tail=3"))
	output := toLines(runKtl(t, 30*time.Second, pod, "--namespace", ns, "--tail=3", "--follow=false", "--json"))
	if !reflect.DeepEqual(expected, output) {
		t.Fatalf("json output mismatch\nexpected: %v\nactual:   %v", expected, output)
	}
}

func TestKtlHighlightAndExclude(t *testing.T) {
	pod := dashboardPod(t)
	if effectiveDashboardNamespace != dashboardNamespace {
		t.Skip("kubernetes-dashboard namespace unavailable; skipping highlight regression test")
	}
	ns := effectiveDashboardNamespace
	out := runKtl(t, 30*time.Second, pod, "--namespace", ns, "--tail=20", "--follow=false", "--highlight", "Using", "--highlight", "token", "--color=always")
	if !strings.Contains(out, "\x1b[43;30mUsing\x1b[0;0m") {
		t.Fatalf("expected highlight sequence, got:\n%s", out)
	}
	excluded := runKtl(t, 30*time.Second, pod, "--namespace", ns, "--tail=20", "--follow=false", "--exclude=Using namespace", "--no-prefix", "--timestamps=false")
	if strings.Contains(excluded, "Using namespace") {
		t.Fatalf("exclude regex did not remove matching line:\n%s", excluded)
	}
}

func TestKtlOutputFormats(t *testing.T) {
	pod := dashboardPod(t)
	if effectiveDashboardNamespace != dashboardNamespace {
		t.Skip("kubernetes-dashboard namespace unavailable; skipping format comparison")
	}
	ns := effectiveDashboardNamespace
	rawKubectl := strings.TrimSpace(runKubectl(t, "logs", "-n", ns, pod, "--tail=1"))
	rawOut := strings.TrimSpace(runKtl(t, 30*time.Second, pod, "--namespace", ns, "--tail=1", "--follow=false", "--output", "raw", "--color=never", "--timestamps=false"))
	if rawOut != rawKubectl {
		t.Fatalf("raw output mismatch\nkubectl: %s\nktl: %s", rawKubectl, rawOut)
	}

	jsonLine := strings.TrimSpace(runKtl(t, 30*time.Second, pod, "--namespace", ns, "--tail=1", "--follow=false", "--output", "json", "--color=never"))
	var payload map[string]any
	if err := json.Unmarshal([]byte(jsonLine), &payload); err != nil {
		t.Fatalf("failed to parse json output: %v\nline: %s", err, jsonLine)
	}
	for _, key := range []string{"namespace", "pod", "container", "message"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected key %q in json output: %v", key, payload)
		}
	}
}

func TestKtlContainerFilters(t *testing.T) {
	t.Run("include-filter", func(t *testing.T) {
		out := runKtl(t, 30*time.Second, testPodName, "--namespace", testNamespace, "--tail=3", "--follow=false", "-c", "alpha", "--no-prefix", "--timestamps=false", "--color=never")
		for _, line := range toLines(out) {
			if !strings.Contains(line, "alpha ") {
				t.Fatalf("expected only alpha logs, got line: %s", line)
			}
		}
	})
	t.Run("exclude-filter", func(t *testing.T) {
		out := runKtl(t, 30*time.Second, testPodName, "--namespace", testNamespace, "--tail=3", "--follow=false", "--exclude-container", "beta", "--no-prefix", "--timestamps=false", "--color=never")
		for _, line := range toLines(out) {
			if strings.Contains(line, "beta ") {
				t.Fatalf("exclude-container failed, line: %s", line)
			}
		}
	})
}

func TestKtlLabelSelector(t *testing.T) {
	out := runKtl(t, 30*time.Second, ".*", "--namespace", testNamespace, "--selector", "app=ktl-logger", "--tail=2", "--follow=false", "--no-prefix", "--timestamps=false")
	lines := toLines(out)
	if len(lines) == 0 {
		t.Fatalf("expected log lines for selector")
	}
	seen := map[string]bool{}
	for _, line := range lines {
		if strings.Contains(line, "alpha ") {
			seen["alpha"] = true
		}
		if strings.Contains(line, "beta ") {
			seen["beta"] = true
		}
	}
	if len(seen) == 0 {
		t.Fatalf("selector did not include target containers: %v", lines)
	}
}

func TestKtlAllNamespaces(t *testing.T) {
	pod := dashboardPod(t)
	if effectiveDashboardNamespace != dashboardNamespace {
		t.Skip("kubernetes-dashboard namespace unavailable; skipping cross-namespace assertion")
	}
	ns := effectiveDashboardNamespace
	query := testPodName
	if pod != testPodName {
		query = fmt.Sprintf("%s|%s.*", testPodName, pod)
	}
	out := runKtl(t, 30*time.Second, query, "--all-namespaces", "--tail=1", "--follow=false")
	if !strings.Contains(out, fmt.Sprintf("%s/%s", testNamespace, testPodName)) {
		t.Fatalf("expected ktl-test namespace output:\n%s", out)
	}
	if ns != testNamespace && !strings.Contains(out, ns) {
		t.Fatalf("expected %s namespace output:\n%s", ns, out)
	}
	if ns == testNamespace {
		t.Log("dashboard namespace not detected; only ktl-test assertions enforced")
	}
}

func TestKtlTailLimit(t *testing.T) {
	pod := dashboardPod(t)
	if effectiveDashboardNamespace != dashboardNamespace {
		t.Skip("kubernetes-dashboard namespace unavailable; skipping tail regression test")
	}
	ns := effectiveDashboardNamespace
	out := toLines(runKtl(t, 30*time.Second, pod, "--namespace", ns, "--tail=2", "--follow=false", "--no-prefix", "--timestamps=false"))
	if len(out) != 2 {
		t.Fatalf("expected exactly 2 lines, got %d: %v", len(out), out)
	}
}

func TestKtlSince(t *testing.T) {
	out := toLines(runKtl(t, 30*time.Second, testPodName, "--namespace", testNamespace, "--since=2s", "--tail=-1", "--follow=false", "--no-prefix", "--timestamps=false", "--color=never"))
	if len(out) == 0 {
		t.Fatalf("expected log lines for --since")
	}
	now := time.Now().Unix()
	for _, line := range out {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			t.Fatalf("unexpected log format: %s", line)
		}
		epochStr := fields[len(fields)-1]
		epoch, err := strconv.ParseInt(epochStr, 10, 64)
		if err != nil {
			t.Fatalf("parse epoch from %s: %v", line, err)
		}
		if now-epoch > 10 {
			t.Fatalf("--since returned stale line: %s", line)
		}
	}
}

func TestKtlFollowStreams(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ktlBin, testPodName, "--namespace", testNamespace, "--tail=1", "--follow=true", "-c", "alpha", "--no-prefix", "--timestamps=false")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected context deadline to cancel follow mode")
	}
	lines := toLines(stdout.String())
	if len(lines) < 2 {
		t.Fatalf("follow mode did not stream additional lines: %v", lines)
	}
}

func TestKtlTemplateRendering(t *testing.T) {
	template := "{{.Namespace}}/{{.PodName}} {{.ContainerName}} :: {{.Message}}"
	out := runKtl(t, 30*time.Second, testPodName, "--namespace", testNamespace, "--tail=1", "--follow=false", "--template", template, "--timestamps=false")
	if !strings.Contains(out, fmt.Sprintf("%s/%s", testNamespace, testPodName)) || !strings.Contains(out, "::") {
		t.Fatalf("custom template not applied: %s", out)
	}
}

func TestKtlContextFlag(t *testing.T) {
	contextName := strings.TrimSpace(runKubectl(t, "config", "current-context"))
	if contextName == "" {
		t.Fatalf("no current context detected")
	}
	pod := dashboardPod(t)
	if effectiveDashboardNamespace != dashboardNamespace {
		t.Skip("kubernetes-dashboard namespace unavailable; skipping context flag test")
	}
	ns := effectiveDashboardNamespace
	out := runKtl(t, 30*time.Second, pod, "--namespace", ns, "--tail=1", "--follow=false", "--context", contextName)
	if len(toLines(out)) == 0 {
		t.Fatalf("expected output when using --context")
	}
}

func TestKtlEventsSnapshot(t *testing.T) {
	reason := "KtlSnapshot"
	createTestEvent(t, "Normal", reason, "snapshot verification")
	out := runKtl(t, 30*time.Second, testPodName, "--namespace", testNamespace, "--events", "--events-only", "--follow=false")
	if !strings.Contains(out, "[EVENT]") || !strings.Contains(out, reason) {
		t.Fatalf("expected snapshot events output, got:\n%s", out)
	}
}

func TestKtlEventsFollow(t *testing.T) {
	reason := "KtlFollow"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ktlBin, testPodName, "--namespace", testNamespace, "--events", "--events-only", "--follow")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	go func() {
		time.Sleep(1 * time.Second)
		createTestEvent(t, "Warning", reason, "follow verification")
	}()
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected ktl follow events to be interrupted by context timeout")
	}
	if !strings.Contains(stdout.String(), reason) {
		t.Fatalf("expected streamed event containing %s, output:\n%s", reason, stdout.String())
	}
}

func TestKtlDiffContainerColors(t *testing.T) {
	out := runKtl(t, 30*time.Second, testPodName, "--namespace", testNamespace, "--tail=1", "--follow=false", "--color=always", "--diff-container")
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected colored container segment when --diff-container is set:\n%s", out)
	}
	if !strings.Contains(out, "[alpha]") {
		t.Fatalf("expected container tag in output:\n%s", out)
	}
}

func TestKtlPodColorOverrides(t *testing.T) {
	pod := dashboardPod(t)
	if effectiveDashboardNamespace != dashboardNamespace {
		t.Skip("kubernetes-dashboard namespace unavailable; skipping pod color override test")
	}
	ns := effectiveDashboardNamespace
	out := runKtl(t, 30*time.Second, pod, "--namespace", ns, "--tail=1", "--follow=false", "--color=always", "--pod-colors", "95")
	if !strings.Contains(out, "\x1b[95m") {
		t.Fatalf("expected custom pod color sequence in output:\n%s", out)
	}
}

func TestKtlAppVendorSyncLocal(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "vendir.yml")
	lockPath := filepath.Join(tmpDir, "vendir.lock.yml")
	cacheDir := filepath.Join(tmpDir, ".vendir-cache")

	config := fmt.Sprintf(`apiVersion: vendir.k14s.io/v1alpha1
kind: Config
directories:
- path: vendor
  contents:
  - path: grafana
    directory:
      path: %s
`, filepath.Join(repoRoot, "testdata", "charts", "grafana"))

	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write vendir config: %v", err)
	}

	t.Setenv("VENDIR_CACHE_DIR", cacheDir)

	runKtl(t, 60*time.Second,
		"app", "vendor", "sync",
		"--file", configPath,
		"--lock-file", lockPath,
		"--chdir", tmpDir,
	)

	vendoredChart := filepath.Join(tmpDir, "vendor", "grafana", "Chart.yaml")
	if _, err := os.Stat(vendoredChart); err != nil {
		t.Fatalf("expected vendored Chart.yaml: %v", err)
	}

	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	if !bytes.Contains(lockBytes, []byte("grafana")) {
		t.Fatalf("lock file missing grafana entry:\n%s", lockBytes)
	}
}

func runKtl(t *testing.T, timeout time.Duration, args ...string) string {
	stdout, stderr, err := execKtl(timeout, args...)
	if err != nil {
		t.Fatalf("ktl %v failed: %v\nstderr:\n%s", args, err, stderr)
	}
	return stdout
}

func runKtlWithStreams(t *testing.T, timeout time.Duration, args ...string) (string, string) {
	stdout, stderr, err := execKtl(timeout, args...)
	if err != nil {
		t.Fatalf("ktl %v failed: %v\nstderr:\n%s", args, err, stderr)
	}
	return stdout, stderr
}

func execKtl(timeout time.Duration, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ktlBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func runKubectl(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("kubectl %v failed: %v\nstderr:\n%s", args, err, stderr.String())
	}
	return stdout.String()
}

func dashboardPod(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("kubectl", "get", "pods", "-n", dashboardNamespace, "-l", dashboardLabel, "-o", "jsonpath={.items[0].metadata.name}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		name := strings.TrimSpace(stdout.String())
		if name != "" {
			effectiveDashboardNamespace = dashboardNamespace
			return name
		}
		t.Log("dashboard namespace has no pods; falling back to ktl-test fixtures")
	} else {
		t.Logf("dashboard pod lookup failed: %v (%s)", err, strings.TrimSpace(stderr.String()))
	}
	effectiveDashboardNamespace = testNamespace
	return testPodName
}

func toLines(input string) []string {
	input = strings.Trim(input, "\n")
	if input == "" {
		return nil
	}
	return strings.Split(input, "\n")
}

func createTestEvent(t *testing.T, eventType, reason, message string) {
	t.Helper()
	uid := podUID(t)
	name := fmt.Sprintf("ktl-%d", time.Now().UnixNano())
	eventTime := time.Now().UTC().Format(time.RFC3339Nano)
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Event
metadata:
  name: %s
  namespace: %s
firstTimestamp: %q
lastTimestamp: %q
type: %s
reason: %s
message: %s
count: 1
source:
  component: ktl.tests
involvedObject:
  apiVersion: v1
  kind: Pod
  name: %s
  namespace: %s
  uid: %s
`, name, testNamespace, eventTime, eventTime, eventType, reason, message, testPodName, testNamespace, uid)
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("create event manifest failed: %v", err)
	}
}

func podUID(t *testing.T) string {
	return strings.TrimSpace(runKubectl(t, "get", "pod", testPodName, "-n", testNamespace, "-o", "jsonpath={.metadata.uid}"))
}
