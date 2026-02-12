package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kubekattle/ktl/internal/appconfig"
	"gopkg.in/yaml.v3"
)

var _, initTestFile, _, _ = runtime.Caller(0)
var initTestRepoRoot = filepath.Clean(filepath.Join(filepath.Dir(initTestFile), "..", ".."))

func copyInitFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join(initTestRepoRoot, "testdata", "init", name)
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("fixture %s missing: %v", name, err)
	}
	dest := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture %s: %v", name, err)
	}
	return dest
}

func TestInitWritesConfig(t *testing.T) {
	dir := t.TempDir()

	kubeconfigPath := filepath.Join(dir, "kubeconfig.yaml")
	kubeconfig := `apiVersion: v1
kind: Config
current-context: dev
contexts:
- name: dev
  context:
    cluster: dev
    user: dev
    namespace: dev
clusters:
- name: dev
  cluster:
    server: https://example.com
users:
- name: dev
  user:
    token: dummy
`
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	t.Setenv("KUBECONFIG", kubeconfigPath)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", dir, "--secrets-file", "./secrets.local.yaml", "--profile", "ci"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(strings.ToLower(errOut.String()), "warning:") {
		t.Fatalf("expected no warnings, got: %q", errOut.String())
	}

	written := filepath.Join(dir, ".ktl.yaml")
	raw, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("read .ktl.yaml: %v", err)
	}

	var cfg appconfig.Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal .ktl.yaml: %v", err)
	}
	if cfg.Build.Profile != "ci" {
		t.Fatalf("expected build.profile to be ci, got %q", cfg.Build.Profile)
	}
	if cfg.Secrets.DefaultProvider != "local" {
		t.Fatalf("expected secrets.defaultProvider local, got %q", cfg.Secrets.DefaultProvider)
	}
	provider, ok := cfg.Secrets.Providers["local"]
	if !ok {
		t.Fatalf("expected local provider in secrets config")
	}
	if provider.Type != "file" {
		t.Fatalf("expected local provider type file, got %q", provider.Type)
	}
	if provider.Path != "./secrets.local.yaml" {
		t.Fatalf("expected local provider path ./secrets.local.yaml, got %q", provider.Path)
	}
}

func TestInitDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", dir, "--dry-run"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".ktl.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no .ktl.yaml to be written, got err=%v", err)
	}
	if !strings.Contains(out.String(), "build:") {
		t.Fatalf("expected dry-run output to include config YAML, got:\n%s", out.String())
	}
}

func TestInitMergePreservesExisting(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	existing := `build:
  profile: secure
secrets:
  defaultProvider: vault
  providers:
    vault:
      type: vault
      address: https://vault.example.com
`
	if err := os.WriteFile(filepath.Join(dir, ".ktl.yaml"), []byte(existing), 0o600); err != nil {
		t.Fatalf("write .ktl.yaml: %v", err)
	}

	root := newRootCommand()
	root.SetArgs([]string{"init", dir, "--merge"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".ktl.yaml"))
	if err != nil {
		t.Fatalf("read .ktl.yaml: %v", err)
	}
	var cfg appconfig.Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal .ktl.yaml: %v", err)
	}
	if cfg.Build.Profile != "secure" {
		t.Fatalf("expected build.profile secure, got %q", cfg.Build.Profile)
	}
	if cfg.Secrets.DefaultProvider != "vault" {
		t.Fatalf("expected secrets.defaultProvider vault, got %q", cfg.Secrets.DefaultProvider)
	}
	if _, ok := cfg.Secrets.Providers["vault"]; !ok {
		t.Fatalf("expected vault provider to remain")
	}
	if _, ok := cfg.Secrets.Providers["local"]; ok {
		t.Fatalf("did not expect local provider to be injected on merge")
	}
}

func TestInitOutputJSONPreset(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", dir, "--dry-run", "--output", "json", "--preset", "prod"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload struct {
		ConfigYAML string `json:"configYaml"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if !strings.Contains(payload.ConfigYAML, "profile: secure") {
		t.Fatalf("expected secure profile in config, got:\n%s", payload.ConfigYAML)
	}
	if !strings.Contains(payload.ConfigYAML, "vault") {
		t.Fatalf("expected vault provider in config, got:\n%s", payload.ConfigYAML)
	}
}

func TestInitShowDiff(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	existing := "build:\n  profile: dev\n"
	if err := os.WriteFile(filepath.Join(dir, ".ktl.yaml"), []byte(existing), 0o600); err != nil {
		t.Fatalf("write .ktl.yaml: %v", err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", dir, "--merge", "--show-diff", "--output", "json", "--dry-run"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if !strings.Contains(payload.Diff, "@@") {
		t.Fatalf("expected unified diff output, got:\n%s", payload.Diff)
	}
}

func TestInitTemplateFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	templatePath := filepath.Join(dir, "init-template.yaml")
	template := "build:\n  cacheDir: .ktl/cache\n"
	if err := os.WriteFile(templatePath, []byte(template), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", dir, "--dry-run", "--output", "json", "--template", templatePath})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload struct {
		ConfigYAML string `json:"configYaml"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if !strings.Contains(payload.ConfigYAML, "cacheDir: .ktl/cache") {
		t.Fatalf("expected template cacheDir in config, got:\n%s", payload.ConfigYAML)
	}
}

func TestInitDetectsChartAndValues(t *testing.T) {
	dir := copyInitFixture(t, "nested-chart")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", dir, "--dry-run", "--output", "json"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload struct {
		NextSteps []string `json:"nextSteps"`
		Project   struct {
			ChartPath      string `json:"ChartPath"`
			ValuesDevPath  string `json:"ValuesDevPath"`
			ValuesProdPath string `json:"ValuesProdPath"`
		} `json:"project"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if payload.Project.ChartPath != "./services/api/chart" {
		t.Fatalf("expected chart path ./services/api/chart, got %q", payload.Project.ChartPath)
	}
	if payload.Project.ValuesDevPath != "./services/api/values/dev.yaml" {
		t.Fatalf("expected dev values path, got %q", payload.Project.ValuesDevPath)
	}
	if payload.Project.ValuesProdPath != "./services/api/values/prod.yaml" {
		t.Fatalf("expected prod values path, got %q", payload.Project.ValuesProdPath)
	}
	foundDev := false
	foundProd := false
	for _, step := range payload.NextSteps {
		if strings.Contains(step, "values/dev.yaml") {
			foundDev = true
		}
		if strings.Contains(step, "values/prod.yaml") {
			foundProd = true
		}
	}
	if !foundDev || !foundProd {
		t.Fatalf("expected next steps to mention dev and prod values, got: %v", payload.NextSteps)
	}
}

func TestInitDetectsRootChartFixture(t *testing.T) {
	dir := copyInitFixture(t, "root-chart")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", dir, "--dry-run", "--output", "json"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload struct {
		Project struct {
			ChartPath      string `json:"ChartPath"`
			ValuesDevPath  string `json:"ValuesDevPath"`
			ValuesProdPath string `json:"ValuesProdPath"`
		} `json:"project"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if payload.Project.ChartPath != "./chart" {
		t.Fatalf("expected chart path ./chart, got %q", payload.Project.ChartPath)
	}
	if payload.Project.ValuesDevPath != "./values/dev.yaml" {
		t.Fatalf("expected dev values path, got %q", payload.Project.ValuesDevPath)
	}
	if payload.Project.ValuesProdPath != "./values/prod.yaml" {
		t.Fatalf("expected prod values path, got %q", payload.Project.ValuesProdPath)
	}
}

func TestInitDetectsMultipleChartsFixture(t *testing.T) {
	dir := copyInitFixture(t, "multi-chart")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", dir, "--dry-run", "--output", "json"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload struct {
		Notes   []string `json:"notes"`
		Project struct {
			ChartPath       string   `json:"ChartPath"`
			ChartCandidates []string `json:"ChartCandidates"`
		} `json:"project"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if payload.Project.ChartPath != "./services/api/chart" {
		t.Fatalf("expected chart path ./services/api/chart, got %q", payload.Project.ChartPath)
	}
	if len(payload.Project.ChartCandidates) < 2 {
		t.Fatalf("expected multiple chart candidates, got %v", payload.Project.ChartCandidates)
	}
	foundNote := false
	for _, note := range payload.Notes {
		if strings.Contains(note, "Multiple charts detected") {
			foundNote = true
			break
		}
	}
	if !foundNote {
		t.Fatalf("expected note about multiple charts, got %v", payload.Notes)
	}
}

func TestInitPlanOutputsActions(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", dir, "--plan", "--layout", "--values", "--stack", "--gitignore"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".ktl.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no .ktl.yaml to be written, got err=%v", err)
	}

	var plan initPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("parse plan json: %v", err)
	}

	hasConfig := false
	hasChartDir := false
	hasValuesDir := false
	hasValuesDev := false
	hasValuesProd := false
	hasStack := false
	hasGitignore := false

	for _, action := range plan.Actions {
		switch action.Kind {
		case "writeConfig":
			hasConfig = true
		case "createDir":
			if strings.HasSuffix(action.Path, "/chart") || strings.HasSuffix(action.Path, "./chart") {
				hasChartDir = true
			}
			if strings.HasSuffix(action.Path, "/values") || strings.HasSuffix(action.Path, "./values") {
				hasValuesDir = true
			}
		case "writeFile":
			if strings.HasSuffix(action.Path, "values/dev.yaml") {
				hasValuesDev = true
			}
			if strings.HasSuffix(action.Path, "values/prod.yaml") {
				hasValuesProd = true
			}
			if strings.HasSuffix(action.Path, "stack.yaml") {
				hasStack = true
			}
		case "gitignore":
			for _, line := range action.Lines {
				if line == "secrets.dev.yaml" {
					hasGitignore = true
				}
			}
		}
	}

	if !hasConfig {
		t.Fatalf("expected plan to include config write action")
	}
	if !hasChartDir {
		t.Fatalf("expected plan to include chart directory creation")
	}
	if !hasValuesDir || !hasValuesDev || !hasValuesProd {
		t.Fatalf("expected plan to include values scaffolding actions")
	}
	if !hasStack {
		t.Fatalf("expected plan to include stack.yaml creation")
	}
	if !hasGitignore {
		t.Fatalf("expected plan to include gitignore entries")
	}
}

func TestInitApplyPlanWritesFiles(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	planPath := filepath.Join(dir, "init-plan.json")
	root := newRootCommand()
	root.SetArgs([]string{"init", dir, "--plan", "--layout", "--values", "--stack", "--gitignore", "--secrets-provider", "vault", "--plan-output", planPath})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute plan: %v", err)
	}

	root = newRootCommand()
	root.SetArgs([]string{"init", "--apply-plan", planPath})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute apply plan: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".ktl.yaml")); err != nil {
		t.Fatalf("expected .ktl.yaml to be written, got err=%v", err)
	}
	valuesDev, err := os.ReadFile(filepath.Join(dir, "values", "dev.yaml"))
	if err != nil {
		t.Fatalf("read values/dev.yaml: %v", err)
	}
	if !strings.Contains(string(valuesDev), "secret://vault/app/db#password") {
		t.Fatalf("expected vault secret reference in values/dev.yaml, got:\n%s", string(valuesDev))
	}
	if _, err := os.Stat(filepath.Join(dir, "stack.yaml")); err != nil {
		t.Fatalf("expected stack.yaml to be written, got err=%v", err)
	}
	gitignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gitignore), "secrets.dev.yaml") {
		t.Fatalf("expected secrets.dev.yaml in .gitignore, got:\n%s", string(gitignore))
	}
}
