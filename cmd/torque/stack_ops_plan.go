package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ingresslabs/torque/internal/ops/inventory"
	"github.com/ingresslabs/torque/internal/ops/locks"
	opspolicy "github.com/ingresslabs/torque/internal/ops/policy"
	"github.com/ingresslabs/torque/internal/ops/targetgraph"
	"github.com/ingresslabs/torque/internal/stack"
	"github.com/spf13/cobra"
)

type stackOpsPlanOptions struct {
	Targets         string
	Selectors       []string
	Groups          []string
	Limit           int
	Facts           []string
	LockDir         string
	LockScopes      []string
	PolicyDecisions []string
}

func (o stackOpsPlanOptions) enabled() bool {
	return strings.TrimSpace(o.Targets) != "" ||
		len(o.Selectors) > 0 ||
		len(o.Groups) > 0 ||
		o.Limit != 0 ||
		len(o.Facts) > 0 ||
		strings.TrimSpace(o.LockDir) != "" ||
		len(o.LockScopes) > 0 ||
		len(o.PolicyDecisions) > 0
}

func addStackOpsPlanFlags(cmd *cobra.Command, opts *stackOpsPlanOptions) {
	cmd.Flags().StringVar(&opts.Targets, "ops-targets", "", "TargetGraph YAML file to attach as a read-only stack plan input")
	cmd.Flags().StringArrayVar(&opts.Selectors, "ops-selector", nil, "TargetGraph label selector as key=value for ops plan inputs (repeatable)")
	cmd.Flags().StringArrayVar(&opts.Groups, "ops-group", nil, "TargetGraph group to include as an ops plan input (repeatable)")
	cmd.Flags().IntVar(&opts.Limit, "ops-limit", 0, "Maximum selected ops targets to include in the plan input")
	cmd.Flags().StringArrayVar(&opts.Facts, "ops-facts", nil, "Facts evidence file or evidence directory to attach as a read-only plan input (repeatable)")
	cmd.Flags().StringVar(&opts.LockDir, "ops-lock-dir", "", "Directory of ops target locks to inspect as read-only plan inputs")
	cmd.Flags().StringArrayVar(&opts.LockScopes, "ops-lock-scope", nil, "Ops lock scope to inspect, usually target/<id> or graph/<id> (repeatable)")
	cmd.Flags().StringArrayVar(&opts.PolicyDecisions, "ops-policy-decision", nil, "Mutation policy decision JSON to attach as a read-only plan input (repeatable)")
}

func attachStackOpsPlanInputs(_ *cobra.Command, p *stack.Plan, opts stackOpsPlanOptions) error {
	if p == nil || !opts.enabled() {
		return nil
	}
	if strings.TrimSpace(opts.Targets) == "" && (len(opts.Selectors) > 0 || len(opts.Groups) > 0 || opts.Limit != 0) {
		return fmt.Errorf("--ops-selector, --ops-group, and --ops-limit require --ops-targets")
	}

	inputs := &stack.OpsPlanInputs{
		APIVersion: "torque.dev/ops/plan-inputs/v1alpha1",
		Kind:       "OpsPlanInputs",
	}

	var graph *targetgraph.TargetGraph
	var selectedTargets []targetgraph.Target
	if strings.TrimSpace(opts.Targets) != "" {
		targetInput, loadedGraph, targets, err := loadStackOpsTargetGraph(opts)
		if err != nil {
			return err
		}
		inputs.TargetGraph = targetInput
		graph = loadedGraph
		selectedTargets = targets
	}

	for _, path := range splitCSV(opts.Facts) {
		facts, err := loadStackOpsFactEvidence(path)
		if err != nil {
			return err
		}
		inputs.FactEvidence = append(inputs.FactEvidence, facts)
		if facts.Summary.Blocked > 0 || facts.Summary.Failed > 0 || facts.Summary.Skipped > 0 {
			inputs.Blockers = append(inputs.Blockers, stack.OpsPlanBlocker{
				Code:     "ops.facts.incomplete",
				Severity: "blocker",
				Source:   facts.Source,
				Reason: fmt.Sprintf(
					"facts evidence has %d blocked, %d failed, and %d skipped target(s)",
					facts.Summary.Blocked,
					facts.Summary.Failed,
					facts.Summary.Skipped,
				),
			})
		}
	}

	lockInputs, err := loadStackOpsLocks(opts, graph, selectedTargets)
	if err != nil {
		return err
	}
	inputs.Locks = append(inputs.Locks, lockInputs...)
	for _, lockInput := range lockInputs {
		if lockInput.Found && lockInput.Status == "held" {
			inputs.Blockers = append(inputs.Blockers, stack.OpsPlanBlocker{
				Code:     "ops.lock.held",
				Severity: "blocker",
				TargetID: lockInput.TargetID,
				Scope:    lockInput.Scope,
				Reason:   fmt.Sprintf("lock is held by %s until %s", firstNonEmpty(lockInput.Holder, "unknown"), firstNonEmpty(lockInput.ExpiresAt, "unknown")),
			})
		}
	}

	for _, path := range splitCSV(opts.PolicyDecisions) {
		decision, err := loadStackOpsPolicyDecision(path)
		if err != nil {
			return err
		}
		inputs.PolicyDecisions = append(inputs.PolicyDecisions, decision)
		if decision.Decision != "allow" {
			inputs.Blockers = append(inputs.Blockers, stack.OpsPlanBlocker{
				Code:     "ops.policy." + firstNonEmpty(decision.Decision, "unknown"),
				Severity: "blocker",
				Source:   decision.Source,
				TargetID: decision.TargetID,
				Reason:   firstNonEmpty(decision.Reason, "policy did not allow mutation"),
			})
		}
	}

	inputs.Summary = summarizeStackOpsPlanInputs(inputs)
	p.Ops = inputs
	return nil
}

func loadStackOpsTargetGraph(opts stackOpsPlanOptions) (*stack.OpsTargetGraphInput, *targetgraph.TargetGraph, []targetgraph.Target, error) {
	path := strings.TrimSpace(opts.Targets)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read --ops-targets %q: %w", path, err)
	}
	graph, err := targetgraph.Load(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load --ops-targets %q: %w", path, err)
	}
	selector, err := inventory.ParseSelector(opts.Selectors)
	if err != nil {
		return nil, nil, nil, err
	}
	selection, err := graph.ResolveSelection(targetgraph.SelectionRequest{
		Selector: selector,
		Groups:   splitCSV(opts.Groups),
		Limit:    opts.Limit,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	byID := map[string]targetgraph.Target{}
	for _, target := range graph.Targets {
		byID[target.ID] = target
	}
	targets := make([]targetgraph.Target, 0, len(selection.MatchedTargetIDs))
	for _, targetID := range selection.MatchedTargetIDs {
		target, ok := byID[targetID]
		if !ok {
			return nil, nil, nil, fmt.Errorf("target selection referenced unknown target %q", targetID)
		}
		targets = append(targets, target)
	}
	summary := graph.Summary()
	input := &stack.OpsTargetGraphInput{
		Path:         path,
		Name:         graph.Metadata.Name,
		SourceDigest: stackOpsDigest(raw),
		GraphDigest:  stackOpsDigestJSON(summary),
		Summary:      stackOpsTargetGraphSummary(summary),
		Selection:    stackOpsTargetSelection(selection),
	}
	return input, graph, targets, nil
}

func loadStackOpsFactEvidence(path string) (stack.OpsFactEvidenceInput, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return stack.OpsFactEvidenceInput{}, fmt.Errorf("--ops-facts path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return stack.OpsFactEvidenceInput{}, fmt.Errorf("stat --ops-facts %q: %w", path, err)
	}
	if info.IsDir() {
		return loadStackOpsFactEvidenceDir(path)
	}
	return loadStackOpsFactEvidenceFile(path)
}

func loadStackOpsFactEvidenceDir(path string) (stack.OpsFactEvidenceInput, error) {
	input := stack.OpsFactEvidenceInput{
		Source: path,
		Kind:   "FactEvidenceBundle",
	}
	manifestPath := filepath.Join(path, "manifest.json")
	if raw, err := os.ReadFile(manifestPath); err == nil {
		input.Digest = stackOpsDigest(raw)
		var manifest opsFactsEvidenceManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return stack.OpsFactEvidenceInput{}, fmt.Errorf("%s: decode manifest.json: %w", path, err)
		}
		input.GraphName = manifest.GraphName
		input.Summary.Selected = manifest.Summary.Selected
		input.Summary.Collected = manifest.Summary.Collected
		input.Summary.Cached = manifest.Summary.Cached
		input.Summary.Blocked = manifest.Summary.Blocked
		input.Summary.Failed = manifest.Summary.Failed
		input.Summary.Skipped = manifest.Summary.Skipped
	} else if !errors.Is(err, os.ErrNotExist) {
		return stack.OpsFactEvidenceInput{}, fmt.Errorf("%s: read manifest.json: %w", path, err)
	}
	factsPath := filepath.Join(path, "facts.json")
	rawFacts, err := os.ReadFile(factsPath)
	if err != nil {
		return stack.OpsFactEvidenceInput{}, fmt.Errorf("%s: read facts.json: %w", path, err)
	}
	if input.Digest == "" {
		input.Digest = stackOpsDigest(rawFacts)
	}
	if err := mergeStackOpsFactCollection(&input, rawFacts); err != nil {
		return stack.OpsFactEvidenceInput{}, fmt.Errorf("%s: %w", factsPath, err)
	}
	freshnessPath := filepath.Join(path, "freshness.json")
	if rawFreshness, err := os.ReadFile(freshnessPath); err == nil {
		if err := mergeStackOpsFactFreshness(&input, rawFreshness); err != nil {
			return stack.OpsFactEvidenceInput{}, fmt.Errorf("%s: %w", freshnessPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return stack.OpsFactEvidenceInput{}, fmt.Errorf("%s: read freshness.json: %w", path, err)
	}
	digest, err := stack.ComputeOpsFactEvidenceDigest(path)
	if err != nil {
		return stack.OpsFactEvidenceInput{}, fmt.Errorf("%s: compute evidence digest: %w", path, err)
	}
	input.Digest = digest
	input.Summary.Targets = len(input.Targets)
	return input, nil
}

func loadStackOpsFactEvidenceFile(path string) (stack.OpsFactEvidenceInput, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return stack.OpsFactEvidenceInput{}, err
	}
	input := stack.OpsFactEvidenceInput{
		Source: path,
		Kind:   "FactEvidence",
		Digest: stackOpsDigest(raw),
	}
	if err := mergeStackOpsFactCollection(&input, raw); err != nil {
		return stack.OpsFactEvidenceInput{}, fmt.Errorf("%s: %w", path, err)
	}
	input.Summary.Targets = len(input.Targets)
	return input, nil
}

func mergeStackOpsFactCollection(input *stack.OpsFactEvidenceInput, raw []byte) error {
	var collection opsFactsCollectResult
	if err := json.Unmarshal(raw, &collection); err == nil && collection.Kind == opsFactsCollectionKind {
		input.Kind = collection.Kind
		input.GraphName = firstNonEmpty(input.GraphName, collection.GraphName)
		input.Summary.Selected = maxInt(input.Summary.Selected, collection.Summary.Selected)
		input.Summary.Collected = maxInt(input.Summary.Collected, collection.Summary.Collected)
		input.Summary.Cached = maxInt(input.Summary.Cached, collection.Summary.Cached)
		input.Summary.Blocked = maxInt(input.Summary.Blocked, collection.Summary.Blocked)
		input.Summary.Failed = maxInt(input.Summary.Failed, collection.Summary.Failed)
		input.Summary.Skipped = maxInt(input.Summary.Skipped, collection.Summary.Skipped)
		for _, result := range collection.Results {
			input.Targets = append(input.Targets, stack.OpsFactTargetInput{
				TargetID:      result.TargetID,
				TargetType:    result.TargetType,
				TransportKind: result.TransportKind,
				Status:        result.Status,
				Source:        result.Source,
				SnapshotKind:  opsFactsSnapshotKind(result),
				Digest:        opsFactsSnapshotDigest(result),
			})
			if result.Snapshot != nil || result.K8sSnapshot != nil {
				input.Summary.Snapshots++
			}
		}
		return nil
	}

	snapshots, err := decodeOpsFactSnapshots(raw)
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		input.Targets = append(input.Targets, stack.OpsFactTargetInput{
			TargetID:     snapshot.TargetID,
			Status:       "snapshot",
			SnapshotKind: snapshot.Kind,
			Digest:       snapshot.stableDigest(),
		})
		input.Summary.Snapshots++
	}
	return nil
}

func mergeStackOpsFactFreshness(input *stack.OpsFactEvidenceInput, raw []byte) error {
	var freshness opsFactsEvidenceFreshness
	if err := json.Unmarshal(raw, &freshness); err != nil {
		return err
	}
	if freshness.Kind == "" {
		return fmt.Errorf("unsupported freshness document")
	}
	input.Freshness = &stack.OpsFactFreshnessInput{
		HostDecisions:   freshness.Summary.HostDecisions,
		K8sObservations: freshness.Summary.K8sObservations,
		Blocked:         freshness.Summary.Blocked,
		Failed:          freshness.Summary.Failed,
	}
	input.Summary.Blocked = maxInt(input.Summary.Blocked, freshness.Summary.Blocked)
	input.Summary.Failed = maxInt(input.Summary.Failed, freshness.Summary.Failed)
	return nil
}

func loadStackOpsLocks(opts stackOpsPlanOptions, graph *targetgraph.TargetGraph, selectedTargets []targetgraph.Target) ([]stack.OpsLockInput, error) {
	lockDir := strings.TrimSpace(opts.LockDir)
	explicitScopes := splitCSV(opts.LockScopes)
	if lockDir == "" && len(explicitScopes) > 0 {
		lockDir = ".torque/ops/locks"
	}
	if lockDir == "" {
		return nil, nil
	}
	lockSource := lockDir
	if abs, err := filepath.Abs(lockDir); err == nil {
		lockSource = abs
	}
	scopes := append([]string(nil), explicitScopes...)
	if len(scopes) == 0 && graph != nil {
		for _, target := range selectedTargets {
			scopes = append(scopes, firstNonEmpty(strings.TrimSpace(target.LockScope), "target/"+target.ID))
		}
		scopes = uniqueSortedStrings(scopes)
	}
	if len(scopes) == 0 {
		return scanStackOpsLockDir(lockDir)
	}

	store := locks.FileStore{Dir: lockDir}
	var out []stack.OpsLockInput
	for _, scope := range scopes {
		record, found, err := store.Inspect(scope)
		if err != nil {
			return nil, err
		}
		if !found {
			out = append(out, stack.OpsLockInput{
				Source:   lockSource,
				Scope:    scope,
				Found:    false,
				TargetID: stackOpsTargetIDForScope(scope),
			})
			continue
		}
		out = append(out, stackOpsLockInputFromRecord(record, lockSource))
	}
	return out, nil
}

func scanStackOpsLockDir(lockDir string) ([]stack.OpsLockInput, error) {
	entries, err := os.ReadDir(lockDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read --ops-lock-dir %q: %w", lockDir, err)
	}
	store := locks.FileStore{Dir: lockDir}
	var out []stack.OpsLockInput
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(lockDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var record locks.Record
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, fmt.Errorf("%s: decode lock: %w", entry.Name(), err)
		}
		if strings.TrimSpace(record.Scope) == "" {
			return nil, fmt.Errorf("%s: lock record missing scope", entry.Name())
		}
		inspected, found, err := store.Inspect(record.Scope)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, stackOpsLockInputFromRecord(inspected, lockDir))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scope < out[j].Scope })
	return out, nil
}

func stackOpsLockInputFromRecord(record *locks.Record, source string) stack.OpsLockInput {
	if record == nil {
		return stack.OpsLockInput{}
	}
	if abs, err := filepath.Abs(source); err == nil {
		source = abs
	}
	return stack.OpsLockInput{
		Source:     strings.TrimSpace(source),
		Scope:      record.Scope,
		Found:      true,
		TargetID:   firstNonEmpty(record.TargetID, stackOpsTargetIDForScope(record.Scope)),
		Status:     record.Status,
		Holder:     record.Holder,
		Operation:  record.Operation,
		AcquiredAt: record.AcquiredAt,
		ExpiresAt:  record.ExpiresAt,
	}
}

func loadStackOpsPolicyDecision(path string) (stack.OpsPolicyDecisionInput, error) {
	path = strings.TrimSpace(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		return stack.OpsPolicyDecisionInput{}, fmt.Errorf("read --ops-policy-decision %q: %w", path, err)
	}
	var decision opspolicy.Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return stack.OpsPolicyDecisionInput{}, fmt.Errorf("%s: decode policy decision: %w", path, err)
	}
	if strings.TrimSpace(decision.Decision) == "" {
		return stack.OpsPolicyDecisionInput{}, fmt.Errorf("%s: policy decision is missing decision", path)
	}
	return stack.OpsPolicyDecisionInput{
		Source:    path,
		Digest:    stackOpsDigest(raw),
		Mode:      string(decision.Mode),
		Decision:  strings.TrimSpace(decision.Decision),
		Reason:    decision.Reason,
		Operation: decision.Operation,
		Adapter:   decision.Adapter,
		TargetID:  decision.TargetID,
		Mutating:  decision.Mutating,
		Unsafe:    decision.Unsafe,
		Required:  append([]string(nil), decision.Required...),
	}, nil
}

func summarizeStackOpsPlanInputs(inputs *stack.OpsPlanInputs) stack.OpsPlanInputSummary {
	var summary stack.OpsPlanInputSummary
	if inputs == nil {
		return summary
	}
	if inputs.TargetGraph != nil {
		summary.TargetCount = inputs.TargetGraph.Summary.TargetCount
		summary.SelectedTargets = len(inputs.TargetGraph.Selection.MatchedTargetIDs)
	}
	summary.FactEvidence = len(inputs.FactEvidence)
	for _, facts := range inputs.FactEvidence {
		summary.FactSnapshots += facts.Summary.Snapshots
		summary.FactBlocked += facts.Summary.Blocked
		summary.FactFailed += facts.Summary.Failed
	}
	for _, lockInput := range inputs.Locks {
		if !lockInput.Found {
			continue
		}
		summary.Locks++
		switch lockInput.Status {
		case "held":
			summary.LockHeld++
		case "stale":
			summary.LockStale++
		}
	}
	summary.PolicyDecisions = len(inputs.PolicyDecisions)
	for _, decision := range inputs.PolicyDecisions {
		if decision.Decision != "allow" {
			summary.PolicyBlocked++
		}
	}
	summary.Blockers = len(inputs.Blockers)
	return summary
}

func stackOpsTargetGraphSummary(summary targetgraph.Summary) stack.OpsTargetGraphSummary {
	return stack.OpsTargetGraphSummary{
		APIVersion:            summary.APIVersion,
		Kind:                  summary.Kind,
		Name:                  summary.Name,
		TargetCount:           summary.TargetCount,
		GroupCount:            summary.GroupCount,
		TransportCount:        summary.TransportCount,
		PrivilegeProfileCount: summary.PrivilegeProfileCount,
		VariableLayerCount:    summary.VariableLayerCount,
		TargetTypes:           copyIntMap(summary.TargetTypes),
		TransportKinds:        copyIntMap(summary.TransportKinds),
		GroupIDs:              append([]string(nil), summary.GroupIDs...),
		TargetIDs:             append([]string(nil), summary.TargetIDs...),
		SecretReferenceCount:  summary.SecretReferenceCount,
		HostReachabilityRefs:  append([]string(nil), summary.HostReachabilityRefs...),
	}
}

func stackOpsTargetSelection(selection targetgraph.SelectionResult) stack.OpsTargetSelectionInput {
	return stack.OpsTargetSelectionInput{
		Selector:           copyStringMap(selection.Selector),
		Groups:             append([]string(nil), selection.Groups...),
		MatchedTargetIDs:   append([]string(nil), selection.MatchedTargetIDs...),
		OmittedTargetIDs:   append([]string(nil), selection.OmittedTargetIDs...),
		SelectorMatchedIDs: append([]string(nil), selection.SelectorMatchedIDs...),
		Limited:            selection.Limited,
		BeforeLimitCount:   selection.BeforeLimitCount,
		AfterLimitCount:    selection.AfterLimitCount,
		ConflictCount:      selection.ConflictCount,
	}
}

func stackOpsDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func stackOpsDigestJSON(value any) string {
	raw, _ := json.Marshal(value)
	return stackOpsDigest(raw)
}

func stackOpsTargetIDForScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if strings.HasPrefix(scope, "target/") {
		return strings.TrimPrefix(scope, "target/")
	}
	return ""
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func copyIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func maxInt(a, b int) int {
	if b > a {
		return b
	}
	return a
}
