package stack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCompile_MergesDefaultsAndProfiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: demo
defaultProfile: dev
profiles:
  dev:
    defaults:
      values: [values-dev.yaml]
defaults:
  cluster: { name: c1 }
  namespace: ns1
  values: [values-common.yaml]
  set: { global.cluster: c1 }
`)
	writeFile(t, filepath.Join(root, "services", "stack.yaml"), `
defaults:
  tags: [svc]
`)
	writeFile(t, filepath.Join(root, "services", "redis", "release.yaml"), `
apiVersion: torque.dev/v1
kind: Release
name: redis
chart: ./chart
values: [values-redis.yaml]
tags: [cache]
`)

	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(p.Nodes) != 1 {
		t.Fatalf("nodes=%d", len(p.Nodes))
	}
	n := p.Nodes[0]
	if n.ID != "c1/ns1/redis" {
		t.Fatalf("id=%q", n.ID)
	}
	wantValues := []string{
		filepath.Join(root, "values-common.yaml"),
		filepath.Join(root, "values-dev.yaml"),
		filepath.Join(root, "services", "redis", "values-redis.yaml"),
	}
	if got := join(n.Values); got != join(wantValues) {
		t.Fatalf("values=%v want=%v", n.Values, wantValues)
	}
	if len(n.Tags) != 2 || n.Tags[0] != "svc" || n.Tags[1] != "cache" {
		t.Fatalf("tags=%v", n.Tags)
	}
	if n.Set["global.cluster"] != "c1" {
		t.Fatalf("set=%v", n.Set)
	}
}

func TestCompile_AllowsSameReleaseNameAcrossNamespaces(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: adopted
defaults:
  cluster: { name: c1 }
releases:
  - name: api
    namespace: dev
    chart: ./api
  - name: api
    namespace: prod
    chart: ./api
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(p.Nodes) != 2 {
		t.Fatalf("nodes=%d", len(p.Nodes))
	}
	if p.Nodes[0].ID == p.Nodes[1].ID {
		t.Fatalf("expected unique node IDs, got %q", p.Nodes[0].ID)
	}
	if _, err := Select(u, p, nil, Selector{Releases: []string{"api"}}); err == nil {
		t.Fatalf("expected bare release selection to remain ambiguous")
	}
}

func TestCompile_MixedNodesAndReleasesAlias(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: mixed
nodes:
  - name: prepare-host
    kind: host.command.run
    input:
      transport: local
      command: "true"
  - name: api
    kind: release.helm
    chart: ./chart
    cluster: { name: c1 }
    namespace: prod
    needs: [prepare-host]
releases:
  - name: legacy
    chart: ./chart
    cluster: { name: c1 }
    namespace: prod
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	host := p.ByID["host.command.run/prepare-host"]
	if host == nil {
		t.Fatalf("missing host node; ids=%v", nodeIDs(p.Nodes))
	}
	if host.Cluster.Name != "" {
		t.Fatalf("host node unexpectedly required cluster: %#v", host.Cluster)
	}
	api := p.ByID["c1/prod/api"]
	if api == nil {
		t.Fatalf("missing helm node; ids=%v", nodeIDs(p.Nodes))
	}
	if got := normalizeNodeKind(api.Kind); got != NodeKindHelm {
		t.Fatalf("api kind=%q", got)
	}
	legacy := p.ByID["c1/prod/legacy"]
	if legacy == nil {
		t.Fatalf("missing releases alias node; ids=%v", nodeIDs(p.Nodes))
	}
	if got := normalizeNodeKind(legacy.Kind); got != NodeKindHelm {
		t.Fatalf("legacy kind=%q", got)
	}
	if host.ExecutionGroup >= api.ExecutionGroup {
		t.Fatalf("expected host dependency to precede api, host=%d api=%d", host.ExecutionGroup, api.ExecutionGroup)
	}
}

func TestCompile_KubernetesCertLifecycleNodesDoNotRequireCluster(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: lifecycle
nodes:
  - name: cert-inspect
    kind: k8s.cert.inspect
    kubernetes:
      provider: kubeadm
      certificates:
        targets:
          - id: cp-1
            transport: local
            target: local://localhost
  - name: cert-renew
    kind: k8s.cert.renew
    needs: [cert-inspect]
    kubernetes:
      provider: custom
      certificates:
        order: control-plane-last
        batchSize: 2
        targets:
          - id: cp-1
            transport: local
            target: local://localhost
            inspectCommand: "printf '{}'"
            renewCommand: "true"
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	inspect := p.ByID["k8s.cert.inspect/cert-inspect"]
	if inspect == nil {
		t.Fatalf("missing cert inspect node; ids=%v", nodeIDs(p.Nodes))
	}
	if inspect.Cluster.Name != "" {
		t.Fatalf("cert inspect unexpectedly required cluster: %#v", inspect.Cluster)
	}
	renew := p.ByID["k8s.cert.renew/cert-renew"]
	if renew == nil {
		t.Fatalf("missing cert renew node; ids=%v", nodeIDs(p.Nodes))
	}
	if inspect.ExecutionGroup >= renew.ExecutionGroup {
		t.Fatalf("expected inspect dependency to precede renew, inspect=%d renew=%d", inspect.ExecutionGroup, renew.ExecutionGroup)
	}
}

func TestCompile_KubernetesCertRejectsInvalidRollingOrder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: lifecycle
nodes:
  - name: cert-renew
    kind: k8s.cert.renew
    kubernetes:
      provider: custom
      certificates:
        order: random
        targets:
          - id: cp-1
            transport: local
            target: local://localhost
            inspectCommand: "printf '{}'"
            renewCommand: "true"
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	_, err = Compile(u, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported kubernetes.certificates.order") {
		t.Fatalf("expected rolling order validation, got %v", err)
	}
}

func TestCompile_KubernetesCertTargetsFromInspectNode(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: lifecycle
nodes:
  - name: cluster-inspect
    kind: k8s.cluster.inspect
    kubernetes:
      cluster:
        transport: local
        target: local://localhost
  - name: cert-inspect
    kind: k8s.cert.inspect
    needs: [cluster-inspect]
    kubernetes:
      provider: auto
      certificates:
        targetsFrom:
          sourceNode: cluster-inspect
          roles: [control-plane, worker]
          transport: local
          targetTemplate: "local://{{ .Name }}"
          inspectCommand: "printf '{}'"
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	certs := p.ByID["k8s.cert.inspect/cert-inspect"]
	if certs == nil {
		t.Fatalf("missing cert inspect node; ids=%v", nodeIDs(p.Nodes))
	}
	if len(certs.Kubernetes.Certificates.Targets) != 0 {
		t.Fatalf("expected no hand-listed targets, got %#v", certs.Kubernetes.Certificates.Targets)
	}
	if got := certs.Kubernetes.Certificates.TargetsFrom.SourceNode; got != "cluster-inspect" {
		t.Fatalf("targetsFrom sourceNode=%q", got)
	}
}

func TestCompile_KubernetesCertTargetsFromRequiresSSHTarget(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: lifecycle
nodes:
  - name: cert-inspect
    kind: k8s.cert.inspect
    kubernetes:
      certificates:
        targetsFrom:
          sourceNode: cluster-inspect
          transport: ssh
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	_, err = Compile(u, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "targetsFrom requires target, targetEnv, or targetTemplate") {
		t.Fatalf("expected targetsFrom ssh target validation, got %v", err)
	}
}

func TestCompile_KubernetesClusterVerifyNodeDoesNotRequireCluster(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: lifecycle
nodes:
  - name: cluster-inspect
    kind: k8s.cluster.inspect
    kubernetes:
      cluster:
        transport: local
        target: local://localhost
  - name: cluster-verify
    kind: k8s.cluster.verify
    needs: [cluster-inspect]
    kubernetes:
      cluster:
        transport: local
        target: local://localhost
        minReadyNodes: 1
        stableIterations: 2
        appProbes:
          - id: gitlab
            command: "curl -fsS http://gitlab.example.test/users/sign_in"
            expect: GitLab
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	inspect := p.ByID["k8s.cluster.inspect/cluster-inspect"]
	if inspect == nil {
		t.Fatalf("missing cluster inspect node; ids=%v", nodeIDs(p.Nodes))
	}
	if inspect.Cluster.Name != "" {
		t.Fatalf("cluster inspect unexpectedly required cluster: %#v", inspect.Cluster)
	}
	verify := p.ByID["k8s.cluster.verify/cluster-verify"]
	if verify == nil {
		t.Fatalf("missing cluster verify node; ids=%v", nodeIDs(p.Nodes))
	}
	if verify.Cluster.Name != "" {
		t.Fatalf("cluster verify unexpectedly required cluster: %#v", verify.Cluster)
	}
}

func TestCompile_KubernetesClusterInspectRequiresSSHTarget(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: lifecycle
nodes:
  - name: cluster-inspect
    kind: k8s.cluster.inspect
    kubernetes:
      cluster:
        transport: ssh
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	_, err = Compile(u, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires kubernetes.cluster.target or targetEnv") {
		t.Fatalf("expected ssh target validation, got %v", err)
	}
}

func TestCompile_KubernetesClusterVerifyRequiresProbeCommand(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: lifecycle
nodes:
  - name: cluster-verify
    kind: k8s.cluster.verify
    kubernetes:
      cluster:
        appProbes:
          - id: gitlab
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	_, err = Compile(u, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires command") {
		t.Fatalf("expected app probe command validation, got %v", err)
	}
}

func TestCompile_MySQLReplicationVerifyNodeDoesNotRequireCluster(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: mysql
nodes:
  - name: mysql-verify
    kind: mysql.replication.verify
    mysql:
      transport: local
      database: torque
      probeTable: probe
      nodes:
        - id: mysql-00
          address: 127.0.0.1
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	verify := p.ByID["mysql.replication.verify/mysql-verify"]
	if verify == nil {
		t.Fatalf("missing mysql verify node; ids=%v", nodeIDs(p.Nodes))
	}
	if verify.Cluster.Name != "" {
		t.Fatalf("mysql verify unexpectedly required cluster: %#v", verify.Cluster)
	}
	if verify.MySQL.ExpectedClusterSize != 1 || verify.MySQL.ExpectedReplicatedNodes != 1 {
		t.Fatalf("mysql verify defaults not applied: %#v", verify.MySQL)
	}
	if verify.MySQL.StableAttempts != defaultMySQLReplicationStableAttempts || verify.MySQL.StableInterval == nil || verify.MySQL.StableInterval.String() != defaultMySQLReplicationStableInterval.String() {
		t.Fatalf("mysql verify stability defaults not applied: %#v", verify.MySQL)
	}
}

func TestCompile_MySQLReplicationVerifyRequiresNodes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: mysql
nodes:
  - name: mysql-verify
    kind: mysql.replication.verify
    mysql:
      transport: ssh
      target: ssh://root@example.test
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	_, err = Compile(u, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires at least one mysql.nodes entry") {
		t.Fatalf("expected mysql node validation, got %v", err)
	}
}

func TestCompile_MySQLReplicationVerifyAllowsNATSTransport(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: mysql
nodes:
  - name: mysql-verify
    kind: mysql.replication.verify
    mysql:
      transport: nats-mesh
      target: torque.lab.assign.agent.mysql
      nodes:
        - id: mysql-00
          address: 127.0.0.1
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	verify := p.ByID["mysql.replication.verify/mysql-verify"]
	if verify == nil {
		t.Fatalf("missing mysql verify node; ids=%v", nodeIDs(p.Nodes))
	}
	if verify.MySQL.Transport != "nats-mesh" || verify.MySQL.Target != "torque.lab.assign.agent.mysql" {
		t.Fatalf("mysql nats transport not preserved: %#v", verify.MySQL)
	}
}

func TestCompile_MySQLReplicationVerifyRequiresNATSTarget(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: mysql
nodes:
  - name: mysql-verify
    kind: mysql.replication.verify
    mysql:
      transport: nats
      nodes:
        - id: mysql-00
          address: 127.0.0.1
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	_, err = Compile(u, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires mysql.target or targetEnv for nats transport") {
		t.Fatalf("expected mysql nats target validation, got %v", err)
	}
}

func TestCompile_KubernetesCertCustomProviderRequiresCommands(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: lifecycle
nodes:
  - name: cert-renew
    kind: k8s.cert.renew
    kubernetes:
      provider: custom
      certificates:
        targets:
          - id: cp-1
            transport: local
            target: local://localhost
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	_, err = Compile(u, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "custom provider requires renewCommand") {
		t.Fatalf("expected custom renew command validation, got %v", err)
	}
}

func TestSelect_ByTagAndIncludeDeps(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: demo
defaults:
  cluster: { name: c1 }
  namespace: ns1
releases:
  - name: db
    chart: ./db
    tags: [core]
  - name: app
    chart: ./app
    tags: [app]
    needs: [db]
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = Select(u, p, nil, Selector{Tags: []string{"app"}})
	if err == nil {
		t.Fatalf("expected missing deps error")
	}
	selected, err := Select(u, p, nil, Selector{Tags: []string{"app"}, IncludeDeps: true})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(selected.Nodes) != 2 {
		t.Fatalf("selected=%d", len(selected.Nodes))
	}
}

func TestSelect_AllowMissingDeps_PrunesNeeds(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: torque.dev/v1
kind: Stack
name: demo
defaults:
  cluster: { name: c1 }
  namespace: ns1
releases:
  - name: db
    chart: ./db
  - name: app
    chart: ./app
    needs: [db]
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	selected, err := Select(u, p, nil, Selector{Releases: []string{"app"}, AllowMissingDeps: true})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(selected.Nodes) != 1 {
		t.Fatalf("selected=%d", len(selected.Nodes))
	}
	if selected.Nodes[0].Name != "app" {
		t.Fatalf("node=%q", selected.Nodes[0].Name)
	}
	if len(selected.Nodes[0].Needs) != 0 {
		t.Fatalf("needs=%v", selected.Nodes[0].Needs)
	}
}

func join(vals []string) string {
	out := ""
	for _, v := range vals {
		out += v + "|"
	}
	return out
}
