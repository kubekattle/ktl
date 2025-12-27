// File: internal/stack/status.go
// Brief: Status/tail helpers for stack run artifacts.

package stack

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type StatusOptions struct {
	RootDir string
	RunID   string
	Follow  bool
	Limit   int
	Format  string // raw|table|json
}

func RunStatus(ctx context.Context, opts StatusOptions, out io.Writer) error {
	root := opts.RootDir
	if root == "" {
		root = "."
	}
	runRoot := ""
	if opts.RunID != "" {
		runRoot = filepath.Join(root, ".ktl", "stack", "runs", opts.RunID)
	} else {
		var err error
		runRoot, err = LoadMostRecentRun(root)
		if err != nil {
			return err
		}
	}

	runID := filepath.Base(runRoot)

	statePath := filepath.Join(root, stackStateSQLiteRelPath)
	useSQLite := false
	if _, err := os.Stat(statePath); err == nil {
		useSQLite = true
	}

	format := opts.Format
	if format == "" {
		format = "raw"
	}

	if format != "raw" && opts.Follow {
		return fmt.Errorf("--follow is only supported with --format raw")
	}

	var summary *RunSummary
	var sqliteStore *stackStateStore
	if useSQLite {
		s, err := openStackStateStore(root, true)
		if err != nil {
			return err
		}
		defer s.Close()
		ss, err := s.GetRunSummary(ctx, runID)
		if err != nil {
			return err
		}
		sqliteStore = s
		summary = ss
	} else {
		summaryPath := filepath.Join(runRoot, "summary.json")
		if raw, err := os.ReadFile(summaryPath); err == nil {
			var s RunSummary
			if jsonErr := json.Unmarshal(raw, &s); jsonErr == nil {
				summary = &s
			}
		}
	}
	if summary == nil {
		return fmt.Errorf("missing or unreadable summary for run %s", runID)
	}

	switch format {
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	case "table":
		return PrintRunStatusTable(out, runID, summary)
	case "raw":
	default:
		return fmt.Errorf("unknown --format %q (expected raw|table|json)", format)
	}

	fmt.Fprintf(out, "RUN\t%s\n", runID)
	fmt.Fprintf(out, "STATE\t%s\n", func() string {
		if useSQLite {
			return filepath.Join(root, stackStateSQLiteRelPath)
		}
		return runRoot
	}())
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
	if useSQLite {
		if sqliteStore == nil {
			return fmt.Errorf("internal error: sqlite state store is nil")
		}
		fmt.Fprintf(out, "EVENTS\t(last %d)\n", limit)

		enc := json.NewEncoder(out)
		events, lastID, err := sqliteStore.TailEvents(ctx, runID, limit)
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

		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		after := lastID
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}

			newEvents, newLast, err := sqliteStore.EventsAfter(ctx, runID, after, 200)
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

	eventsPath := filepath.Join(runRoot, "events.jsonl")
	last, err := readLastJSONLines(eventsPath, limit)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range last {
		fmt.Fprintln(out, line)
	}
	if len(last) > 0 {
		fmt.Fprintln(out)
	}

	if !opts.Follow {
		return nil
	}
	return followJSONLines(ctx, eventsPath, out)
}

func readLastJSONLines(path string, limit int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > limit {
			lines = lines[len(lines)-limit:]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func followJSONLines(ctx context.Context, path string, out io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Start following from the end so we don't re-print the tail.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		_, _ = f.Seek(0, io.SeekStart)
	}

	r := bufio.NewReader(f)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := r.ReadString('\n')
		if err == nil {
			fmt.Fprint(out, line)
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
