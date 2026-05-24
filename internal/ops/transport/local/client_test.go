package localtransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	sshtransport "github.com/ingresslabs/torque/internal/ops/transport/ssh"
)

func TestNewDefaultsToLocalhost(t *testing.T) {
	client, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got, want := client.TargetDigest(), TargetDigest("localhost"); got != want {
		t.Fatalf("TargetDigest = %q, want %q", got, want)
	}
}

func TestOperationResultJSONShapeMatchesSSH(t *testing.T) {
	localFields := jsonFields(reflect.TypeOf(OperationResult{}))
	sshFields := jsonFields(reflect.TypeOf(sshtransport.OperationResult{}))
	if strings.Join(localFields, ",") != strings.Join(sshFields, ",") {
		t.Fatalf("local fields = %#v, ssh fields = %#v", localFields, sshFields)
	}
}

func TestRunBuildsLocalCommandAndRedactsEvidence(t *testing.T) {
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
		Target:       "local://localhost",
		ShellBinary:  "bash",
		RedactValues: []string{"top-secret"},
		Runner:       runner,
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
	if result.Command[len(result.Command)-1] != "printf token=[REDACTED]" {
		t.Fatalf("redacted command = %q", result.Command[len(result.Command)-1])
	}

	call := runner.calls[0]
	if call.name != "bash" {
		t.Fatalf("runner name = %q, want bash", call.name)
	}
	wantArgs := []string{"-c", "printf token=top-secret"}
	if strings.Join(call.args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("runner args = %#v, want %#v", call.args, wantArgs)
	}
}

func TestUploadAndDownloadCopyLocalFiles(t *testing.T) {
	client, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	remote := filepath.Join(root, "remote", "payload.txt")
	download := filepath.Join(root, "download.txt")
	if err := os.WriteFile(source, []byte("local transport\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	upload := client.Upload(context.Background(), source, remote)
	gotDownload := client.Download(context.Background(), remote, download)
	if upload.Status != "succeeded" || gotDownload.Status != "succeeded" {
		t.Fatalf("upload/download statuses = %q/%q, want succeeded", upload.Status, gotDownload.Status)
	}
	got, err := os.ReadFile(download)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if string(got) != "local transport\n" {
		t.Fatalf("download = %q, want source content", got)
	}
}

func TestTimeoutIsRecordedAndRedacted(t *testing.T) {
	client, err := New(Config{
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
	if got, want := NormalizeTarget(" local://controller "), "controller"; got != want {
		t.Fatalf("NormalizeTarget() = %q, want %q", got, want)
	}
	sum := sha256.Sum256([]byte("controller"))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got := TargetDigest("local://controller"); got != want {
		t.Fatalf("TargetDigest() = %q, want %q", got, want)
	}
}

func TestE2EEnvLocalTransport(t *testing.T) {
	output := os.Getenv("TORQUE_OPS_TR_LOCAL_E2E_OUTPUT")
	if output == "" {
		t.Skip("set TORQUE_OPS_TR_LOCAL_E2E_OUTPUT to run the local transport E2E proof")
	}
	root := os.Getenv("TORQUE_OPS_TR_LOCAL_E2E_ROOT")
	if root == "" {
		root = filepath.Join(os.TempDir(), "torque-ops-tr-002-e2e")
	}
	canary := os.Getenv("TORQUE_OPS_TR_LOCAL_E2E_CANARY")
	if canary == "" {
		canary = "torque-redaction-canary-e2e"
	}

	client, err := New(Config{
		Target:       "local://localhost",
		Timeout:      20 * time.Second,
		RedactValues: []string{canary},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove root: %v", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	localDir := t.TempDir()
	uploadPath := filepath.Join(localDir, "upload.txt")
	downloadPath := filepath.Join(localDir, "download.txt")
	uploadBody := "upload-proof:" + time.Now().UTC().Format(time.RFC3339Nano) + "\n"
	if err := os.WriteFile(uploadPath, []byte(uploadBody), 0o600); err != nil {
		t.Fatalf("write upload fixture: %v", err)
	}

	remoteUpload := filepath.Join(root, "upload.txt")
	remoteDownload := filepath.Join(root, "download.txt")
	connect := client.Connect(context.Background())
	prepare := client.Run(
		context.Background(),
		"rm -rf "+ShellQuote(root)+" && mkdir -p "+ShellQuote(root)+" && printf 'prepared token=%s\n' "+ShellQuote(canary),
	)
	upload := client.Upload(context.Background(), uploadPath, remoteUpload)
	copyLocal := client.Run(
		context.Background(),
		"set -eu; test -s "+ShellQuote(remoteUpload)+"; cp "+ShellQuote(remoteUpload)+" "+ShellQuote(remoteDownload)+"; printf 'copied token=%s\n' "+ShellQuote(canary),
	)
	download := client.Download(context.Background(), remoteDownload, downloadPath)

	timeoutClient, err := New(Config{
		Target:       "local://localhost",
		Timeout:      100 * time.Millisecond,
		RedactValues: []string{canary},
	})
	if err != nil {
		t.Fatalf("New(timeout) error = %v", err)
	}
	timeout := timeoutClient.Run(context.Background(), "sleep 2")

	downloadBody, readErr := os.ReadFile(downloadPath)
	contentMatch := readErr == nil && string(downloadBody) == uploadBody
	shapeMatches := evidenceShapeMatchesSSH()
	operations := map[string]OperationResult{
		"connect":  connect,
		"prepare":  prepare,
		"upload":   upload,
		"copy":     copyLocal,
		"download": download,
		"timeout":  timeout,
	}
	errors := validateE2EOperations(operations, contentMatch, shapeMatches)
	status := "succeeded"
	if len(errors) > 0 {
		status = "failed"
	}
	doc := map[string]any{
		"apiVersion":              "torque.dev/e2e/v1",
		"kind":                    "OpsLocalTransportProof",
		"status":                  status,
		"targetDigest":            client.TargetDigest(),
		"localRoot":               root,
		"operations":              operations,
		"downloadContentMatch":    contentMatch,
		"evidenceShapeMatchesSSH": shapeMatches,
		"errors":                  errors,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal proof: %v", err)
	}
	if strings.Contains(string(raw), canary) || strings.Contains(string(raw), "secret://") {
		t.Fatal("local transport proof leaked secret material")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(output, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	if len(errors) > 0 {
		t.Fatalf("local transport E2E failed: %s", strings.Join(errors, "; "))
	}
}

func validateE2EOperations(operations map[string]OperationResult, contentMatch, shapeMatches bool) []string {
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
	if !shapeMatches {
		errs = append(errs, "local operation evidence shape does not match SSH")
	}
	return errs
}

func evidenceShapeMatchesSSH() bool {
	localFields := jsonFields(reflect.TypeOf(OperationResult{}))
	sshFields := jsonFields(reflect.TypeOf(sshtransport.OperationResult{}))
	return strings.Join(localFields, ",") == strings.Join(sshFields, ",")
}

func jsonFields(t reflect.Type) []string {
	var fields []string
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		fields = append(fields, name)
	}
	sort.Strings(fields)
	return fields
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
