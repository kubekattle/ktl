package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	opsadapter "github.com/ingresslabs/torque/internal/ops/adapter"
	"github.com/spf13/cobra"
)

func newOpsAdapterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adapter",
		Short: "Inspect ops adapter contracts",
	}
	cmd.AddCommand(newOpsAdapterCapabilitiesCommand())
	decorateCommandHelp(cmd, "Adapter Flags")
	return cmd
}

func newOpsAdapterCapabilitiesCommand() *cobra.Command {
	var format string
	var target string
	var transport string
	var timeout time.Duration
	var identityFile string
	var sshArgs []string
	cmd := &cobra.Command{
		Use:   "capabilities [adapter]",
		Short: "Show adapter capability contracts",
		Example: `  torque ops adapter capabilities
  torque ops adapter capabilities host.command.run --format json
  torque ops adapter capabilities host.command.run --target ssh://root@lab-host --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			adapterName := ""
			if len(args) > 0 {
				adapterName = strings.TrimSpace(args[0])
			}
			result, err := opsadapter.BuildCapabilityList(cmd.Context(), adapterName, opsadapter.ProbeOptions{
				Target:       target,
				Transport:    transport,
				Timeout:      timeout,
				IdentityFile: identityFile,
				SSHExtraArgs: sshArgs,
			})
			if err != nil {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "", "table":
				return renderOpsAdapterCapabilitiesTable(cmd.OutOrStdout(), result)
			case "json":
				raw, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return err
			default:
				return fmt.Errorf("--format must be table or json")
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table or json")
	cmd.Flags().StringVar(&target, "target", "", "Optional probe target such as local://localhost or ssh://root@host")
	cmd.Flags().StringVar(&transport, "transport", "", "Probe transport override: local or ssh")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Second, "Timeout for each probe operation")
	cmd.Flags().StringVar(&identityFile, "identity-file", "", "SSH identity file for --target ssh://... probes")
	cmd.Flags().StringArrayVar(&sshArgs, "ssh-arg", nil, "Extra SSH argument for probe transport (repeatable)")
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("transport", cobra.FixedCompletions([]string{"local", "ssh"}, cobra.ShellCompDirectiveNoFileComp))
	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for _, name := range opsadapter.KnownAdapterNames() {
			if strings.HasPrefix(name, toComplete) {
				out = append(out, name)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
	decorateCommandHelp(cmd, "Adapter Capability Flags")
	return cmd
}

func renderOpsAdapterCapabilitiesTable(w io.Writer, result *opsadapter.CapabilityList) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ADAPTER\tSTATUS\tCLASS\tMUTATES\tTRANSPORTS\tPHASES\tPROBE")
	if result != nil {
		for _, cap := range result.Adapters {
			probeStatus := "-"
			if cap.Probe != nil {
				probeStatus = strings.TrimSpace(cap.Probe.Status)
				if probeStatus == "" {
					probeStatus = "-"
				}
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%s\t%s\t%s\n",
				cap.Adapter,
				cap.Status,
				cap.Classification,
				cap.Mutating,
				strings.Join(cap.Transports, ","),
				strings.Join(cap.SupportedPhases, ","),
				probeStatus,
			)
		}
	}
	return tw.Flush()
}
