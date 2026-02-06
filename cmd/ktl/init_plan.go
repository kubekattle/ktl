package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type initPlan struct {
	Version       string           `json:"version"`
	GeneratedAt   string           `json:"generatedAt"`
	Mode          string           `json:"mode"`
	RepoRoot      string           `json:"repoRoot"`
	ConfigPath    string           `json:"configPath"`
	ConfigYAML    string           `json:"configYaml"`
	Diff          string           `json:"diff,omitempty"`
	Template      string           `json:"template,omitempty"`
	Preset        string           `json:"preset,omitempty"`
	KubeContext   *kubeContextInfo `json:"kubeContext,omitempty"`
	KubeNote      string           `json:"kubeNote,omitempty"`
	Project       projectLayout    `json:"project"`
	SandboxConfig string           `json:"sandboxConfig,omitempty"`
	SandboxNote   string           `json:"sandboxNote,omitempty"`
	NextSteps     []string         `json:"nextSteps,omitempty"`
	Notes         []string         `json:"notes,omitempty"`
	Actions       []initPlanAction `json:"actions"`
}

type initPlanAction struct {
	Kind    string   `json:"kind"`
	Path    string   `json:"path"`
	Mode    string   `json:"mode,omitempty"`
	Content string   `json:"content,omitempty"`
	Lines   []string `json:"lines,omitempty"`
}

type initPlanOptions struct {
	Mode            string
	RepoRoot        string
	ConfigPath      string
	ConfigYAML      string
	Diff            string
	Template        string
	Preset          string
	KubeContext     *kubeContextInfo
	KubeNote        string
	Project         projectLayout
	Layout          scaffoldResult
	Values          scaffoldResult
	Stack           scaffoldResult
	Gitignore       gitignoreResult
	NextSteps       []string
	Notes           []string
	SandboxConfig   string
	SandboxNote     string
	ValuesEnabled   bool
	SecretsProvider string
}

func buildInitPlan(opts initPlanOptions) initPlan {
	actions := make([]initPlanAction, 0, 8)
	if strings.TrimSpace(opts.ConfigPath) != "" {
		actions = append(actions, initPlanAction{
			Kind:    "writeConfig",
			Path:    relPath(opts.RepoRoot, opts.ConfigPath),
			Mode:    opts.Mode,
			Content: opts.ConfigYAML,
		})
	}
	for _, item := range opts.Layout.Created {
		actions = append(actions, initPlanAction{
			Kind: "createDir",
			Path: item,
		})
	}
	for _, item := range opts.Values.Created {
		if strings.HasSuffix(item, ".yaml") {
			env := "dev"
			if strings.Contains(item, "prod") {
				env = "prod"
			}
			actions = append(actions, initPlanAction{
				Kind:    "writeFile",
				Path:    item,
				Content: buildValuesTemplate(opts.SecretsProvider, env),
			})
			continue
		}
		actions = append(actions, initPlanAction{
			Kind: "createDir",
			Path: item,
		})
	}
	for _, item := range opts.Stack.Created {
		actions = append(actions, initPlanAction{
			Kind:    "writeFile",
			Path:    item,
			Content: renderStackTemplate(opts.RepoRoot, opts.Project, opts.ValuesEnabled),
		})
	}
	if len(opts.Gitignore.Added) > 0 {
		actions = append(actions, initPlanAction{
			Kind:  "gitignore",
			Path:  relPath(opts.RepoRoot, filepath.Join(opts.RepoRoot, ".gitignore")),
			Lines: append([]string(nil), opts.Gitignore.Added...),
		})
	}

	return initPlan{
		Version:       "v1",
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Mode:          opts.Mode,
		RepoRoot:      opts.RepoRoot,
		ConfigPath:    relPath(opts.RepoRoot, opts.ConfigPath),
		ConfigYAML:    opts.ConfigYAML,
		Diff:          opts.Diff,
		Template:      opts.Template,
		Preset:        opts.Preset,
		KubeContext:   opts.KubeContext,
		KubeNote:      opts.KubeNote,
		Project:       opts.Project,
		SandboxConfig: opts.SandboxConfig,
		SandboxNote:   opts.SandboxNote,
		NextSteps:     opts.NextSteps,
		Notes:         opts.Notes,
		Actions:       actions,
	}
}

func writeInitPlan(cmd *cobra.Command, plan initPlan, outputPath string) error {
	if cmd == nil {
		return nil
	}
	w := cmd.OutOrStdout()
	if strings.TrimSpace(outputPath) != "" {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil && filepath.Dir(outputPath) != "." {
			return err
		}
		f, err := os.Create(outputPath)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(plan)
}

func runInitApplyPlan(cmd *cobra.Command, path string, dryRun bool) error {
	plan, err := readInitPlan(path)
	if err != nil {
		return err
	}
	if err := applyInitPlan(plan, dryRun); err != nil {
		return err
	}
	if dryRun && cmd != nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Dry run: plan applied (no changes written).")
	}
	return nil
}

func readInitPlan(path string) (initPlan, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return initPlan{}, fmt.Errorf("plan path is required")
	}
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return initPlan{}, err
		}
		defer f.Close()
		r = f
	}
	var plan initPlan
	dec := json.NewDecoder(r)
	if err := dec.Decode(&plan); err != nil {
		return initPlan{}, err
	}
	if plan.Version == "" {
		return initPlan{}, fmt.Errorf("invalid plan: missing version")
	}
	return plan, nil
}

func applyInitPlan(plan initPlan, dryRun bool) error {
	repoRoot := strings.TrimSpace(plan.RepoRoot)
	for _, action := range plan.Actions {
		switch action.Kind {
		case "createDir":
			path := resolvePlanPath(repoRoot, action.Path)
			if dryRun {
				continue
			}
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case "writeConfig", "writeFile":
			path := resolvePlanPath(repoRoot, action.Path)
			if dryRun {
				continue
			}
			if err := os.WriteFile(path, []byte(action.Content), 0o644); err != nil {
				return err
			}
		case "gitignore":
			if err := applyGitignoreEntries(repoRoot, action.Lines, dryRun); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolvePlanPath(repoRoot string, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if repoRoot == "" {
		return path
	}
	return filepath.Join(repoRoot, strings.TrimPrefix(path, "./"))
}

func applyGitignoreEntries(repoRoot string, entries []string, dryRun bool) error {
	if strings.TrimSpace(repoRoot) == "" {
		return nil
	}
	if len(entries) == 0 {
		return nil
	}
	path := filepath.Join(repoRoot, ".gitignore")
	existing := map[string]struct{}{}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			key := gitignoreKey(line)
			if key != "" {
				existing[key] = struct{}{}
			}
		}
	}
	var toAdd []string
	for _, entry := range entries {
		key := gitignoreKey(entry)
		if key == "" {
			continue
		}
		if _, ok := existing[key]; ok {
			continue
		}
		toAdd = append(toAdd, key)
	}
	if len(toAdd) == 0 || dryRun {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, ok := existing["# ktl"]; !ok {
		if _, err := f.WriteString("# ktl\n"); err != nil {
			return err
		}
	}
	for _, entry := range toAdd {
		if _, err := f.WriteString(entry + "\n"); err != nil {
			return err
		}
	}
	return nil
}
