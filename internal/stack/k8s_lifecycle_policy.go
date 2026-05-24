package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	kubernetesLifecyclePolicyDecisionArtifact = "k8s-lifecycle-policy-decision.json"
	kubernetesLifecyclePolicyOverrideArtifact = "k8s-lifecycle-policy-override.json"
	defaultKubernetesLifecycleMaxInspectAge   = 15 * time.Minute
)

type kubernetesLifecyclePolicyDecision struct {
	APIVersion     string                                      `json:"apiVersion"`
	Kind           string                                      `json:"kind"`
	NodeID         string                                      `json:"nodeId"`
	NodeKind       string                                      `json:"nodeKind"`
	Status         string                                      `json:"status"`
	Message        string                                      `json:"message"`
	TargetCount    int                                         `json:"targetCount"`
	BatchSize      int                                         `json:"batchSize"`
	MaxUnavailable int                                         `json:"maxUnavailable,omitempty"`
	Inspect        *kubernetesLifecyclePolicyInspectRef        `json:"inspect,omitempty"`
	Checks         []kubernetesLifecyclePolicyCheck            `json:"checks,omitempty"`
	AppProbes      []kubernetesLifecyclePolicyAppProbeEvidence `json:"appProbes,omitempty"`
}

type kubernetesLifecyclePolicyInspectRef struct {
	SourceNodeID                string                                      `json:"sourceNodeId"`
	Artifact                    string                                      `json:"artifact"`
	SHA256                      string                                      `json:"sha256,omitempty"`
	CreatedAt                   string                                      `json:"createdAt,omitempty"`
	Status                      string                                      `json:"status,omitempty"`
	Provider                    kubernetesClusterInspectProvider            `json:"provider"`
	CertificateRenewal          kubernetesClusterInspectCertificateRenewal  `json:"certificateRenewal"`
	EffectiveCertificateRenewal *kubernetesClusterInspectCertificateRenewal `json:"effectiveCertificateRenewal,omitempty"`
	TotalNodes                  int                                         `json:"totalNodes"`
	ReadyNodes                  int                                         `json:"readyNodes"`
}

type kubernetesLifecyclePolicyCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type kubernetesLifecyclePolicyAppProbeEvidence struct {
	ID      string                          `json:"id"`
	Expect  string                          `json:"expect,omitempty"`
	Receipt kubernetesClusterInspectReceipt `json:"receipt"`
	Matched bool                            `json:"matched,omitempty"`
}

type kubernetesLifecyclePolicyOverrideDecision struct {
	APIVersion            string                                        `json:"apiVersion"`
	Kind                  string                                        `json:"kind"`
	NodeID                string                                        `json:"nodeId"`
	NodeKind              string                                        `json:"nodeKind"`
	Status                string                                        `json:"status"`
	Message               string                                        `json:"message"`
	RuntimeEnabled        bool                                          `json:"runtimeEnabled"`
	OriginalPolicyStatus  string                                        `json:"originalPolicyStatus,omitempty"`
	OriginalPolicyMessage string                                        `json:"originalPolicyMessage,omitempty"`
	Approval              KubernetesLifecyclePolicyOverrideSpec         `json:"approval,omitempty"`
	RuntimeScope          kubernetesLifecyclePolicyOverrideRuntimeScope `json:"runtimeScope"`
	Checks                []kubernetesLifecyclePolicyCheck              `json:"checks,omitempty"`
}

type kubernetesLifecyclePolicyOverrideRuntimeScope struct {
	RunID           string   `json:"runId,omitempty"`
	NodeID          string   `json:"nodeId"`
	NodeName        string   `json:"nodeName,omitempty"`
	IntentDigest    string   `json:"intentDigest,omitempty"`
	TargetIDs       []string `json:"targetIds,omitempty"`
	TargetSetDigest string   `json:"targetSetDigest,omitempty"`
}

func kubernetesLifecyclePolicyConfigured(spec KubernetesLifecyclePolicySpec) bool {
	return spec.MaxUnavailable != 0 ||
		spec.RequireFreshInspect ||
		spec.MaxInspectAge != nil ||
		spec.RequireHealthyInspect ||
		spec.RequireSupportedProvider ||
		kubernetesMaintenanceWindowConfigured(spec.MaintenanceWindow) ||
		len(spec.AppProbes) > 0
}

func kubernetesLifecyclePolicyOverrideConfigured(spec KubernetesLifecyclePolicyOverrideSpec) bool {
	return strings.TrimSpace(spec.Reason) != "" ||
		strings.TrimSpace(spec.Ticket) != "" ||
		strings.TrimSpace(spec.ChangeID) != "" ||
		strings.TrimSpace(spec.Approver) != "" ||
		strings.TrimSpace(spec.ExpiresAt) != "" ||
		strings.TrimSpace(spec.Scope.NodeID) != "" ||
		strings.TrimSpace(spec.Scope.RunID) != "" ||
		strings.TrimSpace(spec.Scope.IntentDigest) != "" ||
		len(spec.Scope.TargetIDs) > 0 ||
		strings.TrimSpace(spec.Scope.TargetSetDigest) != ""
}

func validateKubernetesLifecyclePolicySpec(kind string, name string, spec KubernetesLifecyclePolicySpec) error {
	if !kubernetesLifecyclePolicyConfigured(spec) {
		return nil
	}
	nodeKind := normalizeNodeKind(kind)
	if spec.MaxUnavailable < 0 {
		return fmt.Errorf("%s node %s requires kubernetes.certificates.policy.maxUnavailable >= 0", nodeKind, name)
	}
	if spec.MaxInspectAge != nil && *spec.MaxInspectAge < 0 {
		return fmt.Errorf("%s node %s requires kubernetes.certificates.policy.maxInspectAge >= 0", nodeKind, name)
	}
	if err := validateKubernetesMaintenanceWindowSpec(nodeKind, name, spec.MaintenanceWindow); err != nil {
		return err
	}
	for i, probe := range spec.AppProbes {
		if strings.TrimSpace(probe.ID) == "" {
			return fmt.Errorf("%s node %s lifecycle policy appProbes[%d].id is required", nodeKind, name, i)
		}
		if strings.TrimSpace(probe.Command) == "" {
			return fmt.Errorf("%s node %s lifecycle policy app probe %s requires command", nodeKind, name, probe.ID)
		}
	}
	return nil
}

func validateKubernetesMaintenanceWindowSpec(kind string, name string, spec KubernetesMaintenanceWindowSpec) error {
	if !kubernetesMaintenanceWindowConfigured(spec) {
		return nil
	}
	if strings.TrimSpace(spec.Start) == "" || strings.TrimSpace(spec.End) == "" {
		return fmt.Errorf("%s node %s lifecycle policy maintenanceWindow requires start and end", kind, name)
	}
	if _, err := kubernetesMaintenanceWindowMinute(spec.Start); err != nil {
		return fmt.Errorf("%s node %s has invalid lifecycle policy maintenanceWindow.start %q", kind, name, spec.Start)
	}
	if _, err := kubernetesMaintenanceWindowMinute(spec.End); err != nil {
		return fmt.Errorf("%s node %s has invalid lifecycle policy maintenanceWindow.end %q", kind, name, spec.End)
	}
	if strings.TrimSpace(spec.TimeZone) != "" {
		if _, err := time.LoadLocation(strings.TrimSpace(spec.TimeZone)); err != nil {
			return fmt.Errorf("%s node %s has invalid lifecycle policy maintenanceWindow.timeZone %q", kind, name, spec.TimeZone)
		}
	}
	if _, _, err := kubernetesMaintenanceWindowDays(spec.Days); err != nil {
		return fmt.Errorf("%s node %s has invalid lifecycle policy maintenanceWindow.days: %w", kind, name, err)
	}
	return nil
}

func (e *customNodeExecutor) evaluateKubernetesLifecyclePolicy(ctx context.Context, node *runNode, certs KubernetesCertSpec, targetsFrom *kubernetesCertTargetsFromEvidence, runners []kubernetesCertTargetRunner, batches []kubernetesCertBatch) kubernetesLifecyclePolicyDecision {
	policy := certs.Policy
	decision := kubernetesLifecyclePolicyDecision{
		APIVersion:  "torque.dev/stack-lifecycle/v1",
		Kind:        "KubernetesLifecyclePolicyDecision",
		NodeID:      strings.TrimSpace(node.ID),
		NodeKind:    normalizeNodeKind(node.Kind),
		Status:      "allowed",
		Message:     "lifecycle policy allowed",
		TargetCount: len(runners),
		BatchSize:   normalizedKubernetesCertBatchSize(certs.BatchSize),
	}
	if policy.MaxUnavailable > 0 {
		decision.MaxUnavailable = policy.MaxUnavailable
	}
	addCheck := func(id string, status string, message string) {
		status = strings.TrimSpace(status)
		if status == "" {
			status = "passed"
		}
		decision.Checks = append(decision.Checks, kubernetesLifecyclePolicyCheck{
			ID:      strings.TrimSpace(id),
			Status:  status,
			Message: strings.TrimSpace(message),
		})
		if status == "blocked" {
			decision.Status = "blocked"
		}
	}
	if !kubernetesLifecyclePolicyConfigured(policy) {
		addCheck("policy.configured", "skipped", "no lifecycle policy configured")
		return decision
	}

	if policy.MaxUnavailable > 0 {
		maxBatch := 0
		for _, batch := range batches {
			if len(batch.Targets) > maxBatch {
				maxBatch = len(batch.Targets)
			}
		}
		if maxBatch > policy.MaxUnavailable {
			addCheck("maxUnavailable", "blocked", fmt.Sprintf("largest maintenance batch has %d target(s), policy allows %d", maxBatch, policy.MaxUnavailable))
		} else {
			addCheck("maxUnavailable", "passed", fmt.Sprintf("largest maintenance batch has %d target(s), policy allows %d", maxBatch, policy.MaxUnavailable))
		}
	} else {
		addCheck("maxUnavailable", "skipped", "no maxUnavailable policy configured")
	}

	inspect, inspectArtifact, inspectErr := e.lifecyclePolicyInspectEvidence(ctx, policy, targetsFrom)
	if inspectErr != nil {
		addCheck("inspect.available", "blocked", inspectErr.Error())
	} else if inspect != nil {
		effectiveRenewal := kubernetesLifecycleEffectiveCertificateRenewal(inspect.CertificateRenewal, certs, targetsFrom)
		decision.Inspect = kubernetesLifecyclePolicyInspectReference(*inspect, inspectArtifact, targetsFrom, &effectiveRenewal)
		addCheck("inspect.available", "passed", "cluster inspect evidence available")
	}
	if inspectErr == nil && inspect != nil {
		effectiveRenewal := kubernetesLifecycleEffectiveCertificateRenewal(inspect.CertificateRenewal, certs, targetsFrom)
		e.evaluateKubernetesLifecycleInspectPolicy(policy, *inspect, inspectArtifact, effectiveRenewal, addCheck)
	}

	e.evaluateKubernetesMaintenanceWindowPolicy(policy.MaintenanceWindow, addCheck)
	e.evaluateKubernetesLifecycleAppProbePolicy(ctx, policy.AppProbes, runners, addCheck, &decision)
	kubernetesLifecyclePolicyFinalize(&decision)
	return decision
}

func (e *customNodeExecutor) lifecyclePolicyInspectEvidence(ctx context.Context, policy KubernetesLifecyclePolicySpec, targetsFrom *kubernetesCertTargetsFromEvidence) (*kubernetesClusterInspectEvidence, RunArtifact, error) {
	if !kubernetesLifecyclePolicyRequiresInspect(policy) {
		return nil, RunArtifact{}, nil
	}
	if targetsFrom == nil || strings.TrimSpace(targetsFrom.SourceNodeID) == "" || strings.TrimSpace(targetsFrom.Artifact) == "" {
		return nil, RunArtifact{}, fmt.Errorf("lifecycle policy requires cluster inspect evidence from targetsFrom")
	}
	inspect, artifact, err := e.loadKubernetesClusterInspectArtifact(ctx, targetsFrom.SourceNodeID, targetsFrom.Artifact)
	if err != nil {
		return nil, artifact, err
	}
	return &inspect, artifact, nil
}

func kubernetesLifecyclePolicyRequiresInspect(spec KubernetesLifecyclePolicySpec) bool {
	return spec.RequireFreshInspect || spec.MaxInspectAge != nil || spec.RequireHealthyInspect || spec.RequireSupportedProvider
}

func kubernetesLifecyclePolicyInspectReference(inspect kubernetesClusterInspectEvidence, artifact RunArtifact, targetsFrom *kubernetesCertTargetsFromEvidence, effectiveRenewal *kubernetesClusterInspectCertificateRenewal) *kubernetesLifecyclePolicyInspectRef {
	ref := &kubernetesLifecyclePolicyInspectRef{
		SourceNodeID:                strings.TrimSpace(artifact.NodeID),
		Artifact:                    strings.TrimSpace(artifact.Name),
		SHA256:                      kubernetesLifecycleArtifactDigest(artifact),
		CreatedAt:                   strings.TrimSpace(artifact.CreatedAt),
		Status:                      strings.TrimSpace(inspect.Status),
		Provider:                    inspect.Provider,
		CertificateRenewal:          inspect.CertificateRenewal,
		EffectiveCertificateRenewal: effectiveRenewal,
		TotalNodes:                  len(inspect.Nodes),
	}
	if targetsFrom != nil {
		ref.SourceNodeID = strings.TrimSpace(targetsFrom.SourceNodeID)
		ref.Artifact = strings.TrimSpace(targetsFrom.Artifact)
	}
	for _, node := range inspect.Nodes {
		if node.Ready {
			ref.ReadyNodes++
		}
	}
	return ref
}

func kubernetesLifecycleEffectiveCertificateRenewal(inspectRenewal kubernetesClusterInspectCertificateRenewal, certs KubernetesCertSpec, targetsFrom *kubernetesCertTargetsFromEvidence) kubernetesClusterInspectCertificateRenewal {
	provider := strings.ToLower(strings.TrimSpace(certs.TargetsFrom.Provider))
	if provider == "" && targetsFrom != nil {
		provider = strings.ToLower(strings.TrimSpace(targetsFrom.Provider))
	}
	if provider == "" {
		return inspectRenewal
	}
	if inspectRenewal.ManagedExternally && provider != "custom" {
		return inspectRenewal
	}
	switch provider {
	case "kubeadm", "k3s", "rke2":
		if inspectRenewal.Supported && strings.EqualFold(strings.TrimSpace(inspectRenewal.Provider), provider) {
			return inspectRenewal
		}
		return kubernetesClusterInspectCertificateRenewal{
			Provider:                provider,
			Supported:               true,
			RequiresExplicitTargets: true,
			Reason:                  "certificate renewal provider was explicitly configured through targetsFrom.",
		}
	case "custom":
		if strings.TrimSpace(certs.TargetsFrom.InspectCommand) != "" && strings.TrimSpace(certs.TargetsFrom.RenewCommand) != "" {
			return kubernetesClusterInspectCertificateRenewal{
				Provider:                "custom",
				Supported:               true,
				RequiresExplicitTargets: true,
				RequiresCustomCommands:  true,
				Reason:                  "custom certificate inspect and renew commands are explicitly configured through targetsFrom.",
			}
		}
	}
	return inspectRenewal
}

func (e *customNodeExecutor) evaluateKubernetesLifecycleInspectPolicy(policy KubernetesLifecyclePolicySpec, inspect kubernetesClusterInspectEvidence, artifact RunArtifact, effectiveRenewal kubernetesClusterInspectCertificateRenewal, addCheck func(string, string, string)) {
	if policy.RequireFreshInspect || policy.MaxInspectAge != nil {
		maxAge := defaultKubernetesLifecycleMaxInspectAge
		if policy.MaxInspectAge != nil && *policy.MaxInspectAge > 0 {
			maxAge = *policy.MaxInspectAge
		}
		createdAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(artifact.CreatedAt))
		if err != nil {
			addCheck("inspect.fresh", "blocked", "inspect artifact has no parseable createdAt timestamp")
		} else {
			age := time.Since(createdAt)
			if age > maxAge {
				addCheck("inspect.fresh", "blocked", fmt.Sprintf("inspect evidence age %s exceeds policy max %s", age.Truncate(time.Second), maxAge))
			} else {
				addCheck("inspect.fresh", "passed", fmt.Sprintf("inspect evidence age %s within policy max %s", age.Truncate(time.Second), maxAge))
			}
		}
	}
	if policy.RequireHealthyInspect {
		issues := kubernetesLifecycleInspectHealthIssues(inspect)
		if len(issues) > 0 {
			addCheck("inspect.healthy", "blocked", strings.Join(issues, "; "))
		} else {
			addCheck("inspect.healthy", "passed", "inspect evidence reports Ready nodes and healthy core pods")
		}
	}
	if policy.RequireSupportedProvider {
		renewal := effectiveRenewal
		if renewal.ManagedExternally {
			addCheck("provider.supported", "blocked", "certificate renewal is managed externally by provider")
		} else if !renewal.Supported {
			msg := strings.TrimSpace(renewal.Reason)
			if msg == "" {
				msg = "certificate renewal provider is not supported"
			}
			addCheck("provider.supported", "blocked", msg)
		} else {
			addCheck("provider.supported", "passed", fmt.Sprintf("provider %q supports Torque certificate renewal", renewal.Provider))
		}
	}
}

func kubernetesLifecycleInspectHealthIssues(inspect kubernetesClusterInspectEvidence) []string {
	var issues []string
	if len(inspect.Nodes) == 0 {
		issues = append(issues, "inspect evidence has no nodes")
	}
	for _, node := range inspect.Nodes {
		if node.Ready {
			continue
		}
		name := strings.TrimSpace(node.Name)
		if name == "" {
			name = "<unknown>"
		}
		if strings.TrimSpace(node.ReadyReason) != "" {
			name += "(" + strings.TrimSpace(node.ReadyReason) + ")"
		}
		issues = append(issues, "node "+name+" is not Ready")
	}
	for _, pod := range inspect.CorePods {
		name := strings.TrimSpace(pod.Namespace)
		if strings.TrimSpace(pod.Name) != "" {
			if name != "" {
				name += "/"
			}
			name += strings.TrimSpace(pod.Name)
		}
		if name == "" {
			name = "<unknown>"
		}
		phase := strings.TrimSpace(pod.Phase)
		if phase == "Succeeded" {
			continue
		}
		if phase != "" && phase != "Running" {
			issues = append(issues, "pod "+name+" is "+phase)
			continue
		}
		if pod.TotalContainers > 0 && pod.ReadyContainers < pod.TotalContainers {
			issues = append(issues, fmt.Sprintf("pod %s has %d/%d ready containers", name, pod.ReadyContainers, pod.TotalContainers))
		}
	}
	sort.Strings(issues)
	return issues
}

func (e *customNodeExecutor) evaluateKubernetesMaintenanceWindowPolicy(window KubernetesMaintenanceWindowSpec, addCheck func(string, string, string)) {
	if !kubernetesMaintenanceWindowConfigured(window) {
		addCheck("maintenanceWindow", "skipped", "no maintenance window policy configured")
		return
	}
	inside, message, err := kubernetesMaintenanceWindowAllows(window, time.Now())
	if err != nil {
		addCheck("maintenanceWindow", "blocked", err.Error())
		return
	}
	if !inside {
		addCheck("maintenanceWindow", "blocked", message)
		return
	}
	addCheck("maintenanceWindow", "passed", message)
}

func (e *customNodeExecutor) evaluateKubernetesLifecycleAppProbePolicy(ctx context.Context, probes []KubernetesAppProbe, runners []kubernetesCertTargetRunner, addCheck func(string, string, string), decision *kubernetesLifecyclePolicyDecision) {
	if len(probes) == 0 {
		addCheck("appProbes", "skipped", "no lifecycle policy app probes configured")
		return
	}
	if len(runners) == 0 {
		addCheck("appProbes", "blocked", "lifecycle policy app probes require at least one maintenance target runner")
		return
	}
	allMatched := true
	for _, probe := range probes {
		receipt := runners[0].runAccess(ctx, probe.Command)
		expect := strings.TrimSpace(probe.Expect)
		matched := nodeStepSucceeded(receipt.Status)
		if matched && expect != "" {
			matched = strings.Contains(receipt.Stdout, expect) || strings.Contains(receipt.Stderr, expect)
		}
		if !matched {
			allMatched = false
		}
		decision.AppProbes = append(decision.AppProbes, kubernetesLifecyclePolicyAppProbeEvidence{
			ID:      strings.TrimSpace(probe.ID),
			Expect:  expect,
			Receipt: compactKubernetesClusterInspectReceipt(receipt),
			Matched: matched,
		})
	}
	if allMatched {
		addCheck("appProbes", "passed", fmt.Sprintf("%d lifecycle policy app probe(s) matched", len(probes)))
		return
	}
	addCheck("appProbes", "blocked", "one or more lifecycle policy app probes failed")
}

func kubernetesLifecyclePolicyFinalize(decision *kubernetesLifecyclePolicyDecision) {
	if decision == nil {
		return
	}
	for _, check := range decision.Checks {
		if check.Status == "blocked" {
			decision.Status = "blocked"
			decision.Message = "lifecycle policy blocked: " + strings.TrimSpace(check.Message)
			return
		}
	}
	decision.Status = "allowed"
	decision.Message = "lifecycle policy allowed"
}

func (e *customNodeExecutor) evaluateKubernetesLifecyclePolicyOverride(node *runNode, certs KubernetesCertSpec, policyDecision kubernetesLifecyclePolicyDecision, runners []kubernetesCertTargetRunner) kubernetesLifecyclePolicyOverrideDecision {
	override := certs.Policy.Override
	runtime := kubernetesLifecyclePolicyOverrideRuntime(node, runners)
	if e != nil && e.run != nil {
		runtime.RunID = strings.TrimSpace(e.run.RunID)
	}
	decision := kubernetesLifecyclePolicyOverrideDecision{
		APIVersion:            "torque.dev/stack-lifecycle/v1",
		Kind:                  "KubernetesLifecyclePolicyOverride",
		NodeID:                strings.TrimSpace(node.ID),
		NodeKind:              normalizeNodeKind(node.Kind),
		Status:                "approved",
		Message:               "lifecycle policy override approved",
		RuntimeEnabled:        e != nil && e.run != nil && e.run.PolicyOverride,
		OriginalPolicyStatus:  strings.TrimSpace(policyDecision.Status),
		OriginalPolicyMessage: strings.TrimSpace(policyDecision.Message),
		Approval:              override,
		RuntimeScope:          runtime,
	}
	addCheck := func(id string, status string, message string) {
		status = strings.TrimSpace(status)
		if status == "" {
			status = "passed"
		}
		decision.Checks = append(decision.Checks, kubernetesLifecyclePolicyCheck{
			ID:      strings.TrimSpace(id),
			Status:  status,
			Message: strings.TrimSpace(message),
		})
		if status == "blocked" {
			decision.Status = "rejected"
		}
	}
	if !decision.RuntimeEnabled {
		addCheck("runtime.enabled", "blocked", "--policy-override was not set")
	} else {
		addCheck("runtime.enabled", "passed", "--policy-override was set")
	}
	if !kubernetesLifecyclePolicyOverrideConfigured(override) {
		addCheck("approval.configured", "blocked", "no lifecycle policy override approval is configured")
	} else {
		addCheck("approval.configured", "passed", "lifecycle policy override approval is configured")
	}
	if strings.TrimSpace(override.Reason) == "" {
		addCheck("approval.reason", "blocked", "override reason is required")
	} else {
		addCheck("approval.reason", "passed", "override reason is present")
	}
	if strings.TrimSpace(override.ChangeID) == "" && strings.TrimSpace(override.Ticket) == "" {
		addCheck("approval.change", "blocked", "override changeId or ticket is required")
	} else {
		addCheck("approval.change", "passed", "override change reference is present")
	}
	if strings.TrimSpace(override.Approver) == "" {
		addCheck("approval.approver", "blocked", "override approver is required")
	} else {
		addCheck("approval.approver", "passed", "override approver is present")
	}
	if expiresAt := strings.TrimSpace(override.ExpiresAt); expiresAt == "" {
		addCheck("approval.expiresAt", "blocked", "override expiresAt is required")
	} else if ts, err := time.Parse(time.RFC3339, expiresAt); err != nil {
		addCheck("approval.expiresAt", "blocked", "override expiresAt must be RFC3339")
	} else if !time.Now().UTC().Before(ts.UTC()) {
		addCheck("approval.expiresAt", "blocked", "override approval is expired")
	} else {
		addCheck("approval.expiresAt", "passed", "override approval is not expired")
	}
	if strings.TrimSpace(override.Scope.NodeID) == "" {
		addCheck("scope.nodeId", "blocked", "override scope.nodeId is required")
	} else if strings.TrimSpace(override.Scope.NodeID) != runtime.NodeID {
		addCheck("scope.nodeId", "blocked", fmt.Sprintf("override scope.nodeId %q does not match node %q", strings.TrimSpace(override.Scope.NodeID), runtime.NodeID))
	} else {
		addCheck("scope.nodeId", "passed", "override is scoped to this node")
	}
	evaluateKubernetesLifecycleOverrideRunOrIntent(override.Scope, runtime, addCheck)
	evaluateKubernetesLifecycleOverrideTargets(override.Scope, runtime, addCheck)
	kubernetesLifecyclePolicyOverrideFinalize(&decision)
	return decision
}

func kubernetesLifecyclePolicyOverrideRuntime(node *runNode, runners []kubernetesCertTargetRunner) kubernetesLifecyclePolicyOverrideRuntimeScope {
	var runtime kubernetesLifecyclePolicyOverrideRuntimeScope
	if node != nil {
		runtime.NodeID = strings.TrimSpace(node.ID)
		runtime.NodeName = strings.TrimSpace(node.Name)
		runtime.IntentDigest = strings.TrimSpace(node.EffectiveInputHash)
	}
	targetIDs := make([]string, 0, len(runners))
	type targetHash struct {
		ID           string `json:"id"`
		Role         string `json:"role,omitempty"`
		Provider     string `json:"provider,omitempty"`
		Transport    string `json:"transport,omitempty"`
		TargetDigest string `json:"targetDigest,omitempty"`
		NodeAddress  string `json:"nodeAddress,omitempty"`
		TargetEnv    string `json:"targetEnv,omitempty"`
	}
	targets := make([]targetHash, 0, len(runners))
	for _, runner := range runners {
		id := strings.TrimSpace(runner.spec.ID)
		if id != "" {
			targetIDs = append(targetIDs, id)
		}
		targets = append(targets, targetHash{
			ID:           id,
			Role:         strings.TrimSpace(runner.spec.Role),
			Provider:     strings.TrimSpace(runner.spec.Provider),
			Transport:    strings.TrimSpace(runner.spec.Transport),
			TargetDigest: runner.runner.TargetDigest(),
			NodeAddress:  strings.TrimSpace(runner.spec.NodeAddress),
			TargetEnv:    strings.TrimSpace(runner.spec.TargetEnv),
		})
	}
	sort.Strings(targetIDs)
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].ID == targets[j].ID {
			return targets[i].TargetDigest < targets[j].TargetDigest
		}
		return targets[i].ID < targets[j].ID
	})
	runtime.TargetIDs = targetIDs
	if digest, err := hashJSONStable(struct {
		Targets []targetHash `json:"targets"`
	}{Targets: targets}); err == nil {
		runtime.TargetSetDigest = digest
	}
	return runtime
}

func evaluateKubernetesLifecycleOverrideRunOrIntent(scope KubernetesLifecyclePolicyOverrideScopeSpec, runtime kubernetesLifecyclePolicyOverrideRuntimeScope, addCheck func(string, string, string)) {
	runID := strings.TrimSpace(scope.RunID)
	intentDigest := strings.TrimSpace(scope.IntentDigest)
	if runID == "" && intentDigest == "" {
		addCheck("scope.runOrIntent", "blocked", "override scope requires runId or intentDigest")
		return
	}
	if runID != "" && runID != runtime.RunID {
		addCheck("scope.runId", "blocked", fmt.Sprintf("override scope.runId %q does not match run %q", runID, runtime.RunID))
	} else if runID != "" {
		addCheck("scope.runId", "passed", "override is scoped to this run")
	}
	if intentDigest != "" && intentDigest != runtime.IntentDigest {
		addCheck("scope.intentDigest", "blocked", "override scope.intentDigest does not match current node intent")
	} else if intentDigest != "" {
		addCheck("scope.intentDigest", "passed", "override is scoped to current node intent")
	}
}

func evaluateKubernetesLifecycleOverrideTargets(scope KubernetesLifecyclePolicyOverrideScopeSpec, runtime kubernetesLifecyclePolicyOverrideRuntimeScope, addCheck func(string, string, string)) {
	wantDigest := strings.TrimSpace(scope.TargetSetDigest)
	wantIDs := normalizedKubernetesLifecycleOverrideTargetIDs(scope.TargetIDs)
	if wantDigest == "" && len(wantIDs) == 0 {
		addCheck("scope.targets", "blocked", "override scope requires targetSetDigest or targetIds")
		return
	}
	if wantDigest != "" {
		if wantDigest != runtime.TargetSetDigest {
			addCheck("scope.targetSetDigest", "blocked", "override scope.targetSetDigest does not match current target set")
		} else {
			addCheck("scope.targetSetDigest", "passed", "override is scoped to current target set digest")
		}
	}
	if len(wantIDs) > 0 {
		gotIDs := append([]string(nil), runtime.TargetIDs...)
		if !stringSlicesEqual(wantIDs, gotIDs) {
			addCheck("scope.targetIds", "blocked", fmt.Sprintf("override targetIds %s do not match current targetIds %s", strings.Join(wantIDs, ","), strings.Join(gotIDs, ",")))
		} else {
			addCheck("scope.targetIds", "passed", "override is scoped to current target IDs")
		}
	}
}

func normalizedKubernetesLifecycleOverrideTargetIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			out = append(out, strings.TrimSpace(id))
		}
	}
	sort.Strings(out)
	return out
}

func stringSlicesEqual(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func kubernetesLifecyclePolicyOverrideFinalize(decision *kubernetesLifecyclePolicyOverrideDecision) {
	if decision == nil {
		return
	}
	for _, check := range decision.Checks {
		if check.Status == "blocked" {
			decision.Status = "rejected"
			decision.Message = "lifecycle policy override rejected: " + strings.TrimSpace(check.Message)
			return
		}
	}
	ref := firstNonEmptyString(decision.Approval.ChangeID, decision.Approval.Ticket)
	if ref != "" {
		decision.Message = "lifecycle policy override approved: " + ref
	} else {
		decision.Message = "lifecycle policy override approved"
	}
	decision.Status = "approved"
}

func (e *customNodeExecutor) loadKubernetesClusterInspectArtifact(ctx context.Context, nodeID string, artifactName string) (kubernetesClusterInspectEvidence, RunArtifact, error) {
	if e == nil || e.run == nil || e.run.store == nil {
		return kubernetesClusterInspectEvidence{}, RunArtifact{}, fmt.Errorf("run state store is not available")
	}
	artifacts, err := e.run.store.ListArtifacts(ctx, e.run.RunID)
	if err != nil {
		return kubernetesClusterInspectEvidence{}, RunArtifact{}, err
	}
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.NodeID) != strings.TrimSpace(nodeID) || strings.TrimSpace(artifact.Name) != strings.TrimSpace(artifactName) {
			continue
		}
		var payload struct {
			Evidence kubernetesClusterInspectEvidence `json:"evidence"`
		}
		if err := json.Unmarshal([]byte(artifact.Body), &payload); err != nil {
			return kubernetesClusterInspectEvidence{}, artifact, fmt.Errorf("parse %s/%s: %w", nodeID, artifactName, err)
		}
		if strings.TrimSpace(payload.Evidence.Kind) != "KubernetesClusterInspect" {
			return kubernetesClusterInspectEvidence{}, artifact, fmt.Errorf("%s/%s is not KubernetesClusterInspect evidence", nodeID, artifactName)
		}
		if strings.TrimSpace(payload.Evidence.Status) != "succeeded" {
			return kubernetesClusterInspectEvidence{}, artifact, fmt.Errorf("%s/%s is not succeeded inspect evidence", nodeID, artifactName)
		}
		return payload.Evidence, artifact, nil
	}
	return kubernetesClusterInspectEvidence{}, RunArtifact{}, fmt.Errorf("missing Kubernetes cluster inspect artifact %s on node %s", artifactName, nodeID)
}

func kubernetesMaintenanceWindowConfigured(spec KubernetesMaintenanceWindowSpec) bool {
	return strings.TrimSpace(spec.Start) != "" ||
		strings.TrimSpace(spec.End) != "" ||
		strings.TrimSpace(spec.TimeZone) != "" ||
		len(spec.Days) > 0
}

func kubernetesMaintenanceWindowAllows(spec KubernetesMaintenanceWindowSpec, now time.Time) (bool, string, error) {
	location := time.UTC
	if strings.TrimSpace(spec.TimeZone) != "" {
		loaded, err := time.LoadLocation(strings.TrimSpace(spec.TimeZone))
		if err != nil {
			return false, "", err
		}
		location = loaded
	}
	now = now.In(location)
	start, err := kubernetesMaintenanceWindowMinute(spec.Start)
	if err != nil {
		return false, "", err
	}
	end, err := kubernetesMaintenanceWindowMinute(spec.End)
	if err != nil {
		return false, "", err
	}
	current := now.Hour()*60 + now.Minute()
	inside := false
	switch {
	case start == end:
		inside = true
	case start < end:
		inside = current >= start && current < end
	default:
		inside = current >= start || current < end
	}
	allowedDays, dayNames, err := kubernetesMaintenanceWindowDays(spec.Days)
	if err != nil {
		return false, "", err
	}
	windowDay := now.Weekday()
	if start > end && current < end {
		windowDay = now.Add(-24 * time.Hour).Weekday()
	}
	if len(allowedDays) > 0 {
		if _, ok := allowedDays[windowDay]; !ok {
			return false, fmt.Sprintf("current maintenance day %s is outside allowed days %s", strings.ToLower(windowDay.String()), strings.Join(dayNames, ",")), nil
		}
	}
	if !inside {
		return false, fmt.Sprintf("current maintenance time %s is outside window %s-%s %s", now.Format("15:04"), strings.TrimSpace(spec.Start), strings.TrimSpace(spec.End), location.String()), nil
	}
	return true, fmt.Sprintf("current maintenance time %s is inside window %s-%s %s", now.Format("15:04"), strings.TrimSpace(spec.Start), strings.TrimSpace(spec.End), location.String()), nil
}

func kubernetesMaintenanceWindowMinute(raw string) (int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(raw))
	if err != nil {
		return 0, err
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func kubernetesMaintenanceWindowDays(days []string) (map[time.Weekday]struct{}, []string, error) {
	if len(days) == 0 {
		return nil, nil, nil
	}
	out := map[time.Weekday]struct{}{}
	var names []string
	for _, raw := range days {
		day, name, ok := kubernetesMaintenanceWindowDay(raw)
		if !ok {
			return nil, nil, fmt.Errorf("unsupported day %q", raw)
		}
		out[day] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return out, names, nil
}

func kubernetesMaintenanceWindowDay(raw string) (time.Weekday, string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "sun", "sunday":
		return time.Sunday, "sunday", true
	case "mon", "monday":
		return time.Monday, "monday", true
	case "tue", "tues", "tuesday":
		return time.Tuesday, "tuesday", true
	case "wed", "wednesday":
		return time.Wednesday, "wednesday", true
	case "thu", "thur", "thurs", "thursday":
		return time.Thursday, "thursday", true
	case "fri", "friday":
		return time.Friday, "friday", true
	case "sat", "saturday":
		return time.Saturday, "saturday", true
	default:
		return time.Sunday, "", false
	}
}
