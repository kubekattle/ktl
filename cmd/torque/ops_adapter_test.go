package main

import (
	"encoding/json"
	"strings"
	"testing"

	opsadapter "github.com/ingresslabs/torque/internal/ops/adapter"
)

func TestOpsAdapterCapabilitiesJSON(t *testing.T) {
	out, errOut, err := runRootForOpsInventory(t, "ops", "adapter", "capabilities", "--format", "json")
	if err != nil {
		t.Fatalf("execute failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	var result opsadapter.CapabilityList
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if result.APIVersion != opsadapter.CapabilityAPIVersion || result.Kind != opsadapter.CapabilityListKind {
		t.Fatalf("identity = %#v", result)
	}
	if result.Summary.Implemented < 1 || result.Summary.Planned < 1 {
		t.Fatalf("summary = %#v", result.Summary)
	}
	host := findAdapterCapability(result.Adapters, "host.command.run")
	if host == nil {
		t.Fatalf("missing host.command.run in %#v", result.Adapters)
	}
	if host.Status != "implemented" || host.Classification != "guarded" || !host.Mutating {
		t.Fatalf("host.command.run capability = %#v", host)
	}
	if !adapterStringSliceContains(host.EvidenceArtifacts, "host-command-verify.json") {
		t.Fatalf("host.command.run missing verify artifact: %#v", host.EvidenceArtifacts)
	}
	render := findAdapterCapability(result.Adapters, "host.file.render")
	if render == nil {
		t.Fatalf("missing host.file.render in %#v", result.Adapters)
	}
	if render.Status != "implemented" || render.DiffQuality != "exact" {
		t.Fatalf("host.file.render capability = %#v", render)
	}
	if !adapterStringSliceContains(render.EvidenceArtifacts, "host-file-diff.json") {
		t.Fatalf("host.file.render missing diff artifact: %#v", render.EvidenceArtifacts)
	}
	copyAdapter := findAdapterCapability(result.Adapters, "host.file.copy")
	if copyAdapter == nil {
		t.Fatalf("missing host.file.copy in %#v", result.Adapters)
	}
	if copyAdapter.Status != "implemented" || copyAdapter.DiffQuality != "exact" {
		t.Fatalf("host.file.copy capability = %#v", copyAdapter)
	}
	if !adapterStringSliceContains(copyAdapter.EvidenceArtifacts, "host-file-copy-diff.json") {
		t.Fatalf("host.file.copy missing diff artifact: %#v", copyAdapter.EvidenceArtifacts)
	}
	if strings.Contains(out, "secret://") {
		t.Fatalf("capability output leaked secret ref: %s", out)
	}
}

func TestOpsAdapterCapabilitiesTable(t *testing.T) {
	out, errOut, err := runRootForOpsInventory(t, "ops", "adapter", "capabilities")
	if err != nil {
		t.Fatalf("execute failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	for _, want := range []string{"ADAPTER", "STATUS", "host.command.run", "host.file.render"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table missing %q:\n%s", want, out)
		}
	}
}

func TestOpsAdapterCapabilitiesLocalProbe(t *testing.T) {
	out, errOut, err := runRootForOpsInventory(t, "ops", "adapter", "capabilities", "host.command.run", "--target", "local://ops-cli-007", "--format", "json")
	if err != nil {
		t.Fatalf("execute probe failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	var result opsadapter.CapabilityList
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if len(result.Adapters) != 1 {
		t.Fatalf("adapter count=%d want 1", len(result.Adapters))
	}
	probe := result.Adapters[0].Probe
	if probe == nil || probe.Status != "succeeded" || probe.Transport != "local" {
		t.Fatalf("probe = %#v", probe)
	}
	if result.Summary.ProbeSucceeded != 1 {
		t.Fatalf("summary = %#v", result.Summary)
	}
	if strings.Contains(out, "torque-adapter-probe-secret") {
		t.Fatalf("probe output leaked raw secret canary: %s", out)
	}
	if !strings.Contains(out, "password=[REDACTED]") {
		t.Fatalf("probe output missing redaction proof: %s", out)
	}
}

func TestOpsAdapterCapabilitiesUnknownAdapter(t *testing.T) {
	out, _, err := runRootForOpsInventory(t, "ops", "adapter", "capabilities", "host.nope", "--format", "json")
	if err == nil {
		t.Fatalf("unknown adapter succeeded:\n%s", out)
	}
}

func findAdapterCapability(items []opsadapter.Capability, name string) *opsadapter.Capability {
	for i := range items {
		if items[i].Adapter == name {
			return &items[i]
		}
	}
	return nil
}

func adapterStringSliceContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
