// File: internal/stack/status.go
// Brief: Status/tail helpers for stack run artifacts.

package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/example/ktl/internal/ui"
)

type StatusOptions struct {
	RootDir string
	RunID   string
	Follow  bool
	Limit   int
	Format  string // raw|table|json|tty

	HelmLogs bool
}

func RunStatus(ctx context.Context, opts StatusOptions, out io.Writer) error {
	root := opts.RootDir
	if root == "" {
		root = "."
	}
	runID := opts.RunID
	if runID == "" {
		var err error
		runID, err = LoadMostRecentRun(root)
		if err != nil {
			return err
		}
	}

	statePath := filepath.Join(root, stackStateSQLiteRelPath)
	if _, err := os.Stat(statePath); err != nil {
		return fmt.Errorf("missing stack state (expected %s)", statePath)
	}

	format := opts.Format
	if format == "" {
		format = "raw"
	}

	if format != "raw" && format != "tty" && opts.Follow {
		return fmt.Errorf("--follow is only supported with --format raw|tty")
	}

	s, err := openStackStateStore(root, true)
	if err != nil {
		return err
	}
	defer s.Close()
	summary, err := s.GetRunSummary(ctx, runID)
	if err != nil {
		return err
	}

	switch format {
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	case "table":
		return PrintRunStatusTable(out, runID, summary)
	case "tty":
		if !ui.IsTerminalWriter(out) {
			return fmt.Errorf("--format tty requires a TTY output")
		}
		plan, err := s.GetRunPlan(ctx, runID)
		if err != nil {
			return err
		}
		width, _ := ui.TerminalWidth(out)
		console := NewRunConsole(out, plan, "", RunConsoleOptions{
			Enabled:      true,
			Verbose:      false,
			Width:        width,
			Color:        true,
			ShowHelmLogs: opts.HelmLogs,
		})
		defer console.Done()

		limit := opts.Limit
		if limit <= 0 {
			limit = 50
		}
		events, lastID, err := s.TailEvents(ctx, runID, limit)
		if err != nil {
			return err
		}
		for _, ev := range events {
			console.ObserveRunEvent(ev)
		}
		if !opts.Follow {
			return nil
		}
		after := lastID
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}

			newEvents, newLast, err := s.EventsAfter(ctx, runID, after, 200)
			if err != nil {
				return err
			}
			if len(newEvents) == 0 {
				continue
			}
			for _, ev := range newEvents {
				console.ObserveRunEvent(ev)
			}
			after = newLast
		}
	case "raw":
	default:
		return fmt.Errorf("unknown --format %q (expected raw|table|json|tty)", format)
	}

	fmt.Fprintf(out, "RUN\t%s\n", runID)
	fmt.Fprintf(out, "STATE\t%s\n", filepath.Join(root, stackStateSQLiteRelPath))
	fmt.Fprintln(out)

	fmt.Fprintf(out, "STATUS\t%s\n", summary.Status)
	fmt.Fprintf(out, "STARTED\t%s\n", summary.StartedAt)
	fmt.Fprintf(out, "UPDATED\t%s\n", summary.UpdatedAt)
	fmt.Fprintf(out, "TOTALS\tplanned=%d succeeded=%d failed=%d blocked=%d running=%d\n", summary.Totals.Planned, summary.Totals.Succeeded, summary.Totals.Failed, summary.Totals.Blocked, summary.Totals.Running)
	fmt.Fprintln(out)

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	fmt.Fprintf(out, "EVENTS\t(last %d)\n", limit)

	enc := json.NewEncoder(out)
	events, lastID, err := s.TailEvents(ctx, runID, limit)
	if err != nil {
		return err
	}
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			return err
		}
	}
	if len(events) > 0 {
		fmt.Fprintln(out)
	}
	if !opts.Follow {
		return nil
	}

	after := lastID
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}

		newEvents, newLast, err := s.EventsAfter(ctx, runID, after, 200)
		if err != nil {
			return err
		}
		if len(newEvents) == 0 {
			continue
		}
		for _, ev := range newEvents {
			if err := enc.Encode(ev); err != nil {
				return err
			}
		}
		after = newLast
	}
}
