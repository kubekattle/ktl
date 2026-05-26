package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
	natsgo "github.com/nats-io/nats.go"
)

func TestNewRejectsMissingSubject(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("New() error = nil, want missing subject")
	}
}

func TestHandleAssignmentRunsLocalCommand(t *testing.T) {
	runner := &recordingRunner{
		output: transport.RunOutput{Stdout: []byte("ok token=top-secret\n"), ExitCode: 0},
	}
	worker, err := New(Config{
		Server:       "nats://127.0.0.1:4222",
		Subject:      "torque.lab.assign.mysql",
		RedactValues: []string{"top-secret"},
		Runner:       runner,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result := worker.HandleAssignment(context.Background(), natstransport.NewCommandAssignment("run", "torque.lab.assign.mysql", "printf token=top-secret", time.Now()))
	if result.Status != "succeeded" {
		t.Fatalf("Status = %q, want succeeded", result.Status)
	}
	if result.TargetDigest != natstransport.TargetDigest("torque.lab.assign.mysql") {
		t.Fatalf("TargetDigest = %q", result.TargetDigest)
	}
	if strings.Contains(result.Stdout, "top-secret") || strings.Contains(strings.Join(result.Command, " "), "top-secret") {
		t.Fatalf("result was not redacted: %#v", result)
	}
	if len(runner.calls) != 1 || !strings.Contains(strings.Join(runner.calls[0].args, " "), "printf token=top-secret") {
		t.Fatalf("runner calls = %#v", runner.calls)
	}
}

func TestHandleAssignmentRunsWhenRequiredCapabilityIsAvailable(t *testing.T) {
	runner := &recordingRunner{
		output: transport.RunOutput{Stdout: []byte("ok\n"), ExitCode: 0},
	}
	worker, err := New(Config{
		Subject:                    "torque.lab.assign.mysql",
		Capabilities:               []string{"host.command.run"},
		DisableCapabilityDiscovery: true,
		AgentID:                    "agent-worker-01",
		Tenant:                     "lab",
		TargetID:                   "host/mysql-01",
		Hostname:                   "mysql-01",
		Runner:                     runner,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	assignment := natstransport.NewCommandAssignmentWithMetadata("run", "torque.lab.assign.mysql", "printf ok", time.Now(), natstransport.CommandAssignmentMetadata{
		RequiredCapability: "host.command.run",
		NodeKind:           "host.command.run",
		RunID:              "run-123",
		NodeID:             "host.command.run/write-marker",
	})
	result := worker.HandleAssignment(context.Background(), assignment)
	if result.Status != "succeeded" {
		t.Fatalf("Status = %q, want succeeded: %#v", result.Status, result)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %#v, want one call", runner.calls)
	}
	assertMetadata(t, result.Metadata, map[string]string{
		"agentId":            "agent-worker-01",
		"tenant":             "lab",
		"targetId":           "host/mysql-01",
		"hostname":           "mysql-01",
		"workerSubject":      "torque.lab.assign.mysql",
		"requiredCapability": "host.command.run",
		"nodeKind":           "host.command.run",
		"runId":              "run-123",
		"nodeId":             "host.command.run/write-marker",
		"workerDecision":     "executed",
	})
	if !strings.HasPrefix(result.Metadata["capabilityDigest"], "sha256:") {
		t.Fatalf("capabilityDigest = %q", result.Metadata["capabilityDigest"])
	}
}

func TestHandleAssignmentBlocksMissingRequiredCapability(t *testing.T) {
	runner := &recordingRunner{
		output: transport.RunOutput{Stdout: []byte("should-not-run\n"), ExitCode: 0},
	}
	worker, err := New(Config{
		Subject:                    "torque.lab.assign.mysql",
		Capabilities:               []string{"mysql.replication.verify"},
		DisableCapabilityDiscovery: true,
		AgentID:                    "agent-worker-02",
		Tenant:                     "lab",
		TargetID:                   "host/mysql-02",
		Hostname:                   "mysql-02",
		Runner:                     runner,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	assignment := natstransport.NewCommandAssignmentWithMetadata("run", "torque.lab.assign.mysql", "printf should-not-run", time.Now(), natstransport.CommandAssignmentMetadata{
		RequiredCapability: "host.command.run",
		NodeKind:           "host.command.run",
		RunID:              "run-123",
		NodeID:             "host.command.run/write-marker",
	})
	result := worker.HandleAssignment(context.Background(), assignment)
	if result.Status != "blocked" || !strings.Contains(result.Error, "missing required capability host.command.run") {
		t.Fatalf("result = %#v, want blocked missing capability", result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner was called despite missing capability: %#v", runner.calls)
	}
	assertMetadata(t, result.Metadata, map[string]string{
		"agentId":            "agent-worker-02",
		"tenant":             "lab",
		"targetId":           "host/mysql-02",
		"hostname":           "mysql-02",
		"workerSubject":      "torque.lab.assign.mysql",
		"requiredCapability": "host.command.run",
		"nodeKind":           "host.command.run",
		"runId":              "run-123",
		"nodeId":             "host.command.run/write-marker",
		"workerDecision":     "blocked",
	})
}

func TestHandleAssignmentRejectsTargetMismatch(t *testing.T) {
	worker, err := New(Config{Subject: "torque.lab.assign.mysql"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result := worker.HandleAssignment(context.Background(), natstransport.NewCommandAssignment("run", "torque.lab.assign.other", "true", time.Now()))
	if result.Status != "failed" || !strings.Contains(result.Error, "does not match") {
		t.Fatalf("result = %#v, want target mismatch failure", result)
	}
}

func TestWorkerExecutesCommandOverLocalNATSServer(t *testing.T) {
	serverURL := startLocalNATSServer(t)
	subject := "torque.test.assign.worker"
	ready := make(chan struct{})
	worker, err := New(Config{
		Server:   serverURL,
		Subject:  subject,
		Ready:    ready,
		Timeout:  2 * time.Second,
		AgentID:  "agent-nats-test",
		Tenant:   "lab",
		TargetID: "host/nats-test",
		Hostname: "nats-test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Run(ctx)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("worker did not become ready")
	}
	conn, err := natsgo.Connect(serverURL, natsgo.Name("torque-worker-test"), natsgo.Timeout(time.Second))
	if err != nil {
		cancel()
		t.Fatalf("connect test client: %v", err)
	}
	defer conn.Close()
	assignment := natstransport.NewCommandAssignment("run", subject, "printf 'nats-worker-ok token=top-secret\\n'", time.Now())
	raw, err := json.Marshal(assignment)
	if err != nil {
		cancel()
		t.Fatalf("marshal assignment: %v", err)
	}
	reqCtx, reqCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer reqCancel()
	msg, err := conn.RequestWithContext(reqCtx, subject, raw)
	if err != nil {
		cancel()
		t.Fatalf("request worker: %v", err)
	}
	var result transport.OperationResult
	if err := json.Unmarshal(msg.Data, &result); err != nil {
		cancel()
		t.Fatalf("parse response: %v", err)
	}
	if result.Status != "succeeded" || !strings.Contains(result.Stdout, "nats-worker-ok") {
		cancel()
		t.Fatalf("result = %#v, want succeeded response", result)
	}
	if strings.Contains(result.Stdout, "top-secret") {
		cancel()
		t.Fatalf("stdout was not redacted: %q", result.Stdout)
	}
	if result.Metadata["agentId"] != "agent-nats-test" || result.Metadata["workerDecision"] != "executed" {
		cancel()
		t.Fatalf("metadata = %#v", result.Metadata)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("worker Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
}

func assertMetadata(t *testing.T, got map[string]string, want map[string]string) {
	t.Helper()
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("metadata[%s] = %q, want %q in %#v", key, got[key], value, got)
		}
	}
}

type recordingRunner struct {
	calls  []recordedCall
	output transport.RunOutput
	err    error
}

type recordedCall struct {
	name string
	args []string
}

func (r *recordingRunner) Run(ctx context.Context, name string, args []string) (transport.RunOutput, error) {
	r.calls = append(r.calls, recordedCall{name: name, args: append([]string(nil), args...)})
	return r.output, r.err
}

func startLocalNATSServer(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("nats-server")
	if err != nil {
		t.Skip("nats-server binary not found")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve nats port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close reserved nats port: %v", err)
	}
	cmd := exec.Command(binary, "-a", "127.0.0.1", "-p", strconv.Itoa(port))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start nats-server: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})
	url := fmt.Sprintf("nats://127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := natsgo.Connect(url, natsgo.NoReconnect(), natsgo.Timeout(100*time.Millisecond))
		if err == nil {
			conn.Close()
			return url
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("nats-server did not become ready")
	}
	t.Fatalf("wait for nats-server: %v", lastErr)
	return ""
}
