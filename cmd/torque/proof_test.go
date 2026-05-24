package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/deploy"
	"github.com/ingresslabs/torque/internal/stack"
)

func TestProofGraphSignsAndVerifiesApplyProof(t *testing.T) {
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "apply.sqlite")
	verifyPath := filepath.Join(dir, "verify.json")
	buildPath := filepath.Join(dir, "build.sqlite")
	sloPath := filepath.Join(dir, "slo.yaml")
	repairDir := filepath.Join(dir, "fixes")
	repairPR := filepath.Join(repairDir, "pr.md")
	for path, body := range map[string]string{
		capturePath: "capture",
		verifyPath:  `{"passed":true}`,
		buildPath:   "build capture",
		sloPath:     "spec:\n  minReadyPercent: 90\n",
		repairPR:    "# Repair\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	slo, err := loadApplyRollbackSLO(sloPath)
	if err != nil {
		t.Fatalf("load slo: %v", err)
	}
	rollback := buildApplyRollbackProof(applyRollbackProofInput{
		Release:       "api",
		Namespace:     "prod",
		Mode:          "helm-rollback",
		Outcome:       "rolled-back",
		TriggerSource: "slo",
		TriggerReason: "SLO failed",
		StartedAt:     time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		SLO:           slo,
		Resources:     []deploy.ResourceStatus{{Kind: "Deployment", Namespace: "prod", Name: "api", Status: "Failed"}},
	})
	plan := &deployPlanResult{
		ReleaseName:    "api",
		Namespace:      "prod",
		ChartRef:       "./chart",
		ChartVersion:   "0.1.0",
		RenderedSHA256: "sha256:rendered",
		Summary:        planSummary{Creates: 1, Updates: 1},
		Images: []planImageRef{{
			Resource:  "Deployment/prod/api",
			Container: "api",
			Image:     "ghcr.io/acme/api@sha256:abc",
			Digest:    "sha256:abc",
			Pinned:    true,
		}},
		VerifyReports: []planVerifyReport{{
			Path:    verifyPath,
			Passed:  true,
			Blocked: false,
		}},
		BuildProvenance: []planBuildProvenance{{
			Source:     buildPath,
			Digest:     "sha256:abc",
			Tags:       []string{"ghcr.io/acme/api:dev"},
			Referenced: true,
			Verdict:    "safe",
		}},
		GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
	proof := buildApplyProofBundle(applyProofBundleInput{
		StartedAt:        time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		Release:          "api",
		Namespace:        "prod",
		Chart:            "./chart",
		Plan:             plan,
		CapturePath:      capturePath,
		RollbackProof:    &rollback,
		ResourceSnapshot: []deploy.ResourceStatus{{Kind: "Deployment", Namespace: "prod", Name: "api", Status: "Failed"}},
	})
	proofPath := filepath.Join(dir, "apply-proof.json")
	if err := writeJSONFileEnsured(proofPath, proof); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	keyPath := writeProofTestKey(t, dir)

	graph, err := buildProofGraph(proofPath, nil)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	if err := signProofGraph(&graph, keyPath); err != nil {
		t.Fatalf("sign graph: %v", err)
	}
	if !proofGraphHasType(graph, "image-digest") || !proofGraphHasType(graph, "verifier-report") || !proofGraphHasType(graph, "build-capture") || !proofGraphHasType(graph, "rollback-proof") || !proofGraphHasType(graph, "repair-pr") {
		t.Fatalf("graph missing expected artifacts: %#v", graph.Artifacts)
	}
	report := verifyProofGraph(graph, "", proofVerifyOptions{RequireSignature: true})
	if !report.Passed || !report.Signature.Verified {
		t.Fatalf("expected signed graph to verify: %#v", report)
	}

	if err := os.WriteFile(verifyPath, []byte(`{"passed":false}`), 0o644); err != nil {
		t.Fatalf("tamper verify report: %v", err)
	}
	report = verifyProofGraph(graph, "", proofVerifyOptions{RequireSignature: true})
	if report.Passed || len(report.Artifacts.Mismatched) == 0 {
		t.Fatalf("expected tampered artifact to fail verification: %#v", report)
	}
}

func TestProofCommandWritesGraphAndHTML(t *testing.T) {
	dir := t.TempDir()
	proofPath := filepath.Join(dir, "apply-proof.json")
	proof := buildApplyProofBundle(applyProofBundleInput{
		StartedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		Release:   "api",
		Namespace: "prod",
		Chart:     "./chart",
		Plan: &deployPlanResult{
			ReleaseName:    "api",
			Namespace:      "prod",
			RenderedSHA256: "sha256:rendered",
			Images: []planImageRef{{
				Resource: "Deployment/prod/api",
				Image:    "ghcr.io/acme/api@sha256:abc",
				Digest:   "sha256:abc",
				Pinned:   true,
			}},
			GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		},
	})
	if err := writeJSONFileEnsured(proofPath, proof); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	keyPath := writeProofTestKey(t, dir)
	graphPath := filepath.Join(dir, "proof.graph.json")
	htmlPath := filepath.Join(dir, "proof.html")

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"proof", "graph", proofPath, "--out", graphPath, "--html", htmlPath, "--key", keyPath})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("proof graph command: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(graphPath); err != nil {
		t.Fatalf("expected graph json: %v", err)
	}
	htmlRaw, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("expected graph html: %v", err)
	}
	if !strings.Contains(string(htmlRaw), "Torque Proof Graph") {
		t.Fatalf("unexpected html: %s", htmlRaw)
	}
	verify, err := verifyProofSource(graphPath, proofVerifyOptions{RequireSignature: true})
	if err != nil {
		t.Fatalf("verify graph source: %v", err)
	}
	if !verify.Passed {
		t.Fatalf("expected graph file to verify: %#v", verify)
	}
}

func TestProofDiffReportsEvidenceChanges(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.json")
	newPath := filepath.Join(dir, "new.json")
	writeProofForDiff(t, oldPath, "sha256:old", false)
	writeProofForDiff(t, newPath, "sha256:new", true)

	report, err := diffProofSources(oldPath, newPath)
	if err != nil {
		t.Fatalf("diff proof sources: %v", err)
	}
	if !report.Changed || report.Summary.Added == 0 || report.Summary.Removed == 0 || report.Summary.Changed == 0 {
		raw, _ := json.MarshalIndent(report, "", "  ")
		t.Fatalf("expected proof diff to show changes: %s", raw)
	}
}

func TestProofAttestSignsVerifiedGraph(t *testing.T) {
	_, graphPath, keyPath := writeProofGateFixture(t, true)
	attestPath := filepath.Join(filepath.Dir(graphPath), "release.attestation.json")

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"proof", "attest", graphPath, "--release", "v1.0.9", "--key", keyPath, "--out", attestPath, "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("proof attest: %v\n%s", err, out.String())
	}
	raw, err := os.ReadFile(attestPath)
	if err != nil {
		t.Fatalf("read attestation: %v", err)
	}
	var attestation proofAttestation
	if err := json.Unmarshal(raw, &attestation); err != nil {
		t.Fatalf("decode attestation: %v", err)
	}
	if attestation.Release != "v1.0.9" || !attestation.Verified || attestation.Signature == nil || attestation.Signature.Algorithm != "ed25519" {
		t.Fatalf("unexpected attestation: %#v", attestation)
	}
	if attestation.Artifacts == 0 || attestation.FilesChecked == 0 || attestation.Commit == "" {
		t.Fatalf("attestation missing release evidence: %#v", attestation)
	}
}

func TestProofGatePassesCompleteReleaseEvidence(t *testing.T) {
	_, graphPath, _ := writeProofGateFixture(t, true)

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"proof", "gate", graphPath, "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("proof gate: %v\n%s", err, out.String())
	}
	var report proofGateReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode gate report: %v\n%s", err, out.String())
	}
	if !report.Passed || report.Summary.Failed != 0 || !report.Verification.SignatureVerified {
		t.Fatalf("expected gate to pass: %#v", report)
	}
}

func TestProofGateBlocksMissingSBOM(t *testing.T) {
	_, graphPath, _ := writeProofGateFixture(t, false)

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"proof", "gate", graphPath, "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatalf("expected proof gate to fail without SBOM")
	}
	var report proofGateReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode gate report: %v\n%s", err, out.String())
	}
	check, ok := findProofGateCheck(report, "artifact.sbom")
	if !ok || check.Passed {
		t.Fatalf("expected missing SBOM check to block: %#v", report.Checks)
	}
}

func writeProofTestKey(t *testing.T, dir string) string {
	t.Helper()
	key, err := stack.GenerateEd25519Key()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyPath := filepath.Join(dir, "ed25519.json")
	if err := writeJSONFileEnsured(keyPath, key); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return keyPath
}

func proofGraphHasType(graph proofGraph, typ string) bool {
	for _, artifact := range graph.Artifacts {
		if artifact.Type == typ {
			return true
		}
	}
	return false
}

func writeProofGateFixture(t *testing.T, complete bool) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	evidenceDir := filepath.Join(dir, "evidence")
	fixDir := filepath.Join(dir, "fixes")
	for _, path := range []string{evidenceDir, fixDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	capturePath := filepath.Join(evidenceDir, "apply.sqlite")
	verifyPath := filepath.Join(evidenceDir, "verifier.report.json")
	buildPath := filepath.Join(evidenceDir, "buildkit-capture.sqlite")
	sloPath := filepath.Join(evidenceDir, "slo.yaml")
	serverDryRunPath := filepath.Join(evidenceDir, "server-dry-run.json")
	logsPath := filepath.Join(evidenceDir, "rollout-logs.sqlite")
	driftPath := filepath.Join(dir, "drift-proof.json")
	runtimePath := filepath.Join(dir, "runtime-events.json")
	sbomPath := filepath.Join(evidenceDir, "sbom.cdx.json")
	provenancePath := filepath.Join(evidenceDir, "provenance.intoto.jsonl")
	for path, body := range map[string]string{
		capturePath:                        "SQLite format 3\napply capture\n",
		verifyPath:                         `{"passed":true,"status":"passed"}`,
		buildPath:                          "SQLite format 3\nbuildkit capture\n",
		sloPath:                            "minReadyPercent: 100\nmaxFailedResources: 0\n",
		serverDryRunPath:                   `{"version":"v1","status":"passed","dryRun":true}`,
		logsPath:                           "SQLite format 3\nrollout logs\n",
		driftPath:                          `{"version":"v1","tool":"torque-guardian","generatedAt":"2026-05-11T12:00:00Z","release":"api","namespace":"prod","chart":"./chart","renderedManifestSha256":"sha256:rendered","status":"passed","blocked":false,"summary":{"resources":1,"unchanged":1},"predictedVsLive":{"version":"v1","passed":true},"managedFields":{"version":"v1"},"driftTimeline":{"version":"v1"},"eventsTimeline":{"version":"v1"},"runtimeSecretBoundary":{"version":"v1","passed":true},"rolloutAftercare":{"version":"v1","passed":true}}`,
		runtimePath:                        `{"version":"v1","tool":"torque-guardian","generatedAt":"2026-05-11T12:00:00Z","namespace":"prod","eventsTimeline":{"version":"v1","events":[{"type":"Normal","reason":"Ready","resource":{"kind":"Deployment","namespace":"prod","name":"api"}}]},"summary":{"events":1,"warnings":0}}`,
		filepath.Join(fixDir, "pr.md"):     "# Repair PR\n",
		filepath.Join(fixDir, "fix.patch"): "diff --git a/chart/values.yaml b/chart/values.yaml\n",
		provenancePath:                     `{"predicateType":"https://slsa.dev/provenance/v1"}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if complete {
		if err := os.WriteFile(sbomPath, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.5"}`), 0o644); err != nil {
			t.Fatalf("write sbom: %v", err)
		}
	}
	minReady := 100
	rollback := applyRollbackProof{
		Version:     1,
		GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Release:     "api",
		Namespace:   "prod",
		Chart:       "./chart",
		Mode:        "helm-rollback",
		Outcome:     "rolled-back",
		Trigger: applyRollbackTrigger{
			Source: "slo",
			Reason: "SLO failed",
		},
		SLO: &applyRollbackSLO{
			Path:            sloPath,
			MinReadyPercent: &minReady,
		},
		RolledBackToRevision: 7,
	}
	plan := &deployPlanResult{
		ReleaseName:    "api",
		Namespace:      "prod",
		ChartRef:       "./chart",
		ChartVersion:   "0.1.0",
		RenderedSHA256: "sha256:rendered",
		Summary:        planSummary{Updates: 1},
		Images: []planImageRef{{
			Resource:  "Deployment/prod/api",
			Container: "api",
			Image:     "ghcr.io/acme/api@sha256:abc",
			Digest:    "sha256:abc",
			Pinned:    true,
		}},
		VerifyReports: []planVerifyReport{{
			Path:    verifyPath,
			Passed:  true,
			Blocked: false,
		}},
		BuildProvenance: []planBuildProvenance{{
			Source:       buildPath,
			Digest:       "sha256:abc",
			Tags:         []string{"ghcr.io/acme/api:e2e"},
			Platforms:    []string{"linux/amd64"},
			Attestations: []map[string]any{{"type": "slsa", "path": provenancePath}},
			Referenced:   true,
			Verdict:      "safe",
		}},
		GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
	proof := buildApplyProofBundle(applyProofBundleInput{
		StartedAt:        time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		Release:          "api",
		Namespace:        "prod",
		Chart:            "./chart",
		Err:              os.ErrInvalid,
		Plan:             plan,
		CapturePath:      capturePath,
		RollbackProof:    &rollback,
		ResourceSnapshot: []deploy.ResourceStatus{{Kind: "Deployment", Namespace: "prod", Name: "api", Status: "Failed"}},
	})
	proofPath := filepath.Join(dir, "apply-proof.json")
	if err := writeJSONFileEnsured(proofPath, proof); err != nil {
		t.Fatalf("write apply proof: %v", err)
	}
	attachments := []string{driftPath, runtimePath, serverDryRunPath, logsPath, provenancePath, filepath.Join(fixDir, "pr.md")}
	if complete {
		attachments = append(attachments, sbomPath)
	}
	graph, err := buildProofGraph(proofPath, attachments)
	if err != nil {
		t.Fatalf("build proof graph: %v", err)
	}
	if !complete {
		graph.Artifacts = filterProofArtifactsByType(graph.Artifacts, "sbom")
	}
	keyPath := writeProofTestKey(t, dir)
	if err := signProofGraph(&graph, keyPath); err != nil {
		t.Fatalf("sign graph: %v", err)
	}
	graphPath := filepath.Join(dir, "proof.graph.json")
	if err := writeJSONFileEnsured(graphPath, graph); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	return dir, graphPath, keyPath
}

func findProofGateCheck(report proofGateReport, id string) (proofGateCheck, bool) {
	for _, check := range report.Checks {
		if check.ID == id {
			return check, true
		}
	}
	return proofGateCheck{}, false
}

func filterProofArtifactsByType(artifacts []proofGraphArtifact, typ string) []proofGraphArtifact {
	var out []proofGraphArtifact
	for _, artifact := range artifacts {
		if artifact.Type == typ {
			continue
		}
		out = append(out, artifact)
	}
	return out
}

func writeProofForDiff(t *testing.T, path, digest string, failed bool) {
	t.Helper()
	errValue := error(nil)
	if failed {
		errValue = os.ErrInvalid
	}
	proof := buildApplyProofBundle(applyProofBundleInput{
		StartedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		Release:   "api",
		Namespace: "prod",
		Chart:     "./chart",
		Err:       errValue,
		Plan: &deployPlanResult{
			ReleaseName:    "api",
			Namespace:      "prod",
			RenderedSHA256: "sha256:rendered",
			Images: []planImageRef{{
				Resource: "Deployment/prod/api",
				Image:    "ghcr.io/acme/api@" + digest,
				Digest:   digest,
				Pinned:   true,
			}},
			GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		},
	})
	if err := writeJSONFileEnsured(path, proof); err != nil {
		t.Fatalf("write proof %s: %v", path, err)
	}
}
