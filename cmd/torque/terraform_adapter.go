package main

import (
	"github.com/ingresslabs/torque/internal/terraformadapter"
	"github.com/spf13/cobra"
)

func newTerraformAdapterCommand() *cobra.Command {
	var terraformBin string
	var workspaceRoot string
	cmd := &cobra.Command{
		Use:    "terraform-adapter",
		Short:  "Run a Terraform/OpenTofu provider resource as a Torque module",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return terraformadapter.Run(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), terraformadapter.Options{
				TerraformBin:  terraformBin,
				WorkspaceRoot: workspaceRoot,
			})
		},
		Example: `  # Used from a module-backed stack node
  torque terraform-adapter < module-request.json`,
	}
	cmd.Flags().StringVar(&terraformBin, "terraform-bin", "", "Terraform/OpenTofu binary to execute (defaults to TORQUE_TERRAFORM_BIN, tofu, then terraform)")
	cmd.Flags().StringVar(&workspaceRoot, "workspace-root", "", "Root for generated Terraform workspaces (defaults to the stack root)")
	decorateCommandHelp(cmd, "Terraform Adapter Flags")
	return cmd
}
