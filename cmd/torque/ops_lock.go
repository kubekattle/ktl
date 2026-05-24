package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ingresslabs/torque/internal/ops/locks"
	"github.com/spf13/cobra"
)

func newOpsLockCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Inspect and manage ops target locks",
	}
	cmd.AddCommand(newOpsLockAcquireCommand(), newOpsLockReleaseCommand(), newOpsLockStatusCommand())
	decorateCommandHelp(cmd, "Lock Flags")
	return cmd
}

func newOpsLockAcquireCommand() *cobra.Command {
	var lockDir string
	var scope string
	var targetID string
	var holder string
	var operation string
	var ttl time.Duration
	var wait time.Duration
	format := "table"
	cmd := &cobra.Command{
		Use:   "acquire",
		Short: "Acquire a target lock",
		Example: `  torque ops lock acquire --scope target/host-01 --holder operator --operation host.command.run
  torque ops lock acquire --scope target/host-01 --lock-dir ./.torque/ops/locks --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops lock acquire does not accept positional arguments")
			}
			result, err := locks.FileStore{Dir: lockDir}.Acquire(cmd.Context(), locks.AcquireRequest{
				Scope:     scope,
				TargetID:  targetID,
				Holder:    holder,
				Operation: operation,
				TTL:       ttl,
				Wait:      wait,
			})
			if err != nil {
				return err
			}
			if err := renderOpsLockAcquire(cmd.OutOrStdout(), result, format); err != nil {
				return err
			}
			if result.Decision != "acquired" {
				return fmt.Errorf("lock acquire %s: %s", result.Decision, result.Reason)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&lockDir, "lock-dir", ".torque/ops/locks", "Directory for ops target locks")
	cmd.Flags().StringVar(&scope, "scope", "", "Lock scope, usually target/<id> or graph/<id>")
	cmd.Flags().StringVar(&targetID, "target", "", "Target ID associated with the lock")
	cmd.Flags().StringVar(&holder, "holder", defaultOpsLockHolder(), "Lock holder identity")
	cmd.Flags().StringVar(&operation, "operation", "", "Operation guarded by the lock")
	cmd.Flags().DurationVar(&ttl, "ttl", 15*time.Minute, "Lock TTL")
	cmd.Flags().DurationVar(&wait, "wait", 0, "How long to wait for an existing lock")
	cmd.Flags().StringVar(&format, "format", format, "Output format: table or json")
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Lock Acquire Flags")
	return cmd
}

func newOpsLockReleaseCommand() *cobra.Command {
	var lockDir string
	var scope string
	var token string
	format := "table"
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Release a target lock by token",
		Example: `  torque ops lock release --scope target/host-01 --token <token>
  torque ops lock release --scope target/host-01 --token <token> --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops lock release does not accept positional arguments")
			}
			record, err := locks.FileStore{Dir: lockDir}.Release(scope, token)
			if err != nil {
				return err
			}
			return renderOpsLockRecord(cmd.OutOrStdout(), record, format)
		},
	}
	cmd.Flags().StringVar(&lockDir, "lock-dir", ".torque/ops/locks", "Directory for ops target locks")
	cmd.Flags().StringVar(&scope, "scope", "", "Lock scope")
	cmd.Flags().StringVar(&token, "token", "", "Lock token returned by acquire")
	cmd.Flags().StringVar(&format, "format", format, "Output format: table or json")
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Lock Release Flags")
	return cmd
}

func newOpsLockStatusCommand() *cobra.Command {
	var lockDir string
	var scope string
	format := "table"
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show a target lock",
		Example: `  torque ops lock status --scope target/host-01
  torque ops lock status --scope target/host-01 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops lock status does not accept positional arguments")
			}
			record, found, err := locks.FileStore{Dir: lockDir}.Inspect(scope)
			if err != nil {
				return err
			}
			result := opsLockStatusResult{
				APIVersion: locks.APIVersion,
				Kind:       "TargetLockStatus",
				Scope:      strings.TrimSpace(scope),
				Found:      found,
				Record:     record,
			}
			return renderOpsLockStatus(cmd.OutOrStdout(), result, format)
		},
	}
	cmd.Flags().StringVar(&lockDir, "lock-dir", ".torque/ops/locks", "Directory for ops target locks")
	cmd.Flags().StringVar(&scope, "scope", "", "Lock scope")
	cmd.Flags().StringVar(&format, "format", format, "Output format: table or json")
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Lock Status Flags")
	return cmd
}

type opsLockStatusResult struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	Scope      string        `json:"scope"`
	Found      bool          `json:"found"`
	Record     *locks.Record `json:"record,omitempty"`
}

func renderOpsLockAcquire(w io.Writer, result locks.AcquireResult, format string) error {
	switch format {
	case "json":
		raw, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(raw))
		return err
	case "table", "":
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, err := fmt.Fprintf(tw, "SCOPE\tDECISION\tREASON\tTOKEN\tHOLDER\tEXPIRES\n")
		if err == nil {
			token, holder, expires := "-", "-", "-"
			if result.Record != nil {
				token, holder, expires = result.Record.Token, result.Record.Holder, result.Record.ExpiresAt
			} else if result.Existing != nil {
				token, holder, expires = result.Existing.Token, result.Existing.Holder, result.Existing.ExpiresAt
			}
			_, err = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", result.Scope, result.Decision, result.Reason, token, holder, expires)
		}
		if err != nil {
			return err
		}
		return tw.Flush()
	default:
		return fmt.Errorf("--format must be table or json")
	}
}

func renderOpsLockRecord(w io.Writer, record *locks.Record, format string) error {
	switch format {
	case "json":
		raw, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(raw))
		return err
	case "table", "":
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, err := fmt.Fprintf(tw, "SCOPE\tSTATUS\tTOKEN\tHOLDER\tEXPIRES\tRELEASED\n")
		if err == nil && record != nil {
			_, err = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", record.Scope, record.Status, record.Token, record.Holder, record.ExpiresAt, firstNonEmpty(record.ReleasedAt, "-"))
		}
		if err != nil {
			return err
		}
		return tw.Flush()
	default:
		return fmt.Errorf("--format must be table or json")
	}
}

func renderOpsLockStatus(w io.Writer, result opsLockStatusResult, format string) error {
	switch format {
	case "json":
		raw, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(raw))
		return err
	case "table", "":
		if !result.Found {
			_, err := fmt.Fprintf(w, "No lock for %s\n", result.Scope)
			return err
		}
		return renderOpsLockRecord(w, result.Record, "table")
	default:
		return fmt.Errorf("--format must be table or json")
	}
}

func defaultOpsLockHolder() string {
	return firstNonEmpty(os.Getenv("USER"), os.Getenv("USERNAME"), "torque-cli")
}
