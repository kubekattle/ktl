package natstransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	}
	raw, err := json.Marshal(workerReceipt)
	if err != nil {
		t.Fatalf("marshal worker receipt: %v", err)
	}
	runner := &recordingRunner{
		outputs: []fakeOutput{
			{
				output: RunOutput{
					Stdout:   raw,
					ExitCode: 0,
				},
			},
		},
	}
	client, err := New(Config{
		Target:       "nats-mesh://torque.lab.assign.agent.mysql",
		Server:       "nats://127.0.0.1:4222",
		Creds:        "/tmp/nats.creds",
		NATSBinary:   "nats",
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
	if strings.Contains(result.Stdout, "top-secret") || strings.Contains(result.Stderr, "top-secret") {
		t.Fatalf("receipt was not redacted: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
	if result.TargetDigest != "worker-digest" {
		t.Fatalf("TargetDigest = %q, want worker-digest", result.TargetDigest)
	}

	call := runner.calls[0]
	if call.name != "nats" {
		t.Fatalf("runner name = %q, want nats", call.name)
	}
	joined := strings.Join(call.args, "\x00")
	for _, want := range []string{"request", "--raw", "--server", "nats://127.0.0.1:4222", "--creds", "/tmp/nats.creds", "torque.lab.assign.agent.mysql"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("runner args missing %q: %#v", want, call.args)
		}
	}
	if strings.Contains(strings.Join(result.Command, " "), "top-secret") || strings.Contains(strings.Join(result.Command, " "), "/tmp/nats.creds") {
		t.Fatalf("command evidence was not redacted: %#v", result.Command)
	}
	var assignment commandAssignment
	if err := json.Unmarshal([]byte(call.args[len(call.args)-1]), &assignment); err != nil {
		t.Fatalf("assignment payload is not JSON: %v", err)
	}
	if assignment.Kind != "CommandAssignment" || assignment.Operation != "run" || assignment.Target != "torque.lab.assign.agent.mysql" {
		t.Fatalf("assignment = %#v", assignment)
	}
}

func TestRunRecordsTimeout(t *testing.T) {
	client, err := New(Config{
		Target:  "torque.lab.assign.agent.slow",
		Timeout: 10 * time.Millisecond,
		Runner:  blockingRunner{},
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

type recordingRunner struct {
	calls   []recordedCall
	outputs []fakeOutput
}

type recordedCall struct {
	name string
	args []string
}

type fakeOutput struct {
	output RunOutput
	err    error
}

func (r *recordingRunner) Run(ctx context.Context, name string, args []string) (RunOutput, error) {
	r.calls = append(r.calls, recordedCall{name: name, args: append([]string(nil), args...)})
	if len(r.outputs) == 0 {
		return RunOutput{ExitCode: 0}, nil
	}
	next := r.outputs[0]
	r.outputs = r.outputs[1:]
	return next.output, next.err
}

type blockingRunner struct{}

func (blockingRunner) Run(ctx context.Context, name string, args []string) (RunOutput, error) {
	<-ctx.Done()
	return RunOutput{Stderr: []byte("token=top-secret\n"), ExitCode: -1}, errors.New("timed out token=top-secret")
}

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
