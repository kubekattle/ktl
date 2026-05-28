package mysql

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

type recordingRunner struct {
	output transport.RunOutput
	err    error
	name   string
	args   []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args []string) (transport.RunOutput, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	return r.output, r.err
}

func TestBuildVerifyCommandIncludesExpectedInputs(t *testing.T) {
	interval := 1500 * time.Millisecond
	requireSynced := false
	command := BuildVerifyCommand(Spec{
		NodeIdentityFile:        "/tmp/lab.key",
		NodeSSHOptions:          "-J jump-host",
		Database:                "torque_ops",
		ProbeTable:              "replication_probe",
		ProbeID:                 "probe-1",
		ProbePayload:            "payload-1",
		StatusPath:              "/tmp/mysql-status.txt",
		ExpectedClusterSize:     2,
		ExpectedReplicatedNodes: 2,
		InsertProbe:             true,
		RequireSynced:           &requireSynced,
		StableAttempts:          3,
		StableInterval:          &interval,
		Nodes: []NodeSpec{
			{ID: "mysql-00", Address: "10.0.0.10"},
			{ID: "mysql-01", Address: "10.0.0.11", SSHUser: "ops", SSHPort: 2200},
		},
	})

	for _, want := range []string{
		"NODE_IDENTITY_FILE=/tmp/lab.key",
		"NODE_SSH_OPTIONS='-J jump-host'",
		"DATABASE_NAME=torque_ops",
		"PROBE_TABLE=replication_probe",
		"PROBE_ID=probe-1",
		"PROBE_PAYLOAD=payload-1",
		"STATUS_PATH=/tmp/mysql-status.txt",
		"EXPECTED_CLUSTER_SIZE=2",
		"EXPECTED_REPLICATED_NODES=2",
		"STABLE_ATTEMPTS=3",
		"STABLE_INTERVAL_SECONDS=1.500",
		"INSERT_PROBE=1",
		"REQUIRE_SYNCED=0",
		"NODES+=('mysql-00|10.0.0.10|root|0')",
		"NODES+=('mysql-01|10.0.0.11|ops|2200')",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("command missing %q:\n%s", want, command)
		}
	}
}

func TestExecuteBuildsSucceededResultFromShellStatus(t *testing.T) {
	runner := &recordingRunner{
		output: transport.RunOutput{
			Stdout: []byte("attempt=1 node=mysql-00 ip=10.0.0.10 count=1 cluster=2 state=Synced\nattempt=1 node=mysql-01 ip=10.0.0.11 count=1 cluster=2 state=Synced\nmysql-replication-verified replicated=2/2 cluster=2\n"),
		},
	}
	var stdout bytes.Buffer
	req := ResourceRequest{
		NodeID:   "mysql.replication.verify/mysql-verify",
		RunID:    "run-mysql",
		NodeKind: "mysql.replication.verify",
		Spec: json.RawMessage(`{
			"database":"torque_ops",
			"probeTable":"replication_probe",
			"probeId":"probe-1",
			"statusPath":"/tmp/mysql-status.txt",
			"expectedClusterSize":2,
			"expectedReplicatedNodes":2,
			"stableAttempts":1,
			"stableInterval":1000000,
			"nodes":[
				{"id":"mysql-00","address":"10.0.0.10"},
				{"id":"mysql-01","address":"10.0.0.11"}
			]
		}`),
	}

	result, err := Runner{CommandRunner: runner, Stdout: &stdout}.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if runner.name != "bash" || len(runner.args) != 2 || runner.args[0] != "-c" {
		t.Fatalf("runner call = %q %#v", runner.name, runner.args)
	}
	if result.Status != "succeeded" || result.Message != "replication verified" {
		t.Fatalf("result status/message = %#v", result)
	}
	if result.ReplicatedNodes != 2 || result.Attempt != 1 {
		t.Fatalf("result counts = %#v", result)
	}
	if result.RequireSynced != true || result.ProbeTable != "torque_ops.replication_probe" {
		t.Fatalf("result sync/table = %#v", result)
	}
	if len(result.Nodes) != 2 || !result.Nodes[0].Replicated || !result.Nodes[1].Replicated {
		t.Fatalf("result nodes = %#v", result.Nodes)
	}
	if result.StatusPathDigest == "" {
		t.Fatalf("missing status path digest: %#v", result)
	}
	if !strings.Contains(stdout.String(), "mysql-replication-verified") {
		t.Fatalf("stdout mirror = %q", stdout.String())
	}
}

func TestExecuteBuildsFailedResultFromShellError(t *testing.T) {
	runner := &recordingRunner{
		output: transport.RunOutput{
			Stdout: []byte("attempt=2 node=mysql-00 ip=10.0.0.10 count=1 cluster=1 state=Donor\n"),
			Stderr: []byte("mysql replication verification failed\n"),
		},
		err: errors.New("exit status 1"),
	}
	var stderr bytes.Buffer
	requireSynced := true
	spec := Spec{
		Database:                "torque_ops",
		ProbeTable:              "replication_probe",
		ProbeID:                 "probe-2",
		ExpectedClusterSize:     2,
		ExpectedReplicatedNodes: 1,
		InsertProbe:             true,
		RequireSynced:           &requireSynced,
		Nodes: []NodeSpec{
			{ID: "mysql-00", Address: "10.0.0.10"},
		},
	}
	rawSpec, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}

	result, execErr := Runner{CommandRunner: runner, Stderr: &stderr}.Execute(context.Background(), ResourceRequest{
		NodeID:   "mysql.replication.verify/mysql-verify",
		NodeKind: "mysql.replication.verify",
		Spec:     rawSpec,
	})
	if execErr == nil || !strings.Contains(execErr.Error(), "exit status 1") {
		t.Fatalf("Execute error = %v, want exit status 1", execErr)
	}
	if result.Status != "failed" || result.Message != "mysql replication verify failed: replicated nodes 0 < required 1" {
		t.Fatalf("failed result = %#v", result)
	}
	if !result.Changed {
		t.Fatalf("InsertProbe should mark changed=true: %#v", result)
	}
	if !strings.Contains(stderr.String(), "mysql replication verification failed") {
		t.Fatalf("stderr mirror = %q", stderr.String())
	}
}

func TestExecuteRejectsUnsupportedKind(t *testing.T) {
	result, err := Execute(context.Background(), ResourceRequest{
		NodeKind: "mysql.user.ensure",
		Spec:     json.RawMessage(`{"nodes":[{"id":"mysql-00","address":"10.0.0.10"}]}`),
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported MySQL resource kind "mysql.user.ensure"`) {
		t.Fatalf("Execute error = %v", err)
	}
	if result.Status != "failed" || result.NodeKind != "mysql.user.ensure" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteReturnsVerificationErrorWithoutRunnerFailure(t *testing.T) {
	runner := &recordingRunner{
		output: transport.RunOutput{
			Stdout: []byte("attempt=1 node=mysql-00 ip=10.0.0.10 count=1 cluster=2 state=Synced\n"),
		},
	}
	rawSpec, err := json.Marshal(Spec{
		ExpectedClusterSize:     2,
		ExpectedReplicatedNodes: 1,
		Nodes: []NodeSpec{
			{ID: "mysql-00", Address: "10.0.0.10"},
			{ID: "mysql-01", Address: "10.0.0.11"},
		},
	})
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	result, execErr := Runner{CommandRunner: runner}.Execute(context.Background(), ResourceRequest{
		NodeKind: "mysql.replication.verify",
		Spec:     rawSpec,
	})
	if execErr == nil || !strings.Contains(execErr.Error(), "missing status for node mysql-01") {
		t.Fatalf("Execute error = %v", execErr)
	}
	if result.Status != "failed" || !strings.Contains(result.Message, "missing status for node mysql-01") {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteFromBase64AndWriteResult(t *testing.T) {
	binDir := t.TempDir()
	bashPath := filepath.Join(binDir, "bash")
	if err := os.WriteFile(bashPath, []byte("#!/bin/sh\nprintf 'attempt=1 node=mysql-00 ip=10.0.0.10 count=1 cluster=2 state=Synced\\nattempt=1 node=mysql-01 ip=10.0.0.11 count=1 cluster=2 state=Synced\\n'\n"), 0o755); err != nil {
		t.Fatalf("write fake bash: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	raw := []byte(`{"apiVersion":"` + RequestAPIVersion + `","kind":"` + RequestKind + `","nodeId":"mysql.replication.verify/mysql-verify","nodeKind":"mysql.replication.verify","spec":{"database":"torque_ops","probeTable":"replication_probe","probeId":"probe-1","expectedClusterSize":2,"expectedReplicatedNodes":2,"stableAttempts":1,"stableInterval":1000000,"nodes":[{"id":"mysql-00","address":"10.0.0.10"},{"id":"mysql-01","address":"10.0.0.11"}]}}`)
	encoded := base64.StdEncoding.EncodeToString(raw)
	var buf strings.Builder
	result, err := ExecuteFromBase64(context.Background(), encoded)
	if err != nil {
		t.Fatalf("ExecuteFromBase64: %v", err)
	}
	if writeErr := WriteResult(&buf, result); writeErr != nil {
		t.Fatalf("WriteResult: %v", writeErr)
	}
	parsed := ParseResultStdout(buf.String())
	if parsed == nil || parsed.Status != "succeeded" || parsed.ReplicatedNodes != 2 {
		t.Fatalf("parsed stdout = %#v", parsed)
	}
	if _, err := ExecuteFromBase64(context.Background(), "%%%"); err == nil {
		t.Fatal("ExecuteFromBase64 error = nil, want invalid base64")
	}
}

func TestEvaluateAndParseReplicationStatus(t *testing.T) {
	requireSynced := false
	spec := Spec{
		ExpectedClusterSize:     2,
		ExpectedReplicatedNodes: 1,
		RequireSynced:           &requireSynced,
		Nodes: []NodeSpec{
			{ID: "mysql-00", Address: "10.0.0.10"},
		},
	}
	nodes := ParseReplicationStatus("attempt=3 node=mysql-00 ip=10.0.0.10 count=1 cluster=2 state=Donor\nattempt=2 node=mysql-extra ip=10.0.0.99 count=0 cluster=1 state=Joiner\n", spec)
	if len(nodes) != 2 || nodes[0].ID != "mysql-00" || nodes[1].ID != "mysql-extra" {
		t.Fatalf("nodes = %#v", nodes)
	}
	if err := Evaluate(spec, nodes); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if err := Evaluate(spec, nil); err == nil || !strings.Contains(err.Error(), "no node status") {
		t.Fatalf("Evaluate nil error = %v", err)
	}
}

func TestBranchCoverageHelpers(t *testing.T) {
	if _, err := Execute(context.Background(), ResourceRequest{
		NodeKind: "mysql.replication.verify",
		Spec:     json.RawMessage(`{`),
	}); err == nil || !strings.Contains(err.Error(), "decode MySQL resource spec") {
		t.Fatalf("Execute invalid spec error = %v", err)
	}

	invalidJSON := base64.StdEncoding.EncodeToString([]byte(`{`))
	if _, err := ExecuteFromBase64(context.Background(), invalidJSON); err == nil || !strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Fatalf("ExecuteFromBase64 invalid JSON error = %v", err)
	}

	if got := ParseResultStdout("not-json\n{\"kind\":\"Other\"}\n"); got != nil {
		t.Fatalf("ParseResultStdout = %#v, want nil", got)
	}

	spec := Spec{
		ExpectedClusterSize:     2,
		ExpectedReplicatedNodes: 1,
		Nodes: []NodeSpec{
			{ID: "mysql-00", Address: "10.0.0.10"},
		},
	}
	if nodes := ParseReplicationStatus("cluster=2 state=Synced\nattempt=1 node=mysql-00 ip=10.0.0.10 count=1 cluster=2 state=Synced\n", spec); len(nodes) != 1 || nodes[0].ID != "mysql-00" {
		t.Fatalf("ParseReplicationStatus ignored-node handling = %#v", nodes)
	}
	if nodes := ParseReplicationStatus("attempt=1 node= ip=10.0.0.10 count=1 cluster=2 state=Synced\n", spec); len(nodes) != 0 {
		t.Fatalf("ParseReplicationStatus empty-node handling = %#v", nodes)
	}
	if err := Evaluate(spec, []NodeResult{{ID: "mysql-00", ClusterSize: 2, State: "Donor", Replicated: true}}); err == nil || !strings.Contains(err.Error(), `state "Donor" != Synced`) {
		t.Fatalf("Evaluate state error = %v", err)
	}
	if err := Evaluate(spec, []NodeResult{{ID: "mysql-00", ClusterSize: 1, State: "Synced", Replicated: true}}); err == nil || !strings.Contains(err.Error(), "cluster size 1 != expected 2") {
		t.Fatalf("Evaluate cluster-size error = %v", err)
	}
	if err := Evaluate(spec, []NodeResult{{ID: "mysql-01", ClusterSize: 2, State: "Synced", Replicated: true}}); err == nil || !strings.Contains(err.Error(), "missing status for node mysql-00") {
		t.Fatalf("Evaluate missing-node error = %v", err)
	}
	requireSynced := true
	addressSpec := Spec{
		ExpectedClusterSize:     1,
		ExpectedReplicatedNodes: 1,
		RequireSynced:           &requireSynced,
		Nodes: []NodeSpec{
			{Address: "10.0.0.10"},
		},
	}
	addressNodes := ParseReplicationStatus("attempt=1 node=10.0.0.10 ip=10.0.0.10 count=1 cluster=1 state=Synced\n", addressSpec)
	if len(addressNodes) != 1 || addressNodes[0].ID != "10.0.0.10" {
		t.Fatalf("ParseReplicationStatus address fallback = %#v", addressNodes)
	}
	if err := Evaluate(addressSpec, addressNodes); err != nil {
		t.Fatalf("Evaluate address fallback: %v", err)
	}

	defaultSpec, err := decodeSpec(nil)
	if err != nil {
		t.Fatalf("decodeSpec nil: %v", err)
	}
	if defaultSpec.StableAttempts != defaultStableAttempts || defaultSpec.StableInterval == nil || *defaultSpec.StableInterval != defaultStableInterval {
		t.Fatalf("decodeSpec defaults = %#v", defaultSpec)
	}

	if got := durationString(nil); got != "" {
		t.Fatalf("durationString(nil) = %q", got)
	}
	if got := qualifyProbeTable(Spec{ProbeTable: "probe"}); got != "probe" {
		t.Fatalf("qualifyProbeTable(table-only) = %q", got)
	}
	if got := qualifyProbeTable(Spec{Database: "torque"}); got != "torque" {
		t.Fatalf("qualifyProbeTable(database-only) = %q", got)
	}
	if got := optionalDigest(""); got != "" {
		t.Fatalf("optionalDigest(empty) = %q", got)
	}
	if got := parseShellKeyValueLine("node=mysql-00 ignored-field cluster=2"); got["node"] != "mysql-00" || got["cluster"] != "2" {
		t.Fatalf("parseShellKeyValueLine = %#v", got)
	}
	if got := firstNonEmptyString(" ", "\n"); got != "" {
		t.Fatalf("firstNonEmptyString(empty) = %q", got)
	}
}
