package stack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
)

func TestRun_FleetReadinessAllowsNATSMutation(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, ".torque", "agent-registry.json")
	writeFleetReadinessStackFixture(t, root, fleetReadinessStackOptions{
		RegistryPath: registryPath,
		Mode:         RunnerModeFleet,
		Transport:    "nats",
		Target:       "torque.lab.assign.mysql",
	})
	writeFleetReadinessAgent(t, registryPath, "mysql-01", heartbeat.StateReady, time.Now().UTC(), NodeKindHostCommandRun)
	writeFleetReadinessAgent(t, registryPath, "mysql-02", heartbeat.StateReady, time.Now().UTC(), NodeKindHostCommandRun)

	p := compileFleetReadinessStack(t, root)
	exec := &recordingExecutor{}
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Executor:    exec,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, errOut.String())
	}
	if calls := exec.calledNames(); len(calls) != 1 || calls[0] != "verify-mysql" {
		t.Fatalf("executor calls = %v", calls)
	}
	audit := fleetReadinessAudit(t, root)
	readiness := fleetReadinessArtifact(t, audit.Artifacts)
	if readiness.Status != "ready" || readiness.Summary.ReadyPercent != 100 || readiness.Summary.TotalAgents != 2 || readiness.Summary.CoveredCapabilities != 1 {
		t.Fatalf("readiness artifact = %#v", readiness)
	}
	if !auditHasEvent(audit.Events, string(FleetReadiness), "ready") {
		t.Fatalf("missing ready fleet readiness event: %#v", audit.Events)
	}
}

func TestRun_FleetReadinessBlocksBeforeMutation(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, ".torque", "agent-registry.json")
	writeFleetReadinessStackFixture(t, root, fleetReadinessStackOptions{
		RegistryPath: registryPath,
		Mode:         RunnerModeFleet,
		Transport:    "nats",
		Target:       "torque.lab.assign.mysql",
	})

	p := compileFleetReadinessStack(t, root)
	exec := &recordingExecutor{}
	var out, errOut bytes.Buffer
	err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Executor:    exec,
	}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "fleet readiness blocked") {
		t.Fatalf("Run error = %v, want fleet readiness blocked", err)
	}
	if calls := exec.calledNames(); len(calls) != 0 {
		t.Fatalf("executor was called despite blocked readiness: %v", calls)
	}
	audit := fleetReadinessAudit(t, root)
	if audit.Status != "blocked" || audit.Summary == nil || audit.Summary.Totals.Blocked != len(p.Nodes) {
		t.Fatalf("audit status/summary = %s %#v", audit.Status, audit.Summary)
	}
	readiness := fleetReadinessArtifact(t, audit.Artifacts)
	if readiness.Status != "blocked" || readiness.Summary.TotalAgents != 0 || len(readiness.Blockers) == 0 {
		t.Fatalf("readiness artifact = %#v", readiness)
	}
	if !auditHasEvent(audit.Events, string(FleetReadiness), "blocked") {
		t.Fatalf("missing blocked fleet readiness event: %#v", audit.Events)
	}
}

func TestRun_FleetReadinessRequiresNATSTransport(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, ".torque", "agent-registry.json")
	writeFleetReadinessStackFixture(t, root, fleetReadinessStackOptions{
		RegistryPath: registryPath,
		Mode:         RunnerModeFleet,
		Transport:    "ssh",
		Target:       "root@example.invalid",
	})
	writeFleetReadinessAgent(t, registryPath, "mysql-01", heartbeat.StateReady, time.Now().UTC(), NodeKindHostCommandRun)

	p := compileFleetReadinessStack(t, root)
	exec := &recordingExecutor{}
	var out, errOut bytes.Buffer
	err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Executor:    exec,
	}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "fleet readiness blocked") {
		t.Fatalf("Run error = %v, want fleet readiness blocked", err)
	}
	if calls := exec.calledNames(); len(calls) != 0 {
		t.Fatalf("executor was called despite transport violation: %v", calls)
	}
	readiness := fleetReadinessArtifact(t, fleetReadinessAudit(t, root).Artifacts)
	if readiness.Status != "blocked" || readiness.Summary.TransportViolations != 1 {
		t.Fatalf("readiness artifact = %#v", readiness)
	}
	if !fleetReadinessHasBlocker(readiness, "fleet.transport.not_nats") {
		t.Fatalf("missing transport blocker: %#v", readiness.Blockers)
	}
}

func TestRun_FleetReadinessRequiresCapability(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, ".torque", "agent-registry.json")
	writeFleetReadinessStackFixture(t, root, fleetReadinessStackOptions{
		RegistryPath: registryPath,
		Mode:         RunnerModeFleet,
		Transport:    "nats",
		Target:       "torque.lab.assign.mysql",
	})
	writeFleetReadinessAgent(t, registryPath, "mysql-01", heartbeat.StateReady, time.Now().UTC(), NodeKindMySQLReplicationVerify)

	p := compileFleetReadinessStack(t, root)
	exec := &recordingExecutor{}
	var out, errOut bytes.Buffer
	err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Executor:    exec,
	}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "fleet readiness blocked") {
		t.Fatalf("Run error = %v, want fleet readiness blocked", err)
	}
	if calls := exec.calledNames(); len(calls) != 0 {
		t.Fatalf("executor was called despite missing capability: %v", calls)
	}
	readiness := fleetReadinessArtifact(t, fleetReadinessAudit(t, root).Artifacts)
	if readiness.Status != "blocked" || readiness.Summary.MissingCapabilities != 1 {
		t.Fatalf("readiness artifact = %#v", readiness)
	}
	if !fleetReadinessHasBlocker(readiness, "fleet.capability.missing") {
		t.Fatalf("missing capability blocker: %#v", readiness.Blockers)
	}
	if len(readiness.Capabilities) != 1 || readiness.Capabilities[0].Capability != NodeKindHostCommandRun || readiness.Capabilities[0].Status != "blocked" {
		t.Fatalf("capability coverage = %#v", readiness.Capabilities)
	}
}

func TestRun_FleetReadinessRequiresCapabilityOnEveryReadyAgent(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, ".torque", "agent-registry.json")
	writeFleetReadinessStackFixture(t, root, fleetReadinessStackOptions{
		RegistryPath: registryPath,
		Mode:         RunnerModeFleet,
		Transport:    "nats",
		Target:       "torque.lab.assign.mysql",
	})
	writeFleetReadinessAgent(t, registryPath, "mysql-01", heartbeat.StateReady, time.Now().UTC(), NodeKindHostCommandRun)
	writeFleetReadinessAgent(t, registryPath, "mysql-02", heartbeat.StateReady, time.Now().UTC(), NodeKindMySQLReplicationVerify)

	p := compileFleetReadinessStack(t, root)
	exec := &recordingExecutor{}
	var out, errOut bytes.Buffer
	err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Executor:    exec,
	}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "fleet readiness blocked") {
		t.Fatalf("Run error = %v, want fleet readiness blocked", err)
	}
	if calls := exec.calledNames(); len(calls) != 0 {
		t.Fatalf("executor was called despite partial capability coverage: %v", calls)
	}
	readiness := fleetReadinessArtifact(t, fleetReadinessAudit(t, root).Artifacts)
	if readiness.Status != "blocked" || readiness.Summary.MissingCapabilities != 1 {
		t.Fatalf("readiness artifact = %#v", readiness)
	}
	if len(readiness.Capabilities) != 1 || len(readiness.Capabilities[0].MissingReadyAgents) != 1 || readiness.Capabilities[0].MissingReadyAgents[0] != "mysql-02" {
		t.Fatalf("capability coverage = %#v", readiness.Capabilities)
	}
}

func TestRun_LocalModeLeavesSSHandNATSUsable(t *testing.T) {
	for _, transport := range []string{"ssh", "nats"} {
		t.Run(transport, func(t *testing.T) {
			root := t.TempDir()
			writeFleetReadinessStackFixture(t, root, fleetReadinessStackOptions{
				Mode:      RunnerModeLocal,
				Transport: transport,
				Target:    "torque.lab.assign.mysql",
			})

			p := compileFleetReadinessStack(t, root)
			exec := &recordingExecutor{}
			var out, errOut bytes.Buffer
			if err := Run(context.Background(), RunOptions{
				Command:     "apply",
				Plan:        p,
				Concurrency: 1,
				Executor:    exec,
			}, &out, &errOut); err != nil {
				t.Fatalf("Run: %v\nstderr=%s", err, errOut.String())
			}
			if calls := exec.calledNames(); len(calls) != 1 || calls[0] != "verify-mysql" {
				t.Fatalf("executor calls = %v", calls)
			}
			audit := fleetReadinessAudit(t, root)
			if fleetReadinessArtifactExists(audit.Artifacts) {
				t.Fatalf("local mode unexpectedly wrote fleet readiness artifact: %#v", audit.Artifacts)
			}
		})
	}
}

func TestEvaluateFleetReadinessWarnModeDoesNotBlock(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, ".torque", "agent-registry.json")
	writeFleetReadinessStackFixture(t, root, fleetReadinessStackOptions{
		RegistryPath:        registryPath,
		Mode:                RunnerModeFleet,
		Transport:           "nats",
		Target:              "torque.lab.assign.mysql",
		OnInsufficientReady: RunnerReadinessOnWarn,
		NoNodes:             true,
	})

	p := compileFleetReadinessStack(t, root)
	readiness := EvaluateFleetReadiness(context.Background(), p)
	if readiness == nil || readiness.Status != "warning" || len(readiness.Blockers) == 0 {
		t.Fatalf("readiness = %#v", readiness)
	}
}

func TestEvaluateFleetReadinessWarnModeStillBlocksNonNATSTransport(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, ".torque", "agent-registry.json")
	writeFleetReadinessStackFixture(t, root, fleetReadinessStackOptions{
		RegistryPath:        registryPath,
		Mode:                RunnerModeFleet,
		Transport:           "ssh",
		Target:              "root@example.invalid",
		OnInsufficientReady: RunnerReadinessOnWarn,
	})
	writeFleetReadinessAgent(t, registryPath, "mysql-01", heartbeat.StateReady, time.Now().UTC(), NodeKindHostCommandRun)

	p := compileFleetReadinessStack(t, root)
	readiness := EvaluateFleetReadiness(context.Background(), p)
	if readiness == nil || readiness.Status != "blocked" {
		t.Fatalf("readiness = %#v", readiness)
	}
	if !fleetReadinessHasBlocker(*readiness, "fleet.transport.not_nats") {
		t.Fatalf("missing transport blocker: %#v", readiness.Blockers)
	}
}

type fleetReadinessStackOptions struct {
	RegistryPath        string
	Mode                string
	Transport           string
	Target              string
	OnInsufficientReady string
	NoNodes             bool
}

func writeFleetReadinessStackFixture(t *testing.T, root string, opts fleetReadinessStackOptions) {
	t.Helper()
	mode := strings.TrimSpace(opts.Mode)
	if mode == "" {
		mode = RunnerModeFleet
	}
	transport := strings.TrimSpace(opts.Transport)
	if transport == "" {
		transport = "nats"
	}
	target := strings.TrimSpace(opts.Target)
	if target == "" {
		target = "torque.lab.assign.mysql"
	}
	onInsufficient := strings.TrimSpace(opts.OnInsufficientReady)
	if onInsufficient == "" {
		onInsufficient = RunnerReadinessOnBlock
	}
	readiness := ""
	if mode == RunnerModeFleet {
		readiness = fmt.Sprintf(`  readiness:
    source: store
    store: file
    storePath: %q
    tenant: lab
    selector:
      role: mysql
    requireAgents: true
    minReadyPercent: 95
    failureBudget: 0
    staleAfter: 45s
    onInsufficientReady: %s
`, opts.RegistryPath, onInsufficient)
	}
	nodesYAML := fmt.Sprintf(`nodes:
  - kind: host.command.run
    name: verify-mysql
    host:
      transport: %s
      target: %s
      command: echo fleet-ready
`, transport, target)
	if opts.NoNodes {
		nodesYAML = "nodes: []\n"
	}
	stackYAML := fmt.Sprintf(`apiVersion: torque.dev/v1
kind: Stack
name: fleet-readiness
runner:
  mode: %s
%s%s`, mode, readiness, nodesYAML)
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatal(err)
	}
}

func compileFleetReadinessStack(t *testing.T, root string) *Plan {
	t.Helper()
	u, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func writeFleetReadinessAgent(t *testing.T, registryPath string, agentID string, state string, observedAt time.Time, capabilities ...string) {
	t.Helper()
	store, err := heartbeat.NewFileStore(registryPath)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	defer store.Close()
	hb := heartbeat.New(heartbeat.Options{
		AgentID:      agentID,
		Tenant:       "lab",
		TargetID:     agentID,
		Hostname:     agentID,
		Labels:       map[string]string{"role": "mysql"},
		Capabilities: capabilities,
		State:        state,
		ObservedAt:   observedAt,
	})
	record, err := heartbeat.NewCompactRecord(hb, heartbeat.StreamOffset{Stream: "TORQUE_AGENT_EVENTS", Sequence: 1}, observedAt, 45*time.Second)
	if err != nil {
		t.Fatalf("compact record: %v", err)
	}
	if err := store.Put(context.Background(), record); err != nil {
		t.Fatalf("put compact record: %v", err)
	}
}

func fleetReadinessAudit(t *testing.T, root string) *RunAudit {
	t.Helper()
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeEvents:    true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return audit
}

func fleetReadinessArtifact(t *testing.T, artifacts []RunArtifact) FleetReadinessResult {
	t.Helper()
	for _, artifact := range artifacts {
		if artifact.Name != "fleet-readiness.json" {
			continue
		}
		var result FleetReadinessResult
		if err := json.Unmarshal([]byte(artifact.Body), &result); err != nil {
			t.Fatalf("decode fleet-readiness.json: %v\n%s", err, artifact.Body)
		}
		return result
	}
	t.Fatalf("missing fleet-readiness.json in artifacts: %#v", artifacts)
	return FleetReadinessResult{}
}

func fleetReadinessArtifactExists(artifacts []RunArtifact) bool {
	for _, artifact := range artifacts {
		if artifact.Name == "fleet-readiness.json" {
			return true
		}
	}
	return false
}

func fleetReadinessHasBlocker(result FleetReadinessResult, code string) bool {
	for _, blocker := range result.Blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}
