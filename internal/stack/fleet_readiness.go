package stack

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
)

const (
	FleetReadinessAPIVersion = "torque.dev/stack/fleet-readiness/v1alpha1"
	FleetReadinessKind       = "FleetReadiness"
)

type FleetReadinessResult struct {
	APIVersion   string                    `json:"apiVersion"`
	Kind         string                    `json:"kind"`
	Status       string                    `json:"status"`
	GeneratedAt  string                    `json:"generatedAt"`
	RunnerMode   string                    `json:"runnerMode"`
	Readiness    FleetReadinessConfig      `json:"readiness"`
	Summary      FleetReadinessSummary     `json:"summary"`
	Snapshot     *heartbeat.Snapshot       `json:"snapshot,omitempty"`
	Capabilities []FleetCapabilityCoverage `json:"capabilities,omitempty"`
	Checks       []FleetReadinessCheck     `json:"checks,omitempty"`
	Blockers     []OpsPlanBlocker          `json:"blockers,omitempty"`
}

type FleetReadinessConfig struct {
	Source              string            `json:"source,omitempty"`
	Store               string            `json:"store,omitempty"`
	StorePath           string            `json:"storePath,omitempty"`
	EtcdEndpoints       []string          `json:"etcdEndpoints,omitempty"`
	EtcdPrefix          string            `json:"etcdPrefix,omitempty"`
	Tenant              string            `json:"tenant,omitempty"`
	Selector            map[string]string `json:"selector,omitempty"`
	RequireAgents       bool              `json:"requireAgents"`
	MinReadyPercent     int               `json:"minReadyPercent"`
	FailureBudget       int               `json:"failureBudget"`
	StaleAfter          string            `json:"staleAfter"`
	OnInsufficientReady string            `json:"onInsufficientReady"`
}

type FleetReadinessSummary struct {
	TotalAgents          int  `json:"totalAgents"`
	ReadyAgents          int  `json:"readyAgents"`
	StaleAgents          int  `json:"staleAgents,omitempty"`
	DegradedAgents       int  `json:"degradedAgents,omitempty"`
	DrainingAgents       int  `json:"drainingAgents,omitempty"`
	OfflineAgents        int  `json:"offlineAgents,omitempty"`
	UnreadyAgents        int  `json:"unreadyAgents,omitempty"`
	ReadyPercent         int  `json:"readyPercent"`
	MinReadyPercent      int  `json:"minReadyPercent"`
	FailureBudget        int  `json:"failureBudget"`
	RequireAgents        bool `json:"requireAgents"`
	TransportViolations  int  `json:"transportViolations,omitempty"`
	RequiredCapabilities int  `json:"requiredCapabilities,omitempty"`
	CoveredCapabilities  int  `json:"coveredCapabilities,omitempty"`
	MissingCapabilities  int  `json:"missingCapabilities,omitempty"`
	Blocked              int  `json:"blocked,omitempty"`
}

type FleetCapabilityCoverage struct {
	Capability          string                       `json:"capability"`
	Status              string                       `json:"status"`
	RequiredBy          []FleetCapabilityRequirement `json:"requiredBy,omitempty"`
	MatchingReadyAgents []string                     `json:"matchingReadyAgents,omitempty"`
	MissingReadyAgents  []string                     `json:"missingReadyAgents,omitempty"`
	Reason              string                       `json:"reason,omitempty"`
}

type FleetCapabilityRequirement struct {
	NodeID   string `json:"nodeId"`
	NodeKind string `json:"nodeKind"`
}

type FleetReadinessCheck struct {
	Code       string `json:"code"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	NodeID     string `json:"nodeId,omitempty"`
	NodeKind   string `json:"nodeKind,omitempty"`
	Transport  string `json:"transport,omitempty"`
	Capability string `json:"capability,omitempty"`
	Store      string `json:"store,omitempty"`
}

func EvaluateFleetReadiness(ctx context.Context, p *Plan) *FleetReadinessResult {
	if p == nil {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(p.Runner.Mode))
	if mode == "" {
		mode = RunnerModeLocal
	}
	if mode != RunnerModeFleet {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	readiness := normalizeFleetReadiness(p.Runner.Readiness)
	result := newFleetReadinessResult(mode, readiness)

	violations := fleetTransportViolations(p)
	if len(violations) == 0 {
		addFleetReadinessCheck(result, FleetReadinessCheck{
			Code:   "fleet.transport.nats",
			Status: "passed",
			Reason: "all transport-backed fleet nodes use NATS",
		})
	} else {
		for _, violation := range violations {
			result.Blockers = append(result.Blockers, violation.blocker())
			addFleetReadinessCheck(result, FleetReadinessCheck{
				Code:      violation.Code,
				Status:    "blocked",
				Reason:    violation.Reason,
				NodeID:    violation.NodeID,
				NodeKind:  violation.Kind,
				Transport: violation.Transport,
			})
		}
		result.Summary.TransportViolations = len(violations)
	}

	store, err := openFleetReadinessStore(ctx, readiness)
	if err != nil {
		addFleetReadinessBlocker(result, OpsPlanBlocker{
			Code:   "fleet.registry.unavailable",
			Source: readiness.Store,
			Reason: fmt.Sprintf("read agent registry: %v", err),
		})
		result.Status = fleetReadinessStatus(result)
		result.Summary.Blocked = len(result.Blockers)
		return result
	}
	defer store.Close()

	snapshot, err := heartbeat.SnapshotFromStore(ctx, store, heartbeat.SnapshotRequest{
		Tenant:     readiness.Tenant,
		Selector:   readiness.Selector,
		StaleAfter: readiness.StaleAfter,
	})
	if err != nil {
		addFleetReadinessBlocker(result, OpsPlanBlocker{
			Code:   "fleet.registry.snapshot_failed",
			Source: readiness.Store,
			Reason: fmt.Sprintf("read agent registry snapshot: %v", err),
		})
		result.Status = fleetReadinessStatus(result)
		result.Summary.Blocked = len(result.Blockers)
		return result
	}
	result.Snapshot = &snapshot
	result.Summary.TotalAgents = snapshot.Summary.Total
	result.Summary.ReadyAgents = snapshot.Summary.Ready
	result.Summary.StaleAgents = snapshot.Summary.Stale
	result.Summary.DegradedAgents = snapshot.Summary.Degraded
	result.Summary.DrainingAgents = snapshot.Summary.Draining
	result.Summary.OfflineAgents = snapshot.Summary.Offline
	result.Summary.UnreadyAgents = snapshot.Summary.Total - snapshot.Summary.Ready
	if snapshot.Summary.Total > 0 {
		result.Summary.ReadyPercent = (snapshot.Summary.Ready * 100) / snapshot.Summary.Total
	}

	evaluateFleetAgentReadiness(result, snapshot)
	evaluateFleetCapabilityReadiness(result, p, snapshot)
	result.Status = fleetReadinessStatus(result)
	result.Summary.Blocked = len(result.Blockers)
	return result
}

func normalizeFleetReadiness(r RunnerReadinessResolved) RunnerReadinessResolved {
	out := r
	if strings.TrimSpace(out.Source) == "" {
		out.Source = RunnerReadinessSourceStore
	}
	out.Source = strings.ToLower(strings.TrimSpace(out.Source))
	if strings.TrimSpace(out.Store) == "" {
		out.Store = RunnerReadinessStoreFile
	}
	out.Store = strings.ToLower(strings.TrimSpace(out.Store))
	out.StorePath = strings.TrimSpace(out.StorePath)
	out.EtcdEndpoints = normalizeTrimmedStringSlice(out.EtcdEndpoints)
	out.EtcdPrefix = strings.TrimSpace(out.EtcdPrefix)
	if strings.TrimSpace(out.Tenant) == "" {
		out.Tenant = heartbeat.DefaultTenant
	}
	out.Tenant = strings.TrimSpace(out.Tenant)
	out.Selector = trimRunnerSelector(out.Selector)
	if out.StaleAfter <= 0 {
		out.StaleAfter = 45 * time.Second
	}
	if strings.TrimSpace(out.OnInsufficientReady) == "" {
		out.OnInsufficientReady = RunnerReadinessOnBlock
	}
	out.OnInsufficientReady = strings.ToLower(strings.TrimSpace(out.OnInsufficientReady))
	return out
}

func newFleetReadinessResult(mode string, readiness RunnerReadinessResolved) *FleetReadinessResult {
	return &FleetReadinessResult{
		APIVersion:  FleetReadinessAPIVersion,
		Kind:        FleetReadinessKind,
		Status:      "checking",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		RunnerMode:  mode,
		Readiness: FleetReadinessConfig{
			Source:              readiness.Source,
			Store:               readiness.Store,
			StorePath:           readiness.StorePath,
			EtcdEndpoints:       append([]string(nil), readiness.EtcdEndpoints...),
			EtcdPrefix:          readiness.EtcdPrefix,
			Tenant:              readiness.Tenant,
			Selector:            cloneStringMap(readiness.Selector),
			RequireAgents:       readiness.RequireAgents,
			MinReadyPercent:     readiness.MinReadyPercent,
			FailureBudget:       readiness.FailureBudget,
			StaleAfter:          readiness.StaleAfter.String(),
			OnInsufficientReady: readiness.OnInsufficientReady,
		},
		Summary: FleetReadinessSummary{
			MinReadyPercent: readiness.MinReadyPercent,
			FailureBudget:   readiness.FailureBudget,
			RequireAgents:   readiness.RequireAgents,
		},
	}
}

func openFleetReadinessStore(ctx context.Context, readiness RunnerReadinessResolved) (heartbeat.RegistryStore, error) {
	switch strings.ToLower(strings.TrimSpace(readiness.Store)) {
	case RunnerReadinessStoreFile:
		path := strings.TrimSpace(readiness.StorePath)
		if path == "" {
			path = strings.TrimSpace(os.Getenv("TORQUE_AGENT_REGISTRY_FILE"))
		}
		return heartbeat.NewFileStore(path)
	case RunnerReadinessStoreEtcd:
		endpoints := append([]string(nil), readiness.EtcdEndpoints...)
		if len(endpoints) == 0 {
			endpoints = heartbeat.ParseEtcdEndpoints(firstNonEmptyString(os.Getenv("TORQUE_ETCD_ENDPOINTS"), os.Getenv("ETCD_ENDPOINTS")))
		}
		prefix := strings.TrimSpace(readiness.EtcdPrefix)
		if prefix == "" {
			prefix = strings.TrimSpace(os.Getenv("TORQUE_AGENT_REGISTRY_PREFIX"))
		}
		if prefix == "" {
			prefix = heartbeat.DefaultStorePrefix
		}
		return heartbeat.NewEtcdStore(ctx, heartbeat.EtcdConfig{
			Endpoints:   endpoints,
			Prefix:      prefix,
			DialTimeout: 5 * time.Second,
		})
	default:
		return nil, fmt.Errorf("unsupported registry store %q", readiness.Store)
	}
}

func evaluateFleetAgentReadiness(result *FleetReadinessResult, snapshot heartbeat.Snapshot) {
	if result == nil {
		return
	}
	if result.Readiness.RequireAgents && snapshot.Summary.Total == 0 {
		addFleetReadinessBlocker(result, OpsPlanBlocker{
			Code:   "fleet.agents.missing",
			Reason: "no live agents matched runner.readiness.selector",
		})
		addFleetReadinessCheck(result, FleetReadinessCheck{
			Code:   "fleet.agents.required",
			Status: "blocked",
			Reason: "no matching agents found",
			Store:  result.Readiness.Store,
		})
		return
	}
	addFleetReadinessCheck(result, FleetReadinessCheck{
		Code:   "fleet.agents.required",
		Status: "passed",
		Reason: "matching agent registry snapshot is present",
		Store:  result.Readiness.Store,
	})

	if result.Summary.ReadyPercent < result.Readiness.MinReadyPercent {
		addFleetReadinessBlocker(result, OpsPlanBlocker{
			Code:   "fleet.agents.min_ready",
			Reason: fmt.Sprintf("ready agents %d%% is below required %d%%", result.Summary.ReadyPercent, result.Readiness.MinReadyPercent),
		})
		addFleetReadinessCheck(result, FleetReadinessCheck{
			Code:   "fleet.agents.min_ready",
			Status: "blocked",
			Reason: fmt.Sprintf("ready agents %d%% is below required %d%%", result.Summary.ReadyPercent, result.Readiness.MinReadyPercent),
			Store:  result.Readiness.Store,
		})
	} else {
		addFleetReadinessCheck(result, FleetReadinessCheck{
			Code:   "fleet.agents.min_ready",
			Status: "passed",
			Reason: fmt.Sprintf("ready agents %d%% meets required %d%%", result.Summary.ReadyPercent, result.Readiness.MinReadyPercent),
			Store:  result.Readiness.Store,
		})
	}

	if result.Summary.UnreadyAgents > result.Readiness.FailureBudget {
		addFleetReadinessBlocker(result, OpsPlanBlocker{
			Code:   "fleet.agents.failure_budget",
			Reason: fmt.Sprintf("unready agents %d exceeds failure budget %d", result.Summary.UnreadyAgents, result.Readiness.FailureBudget),
		})
		addFleetReadinessCheck(result, FleetReadinessCheck{
			Code:   "fleet.agents.failure_budget",
			Status: "blocked",
			Reason: fmt.Sprintf("unready agents %d exceeds failure budget %d", result.Summary.UnreadyAgents, result.Readiness.FailureBudget),
			Store:  result.Readiness.Store,
		})
	} else {
		addFleetReadinessCheck(result, FleetReadinessCheck{
			Code:   "fleet.agents.failure_budget",
			Status: "passed",
			Reason: fmt.Sprintf("unready agents %d is within failure budget %d", result.Summary.UnreadyAgents, result.Readiness.FailureBudget),
			Store:  result.Readiness.Store,
		})
	}
}

func fleetReadinessStatus(result *FleetReadinessResult) string {
	if result == nil || len(result.Blockers) == 0 {
		return "ready"
	}
	for _, blocker := range result.Blockers {
		if strings.HasPrefix(blocker.Code, "fleet.transport.") || strings.HasPrefix(blocker.Code, "fleet.capability.") {
			return "blocked"
		}
	}
	if strings.EqualFold(result.Readiness.OnInsufficientReady, RunnerReadinessOnWarn) {
		return "warning"
	}
	return "blocked"
}

func evaluateFleetCapabilityReadiness(result *FleetReadinessResult, p *Plan, snapshot heartbeat.Snapshot) {
	if result == nil {
		return
	}
	requirements := requiredFleetCapabilities(p)
	result.Summary.RequiredCapabilities = len(requirements)
	if len(requirements) == 0 {
		addFleetReadinessCheck(result, FleetReadinessCheck{
			Code:   "fleet.capability.required",
			Status: "skipped",
			Reason: "no agent-executed capabilities are required by this stack",
		})
		return
	}
	capabilities := make([]string, 0, len(requirements))
	for capability := range requirements {
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	for _, capability := range capabilities {
		requiredBy := requirements[capability]
		matches, missing := readyAgentsForCapability(snapshot.Agents, capability)
		coverage := FleetCapabilityCoverage{
			Capability:          capability,
			RequiredBy:          requiredBy,
			MatchingReadyAgents: matches,
			MissingReadyAgents:  missing,
		}
		readyCount := len(matches) + len(missing)
		switch {
		case readyCount == 0:
			coverage.Status = "blocked"
			coverage.Reason = "no ready agents are available to prove required capability"
			result.Summary.MissingCapabilities++
			addFleetReadinessBlocker(result, OpsPlanBlocker{
				Code:     "fleet.capability.missing",
				Severity: "block",
				Scope:    capability,
				Reason:   fmt.Sprintf("no ready agents are available to prove required capability %s", capability),
			})
			addFleetReadinessCheck(result, FleetReadinessCheck{
				Code:       "fleet.capability.required",
				Status:     "blocked",
				Reason:     coverage.Reason,
				Capability: capability,
			})
		case len(missing) > 0:
			coverage.Status = "blocked"
			coverage.Reason = fmt.Sprintf("%d ready agent(s) do not advertise required capability", len(missing))
			result.Summary.MissingCapabilities++
			addFleetReadinessBlocker(result, OpsPlanBlocker{
				Code:     "fleet.capability.missing",
				Severity: "block",
				Scope:    capability,
				Reason:   fmt.Sprintf("%d ready agent(s) do not advertise required capability %s", len(missing), capability),
			})
			addFleetReadinessCheck(result, FleetReadinessCheck{
				Code:       "fleet.capability.required",
				Status:     "blocked",
				Reason:     coverage.Reason,
				Capability: capability,
			})
		default:
			coverage.Status = "passed"
			coverage.Reason = fmt.Sprintf("%d ready agent(s) advertise required capability", len(matches))
			result.Summary.CoveredCapabilities++
			addFleetReadinessCheck(result, FleetReadinessCheck{
				Code:       "fleet.capability.required",
				Status:     "passed",
				Reason:     coverage.Reason,
				Capability: capability,
			})
		}
		result.Capabilities = append(result.Capabilities, coverage)
	}
}

func requiredFleetCapabilities(p *Plan) map[string][]FleetCapabilityRequirement {
	out := map[string][]FleetCapabilityRequirement{}
	if p == nil {
		return out
	}
	for _, node := range p.Nodes {
		capability := requiredFleetCapabilityForNode(node)
		if capability == "" {
			continue
		}
		out[capability] = append(out[capability], FleetCapabilityRequirement{
			NodeID:   strings.TrimSpace(node.ID),
			NodeKind: normalizeNodeKind(node.Kind),
		})
	}
	for capability := range out {
		sort.Slice(out[capability], func(i, j int) bool {
			if out[capability][i].NodeID == out[capability][j].NodeID {
				return out[capability][i].NodeKind < out[capability][j].NodeKind
			}
			return out[capability][i].NodeID < out[capability][j].NodeID
		})
	}
	return out
}

func requiredFleetCapabilityForNode(node *ResolvedRelease) string {
	if node == nil {
		return ""
	}
	kind := normalizeNodeKind(node.Kind)
	switch kind {
	case NodeKindHostCommandRun, NodeKindHostFileRender, NodeKindHostFileCopy, NodeKindHostPackageInstall, NodeKindHostServiceManage, NodeKindHostUserManage, NodeKindHostCronManage, NodeKindHostSystemdUnit:
		return kind
	case NodeKindMySQLReplicationVerify:
		return kind
	case NodeKindK8sClusterInspect, NodeKindK8sClusterVerify, NodeKindK8sManifestApply, NodeKindK8sManifestDelete, NodeKindK8sResourceWait, NodeKindK8sLogsCapture, NodeKindK8sEventsCapture, NodeKindK8sCertInspect, NodeKindK8sCertRenew:
		return kind
	default:
		if isModuleBackedNode(node) && transportIsNATS(moduleInputTransport(node.Module)) {
			return kind
		}
		return ""
	}
}

func moduleInputTransport(spec ModuleSpec) string {
	if len(spec.Input) == 0 {
		return ""
	}
	value, ok := spec.Input["transport"]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func readyAgentsForCapability(agents []heartbeat.AgentStatus, capability string) ([]string, []string) {
	capability = strings.TrimSpace(capability)
	if capability == "" {
		return nil, nil
	}
	var matching []string
	var missing []string
	for _, agent := range agents {
		if !strings.EqualFold(strings.TrimSpace(agent.Health), "ready") {
			continue
		}
		agentID := strings.TrimSpace(agent.AgentID)
		if agentHasCapability(agent, capability) {
			matching = append(matching, agentID)
		} else {
			missing = append(missing, agentID)
		}
	}
	sort.Strings(matching)
	sort.Strings(missing)
	return matching, missing
}

func agentHasCapability(agent heartbeat.AgentStatus, required string) bool {
	required = strings.TrimSpace(required)
	if required == "" {
		return true
	}
	for _, capability := range agent.Capabilities {
		if capabilityMatches(strings.TrimSpace(capability), required) {
			return true
		}
	}
	return false
}

func capabilityMatches(advertised string, required string) bool {
	advertised = strings.ToLower(strings.TrimSpace(advertised))
	required = strings.ToLower(strings.TrimSpace(required))
	if advertised == "" || required == "" {
		return false
	}
	if advertised == "*" || advertised == "all" || advertised == required {
		return true
	}
	if strings.HasSuffix(advertised, ".*") {
		return strings.HasPrefix(required, strings.TrimSuffix(advertised, "*"))
	}
	return false
}

func addFleetReadinessCheck(result *FleetReadinessResult, check FleetReadinessCheck) {
	if result == nil {
		return
	}
	result.Checks = append(result.Checks, check)
}

func addFleetReadinessBlocker(result *FleetReadinessResult, blocker OpsPlanBlocker) {
	if result == nil {
		return
	}
	result.Blockers = append(result.Blockers, blocker)
}

type fleetTransportViolation struct {
	Code      string
	NodeID    string
	Kind      string
	Transport string
	Reason    string
}

func (v fleetTransportViolation) blocker() OpsPlanBlocker {
	return OpsPlanBlocker{
		Code:     v.Code,
		Severity: "block",
		TargetID: v.NodeID,
		Reason:   v.Reason,
	}
}

func fleetTransportViolations(p *Plan) []fleetTransportViolation {
	if p == nil {
		return nil
	}
	var out []fleetTransportViolation
	for _, node := range p.Nodes {
		if node == nil {
			continue
		}
		kind := normalizeNodeKind(node.Kind)
		switch kind {
		case NodeKindHostCommandRun, NodeKindHostFileRender, NodeKindHostFileCopy, NodeKindHostPackageInstall, NodeKindHostServiceManage, NodeKindHostUserManage, NodeKindHostCronManage, NodeKindHostSystemdUnit:
			out = appendFleetTransportViolation(out, node.ID, kind, node.Host.Transport)
		case NodeKindMySQLReplicationVerify:
			out = appendFleetTransportViolation(out, node.ID, kind, node.MySQL.Transport)
		case NodeKindK8sClusterInspect, NodeKindK8sClusterVerify, NodeKindK8sManifestApply, NodeKindK8sManifestDelete, NodeKindK8sResourceWait, NodeKindK8sLogsCapture, NodeKindK8sEventsCapture:
			out = appendFleetTransportViolation(out, node.ID, kind, node.Kubernetes.Cluster.Transport)
		case NodeKindK8sCertInspect, NodeKindK8sCertRenew:
			out = appendFleetCertTransportViolations(out, node)
		}
	}
	return out
}

func appendFleetCertTransportViolations(out []fleetTransportViolation, node *ResolvedRelease) []fleetTransportViolation {
	if node == nil {
		return out
	}
	kind := normalizeNodeKind(node.Kind)
	targetsFrom := node.Kubernetes.Certificates.TargetsFrom
	hasTargetsFrom := strings.TrimSpace(targetsFrom.SourceNode) != "" || strings.TrimSpace(targetsFrom.SourceNodeID) != ""
	if hasTargetsFrom {
		transport := strings.TrimSpace(targetsFrom.Transport)
		if transport == "" {
			transport = "ssh"
		}
		out = appendFleetTransportViolation(out, node.ID, kind+".targetsFrom", transport)
	}
	for _, target := range node.Kubernetes.Certificates.Targets {
		transport := strings.TrimSpace(target.Transport)
		if transport == "" {
			transport = "ssh"
		}
		out = appendFleetTransportViolation(out, node.ID, kind+".target", transport)
	}
	return out
}

func appendFleetTransportViolation(out []fleetTransportViolation, nodeID string, kind string, transport string) []fleetTransportViolation {
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transportIsNATS(transport) {
		return out
	}
	if transport == "" {
		transport = "local"
	}
	return append(out, fleetTransportViolation{
		Code:      "fleet.transport.not_nats",
		NodeID:    nodeID,
		Kind:      kind,
		Transport: transport,
		Reason:    fmt.Sprintf("fleet mode requires NATS transport, got %s on %s", transport, nodeID),
	})
}

func transportIsNATS(transport string) bool {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "nats", "nats-mesh", "nats-agent":
		return true
	default:
		return false
	}
}
