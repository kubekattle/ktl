// profile.go collects and aggregates syscall samples for 'ktl analyze syscalls'.
package syscalls

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/example/ktl/internal/kube"
)

// Runner executes syscall profiles inside helper containers.
type Runner struct {
	client *kube.Client
}

// NewRunner builds a Runner bound to the provided Kubernetes client.
func NewRunner(client *kube.Client) *Runner {
	return &Runner{client: client}
}

// ProfileRequest configures a syscall capture for a single pod/container.
type ProfileRequest struct {
	Namespace       string
	Pod             string
	Container       string
	TargetContainer string
	TargetPID       int
	Duration        time.Duration
	TraceFilter     string
	Label           string
}

// Row represents a single syscall aggregate from strace -c output.
type Row struct {
	Syscall     string  `json:"syscall"`
	Percent     float64 `json:"percent"`
	Seconds     float64 `json:"seconds"`
	UsecPerCall float64 `json:"usecPerCall"`
	Calls       int     `json:"calls"`
	Errors      int     `json:"errors"`
}

// ProfileResult aggregates the parsed syscall information for a target.
type ProfileResult struct {
	Label           string
	Namespace       string
	Pod             string
	TargetContainer string
	Duration        time.Duration
	TargetPID       int
	TraceFilter     string
	Rows            []Row
	TotalCalls      int
	TotalErrors     int
	TotalSeconds    float64
	Notes           []string
}

// Profile executes strace -c for the requested target and parses the output.
func (r *Runner) Profile(ctx context.Context, req ProfileRequest) (ProfileResult, error) {
	if req.Duration <= 0 {
		return ProfileResult{}, fmt.Errorf("profile duration must be > 0")
	}
	if req.TargetPID <= 0 {
		req.TargetPID = 1
	}
	if req.Namespace == "" {
		req.Namespace = r.client.Namespace
	}
	script := buildProfileScript(req.Duration, req.TargetPID, req.TraceFilter)

	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	cmd := []string{"/bin/bash", "-c", script}
	if err := r.client.Exec(ctx, req.Namespace, req.Pod, req.Container, cmd, nil, stdoutBuf, stderrBuf); err != nil {
		errMsg := strings.TrimSpace(stderrBuf.String())
		if errMsg != "" {
			return ProfileResult{}, fmt.Errorf("syscall profile %s: %s", req.Label, errMsg)
		}
		return ProfileResult{}, fmt.Errorf("syscall profile %s: %w", req.Label, err)
	}

	rows, totalCalls, totalErrors, totalSeconds := parseStraceSummary(stdoutBuf.String())
	notes := collectNotes(rows, stderrBuf.String())

	return ProfileResult{
		Label:           req.Label,
		Namespace:       req.Namespace,
		Pod:             req.Pod,
		TargetContainer: req.TargetContainer,
		Duration:        req.Duration,
		TargetPID:       req.TargetPID,
		TraceFilter:     req.TraceFilter,
		Rows:            rows,
		TotalCalls:      totalCalls,
		TotalErrors:     totalErrors,
		TotalSeconds:    totalSeconds,
		Notes:           notes,
	}, nil
}

func buildProfileScript(duration time.Duration, pid int, filter string) string {
	seconds := duration.Seconds()
	if seconds <= 0 {
		seconds = 1
	}
	return fmt.Sprintf(`set -euo pipefail
TRACE_PID=%d
PROFILE_DURATION="%.3f"
TRACE_FILTER="%s"
TMP=$(mktemp /tmp/ktl-syscalls.XXXXXX)
cleanup() { rm -f "$TMP" 2>/dev/null || true; }
trap cleanup EXIT
STRACE_CMD="strace -qq -f -tt -T -p ${TRACE_PID} -c"
if [ -n "$TRACE_FILTER" ]; then
  STRACE_CMD="$STRACE_CMD -e trace=$TRACE_FILTER"
fi
(
  eval "$STRACE_CMD"
) 2>"$TMP" >/dev/null &
worker=$!
sleep "$PROFILE_DURATION"
kill -INT "$worker" >/dev/null 2>&1 || true
wait "$worker" >/dev/null 2>&1 || true
cat "$TMP"
`, pid, seconds, filter)
}

func parseStraceSummary(raw string) ([]Row, int, int, float64) {
	var (
		rows        []Row
		totalCalls  int
		totalErrors int
		totalSecs   float64
	)

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "% time") || strings.HasPrefix(line, "------") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if strings.EqualFold(fields[len(fields)-1], "total") {
			continue
		}
		row := Row{}
		row.Percent = parseFloat(fields[0])
		row.Seconds = parseFloat(fields[1])
		row.UsecPerCall = parseFloat(fields[2])
		row.Calls = parseInt(fields[3])
		syscallIndex := 4
		if len(fields) >= 6 {
			row.Errors = parseInt(fields[4])
			syscallIndex = 5
		}
		if syscallIndex >= len(fields) {
			continue
		}
		row.Syscall = strings.Join(fields[syscallIndex:], " ")
		rows = append(rows, row)
		totalCalls += row.Calls
		totalErrors += row.Errors
		totalSecs += row.Seconds
	}
	return rows, totalCalls, totalErrors, totalSecs
}

func parseFloat(value string) float64 {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return f
}

func parseInt(value string) int {
	cleaned := strings.ReplaceAll(value, ",", "")
	i, err := strconv.Atoi(cleaned)
	if err != nil {
		return 0
	}
	return i
}

func collectNotes(rows []Row, stderr string) []string {
	var notes []string
	if len(rows) == 0 {
		notes = append(notes, "no syscall activity captured during the window")
	}
	errMsg := strings.TrimSpace(stderr)
	if errMsg != "" {
		notes = append(notes, errMsg)
	}
	return notes
}
