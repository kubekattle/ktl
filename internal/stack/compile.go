// File: internal/stack/compile.go
// Brief: Compiler: discovery + merge + validation into a resolved DAG.

package stack

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
	Ops       *OpsPlanInputs                `json:"ops,omitempty"`
	Sealed    *SealedPlanMetadata           `json:"sealed,omitempty"`
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
			MySQL:        dr.FromFile.MySQL,
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
	case NodeKindMySQLReplicationVerify:
		if err := validateMySQLReplicationVerifySpec(leaf.Name, &n.MySQL); err != nil {
			return nil, fmt.Errorf("%s: %w", dr.Dir, err)
		}
	case NodeKindHostCommandRun:
		if strings.TrimSpace(n.Host.Command) == "" {
			return nil, fmt.Errorf("%s: host.command.run node %s requires input.command or host.command", dr.Dir, leaf.Name)
		}
		if strings.TrimSpace(n.Host.Transport) == "" {
			n.Host.Transport = "local"
		}
	case NodeKindHostFileRender:
		if strings.TrimSpace(n.Host.Path) == "" {
			return nil, fmt.Errorf("%s: host.file.render node %s requires input.path or host.path", dr.Dir, leaf.Name)
		}
		if strings.TrimSpace(n.Host.Content) == "" && strings.TrimSpace(n.Host.Template) == "" && strings.TrimSpace(n.Host.TemplatePath) == "" {
			return nil, fmt.Errorf("%s: host.file.render node %s requires host.content, host.template, or host.templatePath", dr.Dir, leaf.Name)
		}
		if strings.TrimSpace(n.Host.Transport) == "" {
			n.Host.Transport = "local"
		}
	case NodeKindHostFileCopy:
		if strings.TrimSpace(n.Host.Path) == "" {
			return nil, fmt.Errorf("%s: host.file.copy node %s requires input.path or host.path", dr.Dir, leaf.Name)
		}
		if strings.TrimSpace(n.Host.SourcePath) == "" && strings.TrimSpace(n.Host.Content) == "" {
			return nil, fmt.Errorf("%s: host.file.copy node %s requires host.sourcePath or host.content", dr.Dir, leaf.Name)
		}
		if strings.TrimSpace(n.Host.Transport) == "" {
			n.Host.Transport = "local"
		}
	case NodeKindHostPackageInstall:
		if strings.TrimSpace(n.Host.PackageName) == "" {
			return nil, fmt.Errorf("%s: host.package.install node %s requires host.package", dr.Dir, leaf.Name)
		}
		state := strings.ToLower(strings.TrimSpace(n.Host.State))
		if state == "" {
			n.Host.State = "present"
		} else if state != "present" && state != "latest" && state != "absent" {
			return nil, fmt.Errorf("%s: host.package.install node %s has unsupported host.state %q", dr.Dir, leaf.Name, n.Host.State)
		}
		if strings.TrimSpace(n.Host.Transport) == "" {
			n.Host.Transport = "local"
		}
	case NodeKindHostServiceManage:
		if strings.TrimSpace(n.Host.ServiceName) == "" {
			return nil, fmt.Errorf("%s: host.service.manage node %s requires host.service", dr.Dir, leaf.Name)
		}
		state := strings.ToLower(strings.TrimSpace(n.Host.State))
		if state == "" && n.Host.Enabled == nil {
			n.Host.State = "started"
		} else if state != "" && state != "started" && state != "stopped" && state != "restarted" {
			return nil, fmt.Errorf("%s: host.service.manage node %s has unsupported host.state %q", dr.Dir, leaf.Name, n.Host.State)
		}
		if strings.TrimSpace(n.Host.Transport) == "" {
			n.Host.Transport = "local"
		}
	case NodeKindHostUserManage:
		if strings.TrimSpace(n.Host.UserName) == "" && strings.TrimSpace(n.Host.GroupName) == "" {
			return nil, fmt.Errorf("%s: host.user.manage node %s requires host.user or host.groupName", dr.Dir, leaf.Name)
		}
		state := strings.ToLower(strings.TrimSpace(n.Host.State))
		if state == "" {
			n.Host.State = "present"
		} else if state != "present" && state != "absent" {
			return nil, fmt.Errorf("%s: host.user.manage node %s has unsupported host.state %q", dr.Dir, leaf.Name, n.Host.State)
		}
		if strings.TrimSpace(n.Host.Transport) == "" {
			n.Host.Transport = "local"
		}
	case NodeKindHostCronManage:
		if strings.TrimSpace(n.Host.Path) == "" {
			return nil, fmt.Errorf("%s: host.cron.manage node %s requires host.path", dr.Dir, leaf.Name)
		}
		state := strings.ToLower(strings.TrimSpace(n.Host.State))
		if state == "" {
			n.Host.State = "present"
		} else if state != "present" && state != "absent" {
			return nil, fmt.Errorf("%s: host.cron.manage node %s has unsupported host.state %q", dr.Dir, leaf.Name, n.Host.State)
		} else {
			n.Host.State = state
		}
		if n.Host.State == "present" {
			if strings.TrimSpace(n.Host.CronSchedule) == "" {
				return nil, fmt.Errorf("%s: host.cron.manage node %s requires host.schedule", dr.Dir, leaf.Name)
			}
			if strings.TrimSpace(n.Host.CronCommand) == "" {
				return nil, fmt.Errorf("%s: host.cron.manage node %s requires host.cronCommand", dr.Dir, leaf.Name)
			}
		}
		if strings.TrimSpace(n.Host.CronUser) == "" {
			n.Host.CronUser = "root"
		}
		if strings.TrimSpace(n.Host.Mode) == "" {
			n.Host.Mode = "0644"
		}
		if strings.TrimSpace(n.Host.Transport) == "" {
			n.Host.Transport = "local"
		}
	case NodeKindHostSystemdUnit:
		if strings.TrimSpace(n.Host.UnitName) == "" {
			return nil, fmt.Errorf("%s: host.systemd.unit node %s requires host.unit", dr.Dir, leaf.Name)
		}
		state := strings.ToLower(strings.TrimSpace(n.Host.State))
		if state == "" {
			n.Host.State = "present"
		} else if state != "present" && state != "started" && state != "stopped" && state != "restarted" && state != "absent" {
			return nil, fmt.Errorf("%s: host.systemd.unit node %s has unsupported host.state %q", dr.Dir, leaf.Name, n.Host.State)
		} else {
			n.Host.State = state
		}
		if n.Host.State != "absent" && strings.TrimSpace(n.Host.Content) == "" && strings.TrimSpace(n.Host.Template) == "" && strings.TrimSpace(n.Host.TemplatePath) == "" {
			return nil, fmt.Errorf("%s: host.systemd.unit node %s requires host.content, host.template, or host.templatePath", dr.Dir, leaf.Name)
		}
		if strings.TrimSpace(n.Host.Mode) == "" {
			n.Host.Mode = "0644"
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
	case NodeKindK8sManifestApply, NodeKindK8sManifestDelete:
		if err := validateKubernetesManifestSpec(n.Kind, leaf.Name, &n.Kubernetes); err != nil {
			return nil, fmt.Errorf("%s: %w", dr.Dir, err)
		}
	case NodeKindK8sResourceWait:
		if err := validateKubernetesResourceWaitSpec(leaf.Name, &n.Kubernetes); err != nil {
			return nil, fmt.Errorf("%s: %w", dr.Dir, err)
		}
	case NodeKindK8sLogsCapture:
		if err := validateKubernetesLogsCaptureSpec(leaf.Name, &n.Kubernetes); err != nil {
			return nil, fmt.Errorf("%s: %w", dr.Dir, err)
		}
	case NodeKindK8sEventsCapture:
		if err := validateKubernetesEventsCaptureSpec(leaf.Name, &n.Kubernetes); err != nil {
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

func validateMySQLReplicationVerifySpec(name string, spec *MySQLSpec) error {
	if spec == nil {
		return fmt.Errorf("%s node %s requires mysql config", NodeKindMySQLReplicationVerify, name)
	}
	transportKind := strings.ToLower(strings.TrimSpace(spec.Transport))
	if transportKind == "" {
		transportKind = "local"
	}
	switch transportKind {
	case "local", "localhost", "ssh":
		spec.Transport = transportKind
	default:
		return fmt.Errorf("%s node %s has unsupported mysql.transport %q", NodeKindMySQLReplicationVerify, name, spec.Transport)
	}
	if transportKind == "ssh" && strings.TrimSpace(spec.Target) == "" && strings.TrimSpace(spec.TargetEnv) == "" {
		return fmt.Errorf("%s node %s requires mysql.target or targetEnv for ssh transport", NodeKindMySQLReplicationVerify, name)
	}
	if len(spec.Nodes) == 0 {
		return fmt.Errorf("%s node %s requires at least one mysql.nodes entry", NodeKindMySQLReplicationVerify, name)
	}
	for i := range spec.Nodes {
		node := &spec.Nodes[i]
		node.ID = strings.TrimSpace(node.ID)
		node.Address = strings.TrimSpace(node.Address)
		node.SSHUser = strings.TrimSpace(node.SSHUser)
		if node.ID == "" {
			node.ID = node.Address
		}
		if node.Address == "" {
			return fmt.Errorf("%s node %s requires mysql.nodes[%d].address", NodeKindMySQLReplicationVerify, name, i)
		}
		if strings.ContainsAny(node.Address, " \t\r\n'\"|") {
			return fmt.Errorf("%s node %s has invalid mysql.nodes[%d].address %q", NodeKindMySQLReplicationVerify, name, i, node.Address)
		}
		if strings.ContainsAny(node.ID, " \t\r\n'\"|") {
			return fmt.Errorf("%s node %s has invalid mysql.nodes[%d].id %q", NodeKindMySQLReplicationVerify, name, i, node.ID)
		}
		if node.SSHPort < 0 || node.SSHPort > 65535 {
			return fmt.Errorf("%s node %s requires mysql.nodes[%d].sshPort between 0 and 65535", NodeKindMySQLReplicationVerify, name, i)
		}
	}
	if spec.ExpectedClusterSize < 0 {
		return fmt.Errorf("%s node %s requires mysql.expectedClusterSize >= 0", NodeKindMySQLReplicationVerify, name)
	}
	if spec.ExpectedClusterSize == 0 {
		spec.ExpectedClusterSize = len(spec.Nodes)
	}
	if spec.ExpectedReplicatedNodes < 0 {
		return fmt.Errorf("%s node %s requires mysql.expectedReplicatedNodes >= 0", NodeKindMySQLReplicationVerify, name)
	}
	if spec.ExpectedReplicatedNodes == 0 {
		spec.ExpectedReplicatedNodes = len(spec.Nodes)
	}
	if spec.ExpectedReplicatedNodes > len(spec.Nodes) {
		return fmt.Errorf("%s node %s requires mysql.expectedReplicatedNodes <= len(mysql.nodes)", NodeKindMySQLReplicationVerify, name)
	}
	spec.Database = strings.TrimSpace(spec.Database)
	if spec.Database == "" {
		spec.Database = "torque"
	}
	if !mysqlIdentifierOK(spec.Database) {
		return fmt.Errorf("%s node %s has invalid mysql.database %q", NodeKindMySQLReplicationVerify, name, spec.Database)
	}
	spec.ProbeTable = strings.TrimSpace(spec.ProbeTable)
	if spec.ProbeTable == "" {
		spec.ProbeTable = "probe"
	}
	if !mysqlIdentifierOK(spec.ProbeTable) {
		return fmt.Errorf("%s node %s has invalid mysql.probeTable %q", NodeKindMySQLReplicationVerify, name, spec.ProbeTable)
	}
	spec.ProbeID = strings.TrimSpace(spec.ProbeID)
	if spec.ProbeID == "" {
		spec.ProbeID = name
	}
	spec.ProbePayload = strings.TrimSpace(spec.ProbePayload)
	if spec.ProbePayload == "" {
		spec.ProbePayload = "torque replication verify"
	}
	if spec.StableAttempts < 0 {
		return fmt.Errorf("%s node %s requires mysql.stableAttempts >= 0", NodeKindMySQLReplicationVerify, name)
	}
	if spec.StableAttempts == 0 {
		spec.StableAttempts = defaultMySQLReplicationStableAttempts
	}
	if spec.StableInterval == nil {
		interval := defaultMySQLReplicationStableInterval
		spec.StableInterval = &interval
	} else if *spec.StableInterval < 0 {
		return fmt.Errorf("%s node %s requires mysql.stableInterval >= 0", NodeKindMySQLReplicationVerify, name)
	}
	if spec.Timeout == nil {
		timeout := defaultMySQLReplicationTimeout
		spec.Timeout = &timeout
	} else if *spec.Timeout <= 0 {
		return fmt.Errorf("%s node %s requires mysql.timeout > 0", NodeKindMySQLReplicationVerify, name)
	}
	return nil
}

func mysqlIdentifierOK(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r == '_' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		return false
	}
	return true
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

func validateKubernetesManifestSpec(kind string, name string, spec *KubernetesSpec) error {
	kind = normalizeNodeKind(kind)
	if spec == nil {
		return fmt.Errorf("%s node %s requires kubernetes manifest config", kind, name)
	}
	if err := validateKubernetesClusterAccessSpec(kind, name, spec.Cluster); err != nil {
		return err
	}
	manifest := &spec.Manifest
	if strings.TrimSpace(manifest.Content) == "" && strings.TrimSpace(manifest.Path) == "" && strings.TrimSpace(manifest.Template) == "" && strings.TrimSpace(manifest.TemplatePath) == "" {
		return fmt.Errorf("%s node %s requires kubernetes.manifest.content, path, template, or templatePath", kind, name)
	}
	if strings.TrimSpace(manifest.Namespace) == "" {
		manifest.Namespace = "default"
	}
	if strings.TrimSpace(manifest.FieldManager) == "" {
		manifest.FieldManager = "torque"
	}
	if strings.TrimSpace(spec.Cluster.Transport) == "" {
		spec.Cluster.Transport = "local"
	}
	prunePolicy := strings.ToLower(strings.TrimSpace(manifest.PrunePolicy))
	if kind == NodeKindK8sManifestDelete && prunePolicy == "" {
		prunePolicy = "listed-only"
	}
	if prunePolicy != "" && prunePolicy != "listed-only" {
		return fmt.Errorf("%s node %s has unsupported kubernetes.manifest.prunePolicy %q", kind, name, manifest.PrunePolicy)
	}
	manifest.PrunePolicy = prunePolicy
	if kind == NodeKindK8sManifestDelete {
		manifest.RemoveOnDelete = true
	}
	return nil
}

func validateKubernetesResourceWaitSpec(name string, spec *KubernetesSpec) error {
	if spec == nil {
		return fmt.Errorf("%s node %s requires kubernetes resource config", NodeKindK8sResourceWait, name)
	}
	if err := validateKubernetesClusterAccessSpec(NodeKindK8sResourceWait, name, spec.Cluster); err != nil {
		return err
	}
	resource := &spec.Resource
	if strings.TrimSpace(resource.Namespace) == "" {
		resource.Namespace = "default"
	}
	if strings.TrimSpace(spec.Cluster.Transport) == "" {
		spec.Cluster.Transport = "local"
	}
	resource.Resource = strings.TrimSpace(resource.Resource)
	resource.Kind = strings.TrimSpace(resource.Kind)
	resource.Name = strings.TrimSpace(resource.Name)
	resource.Selector = strings.TrimSpace(resource.Selector)
	if resource.Resource != "" && (resource.Kind == "" || resource.Name == "") {
		kind, name := splitKubernetesResourceName(resource.Resource)
		if resource.Kind == "" {
			resource.Kind = kind
		}
		if resource.Name == "" {
			resource.Name = name
		}
	}
	if resource.Resource == "" {
		if resource.Kind == "" {
			return fmt.Errorf("%s node %s requires kubernetes.resource.kind or resource", NodeKindK8sResourceWait, name)
		}
		if resource.Selector == "" && resource.Name == "" {
			return fmt.Errorf("%s node %s requires kubernetes.resource.name or selector", NodeKindK8sResourceWait, name)
		}
		if resource.Name != "" {
			resource.Resource = resource.Kind + "/" + resource.Name
		}
	}
	if strings.TrimSpace(resource.For) == "" {
		resource.For = defaultKubernetesResourceWaitFor(resource.Kind)
	}
	if resource.Timeout == nil {
		timeout := defaultKubernetesResourceWaitTimeout
		resource.Timeout = &timeout
	} else if *resource.Timeout <= 0 {
		return fmt.Errorf("%s node %s requires kubernetes.resource.timeout > 0", NodeKindK8sResourceWait, name)
	}
	if resource.EventLimit < 0 {
		return fmt.Errorf("%s node %s requires kubernetes.resource.eventLimit >= 0", NodeKindK8sResourceWait, name)
	}
	if resource.EventLimit == 0 {
		resource.EventLimit = defaultKubernetesResourceWaitEventLimit
	}
	if spec.Cluster.Timeout == nil && resource.Timeout != nil {
		transportTimeout := *resource.Timeout + 30*time.Second
		spec.Cluster.Timeout = &transportTimeout
	}
	return nil
}

func validateKubernetesLogsCaptureSpec(name string, spec *KubernetesSpec) error {
	if spec == nil {
		return fmt.Errorf("%s node %s requires kubernetes logs config", NodeKindK8sLogsCapture, name)
	}
	if err := validateKubernetesClusterAccessSpec(NodeKindK8sLogsCapture, name, spec.Cluster); err != nil {
		return err
	}
	logs := &spec.Logs
	if strings.TrimSpace(logs.Namespace) == "" {
		logs.Namespace = "default"
	}
	if strings.TrimSpace(spec.Cluster.Transport) == "" {
		spec.Cluster.Transport = "local"
	}
	logs.Resource = strings.TrimSpace(logs.Resource)
	logs.Kind = strings.TrimSpace(logs.Kind)
	logs.Name = strings.TrimSpace(logs.Name)
	logs.Selector = strings.TrimSpace(logs.Selector)
	logs.Container = strings.TrimSpace(logs.Container)
	logs.SinceTime = strings.TrimSpace(logs.SinceTime)
	if logs.Resource != "" && (logs.Kind == "" || logs.Name == "") {
		kind, resourceName := splitKubernetesResourceName(logs.Resource)
		if logs.Kind == "" {
			logs.Kind = kind
		}
		if logs.Name == "" {
			logs.Name = resourceName
		}
	}
	if logs.Resource == "" && logs.Selector == "" {
		if logs.Kind == "" {
			return fmt.Errorf("%s node %s requires kubernetes.logs.kind, resource, or selector", NodeKindK8sLogsCapture, name)
		}
		if logs.Name == "" {
			return fmt.Errorf("%s node %s requires kubernetes.logs.name or selector", NodeKindK8sLogsCapture, name)
		}
		logs.Resource = logs.Kind + "/" + logs.Name
	}
	if logs.Resource != "" && logs.Selector != "" {
		return fmt.Errorf("%s node %s cannot set both kubernetes.logs.resource/name and selector", NodeKindK8sLogsCapture, name)
	}
	if logs.Container != "" && logs.AllContainers {
		return fmt.Errorf("%s node %s cannot set both kubernetes.logs.container and allContainers", NodeKindK8sLogsCapture, name)
	}
	if logs.Since != nil && *logs.Since <= 0 {
		return fmt.Errorf("%s node %s requires kubernetes.logs.since > 0", NodeKindK8sLogsCapture, name)
	}
	if logs.Since != nil && logs.SinceTime != "" {
		return fmt.Errorf("%s node %s cannot set both kubernetes.logs.since and sinceTime", NodeKindK8sLogsCapture, name)
	}
	if logs.TailLines < 0 {
		return fmt.Errorf("%s node %s requires kubernetes.logs.tailLines >= 0", NodeKindK8sLogsCapture, name)
	}
	if logs.TailLines == 0 {
		logs.TailLines = defaultKubernetesLogsCaptureTailLines
	}
	if logs.LimitBytes < 0 {
		return fmt.Errorf("%s node %s requires kubernetes.logs.limitBytes >= 0", NodeKindK8sLogsCapture, name)
	}
	if logs.LimitBytes == 0 {
		logs.LimitBytes = defaultKubernetesLogsCaptureLimitBytes
	}
	if logs.MaxLogRequests < 0 {
		return fmt.Errorf("%s node %s requires kubernetes.logs.maxLogRequests >= 0", NodeKindK8sLogsCapture, name)
	}
	if logs.MaxLogRequests == 0 {
		logs.MaxLogRequests = defaultKubernetesLogsCaptureMaxRequests
	}
	return nil
}

func validateKubernetesEventsCaptureSpec(name string, spec *KubernetesSpec) error {
	if spec == nil {
		return fmt.Errorf("%s node %s requires kubernetes events config", NodeKindK8sEventsCapture, name)
	}
	if err := validateKubernetesClusterAccessSpec(NodeKindK8sEventsCapture, name, spec.Cluster); err != nil {
		return err
	}
	events := &spec.Events
	if strings.TrimSpace(events.Namespace) == "" {
		events.Namespace = "default"
	}
	if strings.TrimSpace(spec.Cluster.Transport) == "" {
		spec.Cluster.Transport = "local"
	}
	events.Resource = strings.TrimSpace(events.Resource)
	events.Kind = strings.TrimSpace(events.Kind)
	events.Name = strings.TrimSpace(events.Name)
	events.FieldSelector = strings.TrimSpace(events.FieldSelector)
	events.SinceTime = strings.TrimSpace(events.SinceTime)
	events.Types = normalizeTrimmedStringSlice(events.Types)
	events.Reasons = normalizeTrimmedStringSlice(events.Reasons)
	if events.Resource != "" && (events.Kind == "" || events.Name == "") {
		kind, resourceName := splitKubernetesResourceName(events.Resource)
		if events.Kind == "" {
			events.Kind = kind
		}
		if events.Name == "" {
			events.Name = resourceName
		}
	}
	if events.Resource == "" && events.Kind != "" && events.Name != "" {
		events.Resource = events.Kind + "/" + events.Name
	}
	if events.Since != nil && *events.Since <= 0 {
		return fmt.Errorf("%s node %s requires kubernetes.events.since > 0", NodeKindK8sEventsCapture, name)
	}
	if events.Since != nil && events.SinceTime != "" {
		return fmt.Errorf("%s node %s cannot set both kubernetes.events.since and sinceTime", NodeKindK8sEventsCapture, name)
	}
	if events.EventLimit < 0 {
		return fmt.Errorf("%s node %s requires kubernetes.events.eventLimit >= 0", NodeKindK8sEventsCapture, name)
	}
	if events.EventLimit == 0 {
		events.EventLimit = defaultKubernetesEventsCaptureLimit
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

func normalizeTrimmedStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
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
