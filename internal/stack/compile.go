// File: internal/stack/compile.go
// Brief: Compiler: discovery + merge + validation into a resolved DAG.

package stack

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type CompileOptions struct {
	Profile string
}

type Plan struct {
	StackRoot string                        `json:"stackRoot"`
	StackName string                        `json:"stackName"`
	Profile   string                        `json:"profile"`
	Nodes     []*ResolvedRelease            `json:"nodes"`
	Order     []string                      `json:"order,omitempty"`
	Runner    RunnerResolved                `json:"runner,omitempty"`
	Hooks     StackHooksConfig              `json:"hooks,omitempty"`
	ByID      map[string]*ResolvedRelease   `json:"-"`
	ByCluster map[string][]*ResolvedRelease `json:"-"`
}

func Compile(u *Universe, opts CompileOptions) (*Plan, error) {
	profile := strings.TrimSpace(opts.Profile)
	if profile == "" {
		profile = strings.TrimSpace(u.DefaultProfile)
	}

	stackHooks, err := ResolveStackHooksConfig(u, profile)
	if err != nil {
		return nil, err
	}

	nodes := make([]*ResolvedRelease, 0, len(u.Releases))
	for _, dr := range u.Releases {
		node, err := resolveRelease(u, dr, profile)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	byID := make(map[string]*ResolvedRelease, len(nodes))
	byCluster := map[string][]*ResolvedRelease{}
	for _, n := range nodes {
		if _, exists := byID[n.ID]; exists {
			return nil, fmt.Errorf("duplicate release id %s", n.ID)
		}
		byID[n.ID] = n
		byCluster[n.Cluster.Name] = append(byCluster[n.Cluster.Name], n)
	}

	for clusterName, list := range byCluster {
		seenName := map[string]string{}
		for _, n := range list {
			if strings.TrimSpace(clusterName) == "" && isHelmNode(n) {
				return nil, fmt.Errorf("cluster.name is required for every helm node (missing on %s)", n.Name)
			}
			if strings.TrimSpace(clusterName) == "" {
				continue
			}
			key := strings.TrimSpace(n.Namespace) + "/" + strings.TrimSpace(n.Name)
			if prev, ok := seenName[key]; ok {
				return nil, fmt.Errorf("duplicate node %q in namespace %q cluster %q (%s vs %s)", n.Name, n.Namespace, clusterName, prev, n.ID)
			}
			seenName[key] = n.ID
		}
	}

	p := &Plan{
		StackRoot: u.RootDir,
		StackName: u.StackName,
		Profile:   profile,
		Nodes:     nodes,
		Hooks:     filterHooksRunOnce(stackHooks, true),
		ByID:      byID,
		ByCluster: byCluster,
	}

	if r, err := ResolveRunnerConfig(u, profile); err == nil {
		p.Runner = r
	} else {
		return nil, err
	}

	if err := assignExecutionGroups(p); err != nil {
		return nil, err
	}
	if order, err := ComputeExecutionOrder(p, "apply"); err == nil {
		p.Order = order
	}
	return p, nil
}

func resolveRelease(u *Universe, dr discoveredRelease, profile string) (*ResolvedRelease, error) {
	var leaf ReleaseSpec
	switch {
	case dr.FromFile != nil:
		leaf = ReleaseSpec{
			Kind:         dr.FromFile.NodeKind,
			Name:         dr.FromFile.Name,
			Chart:        dr.FromFile.Chart,
			ChartVersion: dr.FromFile.ChartVersion,
			Wave:         dr.FromFile.Wave,
			Critical:     dr.FromFile.Critical,
			Parallelism:  dr.FromFile.Parallelism,
			Cluster:      dr.FromFile.Cluster,
			Namespace:    dr.FromFile.Namespace,
			Values:       dr.FromFile.Values,
			Set:          dr.FromFile.Set,
			Tags:         dr.FromFile.Tags,
			Needs:        dr.FromFile.Needs,
			Apply:        dr.FromFile.Apply,
			Delete:       dr.FromFile.Delete,
			Hooks:        dr.FromFile.Hooks,
			Action:       dr.FromFile.Action,
			Database:     dr.FromFile.Database,
			Kubernetes:   dr.FromFile.Kubernetes,
		}
	case dr.FromInline != nil:
		leaf = *dr.FromInline
	default:
		return nil, fmt.Errorf("internal error: discovered release without source")
	}

	if strings.TrimSpace(leaf.Name) == "" {
		return nil, fmt.Errorf("%s: release name is required", dr.Dir)
	}
	n := &ResolvedRelease{
		Name: leaf.Name,
		Dir:  dr.Dir,
		Set:  map[string]string{},
	}
	n.Kind = normalizeNodeKind(leaf.Kind)

	chain, err := stackChain(u, dr.Dir)
	if err != nil {
		return nil, err
	}
	for _, dir := range chain {
		sf, ok := u.Stacks[dir]
		if !ok {
			continue
		}
		mergeDefaults(n, dir, sf.Defaults)
		allowRunOnce := samePath(dir, u.RootDir)
		if err := ValidateHooksConfig(sf.Hooks, allowRunOnce, fmt.Sprintf("%s hooks", dir)); err != nil {
			return nil, err
		}
		mergeHooks(n, dir, sf.Hooks)
		if profile != "" {
			if sp, ok := sf.Profiles[profile]; ok {
				mergeDefaults(n, dir, sp.Defaults)
				if err := ValidateHooksConfig(sp.Hooks, allowRunOnce, fmt.Sprintf("%s profiles.%s.hooks", dir, profile)); err != nil {
					return nil, err
				}
				mergeHooks(n, dir, sp.Hooks)
			}
		}
	}
	if err := ValidateHooksConfig(leaf.Hooks, false, fmt.Sprintf("%s release %s hooks", dr.Dir, leaf.Name)); err != nil {
		return nil, err
	}
	mergeReleaseOverride(n, dr.Dir, leaf)
	if n.Kind == "" {
		n.Kind = NodeKindHelm
	}

	switch n.Kind {
	case NodeKindHelm:
		if strings.TrimSpace(n.Chart) == "" {
			return nil, fmt.Errorf("%s: chart is required for helm node %s", dr.Dir, leaf.Name)
		}
	case NodeKindAction:
		if !n.Action.Idempotent {
			return nil, fmt.Errorf("%s: action.script node %s must set action.idempotent=true", dr.Dir, leaf.Name)
		}
		if n.Action.Apply == nil && n.Action.Delete == nil {
			return nil, fmt.Errorf("%s: action.script node %s requires action.apply or action.delete", dr.Dir, leaf.Name)
		}
	case NodeKindActionPlugin:
		if !n.Action.Idempotent {
			return nil, fmt.Errorf("%s: action.plugin node %s must set action.idempotent=true", dr.Dir, leaf.Name)
		}
		if err := validateActionPluginSpec(n.Action.Plugin); err != nil {
			return nil, fmt.Errorf("%s: action.plugin node %s: %w", dr.Dir, leaf.Name, err)
		}
	case NodeKindDBRestorePoint, NodeKindDBSchemaExpand, NodeKindDBBackfill, NodeKindDBVerify, NodeKindDBCutover, NodeKindDBSchemaContract:
		if strings.TrimSpace(n.Database.Driver) == "" {
			n.Database.Driver = "postgres"
		}
		if _, err := dialectFor(n.Database.Driver); err != nil {
			return nil, fmt.Errorf("%s: db.cutover node %s: %w", dr.Dir, leaf.Name, err)
		}
		if strings.TrimSpace(n.Database.DSN) == "" && strings.TrimSpace(n.Database.DSNEnv) == "" {
			return nil, fmt.Errorf("%s: database node %s requires database.dsn or database.dsnEnv", dr.Dir, leaf.Name)
		}
		switch n.Kind {
		case NodeKindDBRestorePoint:
			if strings.TrimSpace(n.Database.RestorePointSQL) == "" {
				return nil, fmt.Errorf("%s: db.restore-point node %s requires database.restorePointSQL", dr.Dir, leaf.Name)
			}
		case NodeKindDBSchemaExpand:
			if strings.TrimSpace(n.Database.ExpandSQL) == "" {
				return nil, fmt.Errorf("%s: db.schema-expand node %s requires database.expandSQL", dr.Dir, leaf.Name)
			}
		case NodeKindDBBackfill:
			if strings.TrimSpace(n.Database.Backfill.StartSQL) == "" {
				return nil, fmt.Errorf("%s: db.backfill node %s requires database.backfill.startSQL", dr.Dir, leaf.Name)
			}
			if strings.TrimSpace(n.Database.Backfill.EndSQL) == "" {
				return nil, fmt.Errorf("%s: db.backfill node %s requires database.backfill.endSQL", dr.Dir, leaf.Name)
			}
			if strings.TrimSpace(n.Database.Backfill.BatchSQL) == "" {
				return nil, fmt.Errorf("%s: db.backfill node %s requires database.backfill.batchSQL", dr.Dir, leaf.Name)
			}
			if n.Database.Backfill.BatchSize <= 0 {
				return nil, fmt.Errorf("%s: db.backfill node %s requires database.backfill.batchSize > 0", dr.Dir, leaf.Name)
			}
			if strings.TrimSpace(n.Database.Backfill.CheckpointTable) == "" {
				n.Database.Backfill.CheckpointTable = "torque_backfill_state"
			}
		case NodeKindDBVerify:
			if strings.TrimSpace(n.Database.VerifySQL) == "" {
				return nil, fmt.Errorf("%s: db.verify node %s requires database.verifySQL", dr.Dir, leaf.Name)
			}
		case NodeKindDBCutover:
			if strings.TrimSpace(n.Database.CommitSQL) == "" {
				return nil, fmt.Errorf("%s: db.cutover node %s requires database.commitSQL", dr.Dir, leaf.Name)
			}
			if strings.TrimSpace(n.Database.MetadataTable) == "" {
				n.Database.MetadataTable = "torque_cutover_state"
			}
		case NodeKindDBSchemaContract:
			if strings.TrimSpace(n.Database.ContractSQL) == "" {
				return nil, fmt.Errorf("%s: db.schema-contract node %s requires database.contractSQL", dr.Dir, leaf.Name)
			}
		}
	case NodeKindHostCommandRun:
		if strings.TrimSpace(n.Host.Command) == "" {
			return nil, fmt.Errorf("%s: host.command.run node %s requires input.command or host.command", dr.Dir, leaf.Name)
		}
		if strings.TrimSpace(n.Host.Transport) == "" {
			n.Host.Transport = "local"
		}
	case NodeKindK8sCertInspect, NodeKindK8sCertRenew:
		if err := validateKubernetesCertSpec(n.Kind, leaf.Name, n.Kubernetes); err != nil {
			return nil, fmt.Errorf("%s: %w", dr.Dir, err)
		}
	case NodeKindK8sClusterInspect:
		if err := validateKubernetesClusterInspectSpec(leaf.Name, n.Kubernetes.Cluster); err != nil {
			return nil, fmt.Errorf("%s: %w", dr.Dir, err)
		}
	case NodeKindK8sClusterVerify:
		if err := validateKubernetesClusterVerifySpec(leaf.Name, n.Kubernetes.Cluster); err != nil {
			return nil, fmt.Errorf("%s: %w", dr.Dir, err)
		}
	default:
		return nil, fmt.Errorf("%s: unsupported node kind %q for %s", dr.Dir, n.Kind, leaf.Name)
	}

	if n.Namespace == "" {
		n.Namespace = "default"
	}
	n.ID = resolvedNodeID(n)
	if n.Kind == NodeKindDBBackfill && strings.TrimSpace(n.Database.Backfill.CheckpointKey) == "" {
		n.Database.Backfill.CheckpointKey = n.ID
	}
	return n, nil
}

func validateKubernetesClusterInspectSpec(name string, spec KubernetesClusterSpec) error {
	return validateKubernetesClusterAccessSpec(NodeKindK8sClusterInspect, name, spec)
}

func validateKubernetesClusterVerifySpec(name string, spec KubernetesClusterSpec) error {
	if err := validateKubernetesClusterAccessSpec(NodeKindK8sClusterVerify, name, spec); err != nil {
		return err
	}
	if spec.MinReadyNodes < 0 {
		return fmt.Errorf("%s node %s requires kubernetes.cluster.minReadyNodes >= 0", NodeKindK8sClusterVerify, name)
	}
	if spec.StableIterations < 0 {
		return fmt.Errorf("%s node %s requires kubernetes.cluster.stableIterations >= 0", NodeKindK8sClusterVerify, name)
	}
	if spec.StableInterval != nil && *spec.StableInterval < 0 {
		return fmt.Errorf("%s node %s requires kubernetes.cluster.stableInterval >= 0", NodeKindK8sClusterVerify, name)
	}
	for i, probe := range spec.AppProbes {
		if strings.TrimSpace(probe.ID) == "" {
			return fmt.Errorf("%s node %s appProbes[%d].id is required", NodeKindK8sClusterVerify, name, i)
		}
		if strings.TrimSpace(probe.Command) == "" {
			return fmt.Errorf("%s node %s app probe %s requires command", NodeKindK8sClusterVerify, name, probe.ID)
		}
	}
	return nil
}

func validateKubernetesClusterAccessSpec(kind string, name string, spec KubernetesClusterSpec) error {
	transport := strings.ToLower(strings.TrimSpace(spec.Transport))
	if transport == "" {
		transport = "local"
	}
	switch transport {
	case "local", "localhost":
	case "ssh":
		if strings.TrimSpace(spec.Target) == "" && strings.TrimSpace(spec.TargetEnv) == "" {
			return fmt.Errorf("%s node %s requires kubernetes.cluster.target or targetEnv for ssh transport", kind, name)
		}
	default:
		return fmt.Errorf("%s node %s has unsupported kubernetes.cluster.transport %q", kind, name, spec.Transport)
	}
	return nil
}

func validateKubernetesCertSpec(kind string, name string, spec KubernetesSpec) error {
	certs := spec.Certificates
	hasTargetsFrom := strings.TrimSpace(certs.TargetsFrom.SourceNode) != "" || strings.TrimSpace(certs.TargetsFrom.SourceNodeID) != ""
	if len(certs.Targets) == 0 && !hasTargetsFrom {
		return fmt.Errorf("%s node %s requires kubernetes.certificates.targets or targetsFrom", normalizeNodeKind(kind), name)
	}
	provider := strings.ToLower(strings.TrimSpace(spec.Provider))
	if provider == "" {
		provider = "auto"
	}
	if !validKubernetesCertProvider(provider) {
		return fmt.Errorf("%s node %s has unsupported kubernetes.provider %q", normalizeNodeKind(kind), name, spec.Provider)
	}
	if certs.BatchSize < 0 {
		return fmt.Errorf("%s node %s requires kubernetes.certificates.batchSize >= 0", normalizeNodeKind(kind), name)
	}
	if !validKubernetesCertOrder(certs.Order) {
		return fmt.Errorf("%s node %s has unsupported kubernetes.certificates.order %q", normalizeNodeKind(kind), name, certs.Order)
	}
	if err := validateKubernetesLifecyclePolicySpec(kind, name, certs.Policy); err != nil {
		return err
	}
	if hasTargetsFrom {
		if err := validateKubernetesCertTargetsFromSpec(kind, name, certs.TargetsFrom); err != nil {
			return err
		}
	}
	for i, target := range certs.Targets {
		targetID := strings.TrimSpace(target.ID)
		if targetID == "" {
			return fmt.Errorf("%s node %s target[%d].id is required", normalizeNodeKind(kind), name, i)
		}
		targetProvider := strings.ToLower(strings.TrimSpace(target.Provider))
		if targetProvider != "" && !validKubernetesCertProvider(targetProvider) {
			return fmt.Errorf("%s node %s target %s has unsupported provider %q", normalizeNodeKind(kind), name, targetID, target.Provider)
		}
		if strings.TrimSpace(target.Target) == "" && strings.TrimSpace(target.TargetEnv) == "" {
			return fmt.Errorf("%s node %s target %s requires target or targetEnv", normalizeNodeKind(kind), name, targetID)
		}
		if normalizeNodeKind(kind) == NodeKindK8sCertRenew && firstNonEmptyString(targetProvider, provider) == "custom" && strings.TrimSpace(target.RenewCommand) == "" {
			return fmt.Errorf("%s node %s target %s custom provider requires renewCommand", normalizeNodeKind(kind), name, targetID)
		}
		if firstNonEmptyString(targetProvider, provider) == "custom" && strings.TrimSpace(target.InspectCommand) == "" {
			return fmt.Errorf("%s node %s target %s custom provider requires inspectCommand", normalizeNodeKind(kind), name, targetID)
		}
	}
	return nil
}

func validateKubernetesCertTargetsFromSpec(kind string, name string, spec KubernetesCertTargetsFromSpec) error {
	transport := strings.ToLower(strings.TrimSpace(spec.Transport))
	if transport == "" {
		transport = "ssh"
	}
	switch transport {
	case "local", "localhost":
	case "ssh":
		if strings.TrimSpace(spec.Target) == "" && strings.TrimSpace(spec.TargetEnv) == "" && strings.TrimSpace(spec.TargetTemplate) == "" {
			return fmt.Errorf("%s node %s targetsFrom requires target, targetEnv, or targetTemplate for ssh transport", normalizeNodeKind(kind), name)
		}
	default:
		return fmt.Errorf("%s node %s targetsFrom has unsupported transport %q", normalizeNodeKind(kind), name, spec.Transport)
	}
	if strings.TrimSpace(spec.SourceNode) == "" && strings.TrimSpace(spec.SourceNodeID) == "" {
		return fmt.Errorf("%s node %s targetsFrom requires sourceNode or sourceNodeId", normalizeNodeKind(kind), name)
	}
	provider := strings.ToLower(strings.TrimSpace(spec.Provider))
	if provider != "" && !validKubernetesCertProvider(provider) {
		return fmt.Errorf("%s node %s targetsFrom has unsupported provider %q", normalizeNodeKind(kind), name, spec.Provider)
	}
	if provider == "custom" && strings.TrimSpace(spec.InspectCommand) == "" {
		return fmt.Errorf("%s node %s targetsFrom custom provider requires inspectCommand", normalizeNodeKind(kind), name)
	}
	if normalizeNodeKind(kind) == NodeKindK8sCertRenew && provider == "custom" && strings.TrimSpace(spec.RenewCommand) == "" {
		return fmt.Errorf("%s node %s targetsFrom custom provider requires renewCommand", normalizeNodeKind(kind), name)
	}
	return nil
}

func validKubernetesCertOrder(order string) bool {
	switch strings.ToLower(strings.TrimSpace(order)) {
	case "", "as-listed", "control-plane-first", "control-plane-last":
		return true
	default:
		return false
	}
}

func validKubernetesCertProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "auto", "kubeadm", "k3s", "rke2", "custom":
		return true
	default:
		return false
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func resolvedNodeID(n *ResolvedRelease) string {
	if n == nil {
		return ""
	}
	if strings.TrimSpace(n.Cluster.Name) != "" {
		return fmt.Sprintf("%s/%s/%s", strings.TrimSpace(n.Cluster.Name), strings.TrimSpace(n.Namespace), strings.TrimSpace(n.Name))
	}
	return fmt.Sprintf("%s/%s", normalizeNodeKind(n.Kind), strings.TrimSpace(n.Name))
}

func stackChain(u *Universe, dir string) ([]string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root := filepath.Clean(u.RootDir)
	cur := filepath.Clean(absDir)
	var chain []string
	for {
		chain = append(chain, cur)
		if cur == root {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return nil, fmt.Errorf("release %s is outside stack root %s", absDir, root)
		}
		cur = parent
	}
	// Root-to-leaf.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

func resolvePath(baseDir, p string) string {
	pp := strings.TrimSpace(p)
	if pp == "" {
		return pp
	}
	// Keep non-filesystem chart refs (oci://, repo/name) untouched.
	if strings.Contains(pp, "://") {
		return pp
	}
	if filepath.IsAbs(pp) {
		return filepath.Clean(pp)
	}
	return filepath.Clean(filepath.Join(baseDir, pp))
}
