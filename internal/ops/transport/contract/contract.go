package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Runner executes a local process. Transport packages accept this interface so
// unit tests can exercise command construction without touching the host.
type Runner interface {
	Run(ctx context.Context, name string, args []string) (RunOutput, error)
}

// RunOutput captures local command output before evidence redaction.
type RunOutput struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// OperationResult is the evidence-safe receipt for one transport primitive.
type OperationResult struct {
	Operation      string   `json:"operation"`
	Status         string   `json:"status"`
	TargetDigest   string   `json:"targetDigest"`
	Command        []string `json:"command"`
	Stdout         string   `json:"stdout,omitempty"`
	Stderr         string   `json:"stderr,omitempty"`
	ExitCode       int      `json:"exitCode"`
	TimedOut       bool     `json:"timedOut"`
	DurationMillis int64    `json:"durationMillis"`
	Error          string   `json:"error,omitempty"`
}

// ExecRunner runs commands with os/exec and captures stdout, stderr, and exit
// status in the shared transport shape.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string) (RunOutput, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return RunOutput{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode,
	}, err
}

func ValueDigest(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Redactor removes secret material from command and output evidence.
type Redactor struct {
	values []string
}

func NewRedactor(values []string) Redactor {
	unique := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			unique[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(unique))
	for value := range unique {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		return len(out[i]) > len(out[j])
	})
	return Redactor{values: out}
}

var (
	secretRefPattern   = regexp.MustCompile(`secret://[^\s"',;]+`)
	sensitiveKVPattern = regexp.MustCompile(`(?i)\b(password|passwd|token|secret|privatekey)=([^\s"',;]+)`)
	bearerPattern      = regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)([^\s]+)`)
)

func (r Redactor) RedactString(value string) string {
	for _, secret := range r.values {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	value = secretRefPattern.ReplaceAllString(value, "[REDACTED:secret-ref]")
	value = bearerPattern.ReplaceAllString(value, "${1}[REDACTED]")
	value = sensitiveKVPattern.ReplaceAllString(value, "${1}=[REDACTED]")
	return value
}

func (r Redactor) RedactArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, r.RedactString(arg))
	}
	return out
}

// ShellQuote quotes a string for a POSIX shell command argument.
func ShellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
