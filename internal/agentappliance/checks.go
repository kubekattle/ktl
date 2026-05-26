package agentappliance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runChecks(ctx context.Context, repoDir, outDir string, opts Options) ChecksReport {
	report := ChecksReport{Total: len(opts.Checks)}
	if len(opts.Checks) == 0 {
		return report
	}
	checkDir := filepath.Join(outDir, "checks")
	_ = os.MkdirAll(checkDir, 0o755)
	for i, command := range opts.Checks {
		result := runOneCheck(ctx, repoDir, checkDir, i+1, command, opts)
		report.Results = append(report.Results, result)
		if result.TimedOut {
			report.TimedOut++
		}
		if result.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}
	return report
}

func runOneCheck(ctx context.Context, repoDir, checkDir string, index int, command string, opts Options) CheckResult {
	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	result := CheckResult{
		Command:  command,
		ExitCode: -1,
	}
	cmd := exec.CommandContext(runCtx, shellAvailable(), "-lc", command)
	cmd.Dir = repoDir
	stdout := &cappedBuffer{max: opts.MaxOutputBytes}
	stderr := &cappedBuffer{max: opts.MaxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	result.DurationMillis = time.Since(start).Milliseconds()
	result.TimedOut = errors.Is(runCtx.Err(), context.DeadlineExceeded)
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	} else if err == nil {
		result.ExitCode = 0
	}
	if err != nil && !result.TimedOut {
		result.Error = redactMultiline(err.Error())
	}
	if result.TimedOut {
		result.Error = "command timed out"
	}

	stdoutText := redactMultiline(stdout.String())
	stderrText := redactMultiline(stderr.String())
	result.StdoutPreview, _ = preview(stdoutText, 2048)
	result.StderrPreview, _ = preview(stderrText, 2048)
	result.StdoutTruncated = stdout.Truncated()
	result.StderrTruncated = stderr.Truncated()

	stdoutPath := filepath.Join(checkDir, fmt.Sprintf("check-%02d.stdout.txt", index))
	stderrPath := filepath.Join(checkDir, fmt.Sprintf("check-%02d.stderr.txt", index))
	if err := os.WriteFile(stdoutPath, []byte(stdoutText), 0o644); err == nil {
		result.StdoutPath = filepath.ToSlash(filepath.Join("checks", filepath.Base(stdoutPath)))
		if sum, _, hashErr := hashFile(stdoutPath); hashErr == nil {
			result.StdoutSHA256 = sum
		}
	}
	if err := os.WriteFile(stderrPath, []byte(stderrText), 0o644); err == nil {
		result.StderrPath = filepath.ToSlash(filepath.Join("checks", filepath.Base(stderrPath)))
		if sum, _, hashErr := hashFile(stderrPath); hashErr == nil {
			result.StderrSHA256 = sum
		}
	}
	result.Passed = err == nil && !result.TimedOut
	return result
}
