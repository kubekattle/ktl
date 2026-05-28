package postgres

import (
	"context"
	"fmt"
	"io"
)

type progressReporter struct {
	stdout io.Writer
	stderr io.Writer
}

type progressReporterKey struct{}

func withProgressReporter(ctx context.Context, stdout io.Writer, stderr io.Writer) context.Context {
	if stdout == nil && stderr == nil {
		return ctx
	}
	return context.WithValue(ctx, progressReporterKey{}, progressReporter{stdout: stdout, stderr: stderr})
}

func reportProgress(ctx context.Context, format string, args ...any) {
	if ctx == nil {
		return
	}
	reporter, ok := ctx.Value(progressReporterKey{}).(progressReporter)
	if !ok || reporter.stderr == nil {
		return
	}
	_, _ = fmt.Fprintf(reporter.stderr, format+"\n", args...)
}
