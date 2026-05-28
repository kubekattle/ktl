package transport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
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

type LineRecord struct {
	Stream string
	Line   string
}

type LineObserver interface {
	ObserveLine(LineRecord)
}

type LineObserverFunc func(LineRecord)

func (f LineObserverFunc) ObserveLine(rec LineRecord) {
	if f == nil {
		return
	}
	f(rec)
}

type StreamingRunner interface {
	RunStream(ctx context.Context, name string, args []string, observer LineObserver) (RunOutput, error)
}

// OperationResult is the evidence-safe receipt for one transport primitive.
type OperationResult struct {
	Operation      string            `json:"operation"`
	Status         string            `json:"status"`
	TargetDigest   string            `json:"targetDigest"`
	Command        []string          `json:"command"`
	Stdout         string            `json:"stdout,omitempty"`
	Stderr         string            `json:"stderr,omitempty"`
	ExitCode       int               `json:"exitCode"`
	TimedOut       bool              `json:"timedOut"`
	DurationMillis int64             `json:"durationMillis"`
	Error          string            `json:"error,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// ExecRunner runs commands with os/exec and captures stdout, stderr, and exit
// status in the shared transport shape.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string) (RunOutput, error) {
	return ExecRunner{}.RunStream(ctx, name, args, nil)
}

func (ExecRunner) RunStream(ctx context.Context, name string, args []string, observer LineObserver) (RunOutput, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return RunOutput{}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return RunOutput{}, err
	}
	if err := cmd.Start(); err != nil {
		return RunOutput{}, err
	}

	var wg sync.WaitGroup
	var stdoutErr error
	var stderrErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		stdoutErr = captureStream(stdoutPipe, &stdout, "stdout", observer)
	}()
	go func() {
		defer wg.Done()
		stderrErr = captureStream(stderrPipe, &stderr, "stderr", observer)
	}()

	err = cmd.Wait()
	wg.Wait()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	if err == nil {
		if stdoutErr != nil {
			err = stdoutErr
			exitCode = -1
		} else if stderrErr != nil {
			err = stderrErr
			exitCode = -1
		}
	}
	return RunOutput{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode,
	}, err
}

func captureStream(src io.Reader, dst *bytes.Buffer, stream string, observer LineObserver) error {
	reader := bufio.NewReader(src)
	var pending bytes.Buffer
	for {
		chunk, err := reader.ReadBytes('\n')
		if len(chunk) > 0 {
			_, _ = dst.Write(chunk)
			_, _ = pending.Write(chunk)
			for {
				data := pending.Bytes()
				idx := bytes.IndexByte(data, '\n')
				if idx < 0 {
					break
				}
				line := strings.TrimRight(string(data[:idx]), "\r")
				if observer != nil {
					observer.ObserveLine(LineRecord{Stream: stream, Line: line})
				}
				pending.Next(idx + 1)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	if pending.Len() > 0 && observer != nil {
		observer.ObserveLine(LineRecord{Stream: stream, Line: strings.TrimRight(pending.String(), "\r")})
	}
	return nil
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
