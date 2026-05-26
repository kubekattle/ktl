package terraformadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	adapterAPIVersion = "torque.dev/terraform-adapter/v1"
	moduleAPIVersion  = "torque.dev/module-resource/v1"

	defaultLockTimeout = "5m"

	planFileName       = "torque.tfplan"
	diffPlanFileName   = "torque-diff.tfplan"
	verifyPlanFileName = "torque-verify.tfplan"
	planMetaFileName   = "torque-plan-meta.json"
	configFileName     = "main.tf"
	stateFileName      = "terraform.tfstate"
)

// Options controls adapter execution. The command is intentionally small so it
// can be used as a Torque module.command without coupling stack execution to
// Terraform libraries.
type Options struct {
	TerraformBin  string
	WorkspaceRoot string
	Now           func() time.Time
}

type moduleRequest struct {
	APIVersion string         `json:"apiVersion"`
	Phase      string         `json:"phase"`
	Command    string         `json:"command"`
	DryRun     bool           `json:"dryRun,omitempty"`
	Diff       bool           `json:"diff,omitempty"`
	RunID      string         `json:"runId,omitempty"`
	Attempt    int            `json:"attempt,omitempty"`
	Stack      stackContext   `json:"stack"`
	Node       nodeContext    `json:"node"`
	Module     moduleContext  `json:"module"`
	Input      map[string]any `json:"input,omitempty"`
}

type stackContext struct {
	Root    string `json:"root,omitempty"`
	Name    string `json:"name,omitempty"`
	Profile string `json:"profile,omitempty"`
}

type nodeContext struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Kind               string            `json:"kind"`
	Cluster            string            `json:"cluster,omitempty"`
	Namespace          string            `json:"namespace,omitempty"`
	Tags               []string          `json:"tags,omitempty"`
	Needs              []string          `json:"needs,omitempty"`
	EffectiveInputHash string            `json:"effectiveInputHash,omitempty"`
	Set                map[string]string `json:"set,omitempty"`
}

type moduleContext struct {
	Source  string   `json:"source,omitempty"`
	Version string   `json:"version,omitempty"`
	Phases  []string `json:"phases,omitempty"`
}

type adapterInput struct {
	TerraformBin string        `json:"terraformBin,omitempty"`
	WorkspaceDir string        `json:"workspaceDir,omitempty"`
	LockTimeout  string        `json:"lockTimeout,omitempty"`
	AllowDestroy bool          `json:"allowDestroy,omitempty"`
	Provider     providerInput `json:"provider,omitempty"`
	Resource     resourceInput `json:"resource,omitempty"`
}

type providerInput struct {
	Source    string         `json:"source,omitempty"`
	Version   string         `json:"version,omitempty"`
	LocalName string         `json:"localName,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
	Blocks    map[string]any `json:"blocks,omitempty"`
	RawHCL    string         `json:"rawHCL,omitempty"`
}

type resourceInput struct {
	Type   string         `json:"type,omitempty"`
	Name   string         `json:"name,omitempty"`
	Values map[string]any `json:"values,omitempty"`
	Blocks map[string]any `json:"blocks,omitempty"`
	RawHCL string         `json:"rawHCL,omitempty"`
}

type moduleResult struct {
	APIVersion string         `json:"apiVersion,omitempty"`
	Status     string         `json:"status,omitempty"`
	Message    string         `json:"message,omitempty"`
	Changed    bool           `json:"changed,omitempty"`
	SafeToRun  *bool          `json:"safeToRun,omitempty"`
	Risk       string         `json:"risk,omitempty"`
	Before     any            `json:"before,omitempty"`
	After      any            `json:"after,omitempty"`
	Diff       any            `json:"diff,omitempty"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	Receipt    map[string]any `json:"receipt,omitempty"`
	Cursor     map[string]any `json:"cursor,omitempty"`
	Artifacts  map[string]any `json:"artifacts,omitempty"`
}

type adapter struct {
	req          moduleRequest
	input        adapterInput
	opts         Options
	terraformBin string
	workDir      string
	lockTimeout  string
	configDigest string
}

type commandReceipt struct {
	Command        string   `json:"command"`
	Args           []string `json:"args"`
	ExitCode       int      `json:"exitCode"`
	DurationMillis int64    `json:"durationMillis"`
	StdoutDigest   string   `json:"stdoutDigest,omitempty"`
	StderrDigest   string   `json:"stderrDigest,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type commandRun struct {
	receipt commandReceipt
	stdout  []byte
}

type planSummary struct {
	TerraformVersion string              `json:"terraformVersion,omitempty"`
	Changed          bool                `json:"changed"`
	Risk             string              `json:"risk"`
	UnsafeDestroy    bool                `json:"unsafeDestroy,omitempty"`
	Counts           map[string]int      `json:"counts,omitempty"`
	Changes          []planChangeSummary `json:"changes,omitempty"`
}

type planChangeSummary struct {
	Address      string   `json:"address,omitempty"`
	Mode         string   `json:"mode,omitempty"`
	Type         string   `json:"type,omitempty"`
	Name         string   `json:"name,omitempty"`
	ProviderName string   `json:"providerName,omitempty"`
	Actions      []string `json:"actions,omitempty"`
}

type terraformPlanJSON struct {
	TerraformVersion string                    `json:"terraform_version,omitempty"`
	ResourceChanges  []terraformResourceChange `json:"resource_changes,omitempty"`
}

type terraformResourceChange struct {
	Address      string          `json:"address,omitempty"`
	Mode         string          `json:"mode,omitempty"`
	Type         string          `json:"type,omitempty"`
	Name         string          `json:"name,omitempty"`
	ProviderName string          `json:"provider_name,omitempty"`
	Change       terraformChange `json:"change,omitempty"`
}

type terraformChange struct {
	Actions []string `json:"actions,omitempty"`
}

type stateSummary struct {
	Resources int      `json:"resources"`
	Addresses []string `json:"addresses,omitempty"`
	Digest    string   `json:"digest,omitempty"`
}

type terraformStateJSON struct {
	Values *terraformStateValues `json:"values,omitempty"`
}

type terraformStateValues struct {
	RootModule terraformStateModule `json:"root_module,omitempty"`
}

type terraformStateModule struct {
	Resources    []terraformStateResource `json:"resources,omitempty"`
	ChildModules []terraformStateModule   `json:"child_modules,omitempty"`
}

type terraformStateResource struct {
	Address string `json:"address,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Type    string `json:"type,omitempty"`
	Name    string `json:"name,omitempty"`
}

type planMetadata struct {
	APIVersion     string      `json:"apiVersion"`
	CreatedAt      string      `json:"createdAt"`
	RunID          string      `json:"runId,omitempty"`
	NodeID         string      `json:"nodeId"`
	Command        string      `json:"command"`
	IntentDigest   string      `json:"intentDigest,omitempty"`
	ConfigDigest   string      `json:"configDigest"`
	PlanDigest     string      `json:"planDigest"`
	PlanJSONDigest string      `json:"planJsonDigest"`
	SafeToRun      bool        `json:"safeToRun"`
	Changed        bool        `json:"changed"`
	Risk           string      `json:"risk"`
	TerraformBin   string      `json:"terraformBin,omitempty"`
	Provider       providerRef `json:"provider"`
	Resource       resourceRef `json:"resource"`
	Summary        planSummary `json:"summary"`
}

type providerRef struct {
	Source    string `json:"source,omitempty"`
	Version   string `json:"version,omitempty"`
	LocalName string `json:"localName,omitempty"`
}

type resourceRef struct {
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}

// Run reads one Torque module-resource request and writes one module-resource
// result. It returns transport/encoding errors only; Terraform failures are
// represented in the JSON result so the stack runner can record a receipt.
func Run(ctx context.Context, in io.Reader, out io.Writer, _ io.Writer, opts Options) error {
	req, err := decodeRequest(in)
	var result moduleResult
	if err != nil {
		result = errorResult("invalid terraform adapter request: " + err.Error())
	} else {
		result = Execute(ctx, req, opts)
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(result)
}

func decodeRequest(in io.Reader) (moduleRequest, error) {
	dec := json.NewDecoder(in)
	dec.UseNumber()
	var req moduleRequest
	if err := dec.Decode(&req); err != nil {
		return moduleRequest{}, err
	}
	if strings.TrimSpace(req.APIVersion) != "" && strings.TrimSpace(req.APIVersion) != moduleAPIVersion {
		return moduleRequest{}, fmt.Errorf("unsupported apiVersion %q", req.APIVersion)
	}
	return req, nil
}

// Execute runs a single lifecycle phase for tests and for the CLI wrapper.
func Execute(ctx context.Context, req moduleRequest, opts Options) moduleResult {
	a, err := newAdapter(req, opts)
	if err != nil {
		return errorResult(err.Error())
	}
	switch normalizePhase(req.Phase) {
	case "observe":
		return a.observe(ctx)
	case "diff":
		return a.plan(ctx, diffPlanFileName, false)
	case "plan":
		return a.plan(ctx, planFileName, true)
	case "apply":
		return a.applySavedPlan(ctx, "apply")
	case "delete":
		return a.applySavedPlan(ctx, "delete")
	case "verify":
		return a.verify(ctx)
	default:
		return errorResult(fmt.Sprintf("unsupported terraform adapter phase %q", req.Phase))
	}
}

func newAdapter(req moduleRequest, opts Options) (*adapter, error) {
	input, err := parseInput(req.Input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Provider.LocalName) == "" {
		input.Provider.LocalName = inferProviderLocalName(input.Provider.Source, input.Resource.Type)
	}
	if strings.TrimSpace(input.Resource.Name) == "" {
		input.Resource.Name = "this"
	}
	if strings.TrimSpace(input.LockTimeout) == "" {
		input.LockTimeout = defaultLockTimeout
	}
	if err := validateInput(input); err != nil {
		return nil, err
	}
	workDir, err := resolveWorkspaceDir(req, input, opts)
	if err != nil {
		return nil, err
	}
	bin, err := resolveTerraformBin(opts.TerraformBin, input.TerraformBin)
	if err != nil {
		return nil, err
	}
	return &adapter{
		req:          req,
		input:        input,
		opts:         opts,
		terraformBin: bin,
		workDir:      workDir,
		lockTimeout:  input.LockTimeout,
	}, nil
}

func parseInput(raw map[string]any) (adapterInput, error) {
	if raw == nil {
		return adapterInput{}, fmt.Errorf("terraform adapter requires module.input")
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return adapterInput{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var input adapterInput
	if err := dec.Decode(&input); err != nil {
		return adapterInput{}, err
	}
	return input, nil
}

func validateInput(input adapterInput) error {
	if strings.TrimSpace(input.Provider.Source) == "" {
		return fmt.Errorf("terraform adapter requires provider.source")
	}
	if strings.TrimSpace(input.Provider.LocalName) == "" {
		return fmt.Errorf("terraform adapter could not infer provider.localName")
	}
	if !validHCLName(input.Provider.LocalName) {
		return fmt.Errorf("provider.localName %q is not a valid Terraform local provider name", input.Provider.LocalName)
	}
	if strings.TrimSpace(input.Resource.Type) == "" {
		return fmt.Errorf("terraform adapter requires resource.type")
	}
	if !validHCLName(input.Resource.Type) {
		return fmt.Errorf("resource.type %q is not a valid Terraform resource type", input.Resource.Type)
	}
	if strings.TrimSpace(input.Resource.Name) == "" {
		return fmt.Errorf("terraform adapter requires resource.name")
	}
	if strings.ContainsAny(input.LockTimeout, "\r\n") {
		return fmt.Errorf("lockTimeout must be a single Terraform duration")
	}
	return nil
}

func resolveWorkspaceDir(req moduleRequest, input adapterInput, opts Options) (string, error) {
	if dir := strings.TrimSpace(input.WorkspaceDir); dir != "" {
		if filepath.IsAbs(dir) {
			return dir, nil
		}
		root := strings.TrimSpace(req.Stack.Root)
		if root == "" {
			root = "."
		}
		return filepath.Abs(filepath.Join(root, dir))
	}
	root := strings.TrimSpace(opts.WorkspaceRoot)
	if root == "" {
		root = strings.TrimSpace(req.Stack.Root)
	}
	if root == "" {
		root = "."
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(req.Node.ID)
	if id == "" {
		id = firstNonEmpty(req.Node.Name, req.Input["name"])
	}
	if id == "" {
		id = "resource"
	}
	return filepath.Join(rootAbs, ".torque", "terraform", sanitizePathPart(id)), nil
}

func resolveTerraformBin(optBin string, inputBin string) (string, error) {
	for _, candidate := range []string{
		strings.TrimSpace(optBin),
		strings.TrimSpace(inputBin),
		strings.TrimSpace(os.Getenv("TORQUE_TERRAFORM_BIN")),
	} {
		if candidate == "" {
			continue
		}
		if strings.Contains(candidate, string(os.PathSeparator)) {
			return candidate, nil
		}
		resolved, err := exec.LookPath(candidate)
		if err != nil {
			return "", fmt.Errorf("terraform binary %q not found: %w", candidate, err)
		}
		return resolved, nil
	}
	for _, candidate := range []string{"tofu", "terraform"} {
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("terraform adapter requires OpenTofu or Terraform in PATH, or TORQUE_TERRAFORM_BIN")
}

func (a *adapter) observe(ctx context.Context) moduleResult {
	if err := a.ensureConfig(); err != nil {
		return errorResult(err.Error())
	}
	summary, receipt, err := a.stateSummary(ctx)
	result := baseResult("succeeded", "terraform state observed", false, "none", a.safePtr(true))
	result.Before = summary
	result.Evidence = a.baseEvidence()
	result.Artifacts = map[string]any{
		"terraform-config.json": a.configArtifact(),
	}
	if receipt != nil {
		result.Artifacts["terraform-cli-observe.json"] = receipt
	}
	if err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		result.SafeToRun = a.safePtr(false)
	}
	if summary.Resources == 0 {
		result.Status = "noop"
		result.Message = "terraform state has no managed resources"
	}
	return result
}

func (a *adapter) plan(ctx context.Context, planFile string, writeMeta bool) moduleResult {
	if err := a.ensureConfig(); err != nil {
		return errorResult(err.Error())
	}
	initRun := a.runTerraform(ctx, "init", "-input=false", "-no-color")
	if initRun.receipt.ExitCode != 0 {
		return failedWithReceipts("terraform init failed", map[string]any{
			"terraform-cli-init.json": initRun.receipt,
		})
	}
	destroy := normalizeCommand(a.req.Command) == "delete"
	args := []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-lock-timeout=" + a.lockTimeout, "-out=" + planFile}
	if destroy {
		args = append(args, "-destroy")
	}
	planRun := a.runTerraform(ctx, args...)
	if planRun.receipt.ExitCode != 0 && planRun.receipt.ExitCode != 2 {
		return failedWithReceipts("terraform plan failed", map[string]any{
			"terraform-cli-init.json": initRun.receipt,
			"terraform-cli-plan.json": planRun.receipt,
		})
	}
	showRun := a.runTerraform(ctx, "show", "-json", planFile)
	if showRun.receipt.ExitCode != 0 {
		return failedWithReceipts("terraform show failed for saved plan", map[string]any{
			"terraform-cli-init.json": initRun.receipt,
			"terraform-cli-plan.json": planRun.receipt,
			"terraform-cli-show.json": showRun.receipt,
		})
	}
	summary, err := summarizePlan(showRun.stdout, a.input.AllowDestroy || destroy)
	if err != nil {
		return failedWithReceipts("terraform plan JSON could not be summarized: "+err.Error(), map[string]any{
			"terraform-cli-init.json": initRun.receipt,
			"terraform-cli-plan.json": planRun.receipt,
			"terraform-cli-show.json": showRun.receipt,
		})
	}
	planDigest, _ := fileDigest(filepath.Join(a.workDir, planFile))
	planJSONDigest := digestBytes(showRun.stdout)
	safe := !summary.UnsafeDestroy
	status := "planned"
	message := "terraform plan is safe to apply"
	if !summary.Changed {
		message = "terraform plan has no changes"
	}
	if !safe && writeMeta {
		status = "blocked"
		message = "terraform plan includes destructive actions; set module.input.allowDestroy=true or run stack delete"
	}
	artifacts := map[string]any{
		"terraform-config.json":       a.configArtifact(),
		"terraform-plan-summary.json": summary,
		"terraform-cli-init.json":     initRun.receipt,
		"terraform-cli-plan.json":     planRun.receipt,
		"terraform-cli-show.json":     showRun.receipt,
	}
	if writeMeta {
		meta := a.planMetadata(summary, planDigest, planJSONDigest, safe)
		if err := a.writePlanMetadata(meta); err != nil {
			return failedWithReceipts("terraform plan metadata could not be written: "+err.Error(), artifacts)
		}
		artifacts["terraform-plan-metadata.json"] = meta
	}
	result := baseResult(status, message, summary.Changed, summary.Risk, a.safePtr(safe))
	result.Diff = summary
	result.Evidence = a.baseEvidence()
	result.Evidence["planDigest"] = planDigest
	result.Evidence["planJsonDigest"] = planJSONDigest
	result.Artifacts = artifacts
	return result
}

func (a *adapter) applySavedPlan(ctx context.Context, phase string) moduleResult {
	if err := a.ensureConfig(); err != nil {
		return errorResult(err.Error())
	}
	expectedCommand := "apply"
	if phase == "delete" {
		expectedCommand = "delete"
	}
	meta, err := a.readPlanMetadata()
	if err != nil {
		return errorResult("terraform saved plan metadata is required before " + phase + ": " + err.Error())
	}
	if err := a.validatePlanMetadata(meta, expectedCommand); err != nil {
		result := errorResult(err.Error())
		result.Status = "blocked"
		result.SafeToRun = a.safePtr(false)
		return result
	}
	if !meta.SafeToRun {
		result := errorResult("terraform saved plan was not marked safe to run")
		result.Status = "blocked"
		result.SafeToRun = a.safePtr(false)
		return result
	}
	initRun := a.runTerraform(ctx, "init", "-input=false", "-no-color")
	if initRun.receipt.ExitCode != 0 {
		return failedWithReceipts("terraform init failed", map[string]any{
			"terraform-plan-metadata.json": meta,
			"terraform-cli-init.json":      initRun.receipt,
		})
	}
	applyRun := a.runTerraform(ctx, "apply", "-input=false", "-no-color", "-lock-timeout="+a.lockTimeout, planFileName)
	if applyRun.receipt.ExitCode != 0 {
		return failedWithReceipts("terraform "+phase+" failed", map[string]any{
			"terraform-plan-metadata.json": meta,
			"terraform-cli-init.json":      initRun.receipt,
			"terraform-cli-apply.json":     applyRun.receipt,
		})
	}
	state, stateReceipt, _ := a.stateSummary(ctx)
	artifacts := map[string]any{
		"terraform-plan-metadata.json": meta,
		"terraform-cli-init.json":      initRun.receipt,
		"terraform-cli-apply.json":     applyRun.receipt,
		"terraform-state-summary.json": state,
	}
	if stateReceipt != nil {
		artifacts["terraform-cli-state.json"] = stateReceipt
	}
	result := baseResult("succeeded", "terraform "+phase+" completed", meta.Changed, meta.Risk, a.safePtr(true))
	result.After = state
	result.Evidence = a.baseEvidence()
	result.Evidence["planDigest"] = meta.PlanDigest
	result.Evidence["stateDigest"] = state.Digest
	result.Receipt = map[string]any{
		"planDigest":   meta.PlanDigest,
		"configDigest": meta.ConfigDigest,
		"stateDigest":  state.Digest,
	}
	result.Artifacts = artifacts
	return result
}

func (a *adapter) verify(ctx context.Context) moduleResult {
	if err := a.ensureConfig(); err != nil {
		return errorResult(err.Error())
	}
	initRun := a.runTerraform(ctx, "init", "-input=false", "-no-color")
	if initRun.receipt.ExitCode != 0 {
		return failedWithReceipts("terraform init failed", map[string]any{
			"terraform-cli-init.json": initRun.receipt,
		})
	}
	if normalizeCommand(a.req.Command) == "delete" {
		state, stateReceipt, err := a.stateSummary(ctx)
		artifacts := map[string]any{
			"terraform-cli-init.json":      initRun.receipt,
			"terraform-state-summary.json": state,
		}
		if stateReceipt != nil {
			artifacts["terraform-cli-state.json"] = stateReceipt
		}
		if err != nil {
			return failedWithReceipts("terraform delete verification could not read state: "+err.Error(), artifacts)
		}
		if state.Resources != 0 {
			result := failedWithReceipts("terraform delete verification found managed resources still in state", artifacts)
			result.After = state
			return result
		}
		result := baseResult("succeeded", "terraform delete verified: no managed resources remain", false, "none", a.safePtr(true))
		result.After = state
		result.Evidence = a.baseEvidence()
		result.Evidence["stateDigest"] = state.Digest
		result.Artifacts = artifacts
		return result
	}

	args := []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-lock-timeout=" + a.lockTimeout, "-out=" + verifyPlanFileName}
	planRun := a.runTerraform(ctx, args...)
	artifacts := map[string]any{
		"terraform-cli-init.json":   initRun.receipt,
		"terraform-cli-verify.json": planRun.receipt,
	}
	if planRun.receipt.ExitCode == 0 {
		state, stateReceipt, _ := a.stateSummary(ctx)
		if stateReceipt != nil {
			artifacts["terraform-cli-state.json"] = stateReceipt
		}
		artifacts["terraform-state-summary.json"] = state
		result := baseResult("succeeded", "terraform verify found no drift", false, "none", a.safePtr(true))
		result.After = state
		result.Evidence = a.baseEvidence()
		result.Evidence["stateDigest"] = state.Digest
		result.Artifacts = artifacts
		return result
	}
	if planRun.receipt.ExitCode == 2 {
		showRun := a.runTerraform(ctx, "show", "-json", verifyPlanFileName)
		artifacts["terraform-cli-show.json"] = showRun.receipt
		if showRun.receipt.ExitCode == 0 {
			if summary, err := summarizePlan(showRun.stdout, true); err == nil {
				artifacts["terraform-plan-summary.json"] = summary
			}
		}
		return failedWithReceipts("terraform verify found drift after apply", artifacts)
	}
	return failedWithReceipts("terraform verify plan failed", artifacts)
}

func (a *adapter) ensureConfig() error {
	if err := os.MkdirAll(a.workDir, 0o700); err != nil {
		return err
	}
	hcl, err := renderTerraformConfig(a.input)
	if err != nil {
		return err
	}
	a.configDigest = digestBytes([]byte(hcl))
	return os.WriteFile(filepath.Join(a.workDir, configFileName), []byte(hcl), 0o600)
}

func (a *adapter) runTerraform(ctx context.Context, args ...string) commandRun {
	start := time.Now()
	cmd := exec.CommandContext(ctx, a.terraformBin, args...)
	cmd.Dir = a.workDir
	cmd.Env = append(os.Environ(), "TF_IN_AUTOMATION=1", "TF_INPUT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	errText := ""
	if err != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		errText = redactText(err.Error())
	}
	if ctx.Err() != nil {
		errText = redactText(ctx.Err().Error())
	}
	return commandRun{
		receipt: commandReceipt{
			Command:        filepath.Base(a.terraformBin),
			Args:           append([]string(nil), args...),
			ExitCode:       exitCode,
			DurationMillis: time.Since(start).Milliseconds(),
			StdoutDigest:   digestBytes(stdout.Bytes()),
			StderrDigest:   digestBytes(stderr.Bytes()),
			Error:          errText,
		},
		stdout: stdout.Bytes(),
	}
}

func (a *adapter) stateSummary(ctx context.Context) (stateSummary, *commandReceipt, error) {
	statePath := filepath.Join(a.workDir, stateFileName)
	digest, _ := fileDigest(statePath)
	if _, err := os.Stat(statePath); err != nil {
		if os.IsNotExist(err) {
			return stateSummary{Digest: digest}, nil, nil
		}
		return stateSummary{Digest: digest}, nil, err
	}
	run := a.runTerraform(ctx, "show", "-json")
	receipt := run.receipt
	if run.receipt.ExitCode != 0 {
		return stateSummary{Digest: digest}, &receipt, fmt.Errorf("terraform show state failed")
	}
	summary, err := summarizeState(run.stdout)
	summary.Digest = digest
	return summary, &receipt, err
}

func (a *adapter) planMetadata(summary planSummary, planDigest string, planJSONDigest string, safe bool) planMetadata {
	now := time.Now().UTC()
	if a.opts.Now != nil {
		now = a.opts.Now().UTC()
	}
	return planMetadata{
		APIVersion:     adapterAPIVersion,
		CreatedAt:      now.Format(time.RFC3339),
		RunID:          strings.TrimSpace(a.req.RunID),
		NodeID:         strings.TrimSpace(a.req.Node.ID),
		Command:        normalizeCommand(a.req.Command),
		IntentDigest:   strings.TrimSpace(a.req.Node.EffectiveInputHash),
		ConfigDigest:   a.configDigest,
		PlanDigest:     planDigest,
		PlanJSONDigest: planJSONDigest,
		SafeToRun:      safe,
		Changed:        summary.Changed,
		Risk:           summary.Risk,
		TerraformBin:   filepath.Base(a.terraformBin),
		Provider: providerRef{
			Source:    strings.TrimSpace(a.input.Provider.Source),
			Version:   strings.TrimSpace(a.input.Provider.Version),
			LocalName: strings.TrimSpace(a.input.Provider.LocalName),
		},
		Resource: resourceRef{
			Type: strings.TrimSpace(a.input.Resource.Type),
			Name: strings.TrimSpace(a.input.Resource.Name),
		},
		Summary: summary,
	}
}

func (a *adapter) writePlanMetadata(meta planMetadata) error {
	body, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(a.workDir, planMetaFileName), body, 0o600)
}

func (a *adapter) readPlanMetadata() (planMetadata, error) {
	body, err := os.ReadFile(filepath.Join(a.workDir, planMetaFileName))
	if err != nil {
		return planMetadata{}, err
	}
	var meta planMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return planMetadata{}, err
	}
	return meta, nil
}

func (a *adapter) validatePlanMetadata(meta planMetadata, expectedCommand string) error {
	if strings.TrimSpace(meta.APIVersion) != adapterAPIVersion {
		return fmt.Errorf("saved plan metadata has unsupported apiVersion %q", meta.APIVersion)
	}
	if strings.TrimSpace(meta.NodeID) != strings.TrimSpace(a.req.Node.ID) {
		return fmt.Errorf("saved plan node %q does not match request node %q", meta.NodeID, a.req.Node.ID)
	}
	if normalizeCommand(meta.Command) != expectedCommand {
		return fmt.Errorf("saved plan command %q does not match phase command %q", meta.Command, expectedCommand)
	}
	if strings.TrimSpace(meta.IntentDigest) != "" && strings.TrimSpace(a.req.Node.EffectiveInputHash) != "" && strings.TrimSpace(meta.IntentDigest) != strings.TrimSpace(a.req.Node.EffectiveInputHash) {
		return fmt.Errorf("saved plan intent digest does not match current module input")
	}
	if strings.TrimSpace(meta.ConfigDigest) != a.configDigest {
		return fmt.Errorf("saved plan config digest does not match generated Terraform config")
	}
	planDigest, err := fileDigest(filepath.Join(a.workDir, planFileName))
	if err != nil {
		return fmt.Errorf("saved plan file is missing: %w", err)
	}
	if strings.TrimSpace(meta.PlanDigest) != "" && meta.PlanDigest != planDigest {
		return fmt.Errorf("saved plan digest does not match plan file")
	}
	return nil
}

func (a *adapter) baseEvidence() map[string]any {
	return map[string]any{
		"adapter":        adapterAPIVersion,
		"terraformBin":   filepath.Base(a.terraformBin),
		"workspace":      a.workDir,
		"configDigest":   a.configDigest,
		"provider":       a.providerArtifact(),
		"resource":       a.resourceArtifact(),
		"lockFileDigest": optionalFileDigest(filepath.Join(a.workDir, ".terraform.lock.hcl")),
	}
}

func (a *adapter) configArtifact() map[string]any {
	return map[string]any{
		"configDigest": a.configDigest,
		"workspace":    a.workDir,
		"provider":     a.providerArtifact(),
		"resource":     a.resourceArtifact(),
	}
}

func (a *adapter) providerArtifact() providerRef {
	return providerRef{
		Source:    strings.TrimSpace(a.input.Provider.Source),
		Version:   strings.TrimSpace(a.input.Provider.Version),
		LocalName: strings.TrimSpace(a.input.Provider.LocalName),
	}
}

func (a *adapter) resourceArtifact() resourceRef {
	return resourceRef{
		Type: strings.TrimSpace(a.input.Resource.Type),
		Name: strings.TrimSpace(a.input.Resource.Name),
	}
}

func (a *adapter) safePtr(v bool) *bool {
	return &v
}

func summarizePlan(raw []byte, allowDestroy bool) (planSummary, error) {
	var plan terraformPlanJSON
	if err := json.Unmarshal(raw, &plan); err != nil {
		return planSummary{}, err
	}
	summary := planSummary{
		TerraformVersion: plan.TerraformVersion,
		Risk:             "none",
		Counts:           map[string]int{},
	}
	for _, change := range plan.ResourceChanges {
		actions := normalizeActions(change.Change.Actions)
		if len(actions) == 0 || isNoopActions(actions) {
			continue
		}
		key := strings.Join(actions, ",")
		summary.Counts[key]++
		summary.Changed = true
		if actionHas(actions, "delete") && !allowDestroy {
			summary.UnsafeDestroy = true
		}
		summary.Changes = append(summary.Changes, planChangeSummary{
			Address:      change.Address,
			Mode:         change.Mode,
			Type:         change.Type,
			Name:         change.Name,
			ProviderName: change.ProviderName,
			Actions:      actions,
		})
	}
	summary.Risk = riskForChanges(summary.Changes)
	if len(summary.Counts) == 0 {
		summary.Counts = nil
	}
	return summary, nil
}

func summarizeState(raw []byte) (stateSummary, error) {
	var state terraformStateJSON
	if err := json.Unmarshal(raw, &state); err != nil {
		return stateSummary{}, err
	}
	var addresses []string
	if state.Values != nil {
		collectStateResources(state.Values.RootModule, &addresses)
	}
	sort.Strings(addresses)
	return stateSummary{Resources: len(addresses), Addresses: addresses}, nil
}

func collectStateResources(module terraformStateModule, addresses *[]string) {
	for _, res := range module.Resources {
		if strings.TrimSpace(res.Address) != "" {
			*addresses = append(*addresses, res.Address)
		}
	}
	for _, child := range module.ChildModules {
		collectStateResources(child, addresses)
	}
}

func riskForChanges(changes []planChangeSummary) string {
	risk := "none"
	for _, change := range changes {
		switch {
		case actionHas(change.Actions, "delete"):
			return "high"
		case actionHas(change.Actions, "update"):
			risk = "medium"
		case risk == "none" && actionHas(change.Actions, "create"):
			risk = "low"
		}
	}
	return risk
}

func normalizeActions(actions []string) []string {
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		action = strings.ToLower(strings.TrimSpace(action))
		if action != "" {
			out = append(out, action)
		}
	}
	return out
}

func isNoopActions(actions []string) bool {
	return len(actions) == 1 && actions[0] == "no-op"
}

func actionHas(actions []string, target string) bool {
	for _, action := range actions {
		if action == target {
			return true
		}
	}
	return false
}

func baseResult(status string, message string, changed bool, risk string, safe *bool) moduleResult {
	return moduleResult{
		APIVersion: adapterAPIVersion,
		Status:     status,
		Message:    message,
		Changed:    changed,
		SafeToRun:  safe,
		Risk:       risk,
		Artifacts:  map[string]any{},
	}
}

func errorResult(message string) moduleResult {
	safe := false
	return moduleResult{
		APIVersion: adapterAPIVersion,
		Status:     "error",
		Message:    redactText(message),
		SafeToRun:  &safe,
		Risk:       "unknown",
		Artifacts:  map[string]any{},
	}
}

func failedWithReceipts(message string, artifacts map[string]any) moduleResult {
	safe := false
	return moduleResult{
		APIVersion: adapterAPIVersion,
		Status:     "failed",
		Message:    redactText(message),
		SafeToRun:  &safe,
		Risk:       "unknown",
		Artifacts:  artifacts,
	}
}

func digestBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func fileDigest(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return digestBytes(body), nil
}

func optionalFileDigest(path string) string {
	digest, err := fileDigest(path)
	if err != nil {
		return ""
	}
	return digest
}

var secretLineRE = regexp.MustCompile(`(?i)(access[_-]?key|secret[_-]?key|session[_-]?token|token|password|passwd|secret)\s*[:=]\s*[^,\s]+`)

func redactText(s string) string {
	return secretLineRE.ReplaceAllString(s, "$1=<redacted>")
}

func normalizePhase(phase string) string {
	return strings.ToLower(strings.TrimSpace(phase))
}

func normalizeCommand(command string) string {
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "" {
		return "apply"
	}
	return command
}

func firstNonEmpty(values ...any) string {
	for _, value := range values {
		s := strings.TrimSpace(fmt.Sprint(value))
		if s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}

func inferProviderLocalName(source string, resourceType string) string {
	resourceType = strings.TrimSpace(resourceType)
	if idx := strings.Index(resourceType, "_"); idx > 0 {
		return resourceType[:idx]
	}
	source = strings.Trim(strings.TrimSpace(source), "/")
	if source != "" {
		parts := strings.Split(source, "/")
		return parts[len(parts)-1]
	}
	return ""
}

func sanitizePathPart(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "resource"
	}
	return out
}

var hclNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

func validHCLName(s string) bool {
	return hclNameRE.MatchString(strings.TrimSpace(s))
}

func renderTerraformConfig(input adapterInput) (string, error) {
	var b strings.Builder
	local := strings.TrimSpace(input.Provider.LocalName)
	b.WriteString("terraform {\n")
	b.WriteString("  required_providers {\n")
	b.WriteString("    " + local + " = {\n")
	b.WriteString("      source = " + strconv.Quote(strings.TrimSpace(input.Provider.Source)) + "\n")
	if strings.TrimSpace(input.Provider.Version) != "" {
		b.WriteString("      version = " + strconv.Quote(strings.TrimSpace(input.Provider.Version)) + "\n")
	}
	b.WriteString("    }\n")
	b.WriteString("  }\n")
	b.WriteString("}\n\n")

	b.WriteString("provider " + strconv.Quote(local) + " {\n")
	if err := renderBody(&b, "  ", input.Provider.Config, input.Provider.Blocks, input.Provider.RawHCL); err != nil {
		return "", fmt.Errorf("render provider: %w", err)
	}
	b.WriteString("}\n\n")

	b.WriteString("resource " + strconv.Quote(strings.TrimSpace(input.Resource.Type)) + " " + strconv.Quote(strings.TrimSpace(input.Resource.Name)) + " {\n")
	if err := renderBody(&b, "  ", input.Resource.Values, input.Resource.Blocks, input.Resource.RawHCL); err != nil {
		return "", fmt.Errorf("render resource: %w", err)
	}
	b.WriteString("}\n")
	return b.String(), nil
}

func renderBody(b *strings.Builder, indent string, values map[string]any, blocks map[string]any, rawHCL string) error {
	for _, key := range sortedKeys(values) {
		if !validHCLName(key) {
			return fmt.Errorf("attribute name %q is not valid HCL", key)
		}
		rendered, err := renderHCLValue(values[key], indent)
		if err != nil {
			return fmt.Errorf("attribute %s: %w", key, err)
		}
		b.WriteString(indent + key + " = " + rendered + "\n")
	}
	for _, key := range sortedKeys(blocks) {
		if !validHCLName(key) {
			return fmt.Errorf("block name %q is not valid HCL", key)
		}
		if err := renderBlocks(b, indent, key, blocks[key]); err != nil {
			return err
		}
	}
	if strings.TrimSpace(rawHCL) != "" {
		for _, line := range strings.Split(strings.TrimRight(rawHCL, "\n"), "\n") {
			b.WriteString(indent + line + "\n")
		}
	}
	return nil
}

func renderBlocks(b *strings.Builder, indent string, name string, raw any) error {
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if err := renderOneBlock(b, indent, name, item); err != nil {
				return err
			}
		}
	default:
		return renderOneBlock(b, indent, name, raw)
	}
	return nil
}

func renderOneBlock(b *strings.Builder, indent string, name string, raw any) error {
	values, blocks, labels, rawHCL, err := blockParts(raw)
	if err != nil {
		return fmt.Errorf("block %s: %w", name, err)
	}
	labelText := ""
	for _, label := range labels {
		labelText += " " + strconv.Quote(label)
	}
	b.WriteString(indent + name + labelText + " {\n")
	if err := renderBody(b, indent+"  ", values, blocks, rawHCL); err != nil {
		return err
	}
	b.WriteString(indent + "}\n")
	return nil
}

func blockParts(raw any) (map[string]any, map[string]any, []string, string, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, nil, nil, "", fmt.Errorf("block value must be an object")
	}
	special := false
	for _, key := range []string{"values", "blocks", "labels", "rawHCL"} {
		if _, ok := m[key]; ok {
			special = true
			break
		}
	}
	if !special {
		return m, nil, nil, "", nil
	}
	values, err := mapValue(m["values"])
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("values: %w", err)
	}
	blocks, err := mapValue(m["blocks"])
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("blocks: %w", err)
	}
	labels, err := stringSliceValue(m["labels"])
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("labels: %w", err)
	}
	rawHCL, _ := m["rawHCL"].(string)
	return values, blocks, labels, rawHCL, nil
}

func mapValue(raw any) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("must be an object")
	}
	return m, nil
}

func stringSliceValue(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("must be a list")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("labels must be strings")
		}
		out = append(out, s)
	}
	return out, nil
}

func renderHCLValue(value any, indent string) (string, error) {
	switch v := value.(type) {
	case nil:
		return "null", nil
	case string:
		return strconv.Quote(v), nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case json.Number:
		return v.String(), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case []any:
		if len(v) == 0 {
			return "[]", nil
		}
		var parts []string
		for _, item := range v {
			rendered, err := renderHCLValue(item, indent+"  ")
			if err != nil {
				return "", err
			}
			parts = append(parts, rendered)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case map[string]any:
		if raw, ok := v["__hcl"]; ok && len(v) == 1 {
			s, ok := raw.(string)
			if !ok {
				return "", fmt.Errorf("__hcl value must be a string")
			}
			if strings.ContainsAny(s, "\r\n") {
				return "", fmt.Errorf("__hcl expression must be one line")
			}
			return s, nil
		}
		if len(v) == 0 {
			return "{}", nil
		}
		var b strings.Builder
		b.WriteString("{\n")
		for _, key := range sortedKeys(v) {
			rendered, err := renderHCLValue(v[key], indent+"  ")
			if err != nil {
				return "", err
			}
			b.WriteString(indent + "  " + strconv.Quote(key) + " = " + rendered + "\n")
		}
		b.WriteString(indent + "}")
		return b.String(), nil
	default:
		return "", fmt.Errorf("unsupported value type %T", value)
	}
}

func sortedKeys(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
