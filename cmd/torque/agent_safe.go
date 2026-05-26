package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type agentPolicyOptions struct {
	Proof       string
	Policy      string
	Pub         string
	Out         string
	Format      string
	Allow       []string
	RequireGate bool
}

type agentRunOptions struct {
	agentPolicyOptions
	Actor     string
	Operation string
	Command   string
}

type agentPolicyRequest struct {
	Version     string   `json:"version,omitempty"`
	Actor       string   `json:"actor,omitempty"`
	Operation   string   `json:"operation,omitempty"`
	Command     []string `json:"command,omitempty"`
	Release     string   `json:"release,omitempty"`
	Namespace   string   `json:"namespace,omitempty"`
	Proof       string   `json:"proof,omitempty"`
	RequireGate bool     `json:"requireGate,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

type agentPolicyReport struct {
	Version     string             `json:"version"`
	GeneratedAt string             `json:"generatedAt"`
	Request     agentPolicyRequest `json:"request"`
	Proof       string             `json:"proof,omitempty"`
	Allowed     bool               `json:"allowed"`
	RequireGate bool               `json:"requireGate"`
	AllowedOps  []string           `json:"allowedOps,omitempty"`
	Gate        *agentGateSummary  `json:"gate,omitempty"`
	Checks      []agentPolicyCheck `json:"checks"`
}

type agentGateSummary struct {
	Passed       bool `json:"passed"`
	Checks       int  `json:"checks"`
	Failed       int  `json:"failed"`
	FilesChecked int  `json:"filesChecked"`
}

type agentPolicyCheck struct {
	ID      string `json:"id"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

type agentRunReport struct {
	Version     string             `json:"version"`
	GeneratedAt string             `json:"generatedAt"`
	Authorized  bool               `json:"authorized"`
	Executed    bool               `json:"executed"`
	DryRun      bool               `json:"dryRun"`
	Request     agentPolicyRequest `json:"request"`
	Policy      agentPolicyReport  `json:"policy"`
	Message     string             `json:"message"`
}

func newAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Authorize agent operations with proof-backed policy",
		Long:  "Authorize AI or automation agent operations only when the request is explicitly allowed and the referenced proof graph satisfies the release gate.",
	}
	cmd.AddCommand(newAgentPolicyCommand())
	cmd.AddCommand(newAgentRunCommand())
	cmd.AddCommand(newAgentApplianceCommand())
	decorateCommandHelp(cmd, "Agent Commands")
	return cmd
}

func newAgentPolicyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Evaluate agent operation policy",
		Long:  "Evaluate proof-backed policy for agent operations before any mutating Torque action is allowed.",
	}
	cmd.AddCommand(newAgentPolicyCheckCommand())
	decorateCommandHelp(cmd, "Agent Policy Commands")
	return cmd
}

func newAgentPolicyCheckCommand() *cobra.Command {
	opts := agentPolicyOptions{Format: "text", RequireGate: true}
	cmd := &cobra.Command{
		Use:   "check <request.json>",
		Short: "Check whether an agent request is allowed by proof policy",
		Long:  "Check an agent request JSON against an explicit allow-list and a signed proof graph release gate.",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateAgentFormat(opts.Format)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			request, err := loadAgentPolicyRequest(args[0])
			if err != nil {
				return err
			}
			report, err := evaluateAgentPolicy(request, opts)
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeJSONFileEnsured(opts.Out, report); err != nil {
					return fmt.Errorf("write agent policy report: %w", err)
				}
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
			} else {
				renderAgentPolicyText(cmd.OutOrStdout(), report, opts.Out)
			}
			if !report.Allowed {
				return fmt.Errorf("agent policy denied")
			}
			return nil
		},
	}
	bindAgentPolicyFlags(cmd, &opts)
	decorateCommandHelp(cmd, "Agent Policy Check Flags")
	return cmd
}

func newAgentRunCommand() *cobra.Command {
	opts := agentRunOptions{agentPolicyOptions: agentPolicyOptions{Format: "text", RequireGate: true}}
	cmd := &cobra.Command{
		Use:   "run [request.json]",
		Short: "Authorize an agent run with proof-backed policy",
		Long:  "Authorize an agent run with proof-backed policy and return a run record. This command is intentionally non-mutating; it records authorization for the caller to use before invoking a write operation.",
		Args:  cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateAgentFormat(opts.Format)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			request, err := buildAgentRunRequest(args, opts)
			if err != nil {
				return err
			}
			policy, err := evaluateAgentPolicy(request, opts.agentPolicyOptions)
			if err != nil {
				return err
			}
			report := agentRunReport{
				Version:     "v1",
				GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
				Authorized:  policy.Allowed,
				Executed:    false,
				DryRun:      true,
				Request:     request,
				Policy:      policy,
				Message:     "authorized proof-backed agent run; execution is delegated to the caller",
			}
			if !policy.Allowed {
				report.Message = "agent run denied by policy"
			}
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeJSONFileEnsured(opts.Out, report); err != nil {
					return fmt.Errorf("write agent run report: %w", err)
				}
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
			} else {
				renderAgentRunText(cmd.OutOrStdout(), report, opts.Out)
			}
			if !report.Authorized {
				return fmt.Errorf("agent run denied")
			}
			return nil
		},
	}
	bindAgentPolicyFlags(cmd, &opts.agentPolicyOptions)
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "Actor identity to record when no request file is supplied")
	cmd.Flags().StringVar(&opts.Operation, "operation", "", "Requested operation when no request file is supplied")
	cmd.Flags().StringVar(&opts.Command, "command", "", "Command string to record when no request file is supplied")
	decorateCommandHelp(cmd, "Agent Run Flags")
	return cmd
}

func bindAgentPolicyFlags(cmd *cobra.Command, opts *agentPolicyOptions) {
	cmd.Flags().StringVar(&opts.Proof, "proof", "", "Signed proof graph required for mutating operations")
	cmd.Flags().StringVar(&opts.Policy, "policy", "", "Optional proof gate policy file")
	cmd.Flags().StringVar(&opts.Pub, "pub", "", "Optional trusted ed25519 public/private key JSON for graph verification")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write JSON report to this path")
	cmd.Flags().StringArrayVar(&opts.Allow, "allow", nil, "Allowed operation name (repeatable or comma-separated)")
	cmd.Flags().BoolVar(&opts.RequireGate, "require-gate", true, "Require proof gate success before allowing the operation")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
}

func validateAgentFormat(format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text", "json":
		return nil
	default:
		return fmt.Errorf("unsupported --format %q (expected text or json)", format)
	}
}

func loadAgentPolicyRequest(path string) (agentPolicyRequest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return agentPolicyRequest{}, fmt.Errorf("read agent request: %w", err)
	}
	var request agentPolicyRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return agentPolicyRequest{}, fmt.Errorf("parse agent request: %w", err)
	}
	request.Operation = normalizeAgentOperation(firstNonEmpty(request.Operation, inferAgentOperation(request.Command)))
	return request, nil
}

func buildAgentRunRequest(args []string, opts agentRunOptions) (agentPolicyRequest, error) {
	if len(args) > 0 {
		request, err := loadAgentPolicyRequest(args[0])
		if err != nil {
			return agentPolicyRequest{}, err
		}
		return request, nil
	}
	allowed := parseAgentAllowList(opts.Allow)
	operation := normalizeAgentOperation(opts.Operation)
	if operation == "" && len(allowed) == 1 {
		operation = allowed[0]
	}
	command := strings.Fields(strings.TrimSpace(opts.Command))
	if len(command) == 0 && operation != "" {
		command = []string{"torque", operation}
	}
	request := agentPolicyRequest{
		Version:     "v1",
		Actor:       firstNonEmpty(opts.Actor, "agent"),
		Operation:   operation,
		Command:     command,
		Proof:       opts.Proof,
		RequireGate: opts.RequireGate,
	}
	return request, nil
}

func evaluateAgentPolicy(request agentPolicyRequest, opts agentPolicyOptions) (agentPolicyReport, error) {
	request.Operation = normalizeAgentOperation(firstNonEmpty(request.Operation, inferAgentOperation(request.Command)))
	proofPath := firstNonEmpty(opts.Proof, request.Proof)
	requireGate := opts.RequireGate || request.RequireGate || agentOperationRequiresProof(request.Operation)
	allowedOps := parseAgentAllowList(opts.Allow)
	report := agentPolicyReport{
		Version:     "v1",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Request:     request,
		Proof:       proofPath,
		Allowed:     true,
		RequireGate: requireGate,
		AllowedOps:  allowedOps,
	}
	addAgentPolicyCheck(&report, "request.operation", request.Operation != "", "request declares an operation")
	if len(allowedOps) > 0 {
		addAgentPolicyCheck(&report, "operation.allowed", stringInSlice(request.Operation, allowedOps), "operation is explicitly allowed")
	} else {
		addAgentPolicyCheck(&report, "operation.allowed", !agentOperationRequiresProof(request.Operation), "read-only operation allowed without explicit --allow")
	}
	if agentOperationRequiresProof(request.Operation) {
		addAgentPolicyCheck(&report, "proof.required", strings.TrimSpace(proofPath) != "", "mutating operation includes a proof graph")
	}
	if requireGate {
		if strings.TrimSpace(proofPath) == "" {
			addAgentPolicyCheck(&report, "proof.gate", false, "proof gate is required but no proof graph was supplied")
		} else {
			policy, err := loadProofGatePolicy(opts.Policy)
			if err != nil {
				return agentPolicyReport{}, err
			}
			gate, err := gateProofSource(proofPath, policy, proofGateOptions{Policy: opts.Policy, Pub: opts.Pub, Format: "json"})
			if err != nil {
				return agentPolicyReport{}, err
			}
			report.Gate = &agentGateSummary{
				Passed:       gate.Passed,
				Checks:       gate.Summary.Checks,
				Failed:       gate.Summary.Failed,
				FilesChecked: gate.Verification.FilesChecked,
			}
			addAgentPolicyCheck(&report, "proof.gate", gate.Passed, "proof graph passes release gate")
			if request.Release != "" && gate.Release != "" {
				addAgentPolicyCheck(&report, "proof.release", request.Release == gate.Release, "request release matches proof release")
			}
			if request.Namespace != "" && gate.Namespace != "" {
				addAgentPolicyCheck(&report, "proof.namespace", request.Namespace == gate.Namespace, "request namespace matches proof namespace")
			}
		}
	}
	sort.Slice(report.Checks, func(i, j int) bool { return report.Checks[i].ID < report.Checks[j].ID })
	report.Allowed = true
	for _, check := range report.Checks {
		if !check.Passed {
			report.Allowed = false
			break
		}
	}
	return report, nil
}

func addAgentPolicyCheck(report *agentPolicyReport, id string, passed bool, message string) {
	report.Checks = append(report.Checks, agentPolicyCheck{
		ID:      id,
		Passed:  passed,
		Message: message,
	})
}

func parseAgentAllowList(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			op := normalizeAgentOperation(part)
			if op == "" || seen[op] {
				continue
			}
			seen[op] = true
			out = append(out, op)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeAgentOperation(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	switch value {
	case "stack apply":
		return "stack-apply"
	case "stack delete":
		return "stack-delete"
	default:
		return value
	}
}

func inferAgentOperation(command []string) string {
	for i, token := range command {
		token = normalizeAgentOperation(token)
		if token == "" || token == "torque" {
			continue
		}
		if token == "stack" && i+1 < len(command) {
			next := normalizeAgentOperation(command[i+1])
			if next != "" {
				return "stack-" + next
			}
		}
		return token
	}
	return ""
}

func agentOperationRequiresProof(operation string) bool {
	switch normalizeAgentOperation(operation) {
	case "apply", "delete", "revert", "repair", "fix", "ship", "stack-apply", "stack-delete", "release-promote":
		return true
	default:
		return false
	}
}

func stringInSlice(value string, values []string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func renderAgentPolicyText(out io.Writer, report agentPolicyReport, outPath string) {
	fmt.Fprintf(out, "Agent policy: %s\n", strings.ToUpper(mapBool(report.Allowed, "allowed", "denied")))
	fmt.Fprintf(out, "Operation: %s\n", firstNonEmpty(report.Request.Operation, "-"))
	if report.Proof != "" {
		fmt.Fprintf(out, "Proof: %s\n", report.Proof)
	}
	if report.Gate != nil {
		fmt.Fprintf(out, "Gate: %s (%d checks, %d failed)\n", strings.ToUpper(passFail(report.Gate.Passed)), report.Gate.Checks, report.Gate.Failed)
	}
	if strings.TrimSpace(outPath) != "" {
		fmt.Fprintf(out, "Report JSON: %s\n", outPath)
	}
	for _, check := range report.Checks {
		if check.Passed {
			continue
		}
		fmt.Fprintf(out, "Denied: %s: %s\n", check.ID, check.Message)
	}
}

func renderAgentRunText(out io.Writer, report agentRunReport, outPath string) {
	fmt.Fprintf(out, "Agent run: %s\n", strings.ToUpper(mapBool(report.Authorized, "authorized", "denied")))
	fmt.Fprintf(out, "Operation: %s\n", firstNonEmpty(report.Request.Operation, "-"))
	fmt.Fprintf(out, "Executed: %t\n", report.Executed)
	if strings.TrimSpace(outPath) != "" {
		fmt.Fprintf(out, "Run JSON: %s\n", outPath)
	}
	if report.Message != "" {
		fmt.Fprintf(out, "%s\n", report.Message)
	}
}
