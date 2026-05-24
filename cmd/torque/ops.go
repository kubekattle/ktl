package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/ingresslabs/torque/internal/ops/inventory"
	"github.com/ingresslabs/torque/internal/ops/targetgraph"
	"github.com/spf13/cobra"
)

func newOpsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ops",
		Short: "Operate proof-backed target graphs and adapters",
		Long:  "Operate proof-backed target graphs, inventories, and adapter-driven change workflows.",
	}
	cmd.AddCommand(newOpsInventoryCommand())
	decorateCommandHelp(cmd, "Ops Flags")
	return cmd
}

func newOpsInventoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inventory",
		Short: "Inspect ops target inventory",
	}
	cmd.AddCommand(newOpsInventoryShowCommand(), newOpsInventoryGraphCommand())
	decorateCommandHelp(cmd, "Inventory Flags")
	return cmd
}

func newOpsInventoryShowCommand() *cobra.Command {
	var targetGraphPath string
	var selectorValues []string
	var groups []string
	var limit int
	format := "table"

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show resolved ops targets",
		Example: `  torque ops inventory show --targets ./targetgraph.yaml
  torque ops inventory show --targets ./targetgraph.yaml --selector role=db --format json
  torque ops inventory show --targets ./targetgraph.yaml --group web --limit 10`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops inventory show does not accept positional arguments")
			}
			if strings.TrimSpace(targetGraphPath) == "" {
				return fmt.Errorf("--targets is required")
			}
			selector, err := inventory.ParseSelector(selectorValues)
			if err != nil {
				return err
			}
			graph, err := targetgraph.LoadFile(targetGraphPath)
			if err != nil {
				return err
			}
			result, err := inventory.Show(graph, inventory.ShowRequest{
				Selector: selector,
				Groups:   groups,
				Limit:    limit,
			})
			if err != nil {
				return err
			}
			switch format {
			case "json":
				raw, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return err
			case "table", "":
				return renderOpsInventoryTable(cmd.OutOrStdout(), result)
			default:
				return fmt.Errorf("--format must be table or json")
			}
		},
	}
	cmd.Flags().StringVar(&targetGraphPath, "targets", "", "TargetGraph YAML file")
	cmd.Flags().StringArrayVar(&selectorValues, "selector", nil, "Target label selector as key=value (repeatable)")
	cmd.Flags().StringArrayVar(&groups, "group", nil, "Target group to include (repeatable)")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum selected targets to show")
	cmd.Flags().StringVar(&format, "format", format, "Output format: table or json")
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Inventory Show Flags")
	return cmd
}

func newOpsInventoryGraphCommand() *cobra.Command {
	var targetGraphPath string
	var selectorValues []string
	var groups []string
	var limit int
	var outputPath string
	format := "html"

	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Export an ops target inventory graph",
		Example: `  torque ops inventory graph --targets ./targetgraph.yaml --output inventory.html
  torque ops inventory graph --targets ./targetgraph.yaml --selector role=db --format json
  torque ops inventory graph --targets ./targetgraph.yaml --group web --limit 10 --output web.html`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops inventory graph does not accept positional arguments")
			}
			if strings.TrimSpace(targetGraphPath) == "" {
				return fmt.Errorf("--targets is required")
			}
			selector, err := inventory.ParseSelector(selectorValues)
			if err != nil {
				return err
			}
			graph, err := targetgraph.LoadFile(targetGraphPath)
			if err != nil {
				return err
			}
			result, err := inventory.Graph(graph, inventory.ShowRequest{
				Selector: selector,
				Groups:   groups,
				Limit:    limit,
			})
			if err != nil {
				return err
			}
			var raw []byte
			switch format {
			case "json":
				raw, err = result.JSON()
			case "html", "":
				raw, err = inventory.RenderGraphHTML(result)
			default:
				return fmt.Errorf("--format must be html or json")
			}
			if err != nil {
				return err
			}
			raw = append(raw, '\n')
			if strings.TrimSpace(outputPath) != "" {
				return os.WriteFile(outputPath, raw, 0o644)
			}
			_, err = cmd.OutOrStdout().Write(raw)
			return err
		},
	}
	cmd.Flags().StringVar(&targetGraphPath, "targets", "", "TargetGraph YAML file")
	cmd.Flags().StringArrayVar(&selectorValues, "selector", nil, "Target label selector as key=value (repeatable)")
	cmd.Flags().StringArrayVar(&groups, "group", nil, "Target group to include (repeatable)")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum selected targets to mark")
	cmd.Flags().StringVar(&format, "format", format, "Output format: html or json")
	cmd.Flags().StringVar(&outputPath, "output", "", "Write graph output to a file instead of stdout")
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"html", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Inventory Graph Flags")
	return cmd
}

func renderOpsInventoryTable(w io.Writer, result inventory.ShowResult) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintf(tw, "TARGET\tTYPE\tTRANSPORT\tGROUPS\tLABELS\tFACT_TTL\n"); err != nil {
		return err
	}
	for _, target := range result.Targets {
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\n",
			target.ID,
			target.Type,
			firstNonEmpty(target.TransportRef, "-"),
			inventory.FormatList(target.Groups),
			inventory.FormatLabels(target.Labels),
			firstNonEmpty(target.FactsTTL, "-"),
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}
