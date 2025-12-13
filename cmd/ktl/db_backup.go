// db_backup.go backs the 'ktl db' subtree, orchestrating pg_dump/restore invocations inside pods for managed PostgreSQL backups.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/pgdump"
	"github.com/example/ktl/internal/ui"
	"github.com/spf13/cobra"
)

func newDBCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database utilities",
		Long:  "Database-focused helpers including PostgreSQL backups executed inside pods.",
	}

	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Namespace containing the PostgreSQL pod (defaults to the active kube context namespace)")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	cmd.AddCommand(
		newDBBackupCommand(&namespace, kubeconfig, kubeContext),
		newDBRestoreCommand(&namespace, kubeconfig, kubeContext),
	)
	decorateCommandHelp(cmd, "db Flags")
	return cmd
}

func newDBBackupCommand(namespace *string, kubeconfig *string, kubeContext *string) *cobra.Command {
	var container string
	var user = "postgres"
	password := os.Getenv("PGPASSWORD")
	var outputDir string
	compress := true
	var databases []string

	cmd := &cobra.Command{
		Use:   "backup POD",
		Short: "Create per-database PostgreSQL dumps from a pod",
		Long:  "Executes pg_dump inside the selected pod for every user database and downloads each dump to the local filesystem.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			pod := args[0]
			stopSpinner := ui.StartSpinner(cmd.ErrOrStderr(), fmt.Sprintf("Preparing backup for %s", pod))
			defer func() {
				if stopSpinner != nil {
					stopSpinner(false)
				}
			}()
			ns := ""
			if namespace != nil {
				ns = *namespace
			}
			opts := pgdump.Options{
				Namespace: ns,
				Pod:       pod,
				Container: container,
				User:      user,
				Password:  password,
				OutputDir: outputDir,
				Compress:  compress,
				Databases: databases,
				ProgressHook: func(current, total int, database string) {
					if stopSpinner != nil {
						stopSpinner(true)
					}
					stopSpinner = ui.StartSpinner(cmd.ErrOrStderr(), fmt.Sprintf("Backing up %s (%d/%d)", database, current, total))
				},
			}
			result, err := pgdump.DumpAll(ctx, kubeClient, opts)
			if err != nil {
				if stopSpinner != nil {
					stopSpinner(false)
					stopSpinner = nil
				}
				return err
			}
			stopSpinner(true)
			stopSpinner = nil
			fmt.Fprintf(cmd.OutOrStdout(), "Archive\t%s\n", result.ArchivePath)
			return nil
		},
	}

	cmd.Flags().StringVar(&container, "container", "", "Container name when the pod runs multiple containers")
	cmd.Flags().StringVar(&user, "user", user, "PostgreSQL role used for the dump")
	cmd.Flags().StringVar(&password, "password", password, "Password for the PostgreSQL role (defaults to none or $PGPASSWORD if set)")
	cmd.Flags().StringVarP(&outputDir, "output", "o", ".", "Directory to write the dump files")
	cmd.Flags().BoolVar(&compress, "compress", true, "Compress dumps with gzip")
	cmd.Flags().StringSliceVar(&databases, "database", nil, "One or more databases to dump (default: oedk,pko)")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "db backup Flags")

	return cmd
}

func newDBRestoreCommand(namespace *string, kubeconfig *string, kubeContext *string) *cobra.Command {
	var container string
	user := "postgres"
	password := os.Getenv("PGPASSWORD")
	var archivePath string
	dropAll := false
	var assumeYes bool

	cmd := &cobra.Command{
		Use:   "restore POD",
		Short: "Restore PostgreSQL dumps from an archive back into a pod",
		Long:  "Extracts a ktl-generated backup archive and pipes each dump back into psql running inside the target pod.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(archivePath) == "" {
				return fmt.Errorf("--archive is required")
			}
			if _, err := os.Stat(archivePath); err != nil {
				return fmt.Errorf("archive %s: %w", archivePath, err)
			}
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			pod := args[0]
			if dropAll && !assumeYes {
				nsPrompt := ""
				if namespace != nil {
					nsPrompt = *namespace
				}
				if nsPrompt == "" {
					nsPrompt = kubeClient.Namespace
				}
				if nsPrompt == "" {
					nsPrompt = "default"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "WARNING: this will drop every user database in %s/%s before restoring.\nType \"%s\" to continue: ", nsPrompt, pod, pod)
				reader := bufio.NewReader(cmd.InOrStdin())
				resp, _ := reader.ReadString('\n')
				resp = strings.TrimSpace(resp)
				if resp != pod {
					return fmt.Errorf("restore cancelled by user")
				}
			}
			stopSpinner := ui.StartSpinner(cmd.ErrOrStderr(), fmt.Sprintf("Preparing restore for %s", pod))
			defer func() {
				if stopSpinner != nil {
					stopSpinner(false)
				}
			}()

			ns := ""
			if namespace != nil {
				ns = *namespace
			}
			opts := pgdump.RestoreOptions{
				Namespace:            ns,
				Pod:                  pod,
				Container:            container,
				User:                 user,
				Password:             password,
				InputPath:            archivePath,
				DropAllBeforeRestore: dropAll,
				ProgressHook: func(current, total int, database string) {
					if stopSpinner != nil {
						stopSpinner(true)
					}
					stopSpinner = ui.StartSpinner(cmd.ErrOrStderr(), fmt.Sprintf("Restoring %s (%d/%d)", database, current, total))
				},
			}
			result, err := pgdump.Restore(ctx, kubeClient, opts)
			if err != nil {
				if stopSpinner != nil {
					stopSpinner(false)
					stopSpinner = nil
				}
				return err
			}
			stopSpinner(true)
			stopSpinner = nil
			fmt.Fprintf(cmd.OutOrStdout(), "Restored %d databases from %s\n", len(result.Databases), archivePath)
			return nil
		},
	}

	cmd.Flags().StringVar(&container, "container", "", "Container name when the pod runs multiple containers")
	cmd.Flags().StringVar(&user, "user", user, "PostgreSQL role used for restore")
	cmd.Flags().StringVar(&password, "password", password, "Password for the PostgreSQL role (defaults to none or $PGPASSWORD if set)")
	cmd.Flags().StringVarP(&archivePath, "archive", "a", "", "Path to the backup archive (tar or tar.gz)")
	cmd.Flags().BoolVar(&dropAll, "drop-db", false, "Drop every existing user database before restoring")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "Automatically confirm destructive actions (e.g., --drop-db)")
	_ = cmd.MarkFlagRequired("archive")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "db restore Flags")
	return cmd
}
