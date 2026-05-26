package agentappliance

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/secrets"
)

type cappedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.max <= 0 {
		return len(p), nil
	}
	remaining := b.max - b.buf.Len()
	if remaining > 0 {
		if len(p) <= remaining {
			_, _ = b.buf.Write(p)
		} else {
			_, _ = b.buf.Write(p[:remaining])
			b.truncated = true
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	return b.buf.String()
}

func (b *cappedBuffer) Truncated() bool {
	return b.truncated
}

func commandOutput(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = dir
	var out cappedBuffer
	var stderr cappedBuffer
	out.max = 1024 * 1024
	stderr.max = 256 * 1024
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return strings.TrimSpace(out.String()), fmt.Errorf("%s timed out", name)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return strings.TrimSpace(out.String()), fmt.Errorf("%s failed: %s", name, msg)
		}
		return strings.TrimSpace(out.String()), err
	}
	return strings.TrimSpace(out.String()), nil
}

func shellAvailable() string {
	if path, err := exec.LookPath("sh"); err == nil {
		return path
	}
	return "/bin/sh"
}

func redactMultiline(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	var b strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(value))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		b.WriteString(secrets.RedactText(scanner.Text()))
		b.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		b.WriteString("[redaction scanner error: ")
		b.WriteString(secrets.RedactText(err.Error()))
		b.WriteString("]\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func preview(value string, max int) (string, bool) {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value, false
	}
	return value[:max], true
}
