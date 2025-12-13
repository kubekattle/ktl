// snapshot.go provides the 'ktl diag snapshot' family (save/replay/diff) for archiving and comparing namespace state.
package main

import (
	"fmt"

	"github.com/example/ktl/internal/kube"
	snapshotsvc "github.com/example/ktl/internal/snapshot"
	"github.com/spf13/cobra"
)

func newDiagSnapshotCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Capture, replay, and diff namespace snapshots",
	}
	cmd.AddCommand(newDiagSnapshotSaveCommand(kubeconfig, kubeContext), newDiagSnapshotReplayCommand(kubeconfig, kubeContext), newDiagSnapshotDiffCommand())
	return cmd
}

func newDiagSnapshotSaveCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespace string
	var output string
	var logLines int64 = 200

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Capture manifests, pods, logs, and metrics into a tar.gz archive",
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				return fmt.Errorf("--output is required")
			}
			ctx := cmd.Context()
			client, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			targetNamespace := namespace
			if targetNamespace == "" {
				targetNamespace = client.Namespace
			}
			saver := snapshotsvc.Saver{Client: client, LogLines: logLines}
			if err := saver.Save(ctx, targetNamespace, output); err != nil {
				return err
			}
			cmd.Printf("Snapshot saved to %s\n", output)
			return nil
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace to capture (defaults to kube context namespace)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output tar.gz path")
	cmd.Flags().Int64Var(&logLines, "log-lines", logLines, "Number of log lines per container to include")

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd
}

func newDiagSnapshotReplayCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespace string
	var createNamespace bool

	cmd := &cobra.Command{
		Use:   "replay <snapshot.tgz>",
		Short: "Apply captured manifests into a namespace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			archive := args[0]
			ctx := cmd.Context()
			client, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			targetNamespace := namespace
			if targetNamespace == "" {
				targetNamespace = client.Namespace
			}
			if targetNamespace == "" {
				return fmt.Errorf("a target namespace is required")
			}
			replayer := snapshotsvc.Replayer{Client: client}
			if err := replayer.Replay(ctx, archive, targetNamespace, createNamespace); err != nil {
				return err
			}
			cmd.Printf("Replayed snapshot %s into namespace %s\n", archive, targetNamespace)
			return nil
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace to apply resources into")
	cmd.Flags().BoolVar(&createNamespace, "create-namespace", false, "Create the namespace if it does not exist")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd
}

func newDiagSnapshotDiffCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <snapshot-a.tgz> <snapshot-b.tgz>",
		Short: "Show textual differences between two snapshots",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			diff, err := snapshotsvc.DiffArchives(args[0], args[1])
			if err != nil {
				return err
			}
			cmd.Println(diff)
			return nil
		},
	}
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd
}
