package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
	natsworker "github.com/ingresslabs/torque/internal/ops/transport/nats/worker"
	"github.com/ingresslabs/torque/internal/stack"
	natsgo "github.com/nats-io/nats.go"
)

func TestOpsExecAutoFallsBackToLocalTransport(t *testing.T) {
	targetsPath := writeOpsExecLocalFixture(t)
	outDir := filepath.Join(t.TempDir(), "ops-exec-out")
	stdout, stderr, err := runRootForOpsInventory(
		t,
		"ops", "exec",
		"--targets", targetsPath,
		"--selector", "role=local",
		"--command", "printf ready",
		"--format", "json",
		"--out-dir", outDir,
	)
	if err != nil {
		t.Fatalf("ops exec failed: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	var result opsExecResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode ops exec output: %v\n%s", err, stdout)
	}
	if result.Status != "succeeded" || result.Summary.Selected != 2 || result.Summary.Succeeded != 2 || result.Summary.NATSTargets != 0 {
		t.Fatalf("unexpected result summary: %#v", result)
	}
	if len(result.Results) != 2 {
		t.Fatalf("result count = %d, want 2", len(result.Results))
	}
	for _, target := range result.Results {
		if target.Transport != "local" || target.Status != "succeeded" || strings.TrimSpace(target.Stdout) != "ready" {
			t.Fatalf("unexpected target result: %#v", target)
		}
		if target.BundlePath == "" {
			t.Fatalf("expected bundle path for target %#v", target)
		}
		if _, err := os.Stat(target.BundlePath); err != nil {
			t.Fatalf("stat bundle %s: %v", target.BundlePath, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "result.json"))
	if err != nil {
		t.Fatalf("read result.json: %v", err)
	}
	if !strings.Contains(string(raw), `"kind": "OpsExecResult"`) {
		t.Fatalf("result.json missing result payload: %s", raw)
	}
}

func TestOpsExecAutoDurableUsesFilteredNATSFleet(t *testing.T) {
	root := t.TempDir()
	targetsPath := writeOpsExecFleetFixture(t)
	registryPath := filepath.Join(root, "agent-registry.json")
	serverURL := startOpsExecTestNATSJetStreamServer(t)
	markerPath := filepath.Join(root, "fleet-marker.txt")

	writeOpsExecAgentRecord(t, registryPath, "agent-mysql-01", "host/mysql-01", map[string]string{"role": "mysql", "site": "a"}, heartbeat.Slots{Total: 2}, stack.NodeKindHostCommandRun)
	writeOpsExecAgentRecord(t, registryPath, "agent-mysql-02", "host/mysql-02", map[string]string{"role": "mysql", "site": "b"}, heartbeat.Slots{Total: 2}, stack.NodeKindHostCommandRun)

	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	errCh := make(chan error, 2)
	startOpsExecWorker(t, workerCtx, errCh, serverURL, "lab", "agent-mysql-01", "host/mysql-01")
	startOpsExecWorker(t, workerCtx, errCh, serverURL, "lab", "agent-mysql-02", "host/mysql-02")

	stdout, stderr, err := runRootForOpsInventory(
		t,
		"ops", "exec",
		"--targets", targetsPath,
		"--selector", "site=a",
		"--command", "printf 'fleet-hit\n' >> "+markerPath,
		"--transport", "auto",
		"--durable",
		"--tenant", "lab",
		"--store", "file",
		"--store-path", registryPath,
		"--nats-url", serverURL,
		"--timeout", "5s",
		"--ack-wait", "5s",
		"--format", "json",
	)
	if err != nil {
		t.Fatalf("ops exec durable failed: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	var result opsExecResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode durable output: %v\n%s", err, stdout)
	}
	if result.Status != "succeeded" || result.Summary.Selected != 1 || result.Summary.NATSTargets != 1 || result.Summary.Succeeded != 1 {
		t.Fatalf("unexpected durable summary: %#v", result)
	}
	if len(result.Results) != 1 {
		t.Fatalf("durable result count = %d, want 1", len(result.Results))
	}
	target := result.Results[0]
	if target.TargetID != "host/mysql-01" || target.Transport != "nats" || target.Delivery != stack.RunnerFanoutDeliveryJetStream || target.Status != "succeeded" {
		t.Fatalf("unexpected durable target result: %#v", target)
	}
	raw, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if hits := strings.Count(string(raw), "fleet-hit"); hits != 1 {
		t.Fatalf("marker hits = %d, want 1: %q", hits, string(raw))
	}

	cancelWorkers()
	for i := 0; i < 2; i++ {
		select {
		case workerErr := <-errCh:
			if workerErr != nil && !errors.Is(workerErr, context.Canceled) {
				t.Fatalf("worker error: %v", workerErr)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not stop")
		}
	}
}

func writeOpsExecLocalFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targetgraph.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: local-ops-exec
targets:
  - id: host/local-01
    type: host
    transportRef: local/default
    labels:
      role: local
  - id: host/local-02
    type: host
    transportRef: local/default
    labels:
      role: local
transports:
  - id: local/default
    kind: local
`), 0o600); err != nil {
		t.Fatalf("write local fixture: %v", err)
	}
	return path
}

func writeOpsExecFleetFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targetgraph.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: fleet-ops-exec
targets:
  - id: host/mysql-01
    type: host
    transportRef: ssh/bootstrap
    durableTransportRef: nats/mysql-01
    labels:
      role: mysql
      site: a
  - id: host/mysql-02
    type: host
    transportRef: ssh/bootstrap
    durableTransportRef: nats/mysql-02
    labels:
      role: mysql
      site: b
transports:
  - id: ssh/bootstrap
    kind: ssh
    host: 127.0.0.1
    user: root
  - id: nats/mysql-01
    kind: nats
    url: nats://127.0.0.1:4222
  - id: nats/mysql-02
    kind: nats
    url: nats://127.0.0.1:4222
`), 0o600); err != nil {
		t.Fatalf("write fleet fixture: %v", err)
	}
	return path
}

func writeOpsExecAgentRecord(t *testing.T, registryPath string, agentID string, targetID string, labels map[string]string, workerSlots heartbeat.Slots, capabilities ...string) {
	t.Helper()
	store, err := heartbeat.NewFileStore(registryPath)
	if err != nil {
		t.Fatalf("new registry store: %v", err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now().UTC()
	hb := heartbeat.New(heartbeat.Options{
		AgentID:      agentID,
		Tenant:       "lab",
		TargetID:     targetID,
		Hostname:     agentID + ".test",
		Labels:       labels,
		Capabilities: capabilities,
		WorkerSlots:  workerSlots,
		State:        heartbeat.StateReady,
		ObservedAt:   now,
	})
	record, err := heartbeat.NewCompactRecord(hb, heartbeat.StreamOffset{Stream: heartbeat.DefaultEventStream, Sequence: 1}, now, 45*time.Second)
	if err != nil {
		t.Fatalf("new compact record: %v", err)
	}
	if err := store.Put(context.Background(), record); err != nil {
		t.Fatalf("put compact record: %v", err)
	}
}

func startOpsExecWorker(t *testing.T, ctx context.Context, errCh chan<- error, serverURL string, tenant string, agentID string, targetID string) {
	t.Helper()
	ready := make(chan struct{})
	worker, err := natsworker.New(natsworker.Config{
		Server:                     serverURL,
		Subject:                    natstransport.AssignmentSubject(tenant, targetID),
		Delivery:                   natstransport.DeliveryJetStream,
		Ready:                      ready,
		Timeout:                    2 * time.Second,
		Capabilities:               []string{stack.NodeKindHostCommandRun},
		DisableCapabilityDiscovery: true,
		AgentID:                    agentID,
		Tenant:                     tenant,
		TargetID:                   targetID,
		Hostname:                   agentID + ".test",
	})
	if err != nil {
		t.Fatalf("new nats worker %s: %v", agentID, err)
	}
	go func() {
		errCh <- worker.Run(ctx)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatalf("worker %s did not become ready", agentID)
	}
}

func startOpsExecTestNATSJetStreamServer(t *testing.T) string {
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
	args := []string{"-a", "127.0.0.1", "-p", strconv.Itoa(port), "-js", "-sd", t.TempDir()}
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
