package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const kubernetesLifecycleSummaryArtifact = "k8s-lifecycle-summary.json"

type kubernetesLifecycleSummary struct {
	APIVersion         string                                     `json:"apiVersion"`
	Kind               string                                     `json:"kind"`
	NodeID             string                                     `json:"nodeId"`
	NodeKind           string                                     `json:"nodeKind"`
	Status             string                                     `json:"status"`
	Message            string                                     `json:"message"`
	SourceArtifacts    []kubernetesLifecycleSourceArtifact        `json:"sourceArtifacts,omitempty"`
	Inspect            *kubernetesLifecycleInspectSummary         `json:"inspect,omitempty"`
	CertificateInspect *kubernetesLifecycleCertificateSummary     `json:"certificateInspect,omitempty"`
	Policy             *kubernetesLifecyclePolicySummary          `json:"policy,omitempty"`
	PolicyOverride     *kubernetesLifecyclePolicyOverrideSummary  `json:"policyOverride,omitempty"`
	CertificateRenew   *kubernetesLifecycleCertificateSummary     `json:"certificateRenew,omitempty"`
	Verify             *kubernetesLifecycleClusterVerifySummary   `json:"verify,omitempty"`
	ApplicationGate    *kubernetesLifecycleApplicationGateSummary `json:"applicationGate,omitempty"`
}

type kubernetesLifecycleSourceArtifact struct {
	NodeID    string `json:"nodeId"`
	Name      string `json:"name"`
	Phase     string `json:"phase,omitempty"`
	Kind      string `json:"kind,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	SizeBytes int    `json:"sizeBytes,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type kubernetesLifecycleInspectSummary struct {
	SourceArtifact       string                                     `json:"sourceArtifact"`
	SourceArtifactDigest string                                     `json:"sourceArtifactDigest,omitempty"`
	Status               string                                     `json:"status"`
	Message              string                                     `json:"message,omitempty"`
	API                  kubernetesLifecycleAPISummary              `json:"api,omitempty"`
	Provider             kubernetesClusterInspectProvider           `json:"provider"`
	CertificateRenewal   kubernetesClusterInspectCertificateRenewal `json:"certificateRenewal"`
	Topology             kubernetesLifecycleTopologySummary         `json:"topology"`
	Namespaces           kubernetesLifecycleCountSummary            `json:"namespaces"`
	CorePods             kubernetesLifecycleCountSummary            `json:"corePods"`
}

type kubernetesLifecycleAPISummary struct {
	Server        string `json:"server,omitempty"`
	ServerVersion string `json:"serverVersion,omitempty"`
	ClientVersion string `json:"clientVersion,omitempty"`
}

type kubernetesLifecycleTopologySummary struct {
	TotalNodes        int      `json:"totalNodes"`
	ReadyNodes        int      `json:"readyNodes"`
	ControlPlaneNodes int      `json:"controlPlaneNodes"`
	WorkerNodes       int      `json:"workerNodes"`
	UnknownRoleNodes  int      `json:"unknownRoleNodes,omitempty"`
	NotReadyNodes     []string `json:"notReadyNodes,omitempty"`
	KubeletVersions   []string `json:"kubeletVersions,omitempty"`
}

type kubernetesLifecycleCountSummary struct {
	Count int `json:"count"`
}

type kubernetesLifecycleCertificateSummary struct {
	SourceArtifact       string                                 `json:"sourceArtifact"`
	SourceArtifactDigest string                                 `json:"sourceArtifactDigest,omitempty"`
	Phase                string                                 `json:"phase"`
	Status               string                                 `json:"status"`
	Message              string                                 `json:"message,omitempty"`
	Changed              bool                                   `json:"changed,omitempty"`
	Provider             string                                 `json:"provider,omitempty"`
	RenewBefore          string                                 `json:"renewBefore,omitempty"`
	Force                bool                                   `json:"force,omitempty"`
	ForceOnceID          string                                 `json:"forceOnceId,omitempty"`
	Order                string                                 `json:"order,omitempty"`
	BatchSize            int                                    `json:"batchSize,omitempty"`
	TargetCount          int                                    `json:"targetCount"`
	TargetsFrom          *kubernetesLifecycleTargetsFromSummary `json:"targetsFrom,omitempty"`
	Targets              []kubernetesLifecycleCertificateTarget `json:"targets,omitempty"`
}

type kubernetesLifecycleTargetsFromSummary struct {
	SourceNodeID         string                                    `json:"sourceNodeId"`
	Artifact             string                                    `json:"artifact"`
	SourceArtifactDigest string                                    `json:"sourceArtifactDigest,omitempty"`
	Provider             string                                    `json:"provider,omitempty"`
	AddressType          string                                    `json:"addressType,omitempty"`
	RoleFilter           []string                                  `json:"roleFilter,omitempty"`
	IncludeNotReady      bool                                      `json:"includeNotReady,omitempty"`
	DerivedCount         int                                       `json:"derivedCount"`
	Skipped              []kubernetesCertTargetsFromSkipEvidence   `json:"skipped,omitempty"`
	Targets              []kubernetesCertTargetsFromTargetEvidence `json:"targets,omitempty"`
}

type kubernetesLifecycleCertificateTarget struct {
	ID               string `json:"id"`
	Role             string `json:"role,omitempty"`
	Provider         string `json:"provider,omitempty"`
	Batch            int    `json:"batch,omitempty"`
	CheckpointStatus string `json:"checkpointStatus,omitempty"`
	CheckpointPhase  string `json:"checkpointPhase,omitempty"`
	IntentDigest     string `json:"intentDigest,omitempty"`
	HealthDigest     string `json:"healthDigest,omitempty"`
	TargetDigest     string `json:"targetDigest,omitempty"`
	CertificateCount int    `json:"certificateCount,omitempty"`
	EarliestExpiry   string `json:"earliestExpiry,omitempty"`
	RenewalNeeded    bool   `json:"renewalNeeded,omitempty"`
	Renewed          bool   `json:"renewed,omitempty"`
	SkippedReason    string `json:"skippedReason,omitempty"`
}

type kubernetesLifecyclePolicySummary struct {
	SourceArtifact       string                                      `json:"sourceArtifact"`
	SourceArtifactDigest string                                      `json:"sourceArtifactDigest,omitempty"`
	Status               string                                      `json:"status"`
	Message              string                                      `json:"message,omitempty"`
	TargetCount          int                                         `json:"targetCount"`
	BatchSize            int                                         `json:"batchSize"`
	MaxUnavailable       int                                         `json:"maxUnavailable,omitempty"`
	Inspect              *kubernetesLifecyclePolicyInspectRef        `json:"inspect,omitempty"`
	Checks               []kubernetesLifecyclePolicyCheck            `json:"checks,omitempty"`
	AppProbes            []kubernetesLifecyclePolicyAppProbeEvidence `json:"appProbes,omitempty"`
}

type kubernetesLifecyclePolicyOverrideSummary struct {
	SourceArtifact       string                                        `json:"sourceArtifact"`
	SourceArtifactDigest string                                        `json:"sourceArtifactDigest,omitempty"`
	Status               string                                        `json:"status"`
	Message              string                                        `json:"message,omitempty"`
	RuntimeEnabled       bool                                          `json:"runtimeEnabled,omitempty"`
	OriginalPolicyStatus string                                        `json:"originalPolicyStatus,omitempty"`
	Approval             KubernetesLifecyclePolicyOverrideSpec         `json:"approval,omitempty"`
	RuntimeScope         kubernetesLifecyclePolicyOverrideRuntimeScope `json:"runtimeScope"`
	Checks               []kubernetesLifecyclePolicyCheck              `json:"checks,omitempty"`
}

type kubernetesLifecycleClusterVerifySummary struct {
	SourceArtifact       string                                      `json:"sourceArtifact"`
	SourceArtifactDigest string                                      `json:"sourceArtifactDigest,omitempty"`
	Status               string                                      `json:"status"`
	Message              string                                      `json:"message,omitempty"`
	StableIterations     int                                         `json:"stableIterations,omitempty"`
	StableInterval       string                                      `json:"stableInterval,omitempty"`
	TotalNodes           int                                         `json:"totalNodes"`
	ReadyNodes           int                                         `json:"readyNodes"`
	NotReadyNodes        []string                                    `json:"notReadyNodes,omitempty"`
	Namespaces           []kubernetesLifecycleVerifyNamespaceSummary `json:"namespaces,omitempty"`
	AppProbes            []kubernetesLifecycleAppProbeSummary        `json:"appProbes,omitempty"`
}

type kubernetesLifecycleApplicationGateSummary struct {
	Status                     string                               `json:"status"`
	Message                    string                               `json:"message,omitempty"`
	BeforeSourceArtifact       string                               `json:"beforeSourceArtifact,omitempty"`
	BeforeSourceArtifactDigest string                               `json:"beforeSourceArtifactDigest,omitempty"`
	AfterSourceArtifact        string                               `json:"afterSourceArtifact,omitempty"`
	AfterSourceArtifactDigest  string                               `json:"afterSourceArtifactDigest,omitempty"`
	BeforeProbes               []kubernetesLifecycleAppProbeSummary `json:"beforeProbes,omitempty"`
	AfterProbes                []kubernetesLifecycleAppProbeSummary `json:"afterProbes,omitempty"`
}

type kubernetesLifecycleVerifyNamespaceSummary struct {
	Namespace string   `json:"namespace"`
	TotalPods int      `json:"totalPods"`
	ReadyPods int      `json:"readyPods"`
	BadPods   []string `json:"badPods,omitempty"`
}

type kubernetesLifecycleAppProbeSummary struct {
	ID      string                          `json:"id"`
	Expect  string                          `json:"expect,omitempty"`
	Matched bool                            `json:"matched,omitempty"`
	Receipt kubernetesClusterInspectReceipt `json:"receipt"`
}

type kubernetesLifecycleCertificatePayload struct {
	Kind        string                             `json:"kind"`
	NodeID      string                             `json:"nodeId"`
	NodeKind    string                             `json:"nodeKind"`
	Phase       string                             `json:"phase"`
	Status      string                             `json:"status"`
	Message     string                             `json:"message"`
	Changed     bool                               `json:"changed"`
	Provider    string                             `json:"provider"`
	RenewBefore string                             `json:"renewBefore"`
	Force       bool                               `json:"force"`
	ForceOnceID string                             `json:"forceOnceId"`
	Order       string                             `json:"order"`
	BatchSize   int                                `json:"batchSize"`
	TargetCount int                                `json:"targetCount"`
	TargetsFrom *kubernetesCertTargetsFromEvidence `json:"targetsFrom,omitempty"`
	Targets     []kubernetesCertTargetEvidence     `json:"targets"`
}

func (e *customNodeExecutor) recordKubernetesLifecycleSummary(ctx context.Context, node *runNode) {
	summary, err := e.buildKubernetesLifecycleSummary(ctx, node)
	if err != nil {
		return
	}
	e.run.RecordJSONArtifact(node.ID, kubernetesLifecycleSummaryArtifact, summary)
}

func (e *customNodeExecutor) buildKubernetesLifecycleSummary(ctx context.Context, node *runNode) (*kubernetesLifecycleSummary, error) {
	if e == nil || e.run == nil || e.run.store == nil {
		return nil, fmt.Errorf("run state store is not available")
	}
	artifacts, err := e.run.store.ListArtifacts(ctx, e.run.RunID)
	if err != nil {
		return nil, err
	}
	sourceByKey := map[string]kubernetesLifecycleSourceArtifact{}
	nodeScope := kubernetesLifecycleSummaryNodeScope(e.run.Plan, node.ID)
	summary := &kubernetesLifecycleSummary{
		APIVersion: "torque.dev/stack-lifecycle/v1",
		Kind:       "KubernetesLifecycleSummary",
		NodeID:     strings.TrimSpace(node.ID),
		NodeKind:   normalizeNodeKind(node.Kind),
		Status:     "succeeded",
		Message:    "lifecycle evidence summarized",
	}
	for _, artifact := range artifacts {
		if !kubernetesLifecycleSummaryArtifactInScope(nodeScope, artifact) {
			continue
		}
		source, ok := kubernetesLifecycleSourceFromArtifact(artifact)
		if !ok {
			continue
		}
		summary.SourceArtifacts = append(summary.SourceArtifacts, source)
		sourceByKey[kubernetesLifecycleArtifactKey(artifact.NodeID, artifact.Name)] = source
	}
	if len(summary.SourceArtifacts) == 0 {
		return nil, fmt.Errorf("no Kubernetes lifecycle source artifacts found")
	}
	sort.Slice(summary.SourceArtifacts, func(i, j int) bool {
		if summary.SourceArtifacts[i].Phase == summary.SourceArtifacts[j].Phase {
			if summary.SourceArtifacts[i].NodeID == summary.SourceArtifacts[j].NodeID {
				return summary.SourceArtifacts[i].Name < summary.SourceArtifacts[j].Name
			}
			return summary.SourceArtifacts[i].NodeID < summary.SourceArtifacts[j].NodeID
		}
		return kubernetesLifecyclePhaseRank(summary.SourceArtifacts[i].Phase) < kubernetesLifecyclePhaseRank(summary.SourceArtifacts[j].Phase)
	})
	for _, artifact := range artifacts {
		if !kubernetesLifecycleSummaryArtifactInScope(nodeScope, artifact) {
			continue
		}
		switch strings.TrimSpace(artifact.Name) {
		case "k8s-cluster-inspect.json":
			inspect, err := kubernetesLifecycleSummarizeInspect(artifact, sourceByKey)
			if err == nil && summary.Inspect == nil {
				summary.Inspect = inspect
			}
		case "k8s-cert-inspect.json":
			certs, err := kubernetesLifecycleSummarizeCertificate(artifact, sourceByKey)
			if err == nil && summary.CertificateInspect == nil {
				summary.CertificateInspect = certs
			}
		case kubernetesLifecyclePolicyDecisionArtifact:
			policy, err := kubernetesLifecycleSummarizePolicy(artifact, sourceByKey)
			if err == nil && summary.Policy == nil {
				summary.Policy = policy
			}
		case kubernetesLifecyclePolicyOverrideArtifact:
			override, err := kubernetesLifecycleSummarizePolicyOverride(artifact, sourceByKey)
			if err == nil && summary.PolicyOverride == nil {
				summary.PolicyOverride = override
			}
		case "k8s-cert-renew.json":
			certs, err := kubernetesLifecycleSummarizeCertificate(artifact, sourceByKey)
			if err == nil && summary.CertificateRenew == nil {
				summary.CertificateRenew = certs
			}
		case "k8s-cluster-verify.json":
			if strings.TrimSpace(artifact.NodeID) != strings.TrimSpace(node.ID) {
				continue
			}
			verify, err := kubernetesLifecycleSummarizeVerify(artifact, sourceByKey)
			if err == nil {
				summary.Verify = verify
				if strings.TrimSpace(verify.Status) != "" {
					summary.Status = strings.TrimSpace(verify.Status)
				}
				if strings.TrimSpace(verify.Message) != "" {
					summary.Message = strings.TrimSpace(verify.Message)
				}
			}
		}
	}
	summary.ApplicationGate = kubernetesLifecycleBuildApplicationGate(summary.Policy, summary.Verify)
	if summary.ApplicationGate != nil && summary.ApplicationGate.Status == "failed" {
		summary.Status = "failed"
		summary.Message = summary.ApplicationGate.Message
	}
	return summary, nil
}

func kubernetesLifecycleSourceFromArtifact(artifact RunArtifact) (kubernetesLifecycleSourceArtifact, bool) {
	phase := kubernetesLifecycleArtifactPhase(artifact.Name)
	if phase == "" {
		return kubernetesLifecycleSourceArtifact{}, false
	}
	kind := ""
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(artifact.Body), &header); err == nil {
		kind = strings.TrimSpace(header.Kind)
	}
	return kubernetesLifecycleSourceArtifact{
		NodeID:    strings.TrimSpace(artifact.NodeID),
		Name:      strings.TrimSpace(artifact.Name),
		Phase:     phase,
		Kind:      kind,
		SHA256:    kubernetesLifecycleArtifactDigest(artifact),
		SizeBytes: artifact.SizeBytes,
		CreatedAt: strings.TrimSpace(artifact.CreatedAt),
	}, true
}

func kubernetesLifecycleSummarizeInspect(artifact RunArtifact, sourceByKey map[string]kubernetesLifecycleSourceArtifact) (*kubernetesLifecycleInspectSummary, error) {
	var payload struct {
		Status   string                           `json:"status"`
		Message  string                           `json:"message"`
		Evidence kubernetesClusterInspectEvidence `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(artifact.Body), &payload); err != nil {
		return nil, err
	}
	evidence := payload.Evidence
	out := &kubernetesLifecycleInspectSummary{
		SourceArtifact:       kubernetesLifecycleArtifactRef(artifact),
		SourceArtifactDigest: kubernetesLifecycleSourceDigest(sourceByKey, artifact.NodeID, artifact.Name),
		Status:               firstNonEmptyString(strings.TrimSpace(evidence.Status), strings.TrimSpace(payload.Status)),
		Message:              firstNonEmptyString(strings.TrimSpace(evidence.Message), strings.TrimSpace(payload.Message)),
		API: kubernetesLifecycleAPISummary{
			Server:        strings.TrimSpace(evidence.API.Server),
			ServerVersion: strings.TrimSpace(evidence.API.ServerVersion.GitVersion),
			ClientVersion: strings.TrimSpace(evidence.API.ClientVersion.GitVersion),
		},
		Provider:           evidence.Provider,
		CertificateRenewal: evidence.CertificateRenewal,
		Namespaces:         kubernetesLifecycleCountSummary{Count: len(evidence.Namespaces)},
		CorePods:           kubernetesLifecycleCountSummary{Count: len(evidence.CorePods)},
	}
	versions := map[string]struct{}{}
	for _, n := range evidence.Nodes {
		out.Topology.TotalNodes++
		if n.Ready {
			out.Topology.ReadyNodes++
		} else if strings.TrimSpace(n.Name) != "" {
			out.Topology.NotReadyNodes = append(out.Topology.NotReadyNodes, strings.TrimSpace(n.Name))
		}
		role := kubernetesLifecyclePrimaryRole(n.Roles)
		switch role {
		case "control-plane":
			out.Topology.ControlPlaneNodes++
		case "worker":
			out.Topology.WorkerNodes++
		default:
			out.Topology.UnknownRoleNodes++
		}
		if strings.TrimSpace(n.KubeletVersion) != "" {
			versions[strings.TrimSpace(n.KubeletVersion)] = struct{}{}
		}
	}
	for version := range versions {
		out.Topology.KubeletVersions = append(out.Topology.KubeletVersions, version)
	}
	sort.Strings(out.Topology.KubeletVersions)
	sort.Strings(out.Topology.NotReadyNodes)
	return out, nil
}

func kubernetesLifecycleSummarizeCertificate(artifact RunArtifact, sourceByKey map[string]kubernetesLifecycleSourceArtifact) (*kubernetesLifecycleCertificateSummary, error) {
	var payload kubernetesLifecycleCertificatePayload
	if err := json.Unmarshal([]byte(artifact.Body), &payload); err != nil {
		return nil, err
	}
	out := &kubernetesLifecycleCertificateSummary{
		SourceArtifact:       kubernetesLifecycleArtifactRef(artifact),
		SourceArtifactDigest: kubernetesLifecycleSourceDigest(sourceByKey, artifact.NodeID, artifact.Name),
		Phase:                strings.TrimSpace(payload.Phase),
		Status:               strings.TrimSpace(payload.Status),
		Message:              strings.TrimSpace(payload.Message),
		Changed:              payload.Changed,
		Provider:             strings.TrimSpace(payload.Provider),
		RenewBefore:          strings.TrimSpace(payload.RenewBefore),
		Force:                payload.Force,
		ForceOnceID:          strings.TrimSpace(payload.ForceOnceID),
		Order:                strings.TrimSpace(payload.Order),
		BatchSize:            payload.BatchSize,
		TargetCount:          payload.TargetCount,
		Targets:              make([]kubernetesLifecycleCertificateTarget, 0, len(payload.Targets)),
	}
	if payload.TargetsFrom != nil {
		out.TargetsFrom = &kubernetesLifecycleTargetsFromSummary{
			SourceNodeID:         strings.TrimSpace(payload.TargetsFrom.SourceNodeID),
			Artifact:             strings.TrimSpace(payload.TargetsFrom.Artifact),
			SourceArtifactDigest: kubernetesLifecycleSourceDigest(sourceByKey, payload.TargetsFrom.SourceNodeID, payload.TargetsFrom.Artifact),
			Provider:             strings.TrimSpace(payload.TargetsFrom.Provider),
			AddressType:          strings.TrimSpace(payload.TargetsFrom.AddressType),
			RoleFilter:           append([]string(nil), payload.TargetsFrom.RoleFilter...),
			IncludeNotReady:      payload.TargetsFrom.IncludeNotReady,
			DerivedCount:         payload.TargetsFrom.DerivedCount,
			Skipped:              append([]kubernetesCertTargetsFromSkipEvidence(nil), payload.TargetsFrom.Skipped...),
			Targets:              append([]kubernetesCertTargetsFromTargetEvidence(nil), payload.TargetsFrom.Targets...),
		}
	}
	for _, target := range payload.Targets {
		out.Targets = append(out.Targets, kubernetesLifecycleCertificateTarget{
			ID:               strings.TrimSpace(target.ID),
			Role:             strings.TrimSpace(target.Role),
			Provider:         strings.TrimSpace(target.Provider),
			Batch:            target.Batch,
			CheckpointStatus: strings.TrimSpace(target.CheckpointStatus),
			CheckpointPhase:  strings.TrimSpace(target.CheckpointPhase),
			IntentDigest:     strings.TrimSpace(target.IntentDigest),
			HealthDigest:     strings.TrimSpace(target.HealthDigest),
			TargetDigest:     strings.TrimSpace(target.TargetDigest),
			CertificateCount: target.CertificateCount,
			EarliestExpiry:   strings.TrimSpace(target.EarliestExpiry),
			RenewalNeeded:    target.RenewalNeeded,
			Renewed:          target.Renewed,
			SkippedReason:    strings.TrimSpace(target.SkippedReason),
		})
	}
	sort.Slice(out.Targets, func(i, j int) bool {
		return out.Targets[i].ID < out.Targets[j].ID
	})
	return out, nil
}

func kubernetesLifecycleSummarizePolicy(artifact RunArtifact, sourceByKey map[string]kubernetesLifecycleSourceArtifact) (*kubernetesLifecyclePolicySummary, error) {
	var payload kubernetesLifecyclePolicyDecision
	if err := json.Unmarshal([]byte(artifact.Body), &payload); err != nil {
		return nil, err
	}
	return &kubernetesLifecyclePolicySummary{
		SourceArtifact:       kubernetesLifecycleArtifactRef(artifact),
		SourceArtifactDigest: kubernetesLifecycleSourceDigest(sourceByKey, artifact.NodeID, artifact.Name),
		Status:               strings.TrimSpace(payload.Status),
		Message:              strings.TrimSpace(payload.Message),
		TargetCount:          payload.TargetCount,
		BatchSize:            payload.BatchSize,
		MaxUnavailable:       payload.MaxUnavailable,
		Inspect:              payload.Inspect,
		Checks:               append([]kubernetesLifecyclePolicyCheck(nil), payload.Checks...),
		AppProbes:            append([]kubernetesLifecyclePolicyAppProbeEvidence(nil), payload.AppProbes...),
	}, nil
}

func kubernetesLifecycleSummarizePolicyOverride(artifact RunArtifact, sourceByKey map[string]kubernetesLifecycleSourceArtifact) (*kubernetesLifecyclePolicyOverrideSummary, error) {
	var payload kubernetesLifecyclePolicyOverrideDecision
	if err := json.Unmarshal([]byte(artifact.Body), &payload); err != nil {
		return nil, err
	}
	return &kubernetesLifecyclePolicyOverrideSummary{
		SourceArtifact:       kubernetesLifecycleArtifactRef(artifact),
		SourceArtifactDigest: kubernetesLifecycleSourceDigest(sourceByKey, artifact.NodeID, artifact.Name),
		Status:               strings.TrimSpace(payload.Status),
		Message:              strings.TrimSpace(payload.Message),
		RuntimeEnabled:       payload.RuntimeEnabled,
		OriginalPolicyStatus: strings.TrimSpace(payload.OriginalPolicyStatus),
		Approval:             payload.Approval,
		RuntimeScope:         payload.RuntimeScope,
		Checks:               append([]kubernetesLifecyclePolicyCheck(nil), payload.Checks...),
	}, nil
}

func kubernetesLifecycleSummarizeVerify(artifact RunArtifact, sourceByKey map[string]kubernetesLifecycleSourceArtifact) (*kubernetesLifecycleClusterVerifySummary, error) {
	var payload struct {
		Status   string                          `json:"status"`
		Message  string                          `json:"message"`
		Evidence kubernetesClusterVerifyEvidence `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(artifact.Body), &payload); err != nil {
		return nil, err
	}
	evidence := payload.Evidence
	out := &kubernetesLifecycleClusterVerifySummary{
		SourceArtifact:       kubernetesLifecycleArtifactRef(artifact),
		SourceArtifactDigest: kubernetesLifecycleSourceDigest(sourceByKey, artifact.NodeID, artifact.Name),
		Status:               firstNonEmptyString(strings.TrimSpace(evidence.Status), strings.TrimSpace(payload.Status)),
		Message:              firstNonEmptyString(strings.TrimSpace(evidence.Message), strings.TrimSpace(payload.Message)),
		StableIterations:     evidence.StableIterations,
		StableInterval:       strings.TrimSpace(evidence.StableInterval),
		TotalNodes:           evidence.TotalNodes,
		ReadyNodes:           evidence.ReadyNodes,
		NotReadyNodes:        append([]string(nil), evidence.NotReadyNodes...),
	}
	for _, ns := range evidence.Namespaces {
		out.Namespaces = append(out.Namespaces, kubernetesLifecycleVerifyNamespaceSummary{
			Namespace: strings.TrimSpace(ns.Namespace),
			TotalPods: ns.TotalPods,
			ReadyPods: ns.ReadyPods,
			BadPods:   append([]string(nil), ns.BadPods...),
		})
	}
	for _, probe := range evidence.AppProbes {
		out.AppProbes = append(out.AppProbes, kubernetesLifecycleAppProbeSummary{
			ID:      strings.TrimSpace(probe.ID),
			Expect:  strings.TrimSpace(probe.Expect),
			Matched: probe.Matched,
			Receipt: compactKubernetesClusterInspectReceipt(probe.Receipt),
		})
	}
	return out, nil
}

func kubernetesLifecycleBuildApplicationGate(policy *kubernetesLifecyclePolicySummary, verify *kubernetesLifecycleClusterVerifySummary) *kubernetesLifecycleApplicationGateSummary {
	var before []kubernetesLifecycleAppProbeSummary
	var after []kubernetesLifecycleAppProbeSummary
	out := &kubernetesLifecycleApplicationGateSummary{}
	if policy != nil && len(policy.AppProbes) > 0 {
		out.BeforeSourceArtifact = policy.SourceArtifact
		out.BeforeSourceArtifactDigest = policy.SourceArtifactDigest
		before = kubernetesLifecyclePolicyAppProbeSummaries(policy.AppProbes)
	}
	if verify != nil && len(verify.AppProbes) > 0 {
		out.AfterSourceArtifact = verify.SourceArtifact
		out.AfterSourceArtifactDigest = verify.SourceArtifactDigest
		after = append([]kubernetesLifecycleAppProbeSummary(nil), verify.AppProbes...)
	}
	if len(before) == 0 && len(after) == 0 {
		return nil
	}
	sort.Slice(before, func(i, j int) bool { return before[i].ID < before[j].ID })
	sort.Slice(after, func(i, j int) bool { return after[i].ID < after[j].ID })
	out.BeforeProbes = before
	out.AfterProbes = after
	beforeMatched := kubernetesLifecycleAllAppProbesMatched(before)
	afterMatched := kubernetesLifecycleAllAppProbesMatched(after)
	switch {
	case len(before) == 0:
		out.Status = "incomplete"
		out.Message = "application gate has post-maintenance probes but no pre-maintenance probes"
	case len(after) == 0:
		out.Status = "incomplete"
		out.Message = "application gate has pre-maintenance probes but no post-maintenance probes"
	case beforeMatched && afterMatched:
		out.Status = "passed"
		out.Message = "application probes matched before and after maintenance"
	default:
		out.Status = "failed"
		out.Message = "one or more application probes failed before or after maintenance"
	}
	return out
}

func kubernetesLifecyclePolicyAppProbeSummaries(probes []kubernetesLifecyclePolicyAppProbeEvidence) []kubernetesLifecycleAppProbeSummary {
	out := make([]kubernetesLifecycleAppProbeSummary, 0, len(probes))
	for _, probe := range probes {
		out = append(out, kubernetesLifecycleAppProbeSummary{
			ID:      strings.TrimSpace(probe.ID),
			Expect:  strings.TrimSpace(probe.Expect),
			Matched: probe.Matched,
			Receipt: probe.Receipt,
		})
	}
	return out
}

func kubernetesLifecycleAllAppProbesMatched(probes []kubernetesLifecycleAppProbeSummary) bool {
	if len(probes) == 0 {
		return false
	}
	for _, probe := range probes {
		if !probe.Matched {
			return false
		}
	}
	return true
}

func kubernetesLifecycleArtifactPhase(name string) string {
	switch strings.TrimSpace(name) {
	case "k8s-cluster-inspect.json":
		return "clusterInspect"
	case "k8s-cert-inspect.json":
		return "certificateInspect"
	case kubernetesLifecyclePolicyDecisionArtifact:
		return "lifecyclePolicy"
	case kubernetesLifecyclePolicyOverrideArtifact:
		return "policyOverride"
	case "k8s-cert-renew.json":
		return "certificateRenew"
	case "k8s-cluster-verify.json":
		return "clusterVerify"
	default:
		return ""
	}
}

func kubernetesLifecyclePhaseRank(phase string) int {
	switch strings.TrimSpace(phase) {
	case "clusterInspect":
		return 10
	case "certificateInspect":
		return 20
	case "lifecyclePolicy":
		return 25
	case "policyOverride":
		return 26
	case "certificateRenew":
		return 30
	case "clusterVerify":
		return 40
	default:
		return 100
	}
}

func kubernetesLifecycleArtifactDigest(artifact RunArtifact) string {
	if strings.TrimSpace(artifact.SHA256) != "" {
		return strings.TrimSpace(artifact.SHA256)
	}
	if artifact.Body == "" {
		return ""
	}
	return "sha256:" + hashBytes([]byte(artifact.Body))
}

func kubernetesLifecycleArtifactRef(artifact RunArtifact) string {
	nodeID := strings.TrimSpace(artifact.NodeID)
	name := strings.TrimSpace(artifact.Name)
	if nodeID == "" {
		return name
	}
	return nodeID + "/" + name
}

func kubernetesLifecycleArtifactKey(nodeID string, name string) string {
	return strings.TrimSpace(nodeID) + "\x00" + strings.TrimSpace(name)
}

func kubernetesLifecycleSourceDigest(sourceByKey map[string]kubernetesLifecycleSourceArtifact, nodeID string, name string) string {
	source, ok := sourceByKey[kubernetesLifecycleArtifactKey(nodeID, name)]
	if !ok {
		return ""
	}
	return strings.TrimSpace(source.SHA256)
}

func kubernetesLifecycleSummaryNodeScope(plan *Plan, nodeID string) map[string]struct{} {
	if plan == nil || strings.TrimSpace(nodeID) == "" {
		return nil
	}
	byID := map[string]*ResolvedRelease{}
	byName := map[string]string{}
	for _, n := range plan.Nodes {
		if n == nil {
			continue
		}
		id := strings.TrimSpace(n.ID)
		if id == "" {
			continue
		}
		byID[id] = n
		if name := strings.TrimSpace(n.Name); name != "" {
			byName[name] = id
		}
	}
	scope := map[string]struct{}{}
	var visit func(string)
	visit = func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := scope[id]; ok {
			return
		}
		n := byID[id]
		if n == nil {
			return
		}
		scope[id] = struct{}{}
		for _, need := range n.Needs {
			need = strings.TrimSpace(need)
			if need == "" {
				continue
			}
			if _, ok := byID[need]; ok {
				visit(need)
				continue
			}
			if resolved := byName[need]; resolved != "" {
				visit(resolved)
			}
		}
	}
	visit(nodeID)
	return scope
}

func kubernetesLifecycleSummaryArtifactInScope(scope map[string]struct{}, artifact RunArtifact) bool {
	if len(scope) == 0 {
		return true
	}
	_, ok := scope[strings.TrimSpace(artifact.NodeID)]
	return ok
}

func kubernetesLifecyclePrimaryRole(roles []string) string {
	for _, role := range roles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "control-plane" || role == "master" {
			return "control-plane"
		}
	}
	for _, role := range roles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "" {
			return role
		}
	}
	return "worker"
}
