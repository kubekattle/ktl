package worker

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
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
		TargetID:           "host/mysql-01",
		ExpectedAgentID:    "agent-worker-01",
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
		"assignmentTargetId": "host/mysql-01",
		"expectedAgentId":    "agent-worker-01",
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

func TestHandleAssignmentBlocksUnexpectedAgentIdentity(t *testing.T) {
	runner := &recordingRunner{
		output: transport.RunOutput{Stdout: []byte("ok\n"), ExitCode: 0},
	}
	worker, err := New(Config{
		Server:                     "nats://127.0.0.1:4222",
		Subject:                    "torque.lab.assign.mysql",
		Capabilities:               []string{"host.command.run"},
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
		TargetID:           "host/mysql-01",
		ExpectedAgentID:    "agent-worker-01",
		RequiredCapability: "host.command.run",
		NodeKind:           "host.command.run",
		RunID:              "run-123",
		NodeID:             "host.command.run/write-marker",
	})
	result := worker.HandleAssignment(context.Background(), assignment)
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked: %#v", result.Status, result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner was called despite identity mismatch: %#v", runner.calls)
	}
	assertMetadata(t, result.Metadata, map[string]string{
		"agentId":            "agent-worker-02",
		"targetId":           "host/mysql-02",
		"workerDecision":     "blocked",
		"assignmentTargetId": "host/mysql-01",
		"expectedAgentId":    "agent-worker-01",
		"requiredCapability": "host.command.run",
	})
}

func TestHandleAssignmentRejectsTargetMismatch(t *testing.T) {
	worker, err := New(Config{Subject: "torque.lab.assign.mysql"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result := worker.HandleAssignment(context.Background(), natstransport.NewCommandAssignment("run", "torque.lab.assign.other", "true", time.Now()))
	if result.Status != "blocked" || !strings.Contains(result.Error, "does not match") {
		t.Fatalf("result = %#v, want target mismatch block", result)
	}
}

func TestHandleMessageRunsSignedAssignmentEnvelope(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	runner := &recordingRunner{
		output: transport.RunOutput{Stdout: []byte("signed-ok\n"), ExitCode: 0},
	}
	worker, err := New(Config{
		Subject:                    "torque.assign.lab.host_signed",
		Capabilities:               []string{"host.command.run"},
		DisableCapabilityDiscovery: true,
		AgentID:                    "agent-signed",
		Tenant:                     "lab",
		TargetID:                   "host/signed",
		Hostname:                   "signed",
		Runner:                     runner,
		VerifyAssignments:          true,
		TrustedIssuerPublicKey:     pub,
		AssignmentPolicyDigest:     "sha256:policy",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	assignment := natstransport.NewCommandAssignmentWithMetadata("run", "torque.assign.lab.host_signed", "printf signed-ok", time.Now(), natstransport.CommandAssignmentMetadata{
		TargetID:           "host/signed",
		ExpectedAgentID:    "agent-signed",
		RequiredCapability: "host.command.run",
		NodeKind:           "host.command.run",
		RunID:              "run-signed",
		NodeID:             "host.command.run/signed",
		PlanDigest:         "sha256:plan",
	})
	envelope, err := natstransport.SignCommandAssignmentEnvelope(assignment, natstransport.CommandAssignmentEnvelopeOptions{
		PrivateKey:   priv,
		Issuer:       "torque-stack",
		Tenant:       "lab",
		PolicyDigest: "sha256:policy",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("sign envelope: %v", err)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	result := worker.HandleMessage(context.Background(), raw)
	if result.Status != "succeeded" || !strings.Contains(result.Stdout, "signed-ok") {
		t.Fatalf("result = %#v, want signed success", result)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %#v, want one", runner.calls)
	}
	assertMetadata(t, result.Metadata, map[string]string{
		"assignmentEnvelope":       "true",
		"signatureVerified":        "true",
		"assignmentIssuer":         "torque-stack",
		"assignmentEnvelopeTenant": "lab",
		"policyDigest":             "sha256:policy",
		"workerDecision":           "executed",
	})
}

func TestHandleMessageBlocksUnsignedAssignmentWhenVerificationRequired(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	runner := &recordingRunner{
		output: transport.RunOutput{Stdout: []byte("should-not-run\n"), ExitCode: 0},
	}
	worker, err := New(Config{
		Subject:                    "torque.assign.lab.host_signed",
		Capabilities:               []string{"host.command.run"},
		DisableCapabilityDiscovery: true,
		Tenant:                     "lab",
		TargetID:                   "host/signed",
		Runner:                     runner,
		VerifyAssignments:          true,
		TrustedIssuerPublicKey:     pub,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	assignment := natstransport.NewCommandAssignmentWithMetadata("run", "torque.assign.lab.host_signed", "printf should-not-run", time.Now(), natstransport.CommandAssignmentMetadata{
		TargetID: "host/signed",
	})
	raw, err := json.Marshal(assignment)
	if err != nil {
		t.Fatalf("marshal assignment: %v", err)
	}
	result := worker.HandleMessage(context.Background(), raw)
	if result.Status != "blocked" || !strings.Contains(result.Error, "signed assignment envelope is required") {
		t.Fatalf("result = %#v, want unsigned block", result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner was called for unsigned assignment: %#v", runner.calls)
	}
	if result.Metadata["workerDecision"] != "signature-blocked" {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
}

func TestHandleMessageBlocksExpiredSignedEnvelope(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	worker, err := New(Config{
		Subject:                    "torque.assign.lab.host_signed",
		Capabilities:               []string{"host.command.run"},
		DisableCapabilityDiscovery: true,
		Tenant:                     "lab",
		TargetID:                   "host/signed",
		VerifyAssignments:          true,
		TrustedIssuerPublicKey:     pub,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	assignment := natstransport.NewCommandAssignmentWithMetadata("run", "torque.assign.lab.host_signed", "printf should-not-run", time.Now(), natstransport.CommandAssignmentMetadata{TargetID: "host/signed"})
	envelope, err := natstransport.SignCommandAssignmentEnvelope(assignment, natstransport.CommandAssignmentEnvelopeOptions{
		PrivateKey: priv,
		Issuer:     "torque-stack",
		Tenant:     "lab",
		IssuedAt:   time.Now().Add(-2 * time.Minute),
		ExpiresAt:  time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("sign envelope: %v", err)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	result := worker.HandleMessage(context.Background(), raw)
	if result.Status != "blocked" || !strings.Contains(result.Error, "expired") {
		t.Fatalf("result = %#v, want expired block", result)
	}
	if result.Metadata["assignmentEnvelope"] != "true" || result.Metadata["signatureVerified"] != "true" {
		t.Fatalf("metadata = %#v", result.Metadata)
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

func TestWorkerConsumesDurableJetStreamAssignment(t *testing.T) {
	serverURL := startLocalNATSJetStreamServer(t)
	subject := "torque.assign.lab.host_js-1"
	assignmentStream := "TORQUE_ASSIGNMENTS_TEST"
	receiptStream := "TORQUE_RECEIPTS_TEST"
	runner := &recordingRunner{
		output: transport.RunOutput{Stdout: []byte("durable-ok\n"), ExitCode: 0},
	}

	conn, err := natsgo.Connect(serverURL, natsgo.Name("torque-worker-js-test"), natsgo.Timeout(time.Second))
	if err != nil {
		t.Fatalf("connect test client: %v", err)
	}
	defer conn.Close()
	js, err := conn.JetStream(natsgo.MaxWait(2 * time.Second))
	if err != nil {
		t.Fatalf("open JetStream: %v", err)
	}
	ctx := context.Background()
	if err := natstransport.EnsureStream(ctx, js, assignmentStream, []string{natstransport.DefaultAssignmentStreamSubject}, time.Hour); err != nil {
		t.Fatalf("ensure assignment stream: %v", err)
	}
	if err := natstransport.EnsureStream(ctx, js, receiptStream, []string{natstransport.DefaultReceiptStreamSubject}, time.Hour); err != nil {
		t.Fatalf("ensure receipt stream: %v", err)
	}

	assignment := natstransport.NewCommandAssignmentWithMetadata("run", subject, "printf durable-ok", time.Now(), natstransport.CommandAssignmentMetadata{
		TargetID:           "host/js-1",
		ExpectedAgentID:    "agent-js-1",
		RequiredCapability: "host.command.run",
		NodeKind:           "host.command.run",
		RunID:              "run-js-1",
		NodeID:             "host.command.run/write-marker",
	})
	raw, err := json.Marshal(assignment)
	if err != nil {
		t.Fatalf("marshal assignment: %v", err)
	}
	if _, err := js.Publish(subject, raw, natsgo.Context(ctx)); err != nil {
		t.Fatalf("publish offline assignment: %v", err)
	}

	ready := make(chan struct{})
	worker, err := New(Config{
		Server:                     serverURL,
		Subject:                    subject,
		Delivery:                   natstransport.DeliveryJetStream,
		AssignmentStream:           assignmentStream,
		ReceiptStream:              receiptStream,
		Durable:                    "worker-js-1",
		LedgerPath:                 t.TempDir() + "/assignments.sqlite",
		Ready:                      ready,
		Timeout:                    2 * time.Second,
		Capabilities:               []string{"host.command.run"},
		DisableCapabilityDiscovery: true,
		AgentID:                    "agent-js-1",
		Tenant:                     "lab",
		TargetID:                   "host/js-1",
		Hostname:                   "js-1",
		Runner:                     runner,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Run(workerCtx)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("worker did not become ready")
	}

	receiptSubject := natstransport.ReceiptSubject("lab", "run-js-1", "host/js-1")
	sub, err := js.PullSubscribe(
		receiptSubject,
		"worker-js-receipt-test",
		natsgo.BindStream(receiptStream),
		natsgo.DeliverAll(),
		natsgo.AckExplicit(),
		natsgo.ManualAck(),
	)
	if err != nil {
		cancel()
		t.Fatalf("subscribe receipt stream: %v", err)
	}
	msgs, err := sub.Fetch(1, natsgo.MaxWait(5*time.Second))
	if err != nil {
		cancel()
		t.Fatalf("fetch receipt: %v", err)
	}
	if len(msgs) != 1 {
		cancel()
		t.Fatalf("receipts = %d, want one", len(msgs))
	}
	var result transport.OperationResult
	if err := json.Unmarshal(msgs[0].Data, &result); err != nil {
		cancel()
		t.Fatalf("parse receipt: %v", err)
	}
	_ = msgs[0].Ack()
	if result.Status != "succeeded" || !strings.Contains(result.Stdout, "durable-ok") {
		cancel()
		t.Fatalf("result = %#v, want durable success", result)
	}
	if len(runner.calls) != 1 {
		cancel()
		t.Fatalf("runner calls = %#v, want one call", runner.calls)
	}
	assertMetadata(t, result.Metadata, map[string]string{
		"agentId":            "agent-js-1",
		"targetId":           "host/js-1",
		"assignmentTargetId": "host/js-1",
		"expectedAgentId":    "agent-js-1",
		"delivery":           natstransport.DeliveryJetStream,
		"assignmentStream":   assignmentStream,
		"receiptStream":      receiptStream,
		"assignmentConsumer": "worker-js-1",
		"assignmentSubject":  subject,
		"requiredCapability": "host.command.run",
		"workerDecision":     "executed",
	})
	if result.Metadata["assignmentSequence"] == "" {
		cancel()
		t.Fatalf("missing assignmentSequence in metadata: %#v", result.Metadata)
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

func TestWorkerDedupesRepeatedDurableJetStreamAssignment(t *testing.T) {
	serverURL := startLocalNATSJetStreamServer(t)
	subject := "torque.assign.lab.host_js-dedupe"
	assignmentStream := "TORQUE_ASSIGNMENTS_DEDUPE_TEST"
	receiptStream := "TORQUE_RECEIPTS_DEDUPE_TEST"
	runner := &recordingRunner{
		output: transport.RunOutput{Stdout: []byte("dedupe-ok\n"), ExitCode: 0},
	}
	conn, err := natsgo.Connect(serverURL, natsgo.Name("torque-worker-js-dedupe-test"), natsgo.Timeout(time.Second))
	if err != nil {
		t.Fatalf("connect test client: %v", err)
	}
	defer conn.Close()
	js, err := conn.JetStream(natsgo.MaxWait(2 * time.Second))
	if err != nil {
		t.Fatalf("open JetStream: %v", err)
	}
	ctx := context.Background()
	if err := natstransport.EnsureStream(ctx, js, assignmentStream, []string{natstransport.DefaultAssignmentStreamSubject}, time.Hour); err != nil {
		t.Fatalf("ensure assignment stream: %v", err)
	}
	if err := natstransport.EnsureStream(ctx, js, receiptStream, []string{natstransport.DefaultReceiptStreamSubject}, time.Hour); err != nil {
		t.Fatalf("ensure receipt stream: %v", err)
	}
	assignment := natstransport.NewCommandAssignmentWithMetadata("run", subject, "printf dedupe-ok", time.Now(), natstransport.CommandAssignmentMetadata{
		TargetID:           "host/js-dedupe",
		ExpectedAgentID:    "agent-js-dedupe",
		RequiredCapability: "host.command.run",
		NodeKind:           "host.command.run",
		RunID:              "run-js-dedupe",
		NodeID:             "host.command.run/write-marker",
		PlanDigest:         "sha256:plan",
	})
	raw, err := json.Marshal(assignment)
	if err != nil {
		t.Fatalf("marshal assignment: %v", err)
	}
	if _, err := js.Publish(subject, raw, natsgo.Context(ctx)); err != nil {
		t.Fatalf("publish first assignment: %v", err)
	}
	if _, err := js.Publish(subject, raw, natsgo.Context(ctx)); err != nil {
		t.Fatalf("publish duplicate assignment: %v", err)
	}

	ready := make(chan struct{})
	worker, err := New(Config{
		Server:                     serverURL,
		Subject:                    subject,
		Delivery:                   natstransport.DeliveryJetStream,
		AssignmentStream:           assignmentStream,
		ReceiptStream:              receiptStream,
		Durable:                    "worker-js-dedupe",
		LedgerPath:                 t.TempDir() + "/assignments.sqlite",
		Ready:                      ready,
		Timeout:                    2 * time.Second,
		Capabilities:               []string{"host.command.run"},
		DisableCapabilityDiscovery: true,
		AgentID:                    "agent-js-dedupe",
		Tenant:                     "lab",
		TargetID:                   "host/js-dedupe",
		Hostname:                   "js-dedupe",
		Runner:                     runner,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Run(workerCtx)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("worker did not become ready")
	}

	receiptSubject := natstransport.ReceiptSubject("lab", "run-js-dedupe", "host/js-dedupe")
	sub, err := js.PullSubscribe(
		receiptSubject,
		"worker-js-dedupe-receipts",
		natsgo.BindStream(receiptStream),
		natsgo.DeliverAll(),
		natsgo.AckExplicit(),
		natsgo.ManualAck(),
	)
	if err != nil {
		cancel()
		t.Fatalf("subscribe receipt stream: %v", err)
	}
	msgs := fetchNATSReceipts(t, sub, 2, 5*time.Second)
	if len(msgs) != 2 {
		cancel()
		t.Fatalf("receipts = %d, want two", len(msgs))
	}
	executed := 0
	deduped := 0
	for _, msg := range msgs {
		var result transport.OperationResult
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			cancel()
			t.Fatalf("parse receipt: %v", err)
		}
		_ = msg.Ack()
		if result.Metadata["assignmentId"] != assignment.AssignmentID {
			cancel()
			t.Fatalf("assignmentId metadata = %q, want %q", result.Metadata["assignmentId"], assignment.AssignmentID)
		}
		switch result.Metadata["workerDecision"] {
		case "executed":
			executed++
		case "deduped":
			deduped++
			if result.Metadata["deduped"] != "true" || result.Metadata["replayedReceipt"] != "true" {
				cancel()
				t.Fatalf("dedupe metadata missing: %#v", result.Metadata)
			}
		default:
			cancel()
			t.Fatalf("unexpected workerDecision metadata: %#v", result.Metadata)
		}
	}
	if executed != 1 || deduped != 1 {
		cancel()
		t.Fatalf("executed=%d deduped=%d, want one each", executed, deduped)
	}
	if len(runner.calls) != 1 {
		cancel()
		t.Fatalf("runner calls = %#v, want one execution", runner.calls)
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

func TestWorkerRetriesDurableAssignmentUntilSuccess(t *testing.T) {
	serverURL := startLocalNATSJetStreamServer(t)
	subject := "torque.assign.lab.host_js-retry"
	assignmentStream := "TORQUE_ASSIGNMENTS_RETRY_TEST"
	receiptStream := "TORQUE_RECEIPTS_RETRY_TEST"
	runner := &flakyRunner{
		failures: 2,
		output:   transport.RunOutput{Stdout: []byte("retry-ok\n"), ExitCode: 0},
	}
	conn, err := natsgo.Connect(serverURL, natsgo.Name("torque-worker-js-retry-test"), natsgo.Timeout(time.Second))
	if err != nil {
		t.Fatalf("connect test client: %v", err)
	}
	defer conn.Close()
	js, err := conn.JetStream(natsgo.MaxWait(2 * time.Second))
	if err != nil {
		t.Fatalf("open JetStream: %v", err)
	}
	ctx := context.Background()
	if err := natstransport.EnsureStream(ctx, js, assignmentStream, []string{natstransport.DefaultAssignmentStreamSubject}, time.Hour); err != nil {
		t.Fatalf("ensure assignment stream: %v", err)
	}
	if err := natstransport.EnsureStream(ctx, js, receiptStream, []string{natstransport.DefaultReceiptStreamSubject}, time.Hour); err != nil {
		t.Fatalf("ensure receipt stream: %v", err)
	}
	assignment := natstransport.NewCommandAssignmentWithMetadata("run", subject, "printf retry-ok", time.Now(), natstransport.CommandAssignmentMetadata{
		TargetID:           "host/js-retry",
		ExpectedAgentID:    "agent-js-retry",
		RequiredCapability: "host.command.run",
		NodeKind:           "host.command.run",
		RunID:              "run-js-retry",
		NodeID:             "host.command.run/retry-marker",
	})
	raw, err := json.Marshal(assignment)
	if err != nil {
		t.Fatalf("marshal assignment: %v", err)
	}
	if _, err := js.Publish(subject, raw, natsgo.Context(ctx)); err != nil {
		t.Fatalf("publish assignment: %v", err)
	}

	ready := make(chan struct{})
	worker, err := New(Config{
		Server:                     serverURL,
		Subject:                    subject,
		Delivery:                   natstransport.DeliveryJetStream,
		AssignmentStream:           assignmentStream,
		ReceiptStream:              receiptStream,
		Durable:                    "worker-js-retry",
		LedgerPath:                 t.TempDir() + "/assignments.sqlite",
		MaxDeliver:                 3,
		AckWait:                    200 * time.Millisecond,
		NakDelay:                   20 * time.Millisecond,
		Ready:                      ready,
		Timeout:                    2 * time.Second,
		Capabilities:               []string{"host.command.run"},
		DisableCapabilityDiscovery: true,
		AgentID:                    "agent-js-retry",
		Tenant:                     "lab",
		TargetID:                   "host/js-retry",
		Hostname:                   "js-retry",
		Runner:                     runner,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Run(workerCtx)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("worker did not become ready")
	}

	receiptSubject := natstransport.ReceiptSubject("lab", "run-js-retry", "host/js-retry")
	sub, err := js.PullSubscribe(receiptSubject, "worker-js-retry-receipts", natsgo.BindStream(receiptStream), natsgo.DeliverAll(), natsgo.AckExplicit(), natsgo.ManualAck())
	if err != nil {
		cancel()
		t.Fatalf("subscribe receipt stream: %v", err)
	}
	msgs := fetchNATSReceipts(t, sub, 1, 8*time.Second)
	if len(msgs) != 1 {
		cancel()
		t.Fatalf("receipts = %d, want one", len(msgs))
	}
	var result transport.OperationResult
	if err := json.Unmarshal(msgs[0].Data, &result); err != nil {
		cancel()
		t.Fatalf("parse receipt: %v", err)
	}
	_ = msgs[0].Ack()
	if result.Status != "succeeded" || !strings.Contains(result.Stdout, "retry-ok") {
		cancel()
		t.Fatalf("result = %#v, want retry success", result)
	}
	if calls := runner.Calls(); calls != 3 {
		cancel()
		t.Fatalf("runner calls = %d, want 3", calls)
	}
	assertMetadata(t, result.Metadata, map[string]string{
		"assignmentId":   assignment.AssignmentID,
		"workerDecision": "executed",
		"numDelivered":   "3",
		"maxDeliver":     "3",
		"ledgerAttempt":  "3",
	})
	if !strings.Contains(result.Metadata["retryPolicy"], "maxDeliver=3") {
		cancel()
		t.Fatalf("retryPolicy metadata = %#v", result.Metadata)
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

func TestWorkerDeadLettersDurableAssignmentAfterRetryBudget(t *testing.T) {
	serverURL := startLocalNATSJetStreamServer(t)
	subject := "torque.assign.lab.host_js-deadletter"
	assignmentStream := "TORQUE_ASSIGNMENTS_DEADLETTER_TEST"
	receiptStream := "TORQUE_RECEIPTS_DEADLETTER_TEST"
	runner := &flakyRunner{
		failures: 99,
		output:   transport.RunOutput{Stdout: []byte("unused\n"), ExitCode: 0},
	}
	conn, err := natsgo.Connect(serverURL, natsgo.Name("torque-worker-js-deadletter-test"), natsgo.Timeout(time.Second))
	if err != nil {
		t.Fatalf("connect test client: %v", err)
	}
	defer conn.Close()
	js, err := conn.JetStream(natsgo.MaxWait(2 * time.Second))
	if err != nil {
		t.Fatalf("open JetStream: %v", err)
	}
	ctx := context.Background()
	if err := natstransport.EnsureStream(ctx, js, assignmentStream, []string{natstransport.DefaultAssignmentStreamSubject}, time.Hour); err != nil {
		t.Fatalf("ensure assignment stream: %v", err)
	}
	if err := natstransport.EnsureStream(ctx, js, receiptStream, []string{natstransport.DefaultReceiptStreamSubject}, time.Hour); err != nil {
		t.Fatalf("ensure receipt stream: %v", err)
	}
	assignment := natstransport.NewCommandAssignmentWithMetadata("run", subject, "printf never", time.Now(), natstransport.CommandAssignmentMetadata{
		TargetID:           "host/js-deadletter",
		ExpectedAgentID:    "agent-js-deadletter",
		RequiredCapability: "host.command.run",
		NodeKind:           "host.command.run",
		RunID:              "run-js-deadletter",
		NodeID:             "host.command.run/deadletter",
	})
	raw, err := json.Marshal(assignment)
	if err != nil {
		t.Fatalf("marshal assignment: %v", err)
	}
	if _, err := js.Publish(subject, raw, natsgo.Context(ctx)); err != nil {
		t.Fatalf("publish assignment: %v", err)
	}

	ready := make(chan struct{})
	worker, err := New(Config{
		Server:                     serverURL,
		Subject:                    subject,
		Delivery:                   natstransport.DeliveryJetStream,
		AssignmentStream:           assignmentStream,
		ReceiptStream:              receiptStream,
		Durable:                    "worker-js-deadletter",
		LedgerPath:                 t.TempDir() + "/assignments.sqlite",
		MaxDeliver:                 2,
		AckWait:                    200 * time.Millisecond,
		NakDelay:                   20 * time.Millisecond,
		Ready:                      ready,
		Timeout:                    2 * time.Second,
		Capabilities:               []string{"host.command.run"},
		DisableCapabilityDiscovery: true,
		AgentID:                    "agent-js-deadletter",
		Tenant:                     "lab",
		TargetID:                   "host/js-deadletter",
		Hostname:                   "js-deadletter",
		Runner:                     runner,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Run(workerCtx)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("worker did not become ready")
	}

	receiptSubject := natstransport.ReceiptSubject("lab", "run-js-deadletter", "host/js-deadletter")
	sub, err := js.PullSubscribe(receiptSubject, "worker-js-deadletter-receipts", natsgo.BindStream(receiptStream), natsgo.DeliverAll(), natsgo.AckExplicit(), natsgo.ManualAck())
	if err != nil {
		cancel()
		t.Fatalf("subscribe receipt stream: %v", err)
	}
	msgs := fetchNATSReceipts(t, sub, 1, 8*time.Second)
	if len(msgs) != 1 {
		cancel()
		t.Fatalf("receipts = %d, want one", len(msgs))
	}
	var result transport.OperationResult
	if err := json.Unmarshal(msgs[0].Data, &result); err != nil {
		cancel()
		t.Fatalf("parse receipt: %v", err)
	}
	_ = msgs[0].Ack()
	if result.Status != "blocked" || !strings.Contains(result.Error, "retry budget exhausted") {
		cancel()
		t.Fatalf("result = %#v, want blocked dead-letter", result)
	}
	if calls := runner.Calls(); calls != 2 {
		cancel()
		t.Fatalf("runner calls = %d, want 2", calls)
	}
	assertMetadata(t, result.Metadata, map[string]string{
		"assignmentId":   assignment.AssignmentID,
		"workerDecision": "dead-letter",
		"deadLetter":     "true",
		"retryExhausted": "true",
		"numDelivered":   "2",
		"maxDeliver":     "2",
		"ledgerAttempt":  "2",
	})
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

type flakyRunner struct {
	mu       sync.Mutex
	failures int
	calls    int
	output   transport.RunOutput
}

func (r *flakyRunner) Run(ctx context.Context, name string, args []string) (transport.RunOutput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls <= r.failures {
		return transport.RunOutput{Stderr: []byte("transient failure\n"), ExitCode: 42}, fmt.Errorf("transient failure %d", r.calls)
	}
	return r.output, nil
}

func (r *flakyRunner) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func startLocalNATSServer(t *testing.T) string {
	t.Helper()
	return startLocalNATSServerWithArgs(t, nil)
}

func startLocalNATSJetStreamServer(t *testing.T) string {
	t.Helper()
	return startLocalNATSServerWithArgs(t, []string{"-js", "-sd", t.TempDir()})
}

func startLocalNATSServerWithArgs(t *testing.T, extraArgs []string) string {
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
	args := []string{"-a", "127.0.0.1", "-p", strconv.Itoa(port)}
	args = append(args, extraArgs...)
	cmd := exec.Command(binary, args...)
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

func fetchNATSReceipts(t *testing.T, sub *natsgo.Subscription, want int, timeout time.Duration) []*natsgo.Msg {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var out []*natsgo.Msg
	for len(out) < want && time.Now().Before(deadline) {
		wait := time.Until(deadline)
		if wait > 500*time.Millisecond {
			wait = 500 * time.Millisecond
		}
		if wait <= 0 {
			break
		}
		msgs, err := sub.Fetch(want-len(out), natsgo.MaxWait(wait))
		if err != nil {
			if errors.Is(err, natsgo.ErrTimeout) {
				continue
			}
			t.Fatalf("fetch receipts: %v", err)
		}
		out = append(out, msgs...)
	}
	return out
}
