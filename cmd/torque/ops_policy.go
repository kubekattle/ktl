package main

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	opspolicy "github.com/ingresslabs/torque/internal/ops/policy"
	"github.com/spf13/cobra"
)

func newOpsPolicyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Evaluate ops mutation policy",
	}
	cmd.AddCommand(newOpsPolicyCheckCommand())
	decorateCommandHelp(cmd, "Policy Flags")
	return cmd
}

func newOpsPolicyCheckCommand() *cobra.Command {
	var mode string
	var operation string
	var adapter string
	var targetID string
	var mutating bool
	var unsafe bool
	var approved bool
	var proofGatePassed bool
	var allowUnsafe bool
	var localExperiment bool
	format := "table"
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check whether an ops action is allowed by mutation policy",
		Example: `  torque ops policy check --mode guarded --operation host.command.run --mutating
  torque ops policy check --mode observe-only --operation host.command.run --mutating --format json
  torque ops policy check --mode unsafe --unsafe --mutating --allow-unsafe --local-experiment`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops policy check does not accept positional arguments")
			}
			decision := opspolicy.Evaluate(opspolicy.Request{
				Mode:            opspolicy.Mode(mode),
				Operation:       operation,
				Adapter:         adapter,
				TargetID:        targetID,
				Mutating:        mutating,
				Unsafe:          unsafe,
				Approved:        approved,
				ProofGatePassed: proofGatePassed,
				AllowUnsafe:     allowUnsafe,
				LocalExperiment: localExperiment,
			})
			if err := renderOpsPolicyDecision(cmd.OutOrStdout(), decision, format); err != nil {
				return err
			}
			if decision.Decision == "block" || decision.Decision == "approval-required" || decision.Decision == "manual" {
				return fmt.Errorf("policy decision %s: %s", decision.Decision, decision.Reason)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", string(opspolicy.ModeGuarded), "Mutation policy mode: automatic, guarded, manual, observe-only, unsafe")
	cmd.Flags().StringVar(&operation, "operation", "", "Operation kind to evaluate")
	cmd.Flags().StringVar(&adapter, "adapter", "", "Adapter name or kind to evaluate")
	cmd.Flags().StringVar(&targetID, "target", "", "Target ID to evaluate")
	cmd.Flags().BoolVar(&mutating, "mutating", false, "Operation would mutate a target")
	cmd.Flags().BoolVar(&unsafe, "unsafe", false, "Operation is classified unsafe")
	cmd.Flags().BoolVar(&approved, "approved", false, "Manual approval is present")
	cmd.Flags().BoolVar(&proofGatePassed, "proof-gate-passed", false, "External proof gate passed")
	cmd.Flags().BoolVar(&allowUnsafe, "allow-unsafe", false, "Explicitly allow unsafe local experiment mode")
	cmd.Flags().BoolVar(&localExperiment, "local-experiment", false, "Mark this check as a local experiment")
	cmd.Flags().StringVar(&format, "format", format, "Output format: table or json")
	_ = cmd.RegisterFlagCompletionFunc("mode", cobra.FixedCompletions([]string{"automatic", "guarded", "manual", "observe-only", "unsafe"}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Policy Check Flags")
	return cmd
}

func renderOpsPolicyDecision(w io.Writer, decision opspolicy.Decision, format string) error {
	switch format {
	case "json":
		raw, err := json.MarshalIndent(decision, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(raw))
		return err
	case "table", "":
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintf(tw, "MODE\tDECISION\tMUTATING\tUNSAFE\tREASON\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%t\t%t\t%s\n", decision.Mode, decision.Decision, decision.Mutating, decision.Unsafe, decision.Reason); err != nil {
			return err
		}
		return tw.Flush()
	default:
		return fmt.Errorf("--format must be table or json")
	}
}
