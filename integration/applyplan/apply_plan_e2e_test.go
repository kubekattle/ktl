package applyplan_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/tools/clientcmd"
)

const (
	e2eKubeconfigEnv = "KTL_APPLYPLAN_E2E_KUBECONFIG"
	e2eConfirmEnv    = "KTL_APPLYPLAN_E2E_CONFIRM"
	e2eChartsEnv     = "KTL_APPLYPLAN_E2E_CHARTS"
	e2eContextsEnv   = "KTL_APPLYPLAN_E2E_CONTEXTS"
	e2eMaxChartsEnv  = "KTL_APPLYPLAN_E2E_MAX_CHARTS"
	e2eMaxCtxEnv     = "KTL_APPLYPLAN_E2E_MAX_CONTEXTS"
)

var (
	repoRoot   string
	ktlBin     string
	kubeconfig string
	contexts   []contextSelection
	charts     []string
)

type contextSelection struct {
	name      string
	namespace string
}

func TestMain(m *testing.M) {
	rawKubeconfig := strings.TrimSpace(os.Getenv(e2eKubeconfigEnv))
	if rawKubeconfig == "" {
		fmt.Fprintf(os.Stderr, "Skipping apply plan e2e; set %s to enable.\n", e2eKubeconfigEnv)
		os.Exit(0)
	}
	if strings.TrimSpace(os.Getenv(e2eConfirmEnv)) != "1" {
		fmt.Fprintf(os.Stderr, "Skipping apply plan e2e; set %s=1 to confirm running against a real cluster.\n", e2eConfirmEnv)
		os.Exit(0)
	}
	if err := bootstrapEnvironment(rawKubeconfig); err != nil {
		fmt.Fprintf(os.Stderr, "test bootstrap failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func bootstrapEnvironment(rawKubeconfig string) error {
	var err error
	repoRoot, err = resolveRepoRoot()
	if err != nil {
		return err
	}
	kubeconfig, err = resolveKubeconfig(rawKubeconfig)
	if err != nil {
		return err
	}
	if err := buildKtlBinary(); err != nil {
		return err
	}
	contexts, err = loadContexts(kubeconfig)
	if err != nil {
		return err
	}
	charts, err = discoverCharts()
	if err != nil {
		return err
	}
	return nil
}

func resolveRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("unable to resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

func resolveKubeconfig(raw string) (string, error) {
	if strings.HasPrefix(raw, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		raw = filepath.Join(homeDir, strings.TrimPrefix(raw, "~/"))
	}
	raw = filepath.Clean(raw)
	if _, err := os.Stat(raw); err != nil {
		return "", fmt.Errorf("stat kubeconfig %q: %w", raw, err)
	}
	return raw, nil
}

func buildKtlBinary() error {
	binDir := filepath.Join(repoRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	ktlBin = filepath.Join(binDir, "ktl.applyplan.test")
	cmd := exec.Command("go", "build", "-o", ktlBin, "./cmd/ktl")
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build ktl: %w", err)
	}
	return nil
}

func loadContexts(kubeconfigPath string) ([]contextSelection, error) {
	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	allowed := parseCSVEnv(e2eContextsEnv)
	var names []string
	for name := range cfg.Contexts {
		if len(allowed) > 0 && !allowed[name] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	maxContexts, err := parseOptionalIntEnv(e2eMaxCtxEnv)
	if err != nil {
		return nil, err
	}
	if maxContexts > 0 && len(names) > maxContexts {
		names = names[:maxContexts]
	}

	result := make([]contextSelection, 0, len(names))
	for _, name := range names {
		ns := strings.TrimSpace(cfg.Contexts[name].Namespace)
		if ns == "" {
			ns = "default"
		}
		result = append(result, contextSelection{name: name, namespace: ns})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no kubeconfig contexts selected (check %s)", e2eContextsEnv)
	}
	return result, nil
}

func discoverCharts() ([]string, error) {
	if raw := strings.TrimSpace(os.Getenv(e2eChartsEnv)); raw != "" {
		var selected []string
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			path := part
			if !filepath.IsAbs(path) {
				path = filepath.Join(repoRoot, path)
			}
			chart := filepath.Clean(path)
			if _, err := os.Stat(filepath.Join(chart, "Chart.yaml")); err != nil {
				return nil, fmt.Errorf("invalid chart %q (missing Chart.yaml): %w", part, err)
			}
			selected = append(selected, chart)
		}
		sort.Strings(selected)
		return selected, nil
	}

	root := filepath.Join(repoRoot, "testdata", "charts")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var found []string
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		dir := filepath.Join(root, ent.Name())
		if _, err := os.Stat(filepath.Join(dir, "Chart.yaml")); err == nil {
			found = append(found, dir)
		}
	}
	sort.Strings(found)

	maxCharts, err := parseOptionalIntEnv(e2eMaxChartsEnv)
	if err != nil {
		return nil, err
	}
	if maxCharts > 0 && len(found) > maxCharts {
		found = found[:maxCharts]
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no charts discovered under %s", root)
	}
	return found, nil
}

func parseCSVEnv(key string) map[string]bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out[part] = true
	}
	return out
}

func parseOptionalIntEnv(key string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q as int: %w", key, raw, err)
	}
	return n, nil
}

func TestApplyPlanAllChartsAndNamespaces_BasicFormats(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	for _, sel := range contexts {
		for _, chartPath := range charts {
			chartName := filepath.Base(chartPath)
			release := e2eReleaseName(chartName, sel.name, sel.namespace)

			t.Run(fmt.Sprintf("%s/%s/text", sel.name, chartName), func(t *testing.T) {
				if err := runKtl(ctx, []string{
					"apply", "plan",
					"--chart", chartPath,
					"--release", release,
					"--namespace", sel.namespace,
					"--format", "text",
					"--kubeconfig", kubeconfig,
					"--context", sel.name,
				}); err != nil {
					t.Fatalf("apply plan text failed: %v", err)
				}
			})

			t.Run(fmt.Sprintf("%s/%s/json", sel.name, chartName), func(t *testing.T) {
				if err := runKtl(ctx, []string{
					"apply", "plan",
					"--chart", chartPath,
					"--release", release,
					"--namespace", sel.namespace,
					"--format", "json",
					"--kubeconfig", kubeconfig,
					"--context", sel.name,
				}); err != nil {
					t.Fatalf("apply plan json failed: %v", err)
				}
			})
		}
	}
}

func TestApplyPlanFixtureChart_OptionMatrix(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	chartPath := filepath.Join(repoRoot, "testdata", "charts", "ktl-applyplan-e2e")
	valuesFile := filepath.Join(chartPath, "values-e2e.yaml")

	tempDir := t.TempDir()
	setFilePath := filepath.Join(tempDir, "set-file.txt")
	if err := os.WriteFile(setFilePath, []byte("from-set-file"), 0o644); err != nil {
		t.Fatalf("write set-file fixture: %v", err)
	}

	for _, sel := range contexts {
		release := e2eReleaseName("ktl-applyplan-e2e", sel.name, sel.namespace)

		run := func(name string, args []string) {
			t.Run(fmt.Sprintf("%s/%s", sel.name, name), func(t *testing.T) {
				if err := runKtl(ctx, args); err != nil {
					t.Fatalf("%s failed: %v", name, err)
				}
			})
		}

		run("values", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--values", valuesFile,
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		run("set", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--set", "replicaCount=3",
			"--set", "config.extra=from-set",
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		run("set-string", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--set-string", "config.text=from-set-string",
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		run("set-file", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--set-file", fmt.Sprintf("fileBlob=%s", setFilePath),
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		run("include-crds", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--include-crds",
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		run("version", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--version", "0.1.0",
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		htmlOut := filepath.Join(tempDir, fmt.Sprintf("%s-plan.html", sanitizeFileToken(sel.name)))
		run("format-html-output", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--format", "html",
			"--output", htmlOut,
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		vizOut := filepath.Join(tempDir, fmt.Sprintf("%s-viz.html", sanitizeFileToken(sel.name)))
		run("visualize-html", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--visualize",
			"--format", "visualize",
			"--visualize-explain",
			"--output", vizOut,
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		vizJSON := filepath.Join(tempDir, fmt.Sprintf("%s-viz.json", sanitizeFileToken(sel.name)))
		run("visualize-json", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--visualize",
			"--format", "json",
			"--output", vizJSON,
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		vizYAML := filepath.Join(tempDir, fmt.Sprintf("%s-viz.yaml", sanitizeFileToken(sel.name)))
		run("visualize-yaml", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--visualize",
			"--format", "yaml",
			"--output", vizYAML,
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		compareJSON := filepath.Join(tempDir, fmt.Sprintf("%s-compare.json", sanitizeFileToken(sel.name)))
		run("compare-artifact", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--format", "json",
			"--output", compareJSON,
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		compareViz := filepath.Join(tempDir, fmt.Sprintf("%s-compare-viz.html", sanitizeFileToken(sel.name)))
		run("compare-visualize", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--visualize",
			"--format", "visualize",
			"--compare", compareJSON,
			"--output", compareViz,
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})

		run("cluster-scoped", []string{
			"apply", "plan",
			"--chart", chartPath,
			"--release", release,
			"--namespace", sel.namespace,
			"--set", "rbac.clusterWide=true",
			"--set", "createNamespace=true",
			"--kubeconfig", kubeconfig,
			"--context", sel.name,
		})
	}
}

func runKtl(ctx context.Context, args []string) error {
	var stderr limitedBuffer
	cmd := exec.CommandContext(ctx, ktlBin, args...)
	cmd.Dir = repoRoot
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w; stderr:\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return nil
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit == 0 {
		b.limit = 64 * 1024
	}
	remain := b.limit - b.buf.Len()
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) > remain {
		p = p[:remain]
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}

func e2eReleaseName(chartName, contextName, namespace string) string {
	base := strings.ToLower(chartName)
	base = sanitizeFileToken(base)
	if base == "" {
		base = "chart"
	}
	sum := sha1.Sum([]byte(chartName + "\x00" + contextName + "\x00" + namespace))
	hash := hex.EncodeToString(sum[:])[:10]
	if len(base) > 24 {
		base = base[:24]
	}
	return fmt.Sprintf("ktl-e2e-%s-%s", strings.Trim(base, "-"), hash)
}

func sanitizeFileToken(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	return strings.Trim(out.String(), "-")
}
