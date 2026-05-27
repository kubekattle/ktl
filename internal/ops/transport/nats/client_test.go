package natstransport

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	localtransport "github.com/ingresslabs/torque/internal/ops/transport/local"
)

func TestNewRejectsEmptyTarget(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("New() error = nil, want missing target")
	}
}

func TestOperationResultJSONShapeMatchesLocal(t *testing.T) {
	natsFields := jsonFields(reflect.TypeOf(OperationResult{}))
	localFields := jsonFields(reflect.TypeOf(localtransport.OperationResult{}))
	if strings.Join(natsFields, ",") != strings.Join(localFields, ",") {
		t.Fatalf("nats fields = %#v, local fields = %#v", natsFields, localFields)
	}
}

func TestRunBuildsNATSRequestAndParsesWorkerReceipt(t *testing.T) {
	workerReceipt := OperationResult{
		Operation:    "run",
		Status:       "succeeded",
		Stdout:       "mysql-ok token=top-secret\n",
		Stderr:       "authorization: bearer top-secret\n",
		ExitCode:     0,
		TargetDigest: "worker-digest",
		Metadata: map[string]string{
			"agentId":        "agent-mysql-01",
			"workerDecision": "executed",
		},
	}
	raw, err := json.Marshal(workerReceipt)
	if err != nil {
		t.Fatalf("marshal worker receipt: %v", err)
	}
	requester := &recordingRequester{responses: [][]byte{raw}}
	client, err := New(Config{
		Target:             "nats-mesh://torque.lab.assign.agent.mysql",
		Server:             "nats://127.0.0.1:4222",
		Creds:              "/tmp/nats.creds",
		RedactValues:       []string{"top-secret"},
		RequiredCapability: "mysql.replication.verify",
		NodeKind:           "mysql.replication.verify",
		RunID:              "run-123",
		NodeID:             "mysql.replication.verify/mysql",
		PlanDigest:         "sha256:plan",
		TargetID:           "host/mysql-01",
		ExpectedAgentID:    "agent-mysql-01",
		Requester:          requester,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result := client.Run(context.Background(), "printf token=top-secret")
	if result.Status != "succeeded" {
		t.Fatalf("Status = %q, want succeeded", result.Status)
	}
	if strings.Contains(result.Stdout, "top-secret") || strings.Contains(result.Stderr, "top-secret") {
		t.Fatalf("receipt was not redacted: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
	if result.TargetDigest != "worker-digest" {
		t.Fatalf("TargetDigest = %q, want worker-digest", result.TargetDigest)
	}
	if result.Metadata["agentId"] != "agent-mysql-01" || result.Metadata["workerDecision"] != "executed" {
		t.Fatalf("metadata = %#v", result.Metadata)
	}

	call := requester.calls[0]
	if call.subject != "torque.lab.assign.agent.mysql" {
		t.Fatalf("request subject = %q, want torque.lab.assign.agent.mysql", call.subject)
	}
	for _, want := range []string{"nats.request", "--server", "[REDACTED]", "--creds", "[REDACTED]"} {
		joined := strings.Join(result.Command, "\x00")
		if !strings.Contains(joined, want) {
			t.Fatalf("command evidence missing %q: %#v", want, result.Command)
		}
	}
	if strings.Contains(strings.Join(result.Command, " "), "top-secret") || strings.Contains(strings.Join(result.Command, " "), "/tmp/nats.creds") {
		t.Fatalf("command evidence was not redacted: %#v", result.Command)
	}
	var assignment CommandAssignment
	if err := json.Unmarshal(call.payload, &assignment); err != nil {
		t.Fatalf("assignment payload is not JSON: %v", err)
	}
	if assignment.Kind != AssignmentKind || assignment.Operation != "run" || assignment.Target != "torque.lab.assign.agent.mysql" {
		t.Fatalf("assignment = %#v", assignment)
	}
	if assignment.TargetID != "host/mysql-01" || assignment.ExpectedAgentID != "agent-mysql-01" || assignment.RequiredCapability != "mysql.replication.verify" || assignment.NodeKind != "mysql.replication.verify" || assignment.RunID != "run-123" || assignment.NodeID != "mysql.replication.verify/mysql" || assignment.PlanDigest != "sha256:plan" {
		t.Fatalf("assignment metadata = %#v", assignment)
	}
	if assignment.AssignmentID == "" || assignment.AssignmentID != DeriveAssignmentID(assignment) {
		t.Fatalf("assignmentId = %q, want derived stable ID", assignment.AssignmentID)
	}
	assignment.SentAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if got := DeriveAssignmentID(assignment); got != assignment.AssignmentID {
		t.Fatalf("DeriveAssignmentID changed with sentAt: got %q want %q", got, assignment.AssignmentID)
	}
}

func TestRunRecordsTimeout(t *testing.T) {
	client, err := New(Config{
		Target:    "torque.lab.assign.agent.slow",
		Timeout:   10 * time.Millisecond,
		Requester: blockingRequester{},
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
}

func TestRunRecordsRequestErrors(t *testing.T) {
	client, err := New(Config{
		Target:    "torque.lab.assign.agent.missing",
		Requester: errRequester{err: errors.New("no responders token=top-secret")},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result := client.Run(context.Background(), "printf token=top-secret")
	if result.Status != "failed" || result.ExitCode != 1 {
		t.Fatalf("result = %#v, want failed exit 1", result)
	}
	if strings.Contains(result.Error, "top-secret") || strings.Contains(strings.Join(result.Command, " "), "top-secret") {
		t.Fatalf("error evidence was not redacted: %#v", result)
	}
}

func TestNormalizeTargetAndDigest(t *testing.T) {
	if got, want := NormalizeTarget(" nats-mesh://torque.lab.assign "), "torque.lab.assign"; got != want {
		t.Fatalf("NormalizeTarget() = %q, want %q", got, want)
	}
	sum := sha256.Sum256([]byte("torque.lab.assign"))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got := TargetDigest("nats-mesh://torque.lab.assign"); got != want {
		t.Fatalf("TargetDigest() = %q, want %q", got, want)
	}
}

func TestSignedCommandAssignmentEnvelopeVerifies(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	assignment := NewCommandAssignmentWithMetadata("run", "torque.assign.lab.host_1", "printf ok", time.Unix(100, 0), CommandAssignmentMetadata{
		TargetID:           "host/1",
		ExpectedAgentID:    "agent-1",
		RequiredCapability: "host.command.run",
		NodeKind:           "host.command.run",
		RunID:              "run-1",
		NodeID:             "host.command.run/test",
		PlanDigest:         "sha256:plan",
	})
	envelope, err := SignCommandAssignmentEnvelope(assignment, CommandAssignmentEnvelopeOptions{
		PrivateKey:   priv,
		Issuer:       "torque-stack",
		Tenant:       "lab",
		PolicyDigest: "sha256:policy",
		IssuedAt:     time.Unix(100, 0),
		ExpiresAt:    time.Unix(200, 0),
	})
	if err != nil {
		t.Fatalf("sign envelope: %v", err)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	got, verification, err := VerifyCommandAssignmentMessage(raw, CommandAssignmentVerifyOptions{
		RequireSignature:     true,
		TrustedPublicKey:     pub,
		ExpectedTenant:       "lab",
		ExpectedPolicyDigest: "sha256:policy",
		ExpectedTarget:       "torque.assign.lab.host_1",
		ExpectedTargetID:     "host/1",
		Now:                  time.Unix(150, 0),
	})
	if err != nil {
		t.Fatalf("verify envelope: %v", err)
	}
	if got.AssignmentID != assignment.AssignmentID || !verification.EnvelopePresent || !verification.Verified {
		t.Fatalf("assignment=%#v verification=%#v", got, verification)
	}
	metadata := CommandAssignmentVerificationMetadata(verification)
	if metadata["signatureVerified"] != "true" || metadata["assignmentIssuer"] != "torque-stack" || metadata["policyDigest"] != "sha256:policy" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestSignedCommandAssignmentEnvelopeRejectsExpiredAndWrongPolicy(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	assignment := NewCommandAssignmentWithMetadata("run", "torque.assign.lab.host_1", "printf ok", time.Unix(100, 0), CommandAssignmentMetadata{TargetID: "host/1"})
	envelope, err := SignCommandAssignmentEnvelope(assignment, CommandAssignmentEnvelopeOptions{
		PrivateKey:   priv,
		Issuer:       "torque-stack",
		Tenant:       "lab",
		PolicyDigest: "sha256:policy-a",
		IssuedAt:     time.Unix(100, 0),
		ExpiresAt:    time.Unix(120, 0),
	})
	if err != nil {
		t.Fatalf("sign envelope: %v", err)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if _, _, err := VerifyCommandAssignmentMessage(raw, CommandAssignmentVerifyOptions{RequireSignature: true, TrustedPublicKey: pub, ExpectedTenant: "lab", Now: time.Unix(121, 0)}); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired envelope error, got %v", err)
	}
	if _, _, err := VerifyCommandAssignmentMessage(raw, CommandAssignmentVerifyOptions{RequireSignature: true, TrustedPublicKey: pub, ExpectedTenant: "lab", ExpectedPolicyDigest: "sha256:policy-b", Now: time.Unix(110, 0)}); err == nil || !strings.Contains(err.Error(), "policyDigest") {
		t.Fatalf("expected policy digest error, got %v", err)
	}
}

func TestVerifyCommandAssignmentMessageRequiresEnvelope(t *testing.T) {
	raw, err := json.Marshal(NewCommandAssignment("run", "torque.assign.lab.host_1", "printf ok", time.Now()))
	if err != nil {
		t.Fatalf("marshal assignment: %v", err)
	}
	assignment, verification, err := VerifyCommandAssignmentMessage(raw, CommandAssignmentVerifyOptions{RequireSignature: true})
	if err == nil || !strings.Contains(err.Error(), "signed assignment envelope is required") {
		t.Fatalf("expected unsigned error, got assignment=%#v verification=%#v err=%v", assignment, verification, err)
	}
}

type recordingRequester struct {
	calls     []recordedRequest
	responses [][]byte
	errs      []error
}

type recordedRequest struct {
	subject string
	payload []byte
}

func (r *recordingRequester) Request(ctx context.Context, subject string, payload []byte) ([]byte, error) {
	r.calls = append(r.calls, recordedRequest{subject: subject, payload: append([]byte(nil), payload...)})
	var err error
	if len(r.errs) > 0 {
		err = r.errs[0]
		r.errs = r.errs[1:]
	}
	if len(r.responses) == 0 {
		return nil, err
	}
	next := r.responses[0]
	r.responses = r.responses[1:]
	return next, err
}

func (r *recordingRequester) Close() {}

type blockingRequester struct{}

func (blockingRequester) Request(ctx context.Context, subject string, payload []byte) ([]byte, error) {
	<-ctx.Done()
	return nil, fmt.Errorf("timed out token=top-secret: %w", ctx.Err())
}

func (blockingRequester) Close() {}

type errRequester struct {
	err error
}

func (r errRequester) Request(ctx context.Context, subject string, payload []byte) ([]byte, error) {
	return nil, r.err
}

func (r errRequester) Close() {}

func jsonFields(t reflect.Type) []string {
	fields := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		fields = append(fields, strings.Split(tag, ",")[0])
	}
	return fields
}
