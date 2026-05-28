package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
	"github.com/ingresslabs/torque/internal/ops/targetgraph"
	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	sshtransport "github.com/ingresslabs/torque/internal/ops/transport/ssh"
)

func TestOpsAgentBootstrapInstallsDurableServices(t *testing.T) {
	oldClientFactory := newOpsAgentBootstrapSSHClient
	oldBinaryBuilder := buildOpsAgentBootstrapBinary
	defer func() {
		newOpsAgentBootstrapSSHClient = oldClientFactory
		buildOpsAgentBootstrapBinary = oldBinaryBuilder
	}()

	client := &fakeOpsAgentSSHClient{}
	newOpsAgentBootstrapSSHClient = func(config sshtransport.Config) (opsAgentSSHClient, error) {
		client.target = config.Target
		client.identity = config.IdentityFile
		return client, nil
	}
	buildOpsAgentBootstrapBinary = func(ctx context.Context, opts opsAgentBootstrapOptions, platform opsAgentRemotePlatform) (opsAgentBinaryRef, error) {
		path := filepath.Join(t.TempDir(), "torque-agent")
		if err := os.WriteFile(path, []byte("binary"), 0o755); err != nil {
			return opsAgentBinaryRef{}, err
		}
		return opsAgentBinaryRef{Path: path}, nil
	}

	targetsPath := filepath.Join(t.TempDir(), "targetgraph.yaml")
	if err := os.WriteFile(targetsPath, []byte(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: bootstrap
targets:
  - id: host/mysql-01
    type: host
    transportRef: ssh/mysql-01
    labels:
      role: mysql
      site: lab
transports:
  - id: ssh/mysql-01
    kind: ssh
    host: 141.105.65.227
    user: root
`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	stdout, stderr, err := runRootForOpsInventory(
		t,
		"ops", "agent", "bootstrap",
		"--targets", targetsPath,
		"--target-id", "host/mysql-01",
		"--nats-url", "nats://127.0.0.1:4222",
		"--format", "json",
	)
	if err != nil {
		t.Fatalf("bootstrap failed: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	var result opsAgentBootstrapResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode bootstrap output: %v\n%s", err, stdout)
	}
	if result.Status != "succeeded" || result.TargetID != "host/mysql-01" || result.AssignmentSubject == "" {
		t.Fatalf("unexpected bootstrap result: %#v", result)
	}
	if client.target != "ssh://root@141.105.65.227" {
		t.Fatalf("ssh target = %q, want ssh://root@141.105.65.227", client.target)
	}
	if len(client.uploads) < 4 {
		t.Fatalf("upload count = %d, want >= 4", len(client.uploads))
	}
	if !containsExactString(client.runs, "systemctl enable --now torque-agent-heartbeat.service torque-agent-worker.service") {
		t.Fatalf("missing systemctl enable command: %#v", client.runs)
	}
}

func TestOpsAgentEnrollApprovePromotesTargetGraphAndStore(t *testing.T) {
	targetsPath := filepath.Join(t.TempDir(), "targetgraph.yaml")
	if err := os.WriteFile(targetsPath, []byte(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: enroll
targets:
  - id: host/mysql-01
    type: host
    transportRef: ssh/mysql-01
transports:
  - id: ssh/mysql-01
    kind: ssh
    host: 141.105.65.227
    user: root
`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	store, err := heartbeat.NewFileStore(registryPath)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	now := time.Now().UTC()
	record, err := heartbeat.NewCompactRecord(heartbeat.New(heartbeat.Options{
		AgentID:    "agent/mysql-01",
		Tenant:     "lab",
		TargetID:   "host/mysql-01",
		State:      heartbeat.StateReady,
		ObservedAt: now,
	}), heartbeat.StreamOffset{Sequence: 1}, now, 45*time.Second)
	if err != nil {
		t.Fatalf("NewCompactRecord: %v", err)
	}
	if err := store.Put(context.Background(), record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	_ = store.Close()

	stdout, stderr, err := runRootForOpsInventory(
		t,
		"ops", "agent", "enroll", "approve", "agent/mysql-01",
		"--targets", targetsPath,
		"--target", "host/mysql-01",
		"--tenant", "lab",
		"--nats-url", "nats://127.0.0.1:4222",
		"--update-store",
		"--store", "file",
		"--store-path", registryPath,
		"--format", "json",
	)
	if err != nil {
		t.Fatalf("enroll approve failed: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	var result opsAgentEnrollApproveResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode enroll output: %v\n%s", err, stdout)
	}
	if result.Status != "succeeded" || result.DurableTransportRef != "nats/host-mysql-01" {
		t.Fatalf("unexpected enroll result: %#v", result)
	}
	graph, err := targetgraph.LoadFile(targetsPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	target, ok := opsTargetByID(graph, "host/mysql-01")
	if !ok {
		t.Fatalf("target not found after approval")
	}
	if target.DurableTransportRef != "nats/host-mysql-01" {
		t.Fatalf("DurableTransportRef = %q", target.DurableTransportRef)
	}
	transportCfg, ok := opsTransportByID(graph, "nats/host-mysql-01")
	if !ok || transportCfg.Kind != "nats" {
		t.Fatalf("durable transport missing after approval: %#v", graph.Transports)
	}
	store, err = heartbeat.NewFileStore(registryPath)
	if err != nil {
		t.Fatalf("NewFileStore reload: %v", err)
	}
	snapshot, err := heartbeat.SnapshotFromStore(context.Background(), store, heartbeat.SnapshotRequest{
		Tenant:     "lab",
		Now:        time.Now(),
		StaleAfter: 45 * time.Second,
	})
	if err != nil {
		t.Fatalf("SnapshotFromStore: %v", err)
	}
	if len(snapshot.Agents) != 1 || snapshot.Agents[0].Enrollment.State != heartbeat.EnrollmentStateApproved {
		t.Fatalf("unexpected snapshot: %#v", snapshot.Agents)
	}
}

type fakeOpsAgentSSHClient struct {
	target   string
	identity string
	runs     []string
	uploads  [][2]string
}

func (f *fakeOpsAgentSSHClient) Connect(ctx context.Context) transport.OperationResult {
	return transport.OperationResult{Status: "succeeded"}
}

func (f *fakeOpsAgentSSHClient) Run(ctx context.Context, command string) transport.OperationResult {
	f.runs = append(f.runs, command)
	if strings.Contains(command, "uname -s && uname -m") {
		return transport.OperationResult{Status: "succeeded", Stdout: "Linux\nx86_64\n"}
	}
	return transport.OperationResult{Status: "succeeded"}
}

func (f *fakeOpsAgentSSHClient) Upload(ctx context.Context, localPath string, remotePath string) transport.OperationResult {
	f.uploads = append(f.uploads, [2]string{localPath, remotePath})
	return transport.OperationResult{Status: "succeeded"}
}

func containsExactString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
