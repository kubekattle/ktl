package sshtransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewRejectsEmptyTarget(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("New() error = nil, want missing target")
	}
}

func TestRunBuildsOpenSSHCommandAndRedactsEvidence(t *testing.T) {
	runner := &recordingRunner{
		outputs: []fakeOutput{
			{
				output: RunOutput{
					Stdout:   []byte("ok token=top-secret secret://ops/lab#value\n"),
					Stderr:   []byte("authorization: bearer top-secret\n"),
					ExitCode: 0,
				},
			},
		},
	}
	client, err := New(Config{
		Target:       "ssh://root@example.test",
		IdentityFile: "/tmp/lab-key",
		ExtraArgs:    []string{"-p", "2222"},
		RedactValues: []string{
			"top-secret",
		},
		Runner: runner,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result := client.Run(context.Background(), "printf token=top-secret")
	if result.Status != "succeeded" {
		t.Fatalf("Status = %q, want succeeded", result.Status)
	}
	if strings.Contains(result.Stdout, "top-secret") || strings.Contains(result.Stdout, "secret://") {
		t.Fatalf("Stdout was not redacted: %q", result.Stdout)
	}
	if strings.Contains(strings.Join(result.Command, " "), "top-secret") {
		t.Fatalf("Command was not redacted: %#v", result.Command)
	}
	if result.Command[len(result.Command)-1] != "printf token=[REDACTED]" {
		t.Fatalf("redacted remote command = %q", result.Command[len(result.Command)-1])
	}

	call := runner.calls[0]
	if call.name != "ssh" {
		t.Fatalf("runner name = %q, want ssh", call.name)
	}
	wantArgs := []string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new", "-i", "/tmp/lab-key", "-p", "2222", "root@example.test", "printf token=top-secret"}
	if strings.Join(call.args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("runner args = %#v, want %#v", call.args, wantArgs)
	}
}

func TestUploadAndDownloadUseRemoteSpecs(t *testing.T) {
	runner := &recordingRunner{
		outputs: []fakeOutput{
			{output: RunOutput{ExitCode: 0}},
			{output: RunOutput{ExitCode: 0}},
		},
	}
	client, err := New(Config{Target: "ssh://root@example.test", Runner: runner})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	upload := client.Upload(context.Background(), "/tmp/local.txt", "/tmp/remote.txt")
	download := client.Download(context.Background(), "/tmp/remote.txt", "/tmp/local.out")
	if upload.Status != "succeeded" || download.Status != "succeeded" {
		t.Fatalf("upload/download statuses = %q/%q, want succeeded", upload.Status, download.Status)
	}
	if got, want := runner.calls[0].name, "scp"; got != want {
		t.Fatalf("upload command = %q, want %q", got, want)
	}
	if got, want := runner.calls[0].args[len(runner.calls[0].args)-1], "root@example.test:/tmp/remote.txt"; got != want {
		t.Fatalf("upload remote spec = %q, want %q", got, want)
	}
	if got, want := runner.calls[1].args[len(runner.calls[1].args)-2], "root@example.test:/tmp/remote.txt"; got != want {
		t.Fatalf("download remote spec = %q, want %q", got, want)
	}
}

func TestTimeoutIsRecordedAndRedacted(t *testing.T) {
	client, err := New(Config{
		Target:       "ssh://root@example.test",
		Timeout:      10 * time.Millisecond,
		RedactValues: []string{"top-secret"},
		Runner: blockingRunner{
			stderr: []byte("token=top-secret\n"),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result := client.Run(context.Background(), "sleep 60")
	if result.Status != "timeout" {
		t.Fatalf("Status = %q, want timeout", result.Status)
	}
	if !result.TimedOut {
		t.Fatal("TimedOut = false, want true")
	}
	if strings.Contains(result.Stderr, "top-secret") {
		t.Fatalf("Stderr was not redacted: %q", result.Stderr)
	}
}

func TestNormalizeTargetAndDigest(t *testing.T) {
	if got, want := NormalizeTarget(" ssh://root@example.test "), "root@example.test"; got != want {
		t.Fatalf("NormalizeTarget() = %q, want %q", got, want)
	}
	sum := sha256.Sum256([]byte("root@example.test"))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got := TargetDigest("ssh://root@example.test"); got != want {
		t.Fatalf("TargetDigest() = %q, want %q", got, want)
	}
}

func TestE2EEnvOpenSSHTransport(t *testing.T) {
	target := os.Getenv("TORQUE_OPS_TR_E2E_TARGET")
	output := os.Getenv("TORQUE_OPS_TR_E2E_OUTPUT")
	if target == "" && output == "" {
		t.Skip("set TORQUE_OPS_TR_E2E_TARGET and TORQUE_OPS_TR_E2E_OUTPUT to run the SSH transport E2E proof")
	}
	if target == "" || output == "" {
		t.Fatal("TORQUE_OPS_TR_E2E_TARGET and TORQUE_OPS_TR_E2E_OUTPUT must be set together")
	}

	remoteRoot := os.Getenv("TORQUE_OPS_TR_E2E_REMOTE_ROOT")
	if remoteRoot == "" {
		remoteRoot = "/tmp/torque-ops-tr-001-e2e"
	}
	canary := os.Getenv("TORQUE_OPS_TR_E2E_CANARY")
	if canary == "" {
		canary = "torque-redaction-canary-e2e"
	}
	identity := firstNonEmpty(os.Getenv("TORQUE_OPS_TR_E2E_IDENTITY"), os.Getenv("TORQUE_LAB_SSH_IDENTITY"))
	extraArgs := strings.Fields(firstNonEmpty(os.Getenv("TORQUE_OPS_TR_E2E_SSH_OPTS"), os.Getenv("TORQUE_LAB_SSH_OPTS")))

	client, err := New(Config{
		Target:       target,
		IdentityFile: identity,
		ExtraArgs:    extraArgs,
		Timeout:      20 * time.Second,
		RedactValues: []string{
			canary,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	localDir := t.TempDir()
	uploadPath := filepath.Join(localDir, "upload.txt")
	downloadPath := filepath.Join(localDir, "download.txt")
	uploadBody := "upload-proof:" + time.Now().UTC().Format(time.RFC3339Nano) + "\n"
	if err := os.WriteFile(uploadPath, []byte(uploadBody), 0o600); err != nil {
		t.Fatalf("write upload fixture: %v", err)
	}

	remoteUpload := remoteRoot + "/upload.txt"
	remoteDownload := remoteRoot + "/download.txt"
	connect := client.Connect(context.Background())
	prepare := client.Run(
		context.Background(),
		"rm -rf "+ShellQuote(remoteRoot)+" && mkdir -p "+ShellQuote(remoteRoot)+" && printf 'prepared token=%s\n' "+ShellQuote(canary),
	)
	upload := client.Upload(context.Background(), uploadPath, remoteUpload)
	copyRemote := client.Run(
		context.Background(),
		"set -eu; test -s "+ShellQuote(remoteUpload)+"; cp "+ShellQuote(remoteUpload)+" "+ShellQuote(remoteDownload)+"; printf 'copied token=%s\n' "+ShellQuote(canary),
	)
	download := client.Download(context.Background(), remoteDownload, downloadPath)

	timeoutClient, err := New(Config{
		Target:       target,
		IdentityFile: identity,
		ExtraArgs:    extraArgs,
		Timeout:      200 * time.Millisecond,
		RedactValues: []string{
			canary,
		},
	})
	if err != nil {
		t.Fatalf("New(timeout) error = %v", err)
	}
	timeout := timeoutClient.Run(context.Background(), "sleep 2")

	downloadBody, readErr := os.ReadFile(downloadPath)
	contentMatch := readErr == nil && string(downloadBody) == uploadBody
	operations := map[string]OperationResult{
		"connect":  connect,
		"prepare":  prepare,
		"upload":   upload,
		"copy":     copyRemote,
		"download": download,
		"timeout":  timeout,
	}
	errors := validateE2EOperations(operations, contentMatch)
	status := "succeeded"
	if len(errors) > 0 {
		status = "failed"
	}
	doc := map[string]any{
		"apiVersion":           "torque.dev/e2e/v1",
		"kind":                 "OpsSSHTransportProof",
		"status":               status,
		"targetDigest":         client.TargetDigest(),
		"remoteRoot":           remoteRoot,
		"operations":           operations,
		"downloadContentMatch": contentMatch,
		"errors":               errors,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal proof: %v", err)
	}
	if strings.Contains(string(raw), canary) || strings.Contains(string(raw), "secret://") {
		t.Fatal("SSH transport proof leaked secret material")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(output, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	if len(errors) > 0 {
		t.Fatalf("SSH transport E2E failed: %s", strings.Join(errors, "; "))
	}
}

func validateE2EOperations(operations map[string]OperationResult, contentMatch bool) []string {
	var errs []string
	for _, name := range []string{"connect", "prepare", "upload", "copy", "download"} {
		if operations[name].Status != "succeeded" {
			errs = append(errs, fmt.Sprintf("%s status %q", name, operations[name].Status))
		}
	}
	if !operations["timeout"].TimedOut || operations["timeout"].Status != "timeout" {
		errs = append(errs, "timeout operation did not record timeout")
	}
	if !contentMatch {
		errs = append(errs, "downloaded content did not match uploaded content")
	}
	return errs
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type fakeCall struct {
	name string
	args []string
}

type fakeOutput struct {
	output RunOutput
	err    error
}

type recordingRunner struct {
	calls   []fakeCall
	outputs []fakeOutput
}

func (r *recordingRunner) Run(_ context.Context, name string, args []string) (RunOutput, error) {
	r.calls = append(r.calls, fakeCall{name: name, args: append([]string(nil), args...)})
	if len(r.outputs) == 0 {
		return RunOutput{ExitCode: 0}, nil
	}
	next := r.outputs[0]
	r.outputs = r.outputs[1:]
	return next.output, next.err
}

type blockingRunner struct {
	stderr []byte
}

func (r blockingRunner) Run(ctx context.Context, _ string, _ []string) (RunOutput, error) {
	<-ctx.Done()
	return RunOutput{Stderr: r.stderr, ExitCode: -1}, errors.New("token=top-secret")
}
