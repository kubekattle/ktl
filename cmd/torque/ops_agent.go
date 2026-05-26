package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
	"github.com/ingresslabs/torque/internal/ops/inventory"
	"github.com/spf13/cobra"
)

var collectOpsAgentStatus = heartbeat.Collect

func newOpsAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Inspect and manage ops agents",
	}
	cmd.AddCommand(newOpsAgentStatusCommand())
	decorateCommandHelp(cmd, "Ops Agent Flags")
	return cmd
}

func newOpsAgentStatusCommand() *cobra.Command {
	var natsURL string
	var creds string
	var nkey string
	var tenant string
	var selectorValues []string
	var timeout time.Duration
	var staleAfter time.Duration
	format := "table"

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Collect live ops-agent heartbeats from NATS",
		Example: `  torque ops agent status --nats-url nats://127.0.0.1:4222 --tenant lab
  torque ops agent status --selector role=mysql --format json
  torque-agent nats heartbeat --agent-id host-141 --tenant lab --label role=mysql`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops agent status does not accept positional arguments")
			}
			selector, err := inventory.ParseSelector(selectorValues)
			if err != nil {
				return err
			}
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be greater than zero")
			}
			if staleAfter <= 0 {
				return fmt.Errorf("--stale-after must be greater than zero")
			}
			snapshot, err := collectOpsAgentStatus(cmd.Context(), heartbeat.CollectOptions{
				NATS: heartbeat.NATSConfig{
					Server:  strings.TrimSpace(natsURL),
					Creds:   strings.TrimSpace(creds),
					NKey:    strings.TrimSpace(nkey),
					Timeout: timeout,
					Name:    "torque-ops-agent-status",
				},
				Tenant:     tenant,
				Selector:   selector,
				StaleAfter: staleAfter,
				Listen:     timeout,
			})
			if err != nil {
				return err
			}
			switch format {
			case "json":
				raw, err := json.MarshalIndent(snapshot, "", "  ")
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return err
			case "table", "":
				return renderOpsAgentStatusTable(cmd.OutOrStdout(), snapshot)
			default:
				return fmt.Errorf("--format must be table or json")
			}
		},
	}
	cmd.Flags().StringVar(&natsURL, "nats-url", firstNonEmpty(os.Getenv("TORQUE_NATS_URL"), os.Getenv("TORQUE_NATS_SERVER")), "NATS server URL")
	cmd.Flags().StringVar(&creds, "creds", strings.TrimSpace(os.Getenv("TORQUE_NATS_CREDS")), "NATS user credentials file")
	cmd.Flags().StringVar(&nkey, "nkey", strings.TrimSpace(os.Getenv("TORQUE_NATS_NKEY")), "NATS NKey seed file")
	cmd.Flags().StringVar(&tenant, "tenant", firstNonEmpty(os.Getenv("TORQUE_AGENT_TENANT"), heartbeat.DefaultTenant), "Tenant namespace to inspect")
	cmd.Flags().StringArrayVar(&selectorValues, "selector", nil, "Agent label selector as key=value (repeatable)")
	cmd.Flags().DurationVar(&timeout, "timeout", 2*time.Second, "Heartbeat collection window and NATS connection timeout")
	cmd.Flags().DurationVar(&staleAfter, "stale-after", 45*time.Second, "Mark agents stale after this age")
	cmd.Flags().StringVar(&format, "format", format, "Output format: table or json")
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Ops Agent Status Flags")
	return cmd
}

func renderOpsAgentStatusTable(out io.Writer, snapshot heartbeat.Snapshot) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AGENT\tTARGET\tHEALTH\tSTATE\tAGE\tVERSION\tLABELS\tCAPABILITIES")
	for _, agent := range snapshot.Agents {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			emptyDash(agent.AgentID),
			emptyDash(agent.TargetID),
			emptyDash(agent.Health),
			emptyDash(agent.State),
			emptyDash(agent.Age),
			emptyDash(agent.Version),
			inventory.FormatLabels(agent.Labels),
			inventory.FormatList(agent.Capabilities),
		)
	}
	fmt.Fprintf(tw, "\nTOTAL\t%d\tREADY\t%d\tSTALE\t%d\tDEGRADED\t%d\n", snapshot.Summary.Total, snapshot.Summary.Ready, snapshot.Summary.Stale, snapshot.Summary.Degraded)
	return tw.Flush()
}

func emptyDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}
