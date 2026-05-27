package stack

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
	"github.com/ingresslabs/torque/internal/ops/locks"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
	natsworker "github.com/ingresslabs/torque/internal/ops/transport/nats/worker"
	natsgo "github.com/nats-io/nats.go"
	_ "modernc.org/sqlite"
)

func TestCompile_AllowsCustomNodeKinds(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "cutover.db")
	stackYAML := `apiVersion: torque.dev/v1
kind: Stack
name: custom
defaults:
  cluster:
    name: dev
releases:
  - name: smoke
    kind: action.script
    action:
      idempotent: true
      apply:
        command: ["sh", "-c", "true"]
  - name: cutover
    kind: db.cutover
    database:
      driver: sqlite
      dsn: ` + dbPath + `
      commitSQL: CREATE TABLE IF NOT EXISTS switches(value TEXT PRIMARY KEY);
  - name: package
    kind: host.package.install
    host:
      package: torque-fake-pkg
  - name: service
    kind: host.service.manage
    host:
      service: torque-fake.service
  - name: user
    kind: host.user.manage
    host:
      user: torque-fake-user
      groupName: torque-fake-group
  - name: cron
    kind: host.cron.manage
    host:
      path: ` + filepath.ToSlash(filepath.Join(root, "cron.d", "torque-fake")) + `
      state: Present
      schedule: '* * * * *'
      cronCommand: /bin/true
  - name: systemd
    kind: host.systemd.unit
    host:
      unit: torque-fake.service
      path: ` + filepath.ToSlash(filepath.Join(root, "systemd", "torque-fake.service")) + `
      state: Started
      content: |
        [Unit]
        Description=Torque fake unit
        [Service]
        Type=oneshot
        RemainAfterExit=yes
        ExecStart=/bin/true
  - name: manifest
    kind: k8s.manifest.apply
    kubernetes:
      cluster:
        kubectlCommand: kubectl
      manifest:
        namespace: torque-test
        content: |
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: torque-fake
          data:
            ok: "true"
  - name: manifest-delete
    kind: k8s.manifest.delete
    kubernetes:
      cluster:
        kubectlCommand: kubectl
      manifest:
        namespace: torque-test
        content: |
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: torque-delete
  - name: resource-wait
    kind: k8s.resource.wait
    kubernetes:
      cluster:
        kubectlCommand: kubectl
      resource:
        namespace: torque-test
        kind: deployment
        name: torque-wait
  - name: logs-capture
    kind: k8s.logs.capture
    kubernetes:
      cluster:
        kubectlCommand: kubectl
      logs:
        namespace: torque-test
        kind: deployment
        name: torque-logs
        container: app
        tailLines: 7
        limitBytes: 2048
  - name: events-capture
    kind: k8s.events.capture
    kubernetes:
      cluster:
        kubectlCommand: kubectl
      events:
        namespace: torque-test
        types: [Warning]
        reasons: [Failed]
        eventLimit: 25
`
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatalf("write stack.yaml: %v", err)
	}

	u, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/smoke"].Kind); got != NodeKindAction {
		t.Fatalf("smoke kind=%q", got)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/cutover"].Kind); got != NodeKindDBCutover {
		t.Fatalf("cutover kind=%q", got)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/package"].Kind); got != NodeKindHostPackageInstall {
		t.Fatalf("package kind=%q", got)
	}
	if got := p.ByID["dev/default/package"].Host.State; got != "present" {
		t.Fatalf("package state=%q", got)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/service"].Kind); got != NodeKindHostServiceManage {
		t.Fatalf("service kind=%q", got)
	}
	if got := p.ByID["dev/default/service"].Host.State; got != "started" {
		t.Fatalf("service state=%q", got)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/user"].Kind); got != NodeKindHostUserManage {
		t.Fatalf("user kind=%q", got)
	}
	if got := p.ByID["dev/default/user"].Host.State; got != "present" {
		t.Fatalf("user state=%q", got)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/cron"].Kind); got != NodeKindHostCronManage {
		t.Fatalf("cron kind=%q", got)
	}
	if got := p.ByID["dev/default/cron"].Host.State; got != "present" {
		t.Fatalf("cron state=%q", got)
	}
	if got := p.ByID["dev/default/cron"].Host.CronUser; got != "root" {
		t.Fatalf("cron user=%q", got)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/systemd"].Kind); got != NodeKindHostSystemdUnit {
		t.Fatalf("systemd kind=%q", got)
	}
	if got := p.ByID["dev/default/systemd"].Host.State; got != "started" {
		t.Fatalf("systemd state=%q", got)
	}
	if got := p.ByID["dev/default/systemd"].Host.Mode; got != "0644" {
		t.Fatalf("systemd mode=%q", got)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/manifest"].Kind); got != NodeKindK8sManifestApply {
		t.Fatalf("manifest kind=%q", got)
	}
	if got := p.ByID["dev/default/manifest"].Kubernetes.Cluster.Transport; got != "local" {
		t.Fatalf("manifest transport=%q", got)
	}
	if got := p.ByID["dev/default/manifest"].Kubernetes.Manifest.Namespace; got != "torque-test" {
		t.Fatalf("manifest namespace=%q", got)
	}
	if got := p.ByID["dev/default/manifest"].Kubernetes.Manifest.FieldManager; got != "torque" {
		t.Fatalf("manifest fieldManager=%q", got)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/manifest-delete"].Kind); got != NodeKindK8sManifestDelete {
		t.Fatalf("manifest-delete kind=%q", got)
	}
	if got := p.ByID["dev/default/manifest-delete"].Kubernetes.Cluster.Transport; got != "local" {
		t.Fatalf("manifest-delete transport=%q", got)
	}
	if got := p.ByID["dev/default/manifest-delete"].Kubernetes.Manifest.Namespace; got != "torque-test" {
		t.Fatalf("manifest-delete namespace=%q", got)
	}
	if got := p.ByID["dev/default/manifest-delete"].Kubernetes.Manifest.FieldManager; got != "torque" {
		t.Fatalf("manifest-delete fieldManager=%q", got)
	}
	if got := p.ByID["dev/default/manifest-delete"].Kubernetes.Manifest.PrunePolicy; got != "listed-only" {
		t.Fatalf("manifest-delete prunePolicy=%q", got)
	}
	if !p.ByID["dev/default/manifest-delete"].Kubernetes.Manifest.RemoveOnDelete {
		t.Fatalf("manifest-delete removeOnDelete was not defaulted")
	}
	if got := normalizeNodeKind(p.ByID["dev/default/resource-wait"].Kind); got != NodeKindK8sResourceWait {
		t.Fatalf("resource-wait kind=%q", got)
	}
	if got := p.ByID["dev/default/resource-wait"].Kubernetes.Cluster.Transport; got != "local" {
		t.Fatalf("resource-wait transport=%q", got)
	}
	if got := p.ByID["dev/default/resource-wait"].Kubernetes.Resource.Namespace; got != "torque-test" {
		t.Fatalf("resource-wait namespace=%q", got)
	}
	if got := p.ByID["dev/default/resource-wait"].Kubernetes.Resource.Resource; got != "deployment/torque-wait" {
		t.Fatalf("resource-wait resource=%q", got)
	}
	if got := p.ByID["dev/default/resource-wait"].Kubernetes.Resource.For; got != "condition=Available" {
		t.Fatalf("resource-wait for=%q", got)
	}
	if got := p.ByID["dev/default/resource-wait"].Kubernetes.Resource.EventLimit; got != defaultKubernetesResourceWaitEventLimit {
		t.Fatalf("resource-wait eventLimit=%d", got)
	}
	if p.ByID["dev/default/resource-wait"].Kubernetes.Resource.Timeout == nil || *p.ByID["dev/default/resource-wait"].Kubernetes.Resource.Timeout != defaultKubernetesResourceWaitTimeout {
		t.Fatalf("resource-wait timeout=%v", p.ByID["dev/default/resource-wait"].Kubernetes.Resource.Timeout)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/logs-capture"].Kind); got != NodeKindK8sLogsCapture {
		t.Fatalf("logs-capture kind=%q", got)
	}
	if got := p.ByID["dev/default/logs-capture"].Kubernetes.Cluster.Transport; got != "local" {
		t.Fatalf("logs-capture transport=%q", got)
	}
	if got := p.ByID["dev/default/logs-capture"].Kubernetes.Logs.Namespace; got != "torque-test" {
		t.Fatalf("logs-capture namespace=%q", got)
	}
	if got := p.ByID["dev/default/logs-capture"].Kubernetes.Logs.Resource; got != "deployment/torque-logs" {
		t.Fatalf("logs-capture resource=%q", got)
	}
	if got := p.ByID["dev/default/logs-capture"].Kubernetes.Logs.Container; got != "app" {
		t.Fatalf("logs-capture container=%q", got)
	}
	if got := p.ByID["dev/default/logs-capture"].Kubernetes.Logs.TailLines; got != 7 {
		t.Fatalf("logs-capture tailLines=%d", got)
	}
	if got := p.ByID["dev/default/logs-capture"].Kubernetes.Logs.LimitBytes; got != 2048 {
		t.Fatalf("logs-capture limitBytes=%d", got)
	}
	if got := p.ByID["dev/default/logs-capture"].Kubernetes.Logs.MaxLogRequests; got != defaultKubernetesLogsCaptureMaxRequests {
		t.Fatalf("logs-capture maxLogRequests=%d", got)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/events-capture"].Kind); got != NodeKindK8sEventsCapture {
		t.Fatalf("events-capture kind=%q", got)
	}
	if got := p.ByID["dev/default/events-capture"].Kubernetes.Cluster.Transport; got != "local" {
		t.Fatalf("events-capture transport=%q", got)
	}
	if got := p.ByID["dev/default/events-capture"].Kubernetes.Events.Namespace; got != "torque-test" {
		t.Fatalf("events-capture namespace=%q", got)
	}
	if got := p.ByID["dev/default/events-capture"].Kubernetes.Events.EventLimit; got != 25 {
		t.Fatalf("events-capture eventLimit=%d", got)
	}
	if got := p.ByID["dev/default/events-capture"].Kubernetes.Events.Types; len(got) != 1 || got[0] != "Warning" {
		t.Fatalf("events-capture types=%v", got)
	}
	if got := p.ByID["dev/default/events-capture"].Kubernetes.Events.Reasons; len(got) != 1 || got[0] != "Failed" {
		t.Fatalf("events-capture reasons=%v", got)
	}
}

func TestCompile_AllowsMariaDBCutoverNode(t *testing.T) {
	root := t.TempDir()
	stackYAML := `apiVersion: torque.dev/v1
kind: Stack
name: custom
defaults:
  cluster:
    name: dev
releases:
  - name: cutover
    kind: db.cutover
    database:
      driver: mariadb
      dsnEnv: TORQUE_DB_DSN
      commitSQL: UPDATE cutover_flags SET live = TRUE WHERE name = 'api'
`
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatalf("write stack.yaml: %v", err)
	}

	u, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := normalizeNodeKind(p.ByID["dev/default/cutover"].Kind); got != NodeKindDBCutover {
		t.Fatalf("cutover kind=%q", got)
	}
	if got := p.ByID["dev/default/cutover"].Database.Driver; got != "mariadb" {
		t.Fatalf("driver=%q", got)
	}
}

func TestCompile_AllowsFullDBProgramKinds(t *testing.T) {
	root := t.TempDir()
	stackYAML := `apiVersion: torque.dev/v1
kind: Stack
name: custom
defaults:
  cluster:
    name: dev
releases:
  - name: restore
    kind: db.restore-point
    database:
      driver: sqlite
      dsnEnv: TORQUE_DB_DSN
      restorePointSQL: CREATE TABLE IF NOT EXISTS restore_points(marker TEXT PRIMARY KEY)
  - name: expand
    kind: db.schema-expand
    database:
      driver: sqlite
      dsnEnv: TORQUE_DB_DSN
      expandSQL: CREATE TABLE IF NOT EXISTS shadow_users(id INTEGER PRIMARY KEY, name TEXT NOT NULL)
  - name: backfill
    kind: db.backfill
    database:
      driver: sqlite
      dsnEnv: TORQUE_DB_DSN
      backfill:
        startSQL: SELECT 0
        endSQL: SELECT 10
        batchSQL: INSERT INTO shadow_users(id, name) VALUES ({{.cursor_end}}, 'x')
        batchSize: 5
  - name: verify
    kind: db.verify
    database:
      driver: sqlite
      dsnEnv: TORQUE_DB_DSN
      verifySQL: SELECT 1
  - name: cutover
    kind: db.cutover
    database:
      driver: sqlite
      dsnEnv: TORQUE_DB_DSN
      commitSQL: INSERT INTO shadow_users(id, name) VALUES (11, 'y')
  - name: contract
    kind: db.schema-contract
    database:
      driver: sqlite
      dsnEnv: TORQUE_DB_DSN
      contractSQL: CREATE TABLE IF NOT EXISTS contract_log(entry TEXT PRIMARY KEY)
`
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatalf("write stack.yaml: %v", err)
	}
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, name := range []string{"restore", "expand", "backfill", "verify", "cutover", "contract"} {
		id := "dev/default/" + name
		if _, ok := p.ByID[id]; !ok {
			t.Fatalf("missing node %s", id)
		}
	}
}

func TestComputeEffectiveInputHash_CustomNodeDigestChanges(t *testing.T) {
	stackRoot := t.TempDir()
	gid := &GitIdentity{Commit: "abc123", Dirty: false}

	actionA := &ResolvedRelease{
		ID:        "c/default/a",
		Kind:      NodeKindAction,
		Name:      "a",
		Dir:       stackRoot,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "c"},
		Action: ActionSpec{
			Idempotent: true,
			Apply:      &ScriptHookConfig{Command: []string{"sh", "-c", "echo a"}},
		},
	}
	actionB := *actionA
	actionB.Action.Apply = &ScriptHookConfig{Command: []string{"sh", "-c", "echo b"}}

	hashA, _, err := ComputeEffectiveInputHashWithOptions(actionA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash actionA: %v", err)
	}
	hashB, _, err := ComputeEffectiveInputHashWithOptions(&actionB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash actionB: %v", err)
	}
	if hashA == hashB {
		t.Fatalf("expected action hash to change")
	}

	dbA := &ResolvedRelease{
		ID:        "c/default/db",
		Kind:      NodeKindDBCutover,
		Name:      "db",
		Dir:       stackRoot,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "c"},
		Database: DatabaseSpec{
			Driver:    "sqlite",
			DSN:       filepath.Join(stackRoot, "db.sqlite"),
			CommitSQL: "INSERT INTO flags(value) VALUES ('a')",
		},
	}
	dbB := *dbA
	dbB.Database.CommitSQL = "INSERT INTO flags(value) VALUES ('b')"

	dbHashA, _, err := ComputeEffectiveInputHashWithOptions(dbA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash dbA: %v", err)
	}
	dbHashB, _, err := ComputeEffectiveInputHashWithOptions(&dbB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash dbB: %v", err)
	}
	if dbHashA == dbHashB {
		t.Fatalf("expected db hash to change")
	}

	copySource := filepath.Join(stackRoot, "copy-source.conf")
	if err := os.WriteFile(copySource, []byte("copy=a\n"), 0o644); err != nil {
		t.Fatalf("write copy source: %v", err)
	}
	copyA := &ResolvedRelease{
		ID:        "host.file.copy/copy",
		Kind:      NodeKindHostFileCopy,
		Name:      "copy",
		Dir:       stackRoot,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:  "local",
			SourcePath: copySource,
			Path:       filepath.Join(stackRoot, "copied.conf"),
		},
	}
	copyHashA, _, err := ComputeEffectiveInputHashWithOptions(copyA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash copyA: %v", err)
	}
	if err := os.WriteFile(copySource, []byte("copy=b\n"), 0o644); err != nil {
		t.Fatalf("update copy source: %v", err)
	}
	copyHashB, _, err := ComputeEffectiveInputHashWithOptions(copyA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash copyB: %v", err)
	}
	if copyHashA == copyHashB {
		t.Fatalf("expected copy source hash to change")
	}

	pkgA := &ResolvedRelease{
		ID:        "host.package.install/pkg",
		Kind:      NodeKindHostPackageInstall,
		Name:      "pkg",
		Dir:       stackRoot,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:   "local",
			PackageName: "torque-fake-pkg",
			State:       "present",
			Version:     "1.0",
		},
	}
	pkgB := *pkgA
	pkgB.Host.Version = "2.0"
	pkgHashA, _, err := ComputeEffectiveInputHashWithOptions(pkgA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash pkgA: %v", err)
	}
	pkgHashB, _, err := ComputeEffectiveInputHashWithOptions(&pkgB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash pkgB: %v", err)
	}
	if pkgHashA == pkgHashB {
		t.Fatalf("expected package hash to change")
	}

	serviceEnabled := true
	serviceA := &ResolvedRelease{
		ID:        "host.service.manage/svc",
		Kind:      NodeKindHostServiceManage,
		Name:      "svc",
		Dir:       stackRoot,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:   "local",
			ServiceName: "torque-fake.service",
			State:       "started",
			Enabled:     &serviceEnabled,
		},
	}
	serviceB := *serviceA
	serviceB.Host.State = "stopped"
	serviceHashA, _, err := ComputeEffectiveInputHashWithOptions(serviceA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash serviceA: %v", err)
	}
	serviceHashB, _, err := ComputeEffectiveInputHashWithOptions(&serviceB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash serviceB: %v", err)
	}
	if serviceHashA == serviceHashB {
		t.Fatalf("expected service hash to change")
	}

	userUID := 24001
	userGID := 24001
	userA := &ResolvedRelease{
		ID:        "host.user.manage/user",
		Kind:      NodeKindHostUserManage,
		Name:      "user",
		Dir:       stackRoot,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport: "local",
			UserName:  "torque-fake-user",
			GroupName: "torque-fake-group",
			UserGroup: "torque-fake-group",
			State:     "present",
			UID:       &userUID,
			GID:       &userGID,
		},
	}
	userB := *userA
	changedUID := 24002
	userB.Host.UID = &changedUID
	userHashA, _, err := ComputeEffectiveInputHashWithOptions(userA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash userA: %v", err)
	}
	userHashB, _, err := ComputeEffectiveInputHashWithOptions(&userB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash userB: %v", err)
	}
	if userHashA == userHashB {
		t.Fatalf("expected user hash to change")
	}

	cronA := &ResolvedRelease{
		ID:        "host.cron.manage/cron",
		Kind:      NodeKindHostCronManage,
		Name:      "cron",
		Dir:       stackRoot,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:    "local",
			Path:         filepath.Join(stackRoot, "cron.d", "torque-cron"),
			CronName:     "torque-cron",
			CronSchedule: "* * * * *",
			CronUser:     "root",
			CronCommand:  "/bin/true",
			State:        "present",
		},
	}
	cronB := *cronA
	cronB.Host.CronCommand = "/bin/false"
	cronHashA, _, err := ComputeEffectiveInputHashWithOptions(cronA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash cronA: %v", err)
	}
	cronHashB, _, err := ComputeEffectiveInputHashWithOptions(&cronB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash cronB: %v", err)
	}
	if cronHashA == cronHashB {
		t.Fatalf("expected cron hash to change")
	}

	systemdEnabled := true
	systemdA := &ResolvedRelease{
		ID:        "host.systemd.unit/unit",
		Kind:      NodeKindHostSystemdUnit,
		Name:      "unit",
		Dir:       stackRoot,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport: "local",
			UnitName:  "torque-fake.service",
			Path:      filepath.Join(stackRoot, "systemd", "torque-fake.service"),
			Content:   "[Unit]\nDescription=Torque fake\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/bin/true\n",
			State:     "started",
			Enabled:   &systemdEnabled,
		},
	}
	systemdB := *systemdA
	systemdB.Host.Content = strings.Replace(systemdB.Host.Content, "Torque fake", "Torque fake changed", 1)
	systemdHashA, _, err := ComputeEffectiveInputHashWithOptions(systemdA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash systemdA: %v", err)
	}
	systemdHashB, _, err := ComputeEffectiveInputHashWithOptions(&systemdB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash systemdB: %v", err)
	}
	if systemdHashA == systemdHashB {
		t.Fatalf("expected systemd hash to change")
	}

	manifestA := &ResolvedRelease{
		ID:        "k8s.manifest.apply/manifest",
		Kind:      NodeKindK8sManifestApply,
		Name:      "manifest",
		Dir:       stackRoot,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "default"},
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{Transport: "local", KubectlCommand: "kubectl"},
			Manifest: KubernetesManifestSpec{
				Namespace:    "torque-test",
				FieldManager: "torque",
				Content:      "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: torque-fake\ndata:\n  value: a\n",
			},
		},
	}
	manifestB := *manifestA
	manifestB.Kubernetes.Manifest.Content = strings.Replace(manifestB.Kubernetes.Manifest.Content, "value: a", "value: b", 1)
	manifestHashA, _, err := ComputeEffectiveInputHashWithOptions(manifestA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash manifestA: %v", err)
	}
	manifestHashB, _, err := ComputeEffectiveInputHashWithOptions(&manifestB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash manifestB: %v", err)
	}
	if manifestHashA == manifestHashB {
		t.Fatalf("expected manifest hash to change")
	}

	manifestDeleteA := &ResolvedRelease{
		ID:        "k8s.manifest.delete/delete-manifest",
		Kind:      NodeKindK8sManifestDelete,
		Name:      "delete-manifest",
		Dir:       stackRoot,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "default"},
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{Transport: "local", KubectlCommand: "kubectl"},
			Manifest: KubernetesManifestSpec{
				Namespace:    "torque-test",
				FieldManager: "torque",
				PrunePolicy:  "listed-only",
				Content:      "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: torque-delete\ndata:\n  value: a\n",
			},
		},
	}
	manifestDeleteB := *manifestDeleteA
	manifestDeleteB.Kubernetes.Manifest.Content = strings.Replace(manifestDeleteB.Kubernetes.Manifest.Content, "value: a", "value: b", 1)
	manifestDeleteHashA, _, err := ComputeEffectiveInputHashWithOptions(manifestDeleteA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash manifestDeleteA: %v", err)
	}
	manifestDeleteHashB, _, err := ComputeEffectiveInputHashWithOptions(&manifestDeleteB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash manifestDeleteB: %v", err)
	}
	if manifestDeleteHashA == manifestDeleteHashB {
		t.Fatalf("expected manifest delete hash to change")
	}

	waitTimeoutA := 30 * time.Second
	waitTimeoutB := 45 * time.Second
	resourceWaitA := &ResolvedRelease{
		ID:        "k8s.resource.wait/wait-ready",
		Kind:      NodeKindK8sResourceWait,
		Name:      "wait-ready",
		Dir:       stackRoot,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "default"},
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{Transport: "local", KubectlCommand: "kubectl"},
			Resource: KubernetesResourceSpec{
				Namespace:  "torque-test",
				Kind:       "deployment",
				Name:       "torque-ready",
				Resource:   "deployment/torque-ready",
				For:        "condition=Available",
				Timeout:    &waitTimeoutA,
				EventLimit: 10,
			},
		},
	}
	resourceWaitB := *resourceWaitA
	resourceWaitB.Kubernetes.Resource.Timeout = &waitTimeoutB
	resourceWaitHashA, _, err := ComputeEffectiveInputHashWithOptions(resourceWaitA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash resourceWaitA: %v", err)
	}
	resourceWaitHashB, _, err := ComputeEffectiveInputHashWithOptions(&resourceWaitB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash resourceWaitB: %v", err)
	}
	if resourceWaitHashA == resourceWaitHashB {
		t.Fatalf("expected resource wait hash to change")
	}

	logSince := 30 * time.Second
	logsCaptureA := &ResolvedRelease{
		ID:        "k8s.logs.capture/capture-logs",
		Kind:      NodeKindK8sLogsCapture,
		Name:      "capture-logs",
		Dir:       stackRoot,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "default"},
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{Transport: "local", KubectlCommand: "kubectl"},
			Logs: KubernetesLogsSpec{
				Namespace:     "torque-test",
				Kind:          "deployment",
				Name:          "torque-logs",
				Resource:      "deployment/torque-logs",
				Container:     "app",
				Since:         &logSince,
				TailLines:     20,
				LimitBytes:    4096,
				AllContainers: false,
			},
		},
	}
	logsCaptureB := *logsCaptureA
	logsCaptureB.Kubernetes.Logs.TailLines = 25
	logsCaptureHashA, _, err := ComputeEffectiveInputHashWithOptions(logsCaptureA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash logsCaptureA: %v", err)
	}
	logsCaptureHashB, _, err := ComputeEffectiveInputHashWithOptions(&logsCaptureB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash logsCaptureB: %v", err)
	}
	if logsCaptureHashA == logsCaptureHashB {
		t.Fatalf("expected logs capture hash to change")
	}

	eventsCaptureA := &ResolvedRelease{
		ID:        "k8s.events.capture/capture-events",
		Kind:      NodeKindK8sEventsCapture,
		Name:      "capture-events",
		Dir:       stackRoot,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "default"},
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{Transport: "local", KubectlCommand: "kubectl"},
			Events: KubernetesEventsSpec{
				Namespace:  "torque-test",
				Types:      []string{"Warning"},
				Reasons:    []string{"Failed"},
				EventLimit: 50,
			},
		},
	}
	eventsCaptureB := *eventsCaptureA
	eventsCaptureB.Kubernetes.Events.Reasons = []string{"Failed", "BackOff"}
	eventsCaptureHashA, _, err := ComputeEffectiveInputHashWithOptions(eventsCaptureA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash eventsCaptureA: %v", err)
	}
	eventsCaptureHashB, _, err := ComputeEffectiveInputHashWithOptions(&eventsCaptureB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash eventsCaptureB: %v", err)
	}
	if eventsCaptureHashA == eventsCaptureHashB {
		t.Fatalf("expected events capture hash to change")
	}

	renewBefore := 24 * time.Hour
	k8sA := &ResolvedRelease{
		ID:        "k8s.cert.renew/certs",
		Kind:      NodeKindK8sCertRenew,
		Name:      "certs",
		Dir:       stackRoot,
		Namespace: "default",
		Kubernetes: KubernetesSpec{
			Provider: "custom",
			Certificates: KubernetesCertSpec{
				RenewBefore: &renewBefore,
				Force:       true,
				ForceOnceID: "run-a",
				StatePath:   "/var/lib/torque/certs.json",
				Targets: []KubernetesCertTarget{
					{
						ID:             "cp-1",
						Transport:      "local",
						Target:         "local://localhost",
						InspectCommand: "inspect",
						RenewCommand:   "renew",
					},
				},
			},
		},
	}
	k8sB := *k8sA
	k8sB.Kubernetes.Certificates.ForceOnceID = "run-b"

	k8sHashA, _, err := ComputeEffectiveInputHashWithOptions(k8sA, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash k8sA: %v", err)
	}
	k8sHashB, _, err := ComputeEffectiveInputHashWithOptions(&k8sB, EffectiveInputHashOptions{StackRoot: stackRoot, StackGitIdentity: gid})
	if err != nil {
		t.Fatalf("hash k8sB: %v", err)
	}
	if k8sHashA == k8sHashB {
		t.Fatalf("expected k8s hash to change")
	}
}

func TestRun_ActionScriptNode(t *testing.T) {
	root := t.TempDir()
	outFile := filepath.Join(root, "action.txt")
	node := &ResolvedRelease{
		ID:        "local/default/action",
		Kind:      NodeKindAction,
		Name:      "action",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "local"},
		Action: ActionSpec{
			Idempotent: true,
			Apply: &ScriptHookConfig{
				Command: []string{"sh", "-c", "printf '%s\n' '{\"status\":\"ok\",\"component\":\"precheck\"}' && echo ok > " + shellQuoteForTest(outFile)},
			},
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, errOut.String())
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read action output: %v", err)
	}
	if got := string(bytes.TrimSpace(raw)); got != "ok" {
		t.Fatalf("action output=%q", got)
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	if !audit.Integrity.EventsOK || !audit.Integrity.RunDigestOK {
		t.Fatalf("unexpected audit integrity: %#v", audit.Integrity)
	}
	found := false
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID == node.ID && artifact.Name == "script-output.json" {
			found = strings.Contains(artifact.Body, `"outputFormat": "json"`) &&
				strings.Contains(artifact.Body, `"component": "precheck"`) &&
				strings.Contains(artifact.Body, `"status": "ok"`)
			break
		}
	}
	if !found {
		t.Fatalf("missing script-output.json artifact in %+v", audit.Artifacts)
	}
}

func TestRun_HostCommandRunLocalNode(t *testing.T) {
	root := t.TempDir()
	outFile := filepath.Join(root, "host-command.txt")
	node := &ResolvedRelease{
		ID:        "host.command.run/write-marker",
		Kind:      NodeKindHostCommandRun,
		Name:      "write-marker",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:     "local",
			Command:       "printf ok > " + shellQuoteForTest(outFile),
			DeleteCommand: "rm -f " + shellQuoteForTest(outFile),
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read host command output: %v", err)
	}
	if got := string(bytes.TrimSpace(raw)); got != "ok" {
		t.Fatalf("host command output=%q", got)
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	found := false
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID == node.ID && artifact.Name == "host-command.json" {
			found = strings.Contains(artifact.Body, `"HostCommandNodeArtifact"`) &&
				strings.Contains(artifact.Body, `"status": "succeeded"`)
			break
		}
	}
	if !found {
		t.Fatalf("missing host-command.json artifact in %+v", audit.Artifacts)
	}
	if audit.Ops != nil {
		t.Fatalf("legacy host command run unexpectedly produced ops audit: %#v", audit.Ops)
	}
	bundlePath := filepath.Join(root, "host-export.tgz")
	if _, err := ExportRunBundle(context.Background(), root, runID, bundlePath); err != nil {
		t.Fatalf("ExportRunBundle: %v", err)
	}
	bundleAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		BundlePath:       bundlePath,
		VerifyBundle:     true,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit from bundle: %v", err)
	}
	if bundleAudit.Ops != nil {
		t.Fatalf("legacy host command bundle unexpectedly produced ops audit: %#v", bundleAudit.Ops)
	}
	extracted, err := ExtractBundleToTempDir(bundlePath)
	if err != nil {
		t.Fatalf("ExtractBundleToTempDir: %v", err)
	}
	defer os.RemoveAll(extracted)
	exportDB, err := sql.Open("sqlite", filepath.Join(extracted, "state.sqlite"))
	if err != nil {
		t.Fatalf("open exported sqlite: %v", err)
	}
	defer exportDB.Close()
	var fieldsJSON string
	if err := exportDB.QueryRow(`SELECT fields_json FROM torque_stack_events WHERE node_id = ? AND type = ? AND message = ?`, node.ID, string(PhaseCompleted), "success").Scan(&fieldsJSON); err != nil {
		t.Fatalf("query exported host event fields: %v", err)
	}
	if !strings.Contains(fieldsJSON, `"receipt"`) || !strings.Contains(fieldsJSON, `"targetDigest"`) {
		t.Fatalf("exported host event lost fields_json: %q", fieldsJSON)
	}

	if err := Run(context.Background(), RunOptions{
		Command:     "delete",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run delete: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(outFile); !os.IsNotExist(err) {
		t.Fatalf("expected delete command to remove marker, stat err=%v", err)
	}
}

func TestRun_HostFileRenderLocalNode(t *testing.T) {
	root := t.TempDir()
	outFile := filepath.Join(root, "rendered.conf")
	node := &ResolvedRelease{
		ID:        "host.file.render/render-config",
		Kind:      NodeKindHostFileRender,
		Name:      "render-config",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:      "local",
			Path:           outFile,
			Template:       "name={{ .Name }}\nrun={{ .RunID }}\n",
			Data:           map[string]any{"Name": "api", "RunID": "ops-host-002"},
			Mode:           "0600",
			Validate:       `test -s "$TORQUE_FILE_RENDER_TEMP_PATH"`,
			RemoveOnDelete: true,
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read rendered file: %v", err)
	}
	if got := string(raw); got != "name=api\nrun=ops-host-002\n" {
		t.Fatalf("rendered content=%q", got)
	}
	if info, err := os.Stat(outFile); err != nil {
		t.Fatalf("stat rendered file: %v", err)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode=%#o want 0600", got)
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"host-file-observe.json", "host-file-plan.json", "host-file-diff.json", "host-file-apply.json", "host-file-verify.json", "host-file-render.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	applyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-file-apply.json")
	if !strings.Contains(applyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(applyArtifact, `"changed": true`) ||
		strings.Contains(applyArtifact, "name=api") {
		t.Fatalf("host file apply artifact did not record a redacted changed receipt:\n%s", applyArtifact)
	}
	diffArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-file-diff.json")
	if !strings.Contains(diffArtifact, `"diffQuality": "exact"`) ||
		!strings.Contains(diffArtifact, `"content": true`) {
		t.Fatalf("host file diff artifact missing exact content diff:\n%s", diffArtifact)
	}

	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run repeat apply: %v\nstderr=%s", err, errOut.String())
	}
	repeatRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun repeat: %v", err)
	}
	repeatAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            repeatRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit repeat: %v", err)
	}
	repeatApply := auditArtifactBody(t, repeatAudit.Artifacts, node.ID, "host-file-apply.json")
	if !strings.Contains(repeatApply, `"changed": false`) {
		t.Fatalf("repeat render was not a no-op:\n%s", repeatApply)
	}

	if err := Run(context.Background(), RunOptions{
		Command:     "delete",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run delete: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(outFile); !os.IsNotExist(err) {
		t.Fatalf("expected delete to remove rendered file, stat err=%v", err)
	}
}

func TestRun_HostFileCopyLocalNode(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "files")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	sourceFile := filepath.Join(sourceDir, "source.conf")
	if err := os.WriteFile(sourceFile, []byte("copied-value=ops-host-003\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	outFile := filepath.Join(root, "copied.conf")
	original := "original-value=restore-me\n"
	if err := os.WriteFile(outFile, []byte(original), 0o644); err != nil {
		t.Fatalf("write original file: %v", err)
	}
	node := &ResolvedRelease{
		ID:        "host.file.copy/copy-config",
		Kind:      NodeKindHostFileCopy,
		Name:      "copy-config",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:       "local",
			SourcePath:      sourceFile,
			Path:            outFile,
			Mode:            "0600",
			Validate:        `grep -q copied-value "$TORQUE_FILE_COPY_TEMP_PATH"`,
			Backup:          true,
			RestoreOnDelete: true,
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if got := string(raw); got != "copied-value=ops-host-003\n" {
		t.Fatalf("copied content=%q", got)
	}
	if info, err := os.Stat(outFile); err != nil {
		t.Fatalf("stat copied file: %v", err)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode=%#o want 0600", got)
	}
	backupPath := outFile + ".torque-backup"
	backupRaw, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup file: %v", err)
	}
	if string(backupRaw) != original {
		t.Fatalf("backup content=%q", string(backupRaw))
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"host-file-copy-observe.json", "host-file-copy-plan.json", "host-file-copy-diff.json", "host-file-copy-apply.json", "host-file-copy-verify.json", "host-file-copy.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	applyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-file-copy-apply.json")
	if !strings.Contains(applyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(applyArtifact, `"changed": true`) ||
		strings.Contains(applyArtifact, "ops-host-003") ||
		strings.Contains(applyArtifact, "original-value") {
		t.Fatalf("host file copy apply artifact did not record a redacted changed receipt:\n%s", applyArtifact)
	}
	diffArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-file-copy-diff.json")
	if !strings.Contains(diffArtifact, `"diffQuality": "exact"`) ||
		!strings.Contains(diffArtifact, `"content": true`) {
		t.Fatalf("host file copy diff artifact missing exact content diff:\n%s", diffArtifact)
	}

	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run repeat apply: %v\nstderr=%s", err, errOut.String())
	}
	repeatRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun repeat: %v", err)
	}
	repeatAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            repeatRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit repeat: %v", err)
	}
	repeatApply := auditArtifactBody(t, repeatAudit.Artifacts, node.ID, "host-file-copy-apply.json")
	if !strings.Contains(repeatApply, `"changed": false`) {
		t.Fatalf("repeat copy was not a no-op:\n%s", repeatApply)
	}

	if err := Run(context.Background(), RunOptions{
		Command:     "delete",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run delete: %v\nstderr=%s", err, errOut.String())
	}
	restored, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(restored) != original {
		t.Fatalf("restored content=%q", string(restored))
	}
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("expected backup to be removed after restore, stat err=%v", err)
	}
	deleteRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun delete: %v", err)
	}
	deleteAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            deleteRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit delete: %v", err)
	}
	deleteApply := auditArtifactBody(t, deleteAudit.Artifacts, node.ID, "host-file-copy-apply.json")
	if !strings.Contains(deleteApply, `"restored": true`) {
		t.Fatalf("delete did not record restore:\n%s", deleteApply)
	}
}

func TestRun_HostPackageInstallLocalNode(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	stateFile := filepath.Join(root, "package-installed")
	logFile := filepath.Join(root, "package-manager.log")
	writeExecutableForTest(t, filepath.Join(binDir, "dpkg-query"), `#!/bin/sh
if [ -f `+shellQuoteForTest(stateFile)+` ]; then
  printf 'install ok installed\t1.0'
  exit 0
fi
exit 1
`)
	writeExecutableForTest(t, filepath.Join(binDir, "apt-cache"), `#!/bin/sh
printf '%s\n' 'torque-fake-pkg:' '  Installed: (none)' '  Candidate: 1.0'
`)
	writeExecutableForTest(t, filepath.Join(binDir, "apt-get"), `#!/bin/sh
printf 'password=package-secret\n'
printf 'token=package-stderr\n' >&2
case "$1" in
  install)
    printf 'install\n' >> `+shellQuoteForTest(logFile)+`
    printf installed > `+shellQuoteForTest(stateFile)+`
    ;;
  remove|purge)
    printf 'remove\n' >> `+shellQuoteForTest(logFile)+`
    rm -f `+shellQuoteForTest(stateFile)+`
    ;;
  update)
    printf 'update\n' >> `+shellQuoteForTest(logFile)+`
    ;;
  *)
    exit 2
    ;;
esac
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	node := &ResolvedRelease{
		ID:        "host.package.install/install-pkg",
		Kind:      NodeKindHostPackageInstall,
		Name:      "install-pkg",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:      "local",
			PackageName:    "torque-fake-pkg",
			PackageManager: "apt",
			State:          "present",
			RemoveOnDelete: true,
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("package was not installed: %v", err)
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"host-package-observe.json", "host-package-plan.json", "host-package-diff.json", "host-package-apply.json", "host-package-verify.json", "host-package.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	applyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-package-apply.json")
	if !strings.Contains(applyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(applyArtifact, `"changed": true`) ||
		!strings.Contains(applyArtifact, `"packageManager": "apt"`) ||
		strings.Contains(applyArtifact, "package-secret") ||
		strings.Contains(applyArtifact, "package-stderr") {
		t.Fatalf("host package apply artifact did not record a redacted changed receipt:\n%s", applyArtifact)
	}

	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run repeat apply: %v\nstderr=%s", err, errOut.String())
	}
	repeatRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun repeat: %v", err)
	}
	repeatAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            repeatRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit repeat: %v", err)
	}
	repeatApply := auditArtifactBody(t, repeatAudit.Artifacts, node.ID, "host-package-apply.json")
	if !strings.Contains(repeatApply, `"changed": false`) {
		t.Fatalf("repeat package apply was not a no-op:\n%s", repeatApply)
	}

	if err := Run(context.Background(), RunOptions{
		Command:     "delete",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run delete: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("expected delete to remove fake package state, stat err=%v", err)
	}
	deleteRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun delete: %v", err)
	}
	deleteAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            deleteRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit delete: %v", err)
	}
	deleteApply := auditArtifactBody(t, deleteAudit.Artifacts, node.ID, "host-package-apply.json")
	if !strings.Contains(deleteApply, `"desiredState": "absent"`) ||
		!strings.Contains(deleteApply, `"changed": true`) ||
		!strings.Contains(deleteApply, `"installed": false`) {
		t.Fatalf("delete did not record package removal:\n%s", deleteApply)
	}
}

func TestRun_HostServiceManageLocalNode(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	activeFile := filepath.Join(root, "service-active")
	enabledFile := filepath.Join(root, "service-enabled")
	logFile := filepath.Join(root, "service-manager.log")
	writeExecutableForTest(t, filepath.Join(binDir, "systemctl"), `#!/bin/sh
active_file=`+shellQuoteForTest(activeFile)+`
enabled_file=`+shellQuoteForTest(enabledFile)+`
log_file=`+shellQuoteForTest(logFile)+`
emit_sensitive() {
  printf 'password=service-secret\n'
  printf 'token=service-stderr\n' >&2
}
cmd="$1"
shift || true
case "$cmd" in
  --version)
    printf 'systemd 255\n'
    ;;
  show)
    active=inactive
    sub=dead
    if [ -f "$active_file" ]; then
      active=active
      sub=running
    fi
    unit=disabled
    if [ -f "$enabled_file" ]; then
      unit=enabled
    fi
    printf 'LoadState=loaded\n'
    printf 'ActiveState=%s\n' "$active"
    printf 'SubState=%s\n' "$sub"
    printf 'UnitFileState=%s\n' "$unit"
    ;;
  start)
    emit_sensitive
    printf 'start\n' >> "$log_file"
    printf active > "$active_file"
    ;;
  stop)
    emit_sensitive
    printf 'stop\n' >> "$log_file"
    rm -f "$active_file"
    ;;
  restart)
    emit_sensitive
    printf 'restart\n' >> "$log_file"
    printf active > "$active_file"
    ;;
  enable)
    emit_sensitive
    printf 'enable\n' >> "$log_file"
    printf enabled > "$enabled_file"
    ;;
  disable)
    emit_sensitive
    printf 'disable\n' >> "$log_file"
    rm -f "$enabled_file"
    ;;
  *)
    exit 2
    ;;
esac
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	enabled := true
	node := &ResolvedRelease{
		ID:        "host.service.manage/manage-svc",
		Kind:      NodeKindHostServiceManage,
		Name:      "manage-svc",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:       "local",
			ServiceName:     "torque-fake.service",
			ServiceManager:  "systemd",
			State:           "started",
			Enabled:         &enabled,
			StopOnDelete:    true,
			DisableOnDelete: true,
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(activeFile); err != nil {
		t.Fatalf("service was not started: %v", err)
	}
	if _, err := os.Stat(enabledFile); err != nil {
		t.Fatalf("service was not enabled: %v", err)
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"host-service-observe.json", "host-service-plan.json", "host-service-diff.json", "host-service-apply.json", "host-service-verify.json", "host-service.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	applyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-service-apply.json")
	if !strings.Contains(applyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(applyArtifact, `"changed": true`) ||
		!strings.Contains(applyArtifact, `"serviceManager": "systemd"`) ||
		!strings.Contains(applyArtifact, `"active": true`) ||
		!strings.Contains(applyArtifact, `"enabled": true`) ||
		strings.Contains(applyArtifact, "service-secret") ||
		strings.Contains(applyArtifact, "service-stderr") {
		t.Fatalf("host service apply artifact did not record a redacted changed receipt:\n%s", applyArtifact)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run repeat apply: %v\nstderr=%s", err, errOut.String())
	}
	repeatRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun repeat: %v", err)
	}
	repeatAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            repeatRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit repeat: %v", err)
	}
	repeatApply := auditArtifactBody(t, repeatAudit.Artifacts, node.ID, "host-service-apply.json")
	if !strings.Contains(repeatApply, `"changed": false`) {
		t.Fatalf("repeat service apply was not a no-op:\n%s", repeatApply)
	}

	restartNode := *node
	restartNode.ID = "host.service.manage/restart-svc"
	restartNode.Name = "restart-svc"
	restartNode.Host.State = "restarted"
	restartNode.Host.Enabled = nil
	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        planForTest(root, &restartNode),
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run restart apply: %v\nstderr=%s", err, errOut.String())
	}
	restartRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun restart: %v", err)
	}
	restartAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            restartRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit restart: %v", err)
	}
	restartApply := auditArtifactBody(t, restartAudit.Artifacts, restartNode.ID, "host-service-apply.json")
	if !strings.Contains(restartApply, `"restart": true`) || !strings.Contains(restartApply, `"changed": true`) {
		t.Fatalf("restart service apply did not record restart change:\n%s", restartApply)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "delete",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run delete: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(activeFile); !os.IsNotExist(err) {
		t.Fatalf("expected delete to stop fake service, stat err=%v", err)
	}
	if _, err := os.Stat(enabledFile); !os.IsNotExist(err) {
		t.Fatalf("expected delete to disable fake service, stat err=%v", err)
	}
	deleteRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun delete: %v", err)
	}
	deleteAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            deleteRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit delete: %v", err)
	}
	deleteApply := auditArtifactBody(t, deleteAudit.Artifacts, node.ID, "host-service-apply.json")
	if !strings.Contains(deleteApply, `"desiredState": "stopped"`) ||
		!strings.Contains(deleteApply, `"desiredEnabled": false`) ||
		!strings.Contains(deleteApply, `"changed": true`) ||
		!strings.Contains(deleteApply, `"active": false`) ||
		!strings.Contains(deleteApply, `"enabled": false`) {
		t.Fatalf("delete did not record service stop/disable:\n%s", deleteApply)
	}
}

func TestRun_HostSystemdUnitLocalNode(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	unitPath := filepath.Join(root, "systemd", "torque-fake.service")
	activeFile := filepath.Join(root, "systemd-active")
	enabledFile := filepath.Join(root, "systemd-enabled")
	logFile := filepath.Join(root, "systemd-manager.log")
	writeExecutableForTest(t, filepath.Join(binDir, "systemctl"), `#!/bin/sh
unit_path=`+shellQuoteForTest(unitPath)+`
active_file=`+shellQuoteForTest(activeFile)+`
enabled_file=`+shellQuoteForTest(enabledFile)+`
log_file=`+shellQuoteForTest(logFile)+`
emit_sensitive() {
  printf 'password=systemd-secret\n'
  printf 'token=systemd-stderr\n' >&2
}
cmd="$1"
shift || true
case "$cmd" in
  --version)
    printf 'systemd 255\n'
    ;;
  show)
    active=inactive
    sub=dead
    if [ -f "$active_file" ]; then
      active=active
      sub=running
    fi
    unit=disabled
    if [ -f "$enabled_file" ]; then
      unit=enabled
    fi
    load=not-found
    if [ -f "$unit_path" ]; then
      load=loaded
    fi
    printf 'LoadState=%s\n' "$load"
    printf 'ActiveState=%s\n' "$active"
    printf 'SubState=%s\n' "$sub"
    printf 'UnitFileState=%s\n' "$unit"
    ;;
  daemon-reload)
    emit_sensitive
    printf 'daemon-reload\n' >> "$log_file"
    ;;
  start)
    emit_sensitive
    printf 'start\n' >> "$log_file"
    printf active > "$active_file"
    ;;
  stop)
    emit_sensitive
    printf 'stop\n' >> "$log_file"
    rm -f "$active_file"
    ;;
  restart)
    emit_sensitive
    printf 'restart\n' >> "$log_file"
    printf active > "$active_file"
    ;;
  enable)
    emit_sensitive
    printf 'enable\n' >> "$log_file"
    printf enabled > "$enabled_file"
    ;;
  disable)
    emit_sensitive
    printf 'disable\n' >> "$log_file"
    rm -f "$enabled_file"
    ;;
  *)
    exit 2
    ;;
esac
`)
	writeExecutableForTest(t, filepath.Join(binDir, "journalctl"), `#!/bin/sh
printf '2026-05-25T00:00:00Z torque-fake systemd-secret journal line\n'
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	enabled := true
	node := &ResolvedRelease{
		ID:        "host.systemd.unit/manage-unit",
		Kind:      NodeKindHostSystemdUnit,
		Name:      "manage-unit",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:       "local",
			UnitName:        "torque-fake.service",
			Path:            unitPath,
			Content:         "[Unit]\nDescription=Torque fake unit\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/bin/true\n",
			Mode:            "0644",
			State:           "started",
			Enabled:         &enabled,
			StopOnDelete:    true,
			DisableOnDelete: true,
			RemoveOnDelete:  true,
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(unitPath); err != nil {
		t.Fatalf("systemd unit file was not written: %v", err)
	}
	if _, err := os.Stat(activeFile); err != nil {
		t.Fatalf("systemd unit was not started: %v", err)
	}
	if _, err := os.Stat(enabledFile); err != nil {
		t.Fatalf("systemd unit was not enabled: %v", err)
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"host-systemd-observe.json", "host-systemd-plan.json", "host-systemd-diff.json", "host-systemd-apply.json", "host-systemd-verify.json", "host-systemd-journal.json", "journal-evidence.json", "host-systemd.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	applyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-systemd-apply.json")
	if !strings.Contains(applyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(applyArtifact, `"changed": true`) ||
		!strings.Contains(applyArtifact, `"daemonReload": true`) ||
		!strings.Contains(applyArtifact, `"active": true`) ||
		!strings.Contains(applyArtifact, `"enabled": true`) ||
		!strings.Contains(applyArtifact, `"journal"`) ||
		strings.Contains(applyArtifact, "systemd-secret") ||
		strings.Contains(applyArtifact, "systemd-stderr") {
		t.Fatalf("host systemd apply artifact did not record a redacted changed receipt:\n%s", applyArtifact)
	}
	diffArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-systemd-diff.json")
	if !strings.Contains(diffArtifact, `"diffQuality": "exact"`) ||
		!strings.Contains(diffArtifact, `"desiredDigest": "sha256:`) {
		t.Fatalf("host systemd diff artifact missing exact digest diff:\n%s", diffArtifact)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run repeat apply: %v\nstderr=%s", err, errOut.String())
	}
	repeatRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun repeat: %v", err)
	}
	repeatAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            repeatRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit repeat: %v", err)
	}
	repeatApply := auditArtifactBody(t, repeatAudit.Artifacts, node.ID, "host-systemd-apply.json")
	if !strings.Contains(repeatApply, `"changed": false`) {
		t.Fatalf("repeat systemd unit apply was not a no-op:\n%s", repeatApply)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "delete",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run delete: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected delete to remove fake unit, stat err=%v", err)
	}
	if _, err := os.Stat(activeFile); !os.IsNotExist(err) {
		t.Fatalf("expected delete to stop fake unit, stat err=%v", err)
	}
	if _, err := os.Stat(enabledFile); !os.IsNotExist(err) {
		t.Fatalf("expected delete to disable fake unit, stat err=%v", err)
	}
	deleteRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun delete: %v", err)
	}
	deleteAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            deleteRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit delete: %v", err)
	}
	deleteApply := auditArtifactBody(t, deleteAudit.Artifacts, node.ID, "host-systemd-apply.json")
	if !strings.Contains(deleteApply, `"desiredState": "absent"`) ||
		!strings.Contains(deleteApply, `"changed": true`) ||
		!strings.Contains(deleteApply, `"active": false`) ||
		!strings.Contains(deleteApply, `"enabled": false`) ||
		!strings.Contains(deleteApply, `"exists": false`) {
		t.Fatalf("delete did not record systemd unit cleanup:\n%s", deleteApply)
	}
}

func TestRun_HostUserManageLocalNode(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	userFile := filepath.Join(root, "user-present")
	groupFile := filepath.Join(root, "group-present")
	homeDir := filepath.Join(root, "home")
	logFile := filepath.Join(root, "user-manager.log")
	fakeCommand := `#!/bin/sh
cmd="$(basename "$0")"
user_file=` + shellQuoteForTest(userFile) + `
group_file=` + shellQuoteForTest(groupFile) + `
home_dir=` + shellQuoteForTest(homeDir) + `
log_file=` + shellQuoteForTest(logFile) + `
user="torque-fake-user"
group="torque-fake-group"
uid="24001"
gid="24001"
emit_sensitive() {
  printf 'password=user-secret\n'
  printf 'token=user-stderr\n' >&2
}
case "$cmd" in
  getent)
    db="$1"
    key="$2"
    if [ "$db" = passwd ] && [ "$key" = "$user" ] && [ -f "$user_file" ]; then
      printf '%s:x:%s:%s:Torque Fake:%s:/usr/sbin/nologin\n' "$user" "$uid" "$gid" "$home_dir"
      exit 0
    fi
    if [ "$db" = group ] && { [ "$key" = "$group" ] || [ "$key" = "$gid" ]; } && [ -f "$group_file" ]; then
      printf '%s:x:%s:\n' "$group" "$gid"
      exit 0
    fi
    exit 2
    ;;
  id)
    if [ "$1" = "-nG" ] && [ "$2" = "$user" ] && [ -f "$user_file" ]; then
      printf '%s\n' "$group"
      exit 0
    fi
    exit 1
    ;;
  groupadd|groupmod)
    emit_sensitive
    printf '%s\n' "$cmd" >> "$log_file"
    printf group > "$group_file"
    ;;
  useradd|usermod)
    emit_sensitive
    printf '%s\n' "$cmd" >> "$log_file"
    printf user > "$user_file"
    mkdir -p "$home_dir"
    ;;
  userdel)
    emit_sensitive
    printf 'userdel\n' >> "$log_file"
    rm -f "$user_file"
    rm -rf "$home_dir"
    ;;
  groupdel)
    emit_sensitive
    printf 'groupdel\n' >> "$log_file"
    rm -f "$group_file"
    ;;
  *)
    exit 2
    ;;
esac
`
	for _, name := range []string{"getent", "id", "groupadd", "groupmod", "groupdel", "useradd", "usermod", "userdel"} {
		writeExecutableForTest(t, filepath.Join(binDir, name), fakeCommand)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	uid := 24001
	gid := 24001
	node := &ResolvedRelease{
		ID:        "host.user.manage/manage-user",
		Kind:      NodeKindHostUserManage,
		Name:      "manage-user",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:      "local",
			UserName:       "torque-fake-user",
			GroupName:      "torque-fake-group",
			UserGroup:      "torque-fake-group",
			State:          "present",
			UID:            &uid,
			GID:            &gid,
			Home:           homeDir,
			Shell:          "/usr/sbin/nologin",
			Comment:        "Torque Fake",
			CreateHome:     true,
			RemoveHome:     true,
			RemoveOnDelete: true,
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(userFile); err != nil {
		t.Fatalf("user was not created: %v", err)
	}
	if _, err := os.Stat(groupFile); err != nil {
		t.Fatalf("group was not created: %v", err)
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"host-user-observe.json", "host-user-plan.json", "host-user-diff.json", "host-user-apply.json", "host-user-verify.json", "host-user.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	applyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-user-apply.json")
	if !strings.Contains(applyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(applyArtifact, `"changed": true`) ||
		!strings.Contains(applyArtifact, `"uid": 24001`) ||
		!strings.Contains(applyArtifact, `"gid": 24001`) ||
		!strings.Contains(applyArtifact, `"group": "torque-fake-group"`) ||
		strings.Contains(applyArtifact, "user-secret") ||
		strings.Contains(applyArtifact, "user-stderr") {
		t.Fatalf("host user apply artifact did not record a redacted changed receipt:\n%s", applyArtifact)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run repeat apply: %v\nstderr=%s", err, errOut.String())
	}
	repeatRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun repeat: %v", err)
	}
	repeatAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            repeatRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit repeat: %v", err)
	}
	repeatApply := auditArtifactBody(t, repeatAudit.Artifacts, node.ID, "host-user-apply.json")
	if !strings.Contains(repeatApply, `"changed": false`) {
		t.Fatalf("repeat user apply was not a no-op:\n%s", repeatApply)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "delete",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run delete: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(userFile); !os.IsNotExist(err) {
		t.Fatalf("expected delete to remove fake user, stat err=%v", err)
	}
	if _, err := os.Stat(groupFile); !os.IsNotExist(err) {
		t.Fatalf("expected delete to remove fake group, stat err=%v", err)
	}
	deleteRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun delete: %v", err)
	}
	deleteAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            deleteRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit delete: %v", err)
	}
	deleteApply := auditArtifactBody(t, deleteAudit.Artifacts, node.ID, "host-user-apply.json")
	if !strings.Contains(deleteApply, `"desiredState": "absent"`) ||
		!strings.Contains(deleteApply, `"changed": true`) ||
		!strings.Contains(deleteApply, `"exists": false`) {
		t.Fatalf("delete did not record user/group removal:\n%s", deleteApply)
	}
}

func TestRun_HostCronManageLocalNode(t *testing.T) {
	root := t.TempDir()
	cronPath := filepath.Join(root, "cron.d", "torque-cron")
	node := &ResolvedRelease{
		ID:        "host.cron.manage/manage-cron",
		Kind:      NodeKindHostCronManage,
		Name:      "manage-cron",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport:      "local",
			Path:           cronPath,
			CronName:       "torque-cron",
			CronSchedule:   "* * * * *",
			CronUser:       "root",
			CronCommand:    "/bin/sh -c 'printf token=cron-secret >/tmp/torque-cron'",
			State:          "present",
			Mode:           "0644",
			RemoveOnDelete: true,
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	content, err := os.ReadFile(cronPath)
	if err != nil {
		t.Fatalf("read cron file: %v", err)
	}
	if !strings.Contains(string(content), "torque managed: torque-cron") || !strings.Contains(string(content), "token=cron-secret") {
		t.Fatalf("unexpected cron content: %q", string(content))
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"host-cron-observe.json", "host-cron-plan.json", "host-cron-diff.json", "host-cron-apply.json", "host-cron-verify.json", "host-cron.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	applyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-cron-apply.json")
	if !strings.Contains(applyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(applyArtifact, `"changed": true`) ||
		!strings.Contains(applyArtifact, `"exists": true`) ||
		strings.Contains(applyArtifact, "cron-secret") {
		t.Fatalf("host cron apply artifact did not record a redacted changed receipt:\n%s", applyArtifact)
	}
	diffArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-cron-diff.json")
	if !strings.Contains(diffArtifact, `"diffQuality": "exact"`) || !strings.Contains(diffArtifact, `"desiredDigest": "sha256:`) {
		t.Fatalf("host cron diff artifact missing exact digest diff:\n%s", diffArtifact)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run repeat apply: %v\nstderr=%s", err, errOut.String())
	}
	repeatRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun repeat: %v", err)
	}
	repeatAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            repeatRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit repeat: %v", err)
	}
	repeatApply := auditArtifactBody(t, repeatAudit.Artifacts, node.ID, "host-cron-apply.json")
	if !strings.Contains(repeatApply, `"changed": false`) {
		t.Fatalf("repeat cron apply was not a no-op:\n%s", repeatApply)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "delete",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run delete: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(cronPath); !os.IsNotExist(err) {
		t.Fatalf("expected delete to remove cron file, stat err=%v", err)
	}
	deleteRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun delete: %v", err)
	}
	deleteAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            deleteRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit delete: %v", err)
	}
	deleteApply := auditArtifactBody(t, deleteAudit.Artifacts, node.ID, "host-cron-apply.json")
	if !strings.Contains(deleteApply, `"desiredState": "absent"`) ||
		!strings.Contains(deleteApply, `"changed": true`) ||
		!strings.Contains(deleteApply, `"exists": false`) {
		t.Fatalf("delete did not record cron removal:\n%s", deleteApply)
	}
}

func TestRun_KubernetesManifestApplyLocalNode(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	stateFile := filepath.Join(root, "manifest-state.yaml")
	writeExecutableForTest(t, filepath.Join(binDir, "kubectl"), `#!/usr/bin/env python3
import json
import os
import shutil
import sys

state_file = os.environ["FAKE_KUBECTL_STATE"]
args = sys.argv[1:]
namespace = "default"
i = 0
while i < len(args):
    arg = args[i]
    if arg in ("--kubeconfig", "--context", "-n", "--namespace"):
        if arg in ("-n", "--namespace"):
            namespace = args[i + 1]
        i += 2
        continue
    if arg.startswith("--kubeconfig=") or arg.startswith("--context="):
        i += 1
        continue
    break
if i >= len(args):
    sys.exit(2)
cmd = args[i]
rest = args[i + 1:]

def manifest_path(values):
    for idx, value in enumerate(values):
        if value == "-f" and idx + 1 < len(values):
            return values[idx + 1]
    return ""

def same_manifest(path):
    if not path or not os.path.exists(state_file):
        return False
    with open(path, "rb") as desired:
        desired_raw = desired.read()
    with open(state_file, "rb") as current:
        current_raw = current.read()
    return desired_raw == current_raw

if cmd == "diff":
    path = manifest_path(rest)
    sys.exit(0 if same_manifest(path) else 1)
if cmd == "apply":
    path = manifest_path(rest)
    if not path:
        sys.exit(2)
    print("password=k8s-apply-secret")
    print("token=k8s-apply-stderr", file=sys.stderr)
    shutil.copyfile(path, state_file)
    sys.exit(0)
if cmd == "delete":
    if os.path.exists(state_file):
        os.unlink(state_file)
    sys.exit(0)
if cmd == "get":
    if len(rest) < 2 or not os.path.exists(state_file):
        print("NotFound", file=sys.stderr)
        sys.exit(1)
    kind = rest[0]
    name = rest[1]
    api = "apps/v1" if kind.lower() == "deployment" else "v1"
    doc = {
        "apiVersion": api,
        "kind": kind,
        "metadata": {
            "name": name,
            "namespace": namespace,
            "uid": "uid-" + kind.lower() + "-" + name,
            "resourceVersion": "42",
            "generation": 1,
            "managedFields": [{"manager": "torque", "operation": "Apply"}],
        },
        "data": {"secret": "k8s-object-secret"},
    }
    print(json.dumps(doc))
    sys.exit(0)
sys.exit(2)
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_KUBECTL_STATE", stateFile)
	node := &ResolvedRelease{
		ID:        "k8s.manifest.apply/apply-manifest",
		Kind:      NodeKindK8sManifestApply,
		Name:      "apply-manifest",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "default"},
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{Transport: "local", KubectlCommand: "kubectl"},
			Manifest: KubernetesManifestSpec{
				Namespace:      "torque-test",
				FieldManager:   "torque",
				RemoveOnDelete: true,
				Content: `apiVersion: v1
kind: ConfigMap
metadata:
  name: torque-fake-config
data:
  marker: OPS-K8S-001
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: torque-fake-deploy
spec:
  replicas: 0
  selector:
    matchLabels:
      app: torque-fake
  template:
    metadata:
      labels:
        app: torque-fake
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.9
`,
			},
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("manifest was not applied: %v", err)
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"k8s-manifest-observe.json", "k8s-manifest-plan.json", "k8s-manifest-diff.json", "k8s-manifest-apply.json", "k8s-manifest-verify.json", "k8s-manifest.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	applyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "k8s-manifest-apply.json")
	if !strings.Contains(applyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(applyArtifact, `"changed": true`) ||
		!strings.Contains(applyArtifact, `"owned": true`) ||
		strings.Contains(applyArtifact, "k8s-apply-secret") ||
		strings.Contains(applyArtifact, "k8s-object-secret") {
		t.Fatalf("k8s manifest apply artifact did not record a redacted changed receipt:\n%s", applyArtifact)
	}
	diffArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "k8s-manifest-diff.json")
	if !strings.Contains(diffArtifact, `"diffQuality": "server-side"`) ||
		!strings.Contains(diffArtifact, `"objects": true`) {
		t.Fatalf("k8s manifest diff artifact missing server-side change evidence:\n%s", diffArtifact)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run repeat apply: %v\nstderr=%s", err, errOut.String())
	}
	repeatRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun repeat: %v", err)
	}
	repeatAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            repeatRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit repeat: %v", err)
	}
	repeatApply := auditArtifactBody(t, repeatAudit.Artifacts, node.ID, "k8s-manifest-apply.json")
	if !strings.Contains(repeatApply, `"changed": false`) {
		t.Fatalf("repeat manifest apply was not a no-op:\n%s", repeatApply)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "delete",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run delete: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("expected delete to remove manifest state, stat err=%v", err)
	}
	deleteRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun delete: %v", err)
	}
	deleteAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            deleteRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit delete: %v", err)
	}
	deleteApply := auditArtifactBody(t, deleteAudit.Artifacts, node.ID, "k8s-manifest-apply.json")
	if !strings.Contains(deleteApply, `"desiredState": "absent"`) ||
		!strings.Contains(deleteApply, `"changed": true`) ||
		!strings.Contains(deleteApply, `"exists": false`) {
		t.Fatalf("delete did not record manifest cleanup:\n%s", deleteApply)
	}
}

func TestRun_KubernetesManifestDeleteLocalNode(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	stateDir := filepath.Join(root, "fake-kubectl-state")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir fake state: %v", err)
	}
	resourcePath := func(kind, name string) string {
		return filepath.Join(stateDir, strings.ToLower(kind)+"__"+name+".txt")
	}
	writeResource := func(kind, name, manager string) {
		if err := os.WriteFile(resourcePath(kind, name), []byte(manager), 0o644); err != nil {
			t.Fatalf("write fake %s/%s: %v", kind, name, err)
		}
	}
	configPath := resourcePath("ConfigMap", "torque-delete-config")
	deployPath := resourcePath("Deployment", "torque-delete-deploy")
	unrelatedPath := resourcePath("ConfigMap", "torque-unrelated-config")
	writeResource("ConfigMap", "torque-delete-config", "torque")
	writeResource("Deployment", "torque-delete-deploy", "torque")
	writeResource("ConfigMap", "torque-unrelated-config", "other")
	writeExecutableForTest(t, filepath.Join(binDir, "kubectl"), `#!/usr/bin/env python3
import json
import os
import sys

state_dir = os.environ["FAKE_KUBECTL_STATE_DIR"]
args = sys.argv[1:]
namespace = "default"
i = 0
while i < len(args):
    arg = args[i]
    if arg in ("--kubeconfig", "--context", "-n", "--namespace"):
        if arg in ("-n", "--namespace"):
            namespace = args[i + 1]
        i += 2
        continue
    if arg.startswith("--kubeconfig=") or arg.startswith("--context="):
        i += 1
        continue
    break
if i >= len(args):
    sys.exit(2)
cmd = args[i]
rest = args[i + 1:]

def state_path(kind, name):
    return os.path.join(state_dir, kind.lower() + "__" + name + ".txt")

def manifest_path(values):
    for idx, value in enumerate(values):
        if value == "-f" and idx + 1 < len(values):
            return values[idx + 1]
    return ""

def refs_from_manifest(path):
    if not path:
        return []
    refs = []
    with open(path, "r", encoding="utf-8") as f:
        raw = f.read()
    for doc in raw.split("---"):
        kind = ""
        name = ""
        in_metadata = False
        for line in doc.splitlines():
            stripped = line.strip()
            if not stripped or stripped.startswith("#"):
                continue
            if stripped.startswith("kind:"):
                kind = stripped.split(":", 1)[1].strip().strip('"')
                continue
            if stripped == "metadata:":
                in_metadata = True
                continue
            if in_metadata and stripped.startswith("name:"):
                name = stripped.split(":", 1)[1].strip().strip('"')
                continue
            if in_metadata and not line.startswith((" ", "\t")):
                in_metadata = False
        if kind and name:
            refs.append((kind, name))
    return refs

if cmd == "get":
    if len(rest) < 2:
        sys.exit(2)
    kind = rest[0]
    name = rest[1]
    path = state_path(kind, name)
    if not os.path.exists(path):
        print("NotFound", file=sys.stderr)
        sys.exit(1)
    with open(path, "r", encoding="utf-8") as f:
        manager = f.read().strip() or "unknown"
    api = "apps/v1" if kind.lower() == "deployment" else "v1"
    doc = {
        "apiVersion": api,
        "kind": kind,
        "metadata": {
            "name": name,
            "namespace": namespace,
            "uid": "uid-" + kind.lower() + "-" + name,
            "resourceVersion": "42",
            "generation": 1,
            "managedFields": [{"manager": manager, "operation": "Apply"}],
        },
        "data": {"secret": "k8s-delete-object-secret"},
    }
    print(json.dumps(doc))
    sys.exit(0)
if cmd == "delete":
    for kind, name in refs_from_manifest(manifest_path(rest)):
        path = state_path(kind, name)
        if os.path.exists(path):
            os.unlink(path)
    sys.exit(0)
sys.exit(2)
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_KUBECTL_STATE_DIR", stateDir)
	node := &ResolvedRelease{
		ID:        "k8s.manifest.delete/delete-manifest",
		Kind:      NodeKindK8sManifestDelete,
		Name:      "delete-manifest",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "default"},
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{Transport: "local", KubectlCommand: "kubectl"},
			Manifest: KubernetesManifestSpec{
				Namespace:    "torque-test",
				FieldManager: "torque",
				PrunePolicy:  "listed-only",
				Content: `apiVersion: v1
kind: ConfigMap
metadata:
  name: torque-delete-config
data:
  marker: OPS-K8S-002
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: torque-delete-deploy
spec:
  replicas: 0
  selector:
    matchLabels:
      app: torque-delete
  template:
    metadata:
      labels:
        app: torque-delete
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.9
`,
			},
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply/delete: %v\nstderr=%s", err, errOut.String())
	}
	for _, path := range []string{configPath, deployPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be deleted, stat err=%v", path, err)
		}
	}
	if _, err := os.Stat(unrelatedPath); err != nil {
		t.Fatalf("unrelated object was not preserved: %v", err)
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"k8s-manifest-delete-observe.json", "k8s-manifest-delete-plan.json", "k8s-manifest-delete-diff.json", "k8s-manifest-delete-apply.json", "k8s-manifest-delete-verify.json", "k8s-manifest-delete.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	applyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "k8s-manifest-delete-apply.json")
	if !strings.Contains(applyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(applyArtifact, `"changed": true`) ||
		!strings.Contains(applyArtifact, `"desiredState": "absent"`) ||
		!strings.Contains(applyArtifact, `"ownershipRequired": true`) ||
		!strings.Contains(applyArtifact, `"prunePolicy": "listed-only"`) ||
		!strings.Contains(applyArtifact, `"exists": false`) ||
		strings.Contains(applyArtifact, "k8s-delete-object-secret") {
		t.Fatalf("k8s manifest delete artifact did not record redacted deletion proof:\n%s", applyArtifact)
	}
	diffArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "k8s-manifest-delete-diff.json")
	if !strings.Contains(diffArtifact, `"diffQuality": "ownership-gated-listed-only"`) ||
		!strings.Contains(diffArtifact, `"objects": true`) {
		t.Fatalf("k8s manifest delete diff artifact missing listed-only evidence:\n%s", diffArtifact)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run repeat apply/delete: %v\nstderr=%s", err, errOut.String())
	}
	repeatRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun repeat: %v", err)
	}
	repeatAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            repeatRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit repeat: %v", err)
	}
	repeatApply := auditArtifactBody(t, repeatAudit.Artifacts, node.ID, "k8s-manifest-delete-apply.json")
	if !strings.Contains(repeatApply, `"changed": false`) {
		t.Fatalf("repeat manifest delete was not a no-op:\n%s", repeatApply)
	}
	if _, err := os.Stat(unrelatedPath); err != nil {
		t.Fatalf("unrelated object was not preserved after repeat: %v", err)
	}

	writeResource("ConfigMap", "torque-delete-config", "other")
	writeResource("Deployment", "torque-delete-deploy", "other")
	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err == nil {
		t.Fatalf("Run unowned apply/delete succeeded unexpectedly\nstdout=%s\nstderr=%s", out.String(), errOut.String())
	}
	for _, path := range []string{configPath, deployPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unowned target should not have been deleted: %v", err)
		}
	}
	blockedRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun blocked: %v", err)
	}
	blockedAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            blockedRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit blocked: %v", err)
	}
	blockedApply := auditArtifactBody(t, blockedAudit.Artifacts, node.ID, "k8s-manifest-delete-apply.json")
	if !strings.Contains(blockedApply, `"status": "failed"`) ||
		!strings.Contains(blockedApply, `"reason": "ownership check failed"`) ||
		!strings.Contains(blockedApply, `"blockedResources"`) ||
		!strings.Contains(blockedApply, `"owned": false`) {
		t.Fatalf("unowned manifest delete did not record ownership gate:\n%s", blockedApply)
	}
}

func TestRun_KubernetesResourceWaitLocalNode(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	stateFile := filepath.Join(root, "resource-ready.txt")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	writeExecutableForTest(t, filepath.Join(binDir, "kubectl"), `#!/usr/bin/env python3
import json
import os
import sys

state_file = os.environ["FAKE_KUBECTL_READY"]
args = sys.argv[1:]
namespace = "default"
i = 0
while i < len(args):
    arg = args[i]
    if arg in ("--kubeconfig", "--context", "-n", "--namespace"):
        if arg in ("-n", "--namespace"):
            namespace = args[i + 1]
        i += 2
        continue
    if arg.startswith("--kubeconfig=") or arg.startswith("--context="):
        i += 1
        continue
    break
if i >= len(args):
    sys.exit(2)
cmd = args[i]
rest = args[i + 1:]

def ready():
    try:
        return open(state_file, "r", encoding="utf-8").read().strip() == "true"
    except FileNotFoundError:
        return False

def deployment_doc():
    is_ready = ready()
    return {
        "apiVersion": "apps/v1",
        "kind": "Deployment",
        "metadata": {
            "name": "torque-ready",
            "namespace": namespace,
            "uid": "uid-deployment-torque-ready",
            "resourceVersion": "42" if is_ready else "41",
            "generation": 1,
        },
        "status": {
            "observedGeneration": 1,
            "replicas": 1,
            "readyReplicas": 1 if is_ready else 0,
            "availableReplicas": 1 if is_ready else 0,
            "updatedReplicas": 1,
            "conditions": [
                {
                    "type": "Available",
                    "status": "True" if is_ready else "False",
                    "reason": "MinimumReplicasAvailable" if is_ready else "MinimumReplicasUnavailable",
                    "message": "token=resource-wait-object-secret",
                }
            ],
        },
    }

if cmd == "get" and rest and rest[0] == "events":
    doc = {
        "items": [
            {
                "metadata": {"name": "torque-ready.1", "namespace": namespace},
                "type": "Normal" if ready() else "Warning",
                "reason": "Available" if ready() else "Failed",
                "message": "password=resource-wait-event-secret",
                "count": 1,
                "firstTimestamp": "2026-01-01T00:00:00Z",
                "lastTimestamp": "2026-01-01T00:00:01Z",
                "involvedObject": {"kind": "Deployment", "name": "torque-ready", "namespace": namespace},
            }
        ]
    }
    print(json.dumps(doc))
    sys.exit(0)
if cmd == "get":
    if not rest or rest[0] not in ("deployment/torque-ready", "deploy/torque-ready"):
        print("NotFound", file=sys.stderr)
        sys.exit(1)
    print(json.dumps(deployment_doc()))
    sys.exit(0)
if cmd == "wait":
    if ready():
        print("deployment.apps/torque-ready condition met")
        sys.exit(0)
    print("timed out waiting for the condition on deployments/torque-ready", file=sys.stderr)
    sys.exit(1)
sys.exit(2)
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_KUBECTL_READY", stateFile)
	timeout := 2 * time.Second
	node := &ResolvedRelease{
		ID:        "k8s.resource.wait/wait-ready",
		Kind:      NodeKindK8sResourceWait,
		Name:      "wait-ready",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "default"},
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{Transport: "local", KubectlCommand: "kubectl"},
			Resource: KubernetesResourceSpec{
				Namespace:  "torque-test",
				Kind:       "deployment",
				Name:       "torque-ready",
				Resource:   "deployment/torque-ready",
				For:        "condition=Available",
				Timeout:    &timeout,
				EventLimit: 5,
			},
		},
	}
	plan := planForTest(root, node)
	if err := os.WriteFile(stateFile, []byte("true"), 0o644); err != nil {
		t.Fatalf("write ready state: %v", err)
	}
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run resource wait apply: %v\nstderr=%s", err, errOut.String())
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"k8s-resource-wait-observe.json", "k8s-resource-wait-plan.json", "k8s-resource-wait-apply.json", "k8s-resource-wait-events.json", "k8s-resource-wait-verify.json", "k8s-resource-wait.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	applyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "k8s-resource-wait-apply.json")
	if !strings.Contains(applyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(applyArtifact, `"ready": true`) ||
		!strings.Contains(applyArtifact, `"condition": "Available"`) ||
		!strings.Contains(applyArtifact, `"changed": false`) ||
		strings.Contains(applyArtifact, "resource-wait-object-secret") {
		t.Fatalf("resource wait apply artifact missing readiness proof or leaked object message:\n%s", applyArtifact)
	}
	eventsArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "k8s-resource-wait-events.json")
	if !strings.Contains(eventsArtifact, `"messageDigest"`) ||
		strings.Contains(eventsArtifact, "resource-wait-event-secret") {
		t.Fatalf("resource wait events artifact missing redacted event proof:\n%s", eventsArtifact)
	}

	if err := os.WriteFile(stateFile, []byte("false"), 0o644); err != nil {
		t.Fatalf("write not-ready state: %v", err)
	}
	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err == nil {
		t.Fatalf("Run resource wait timeout succeeded unexpectedly\nstdout=%s\nstderr=%s", out.String(), errOut.String())
	}
	failedRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun failed: %v", err)
	}
	failedAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            failedRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit failed: %v", err)
	}
	failedApply := auditArtifactBody(t, failedAudit.Artifacts, node.ID, "k8s-resource-wait-apply.json")
	if !strings.Contains(failedApply, `"status": "failed"`) ||
		!strings.Contains(failedApply, "timed out waiting for the condition") ||
		!strings.Contains(failedApply, `"ready": false`) {
		t.Fatalf("resource wait timeout artifact missing failure proof:\n%s", failedApply)
	}
	failedEvents := auditArtifactBody(t, failedAudit.Artifacts, node.ID, "k8s-resource-wait-events.json")
	if !strings.Contains(failedEvents, `"type": "Warning"`) ||
		!strings.Contains(failedEvents, `"messageDigest"`) ||
		strings.Contains(failedEvents, "resource-wait-event-secret") {
		t.Fatalf("resource wait timeout events missing warning proof:\n%s", failedEvents)
	}
}

func TestRun_KubernetesLogsCaptureLocalNode(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	modeFile := filepath.Join(root, "logs-mode.txt")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	writeExecutableForTest(t, filepath.Join(binDir, "kubectl"), `#!/usr/bin/env python3
import json
import os
import sys

mode_file = os.environ["FAKE_KUBECTL_LOGS_MODE"]
args = sys.argv[1:]
namespace = "default"
i = 0
while i < len(args):
    arg = args[i]
    if arg in ("--kubeconfig", "--context", "-n", "--namespace"):
        if arg in ("-n", "--namespace"):
            namespace = args[i + 1]
        i += 2
        continue
    if arg.startswith("--kubeconfig=") or arg.startswith("--context="):
        i += 1
        continue
    break
if i >= len(args):
    sys.exit(2)
cmd = args[i]
rest = args[i + 1:]

def mode():
    try:
        return open(mode_file, "r", encoding="utf-8").read().strip()
    except FileNotFoundError:
        return "ok"

if cmd == "get":
    target = rest[0] if rest else ""
    if target not in ("deployment/torque-logs", "deploy/torque-logs"):
        print("NotFound", file=sys.stderr)
        sys.exit(1)
    print(json.dumps({
        "apiVersion": "apps/v1",
        "kind": "Deployment",
        "metadata": {
            "name": "torque-logs",
            "namespace": namespace,
            "uid": "uid-deployment-torque-logs",
            "resourceVersion": "77",
            "annotations": {"note": "token=logs-object-secret"},
        },
    }))
    sys.exit(0)
if cmd == "logs":
    if mode() == "fail":
        print("pod log capture failed", file=sys.stderr)
        sys.exit(1)
    print("2026-01-01T00:00:00Z safe-line-1")
    print("2026-01-01T00:00:01Z safe-line-2")
    print("2026-01-01T00:00:02Z password=logs-capture-secret")
    print("2026-01-01T00:00:03Z token=logs-token-secret")
    print("2026-01-01T00:00:04Z final-line")
    sys.exit(0)
sys.exit(2)
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_KUBECTL_LOGS_MODE", modeFile)
	node := &ResolvedRelease{
		ID:        "k8s.logs.capture/capture-logs",
		Kind:      NodeKindK8sLogsCapture,
		Name:      "capture-logs",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "default"},
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{Transport: "local", KubectlCommand: "kubectl"},
			Logs: KubernetesLogsSpec{
				Namespace:  "torque-test",
				Kind:       "deployment",
				Name:       "torque-logs",
				Resource:   "deployment/torque-logs",
				Container:  "app",
				Timestamps: true,
				TailLines:  5,
				LimitBytes: 2048,
			},
		},
	}
	plan := planForTest(root, node)
	if err := os.WriteFile(modeFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write logs mode: %v", err)
	}
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run logs capture apply: %v\nstderr=%s", err, errOut.String())
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"k8s-logs-capture-observe.json", "k8s-logs-capture-plan.json", "k8s-logs-capture-logs.json", "k8s-logs-capture-verify.json", "k8s-logs-capture.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	logsArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "k8s-logs-capture-logs.json")
	if !strings.Contains(logsArtifact, `"status": "succeeded"`) ||
		!strings.Contains(logsArtifact, `"changed": false`) ||
		!strings.Contains(logsArtifact, `password=[REDACTED]`) ||
		!strings.Contains(logsArtifact, `token=[REDACTED]`) ||
		!strings.Contains(logsArtifact, `"capturedLineCount": 5`) ||
		!strings.Contains(logsArtifact, `"noSensitiveKeyValues": true`) ||
		strings.Contains(logsArtifact, "logs-capture-secret") ||
		strings.Contains(logsArtifact, "logs-token-secret") ||
		strings.Contains(logsArtifact, "logs-object-secret") {
		t.Fatalf("logs capture artifact missing bounded redacted proof:\n%s", logsArtifact)
	}
	verifyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "k8s-logs-capture-verify.json")
	if !strings.Contains(verifyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(verifyArtifact, `"lineCount": 5`) ||
		!strings.Contains(verifyArtifact, `"noSensitiveKeyValues": true`) {
		t.Fatalf("logs capture verify artifact missing redaction proof:\n%s", verifyArtifact)
	}

	if err := os.WriteFile(modeFile, []byte("fail"), 0o644); err != nil {
		t.Fatalf("write failing logs mode: %v", err)
	}
	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err == nil {
		t.Fatalf("Run logs capture failure succeeded unexpectedly\nstdout=%s\nstderr=%s", out.String(), errOut.String())
	}
	failedRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun failed: %v", err)
	}
	failedAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            failedRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit failed: %v", err)
	}
	failedLogs := auditArtifactBody(t, failedAudit.Artifacts, node.ID, "k8s-logs-capture-logs.json")
	if !strings.Contains(failedLogs, `"status": "failed"`) ||
		!strings.Contains(failedLogs, "pod log capture failed") {
		t.Fatalf("logs capture failure artifact missing failure proof:\n%s", failedLogs)
	}
}

func TestRun_KubernetesEventsCaptureLocalNode(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	modeFile := filepath.Join(root, "events-mode.txt")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	writeExecutableForTest(t, filepath.Join(binDir, "kubectl"), `#!/usr/bin/env python3
import json
import os
import sys

mode_file = os.environ["FAKE_KUBECTL_EVENTS_MODE"]
args = sys.argv[1:]
i = 0
namespace = "default"
while i < len(args):
    arg = args[i]
    if arg in ("--kubeconfig", "--context", "-n", "--namespace"):
        if arg in ("-n", "--namespace"):
            namespace = args[i + 1]
        i += 2
        continue
    if arg.startswith("--kubeconfig=") or arg.startswith("--context="):
        i += 1
        continue
    break
if i >= len(args):
    sys.exit(2)
cmd = args[i]
rest = args[i + 1:]

def mode():
    try:
        return open(mode_file, "r", encoding="utf-8").read().strip()
    except FileNotFoundError:
        return "ok"

if cmd == "get" and rest[:1] == ["namespace"]:
    if mode() == "namespace-fail":
        print("namespace missing", file=sys.stderr)
        sys.exit(1)
    print(json.dumps({
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {
            "name": rest[1],
            "uid": "uid-namespace-torque-events",
            "resourceVersion": "101",
            "annotations": {"note": "password=namespace-secret"},
        },
    }))
    sys.exit(0)
if cmd == "get" and rest[:1] == ["events"]:
    if mode() == "events-fail":
        print("events forbidden", file=sys.stderr)
        sys.exit(1)
    print(json.dumps({
        "items": [
            {
                "metadata": {"name": "normal.1", "namespace": namespace, "creationTimestamp": "2026-01-01T00:00:00Z"},
                "type": "Normal",
                "reason": "Scheduled",
                "message": "scheduled password=events-normal-secret",
                "count": 1,
                "firstTimestamp": "2026-01-01T00:00:00Z",
                "lastTimestamp": "2026-01-01T00:00:01Z",
                "involvedObject": {"kind": "Pod", "name": "torque-ok", "namespace": namespace, "uid": "uid-pod-ok"},
            },
            {
                "metadata": {"name": "warning.1", "namespace": namespace, "creationTimestamp": "2026-01-01T00:00:02Z"},
                "type": "Warning",
                "reason": "Failed",
                "message": "failed token=events-warning-secret",
                "count": 2,
                "firstTimestamp": "2026-01-01T00:00:02Z",
                "lastTimestamp": "2026-01-01T00:00:03Z",
                "involvedObject": {"kind": "Pod", "name": "torque-bad", "namespace": namespace, "uid": "uid-pod-bad"},
            },
        ]
    }))
    sys.exit(0)
sys.exit(2)
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_KUBECTL_EVENTS_MODE", modeFile)
	node := &ResolvedRelease{
		ID:        "k8s.events.capture/capture-events",
		Kind:      NodeKindK8sEventsCapture,
		Name:      "capture-events",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "default"},
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{Transport: "local", KubectlCommand: "kubectl"},
			Events: KubernetesEventsSpec{
				Namespace:  "torque-test",
				Types:      []string{"Warning"},
				Reasons:    []string{"Failed"},
				EventLimit: 10,
			},
		},
	}
	plan := planForTest(root, node)
	if err := os.WriteFile(modeFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write events mode: %v", err)
	}
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run events capture apply: %v\nstderr=%s", err, errOut.String())
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"k8s-events-capture-observe.json", "k8s-events-capture-plan.json", "k8s-events-capture-events.json", "k8s-events-capture-verify.json", "k8s-events-capture.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	eventsArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "k8s-events-capture-events.json")
	if !strings.Contains(eventsArtifact, `"status": "succeeded"`) ||
		!strings.Contains(eventsArtifact, `"changed": false`) ||
		!strings.Contains(eventsArtifact, `"capturedCount": 1`) ||
		!strings.Contains(eventsArtifact, `"filteredOutCount": 1`) ||
		!strings.Contains(eventsArtifact, `"type": "Warning"`) ||
		!strings.Contains(eventsArtifact, `"reason": "Failed"`) ||
		!strings.Contains(eventsArtifact, `"messageDigest"`) ||
		!strings.Contains(eventsArtifact, `"noSensitiveKeyValues": true`) ||
		strings.Contains(eventsArtifact, "events-warning-secret") ||
		strings.Contains(eventsArtifact, "events-normal-secret") ||
		strings.Contains(eventsArtifact, "namespace-secret") ||
		strings.Contains(eventsArtifact, `"type": "Normal"`) {
		t.Fatalf("events capture artifact missing filtered redacted proof:\n%s", eventsArtifact)
	}
	verifyArtifact := auditArtifactBody(t, audit.Artifacts, node.ID, "k8s-events-capture-verify.json")
	if !strings.Contains(verifyArtifact, `"status": "succeeded"`) ||
		!strings.Contains(verifyArtifact, `"capturedCount": 1`) ||
		!strings.Contains(verifyArtifact, `"Warning": 1`) {
		t.Fatalf("events capture verify artifact missing count proof:\n%s", verifyArtifact)
	}

	if err := os.WriteFile(modeFile, []byte("events-fail"), 0o644); err != nil {
		t.Fatalf("write failing events mode: %v", err)
	}
	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err == nil {
		t.Fatalf("Run events capture failure succeeded unexpectedly\nstdout=%s\nstderr=%s", out.String(), errOut.String())
	}
	failedRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun failed: %v", err)
	}
	failedAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            failedRunID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit failed: %v", err)
	}
	failedEvents := auditArtifactBody(t, failedAudit.Artifacts, node.ID, "k8s-events-capture-events.json")
	if !strings.Contains(failedEvents, `"status": "failed"`) ||
		!strings.Contains(failedEvents, "events forbidden") {
		t.Fatalf("events capture failure artifact missing failure proof:\n%s", failedEvents)
	}
}

func TestRun_HostCommandDryRunDoesNotExecute(t *testing.T) {
	root := t.TempDir()
	outFile := filepath.Join(root, "dry-run-marker.txt")
	node := &ResolvedRelease{
		ID:        "host.command.run/dry-run-marker",
		Kind:      NodeKindHostCommandRun,
		Name:      "dry-run-marker",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport: "local",
			Command:   "printf should-not-run > " + shellQuoteForTest(outFile),
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
		DryRun:      true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run dry-run: %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(outFile); !os.IsNotExist(err) {
		t.Fatalf("dry-run executed host command, stat err=%v", err)
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	found := false
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID == node.ID && artifact.Name == "host-command.json" {
			found = strings.Contains(artifact.Body, `"status": "skipped"`) &&
				strings.Contains(artifact.Body, `"reason": "dry-run"`)
			break
		}
	}
	if !found {
		t.Fatalf("missing dry-run host-command artifact in %+v", audit.Artifacts)
	}
}

func TestRun_HostCommandOpsGuardReceipts(t *testing.T) {
	root := t.TempDir()
	outFile := filepath.Join(root, "ops-host-marker.txt")
	node := &ResolvedRelease{
		ID:        "host.command.run/ops-marker",
		Kind:      NodeKindHostCommandRun,
		Name:      "ops-marker",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport: "local",
			TargetID:  "host/web-01",
			Command:   "printf 'password=super-secret-value\\n' && printf ok > " + shellQuoteForTest(outFile),
		},
	}
	plan := planForTest(root, node)
	plan.Ops = eligibleHostCommandOpsForTest(t, root, "host/web-01")

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read host command output: %v", err)
	}
	if got := string(bytes.TrimSpace(raw)); got != "ok" {
		t.Fatalf("host command output=%q", got)
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	for _, name := range []string{"host-command-observe.json", "host-command-plan.json", "host-command-execute.json", "host-command-verify.json", "host-command.json"} {
		if !auditHasArtifact(audit.Artifacts, node.ID, name) {
			t.Fatalf("missing %s in %+v", name, audit.Artifacts)
		}
	}
	artifact := auditArtifactBody(t, audit.Artifacts, node.ID, "host-command.json")
	if !strings.Contains(artifact, `"guardMode": "ops"`) ||
		!strings.Contains(artifact, `"targetId": "host/web-01"`) ||
		!strings.Contains(artifact, `"HostCommandNodeArtifact"`) ||
		!strings.Contains(artifact, `password=[REDACTED]`) ||
		strings.Contains(artifact, "super-secret-value") {
		t.Fatalf("host command artifact did not record guarded redacted receipts:\n%s", artifact)
	}
	verify := auditArtifactBody(t, audit.Artifacts, node.ID, "host-command-verify.json")
	if !strings.Contains(verify, `"status": "succeeded"`) ||
		!strings.Contains(verify, `"stdoutRedacted": true`) ||
		!strings.Contains(verify, `"noSensitiveKeyValues": true`) {
		t.Fatalf("host verify receipt did not prove redaction:\n%s", verify)
	}
	assertOpsAuditPassed(t, audit, 1)

	bundlePath := filepath.Join(root, "ops-host-audit.tgz")
	if _, err := ExportRunBundle(context.Background(), root, runID, bundlePath); err != nil {
		t.Fatalf("ExportRunBundle: %v", err)
	}
	bundleAudit, err := GetRunAudit(context.Background(), RunAuditOptions{
		BundlePath:       bundlePath,
		VerifyBundle:     true,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit from bundle: %v", err)
	}
	if bundleAudit.RunID != runID {
		t.Fatalf("bundle audit run ID = %s, want %s", bundleAudit.RunID, runID)
	}
	assertOpsAuditPassed(t, bundleAudit, 1)

	extracted, err := ExtractBundleToTempDir(bundlePath)
	if err != nil {
		t.Fatalf("ExtractBundleToTempDir: %v", err)
	}
	defer os.RemoveAll(extracted)
	exportDB, err := sql.Open("sqlite", filepath.Join(extracted, "state.sqlite"))
	if err != nil {
		t.Fatalf("open exported sqlite: %v", err)
	}
	defer exportDB.Close()
	var planJSON, summaryJSON, lastDigest, exportedRunDigest string
	if err := exportDB.QueryRow(`SELECT plan_json, summary_json, last_event_digest, run_digest FROM torque_stack_runs WHERE run_id = ?`, runID).Scan(&planJSON, &summaryJSON, &lastDigest, &exportedRunDigest); err != nil {
		t.Fatalf("query exported run row: %v", err)
	}
	if strings.Contains(planJSON, "super-secret-value") || strings.Contains(summaryJSON, "super-secret-value") {
		t.Fatalf("exported run row leaked raw sensitive value:\nplan=%s\nsummary=%s", planJSON, summaryJSON)
	}
	if !strings.Contains(planJSON, `password=[REDACTED]`) {
		t.Fatalf("exported plan JSON did not preserve redacted command preview:\n%s", planJSON)
	}
	if want := computeRunDigest(planJSON, summaryJSON, lastDigest); exportedRunDigest != want {
		t.Fatalf("exported run digest=%s want %s", exportedRunDigest, want)
	}
}

func TestGetRunAuditOpsRunFailsTamperedHostCommandReceipts(t *testing.T) {
	root := t.TempDir()
	node := &ResolvedRelease{
		ID:        "host.command.run/tamper",
		Kind:      NodeKindHostCommandRun,
		Name:      "tamper",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport: "local",
			TargetID:  "host/web-01",
			Command:   "printf 'password=super-secret-value\\n'",
		},
	}
	plan := planForTest(root, node)
	plan.Ops = eligibleHostCommandOpsForTest(t, root, "host/web-01")

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, stackStateSQLiteRelPath))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `
DELETE FROM torque_stack_run_artifacts
WHERE run_id = ? AND node_id = ? AND artifact_name = 'host-command-verify.json'
`, runID, node.ID); err != nil {
		t.Fatalf("delete verify receipt: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
UPDATE torque_stack_run_artifacts
SET body_text = ?, sha256 = '', size_bytes = ?
WHERE run_id = ? AND node_id = ? AND artifact_name = 'host-command-execute.json'
`, `{"status":"succeeded","stdout":"password=super-secret-value\n"}`, len(`{"status":"succeeded","stdout":"password=super-secret-value\n"}`), runID, node.ID); err != nil {
		t.Fatalf("tamper execute receipt: %v", err)
	}

	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	if audit.Ops == nil || audit.Ops.Verification.Status != "failed" {
		t.Fatalf("ops verification = %#v, want failed", audit.Ops)
	}
	if !opsAuditHasFinding(audit, "ops.host_command.verify_missing") {
		t.Fatalf("missing verify_missing finding: %#v", audit.Ops.Findings)
	}
	if !opsAuditHasFinding(audit, "ops.host_command.redaction_leak") {
		t.Fatalf("missing redaction_leak finding: %#v", audit.Ops.Findings)
	}
}

func TestRun_HostCommandOpsGuardBlocksUnselectedTarget(t *testing.T) {
	root := t.TempDir()
	outFile := filepath.Join(root, "blocked-marker.txt")
	node := &ResolvedRelease{
		ID:        "host.command.run/blocked-marker",
		Kind:      NodeKindHostCommandRun,
		Name:      "blocked-marker",
		Dir:       root,
		Namespace: "default",
		Host: HostCommandSpec{
			Transport: "local",
			TargetID:  "host/web-01",
			Command:   "printf should-not-run > " + shellQuoteForTest(outFile),
		},
	}
	plan := planForTest(root, node)
	plan.Ops = eligibleHostCommandOpsForTest(t, root, "host/other-01")

	var out, errOut bytes.Buffer
	err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "was not selected by TargetGraph") {
		t.Fatalf("Run error = %v, want target selection block\nstderr=%s", err, errOut.String())
	}
	if _, statErr := os.Stat(outFile); !os.IsNotExist(statErr) {
		t.Fatalf("blocked command wrote marker, stat err=%v", statErr)
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	planReceipt := auditArtifactBody(t, audit.Artifacts, node.ID, "host-command-plan.json")
	if !strings.Contains(planReceipt, `"status": "blocked"`) ||
		!strings.Contains(planReceipt, "was not selected by TargetGraph") {
		t.Fatalf("host plan receipt did not record guard block:\n%s", planReceipt)
	}
	if auditHasArtifact(audit.Artifacts, node.ID, "host-command-execute.json") {
		t.Fatalf("blocked command wrote execute receipt")
	}
}

func TestRun_KubernetesCertInspectCustomLocalNode(t *testing.T) {
	root := t.TempDir()
	node := &ResolvedRelease{
		ID:        "k8s.cert.inspect/certs",
		Kind:      NodeKindK8sCertInspect,
		Name:      "certs",
		Dir:       root,
		Namespace: "default",
		Kubernetes: KubernetesSpec{
			Provider: "custom",
			Certificates: KubernetesCertSpec{
				Targets: []KubernetesCertTarget{
					{
						ID:             "cp-1",
						Transport:      "local",
						Target:         "local://localhost",
						InspectCommand: `printf '%s\n' '{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}'`,
					},
				},
			},
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	found := false
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID == node.ID && artifact.Name == "k8s-cert-inspect.json" {
			found = strings.Contains(artifact.Body, `"KubernetesCertificateLifecycle"`) &&
				strings.Contains(artifact.Body, `"status": "succeeded"`) &&
				strings.Contains(artifact.Body, `"earliestExpiry": "2035-01-01T00:00:00Z"`)
			break
		}
	}
	if !found {
		t.Fatalf("missing k8s cert lifecycle artifact in %+v", audit.Artifacts)
	}
}

func TestRun_KubernetesClusterInspectLocalNode(t *testing.T) {
	root := t.TempDir()
	node := &ResolvedRelease{
		ID:        "k8s.cluster.inspect/cluster",
		Kind:      NodeKindK8sClusterInspect,
		Name:      "cluster",
		Dir:       root,
		Namespace: "default",
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{
				Transport:         "local",
				Target:            "local://localhost",
				Namespaces:        []string{"gitlab"},
				ConfigCommand:     `printf '%s\n' '{"clusters":[{"name":"lab","cluster":{"server":"https://127.0.0.1:6443"}}]}'`,
				APICommand:        `printf '%s\n' '{"clientVersion":{"gitVersion":"v1.30.0"},"serverVersion":{"gitVersion":"v1.30.4+k3s1","major":"1","minor":"30","platform":"linux/amd64"}}'`,
				NodesCommand:      `printf '%s\n' '{"items":[{"metadata":{"name":"node-1","labels":{"node-role.kubernetes.io/control-plane":"","k3s.io/hostname":"node-1"}},"spec":{"providerID":"firecracker://node-1"},"status":{"addresses":[{"type":"InternalIP","address":"172.31.245.10"}],"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.30.4+k3s1","osImage":"Ubuntu","kernelVersion":"6.8.0","containerRuntimeVersion":"containerd://1.7.0"}}}]}'`,
				NamespacesCommand: `printf '%s\n' '{"items":[{"metadata":{"name":"kube-system"},"status":{"phase":"Active"}},{"metadata":{"name":"gitlab"},"status":{"phase":"Active"}}]}'`,
				PodsCommand:       `case "{{namespace}}" in kube-system) printf '%s\n' '{"items":[{"metadata":{"name":"coredns","namespace":"kube-system"},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}]}' ;; gitlab) printf '%s\n' '{"items":[{"metadata":{"name":"webservice","namespace":"gitlab"},"status":{"phase":"Running","containerStatuses":[{"ready":true},{"ready":true}]}}]}' ;; esac`,
			},
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	found := false
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID != node.ID || artifact.Name != "k8s-cluster-inspect.json" {
			continue
		}
		found = strings.Contains(artifact.Body, `"KubernetesClusterInspect"`) &&
			strings.Contains(artifact.Body, `"status": "succeeded"`) &&
			strings.Contains(artifact.Body, `"server": "https://127.0.0.1:6443"`) &&
			strings.Contains(artifact.Body, `"distribution": "k3s"`) &&
			strings.Contains(artifact.Body, `"provider": "k3s"`) &&
			strings.Contains(artifact.Body, `"namespace": "gitlab"`) &&
			strings.Contains(artifact.Body, `"stdoutDigest"`) &&
			!strings.Contains(artifact.Body, `"stdout":`)
		break
	}
	if !found {
		t.Fatalf("missing k8s cluster inspect artifact in %+v", audit.Artifacts)
	}
}

func TestRun_KubernetesCertTargetsFromClusterInspect(t *testing.T) {
	root := t.TempDir()
	inspect := &ResolvedRelease{
		ID:        "k8s.cluster.inspect/cluster",
		Kind:      NodeKindK8sClusterInspect,
		Name:      "cluster",
		Dir:       root,
		Namespace: "default",
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{
				Transport:         "local",
				Target:            "local://localhost",
				ConfigCommand:     `printf '%s\n' '{"clusters":[{"name":"lab","cluster":{"server":"https://127.0.0.1:6443"}}]}'`,
				APICommand:        `printf '%s\n' '{"serverVersion":{"gitVersion":"v1.30.4+k3s1","major":"1","minor":"30"}}'`,
				NodesCommand:      `printf '%s\n' '{"items":[{"metadata":{"name":"cp-1","labels":{"node-role.kubernetes.io/control-plane":""}},"status":{"addresses":[{"type":"InternalIP","address":"10.0.0.10"}],"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.30.4+k3s1"}}},{"metadata":{"name":"worker-1","labels":{}},"status":{"addresses":[{"type":"InternalIP","address":"10.0.0.11"}],"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.30.4+k3s1"}}}]}'`,
				NamespacesCommand: `printf '%s\n' '{"items":[{"metadata":{"name":"kube-system"},"status":{"phase":"Active"}}]}'`,
				PodsCommand:       `printf '%s\n' '{"items":[]}'`,
			},
		},
	}
	certs := &ResolvedRelease{
		ID:        "k8s.cert.inspect/certs",
		Kind:      NodeKindK8sCertInspect,
		Name:      "certs",
		Dir:       root,
		Namespace: "default",
		Needs:     []string{"cluster"},
		Kubernetes: KubernetesSpec{
			Provider: "auto",
			Certificates: KubernetesCertSpec{
				TargetsFrom: KubernetesCertTargetsFromSpec{
					SourceNode:     "cluster",
					Roles:          []string{"control-plane", "worker"},
					Transport:      "local",
					TargetTemplate: "local://{{ .Name }}",
					InspectCommand: `printf '%s\n' '{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}'`,
				},
			},
		},
	}
	plan := planForTest(root, inspect, certs)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	found := false
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID != certs.ID || artifact.Name != "k8s-cert-inspect.json" {
			continue
		}
		found = strings.Contains(artifact.Body, `"targetsFrom"`) &&
			strings.Contains(artifact.Body, `"derivedCount": 2`) &&
			strings.Contains(artifact.Body, `"id": "cp-1"`) &&
			strings.Contains(artifact.Body, `"id": "worker-1"`) &&
			strings.Contains(artifact.Body, `"provider": "k3s"`) &&
			strings.Contains(artifact.Body, `"targetCount": 2`)
		break
	}
	if !found {
		t.Fatalf("missing dynamic targetsFrom cert artifact in %+v", audit.Artifacts)
	}
}

func TestRun_KubernetesClusterVerifyLocalNode(t *testing.T) {
	root := t.TempDir()
	node := &ResolvedRelease{
		ID:        "k8s.cluster.verify/cluster",
		Kind:      NodeKindK8sClusterVerify,
		Name:      "cluster",
		Dir:       root,
		Namespace: "default",
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{
				Transport:        "local",
				Target:           "local://localhost",
				StableIterations: 2,
				StableInterval:   durationPtrCustom(0),
				MinReadyNodes:    1,
				Namespaces:       []string{"kube-system"},
				APICommand:       "printf api-ok",
				NodesCommand:     `printf '%s\n' '{"items":[{"metadata":{"name":"node-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}'`,
				PodsCommand:      `printf '%s\n' '{"items":[{"metadata":{"name":"coredns","namespace":"kube-system"},"status":{"phase":"Running","containerStatuses":[{"name":"coredns","ready":true}]}}]}'`,
				AppProbes: []KubernetesAppProbe{
					{ID: "gitlab", Command: "printf 'Sign in - GitLab'", Expect: "GitLab"},
				},
			},
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	found := false
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID == node.ID && artifact.Name == "k8s-cluster-verify.json" {
			found = strings.Contains(artifact.Body, `"KubernetesClusterVerify"`) &&
				strings.Contains(artifact.Body, `"status": "succeeded"`) &&
				strings.Contains(artifact.Body, `"readyNodes": 1`) &&
				strings.Contains(artifact.Body, `"id": "gitlab"`)
			break
		}
	}
	if !found {
		t.Fatalf("missing k8s cluster verify artifact in %+v", audit.Artifacts)
	}
}

func TestParseMySQLReplicationStatus(t *testing.T) {
	requireSynced := true
	spec := MySQLSpec{
		ExpectedClusterSize:     3,
		ExpectedReplicatedNodes: 3,
		RequireSynced:           &requireSynced,
		Nodes: []MySQLNodeSpec{
			{ID: "mysql-00", Address: "172.31.235.10"},
			{ID: "mysql-01", Address: "172.31.235.11"},
			{ID: "mysql-02", Address: "172.31.235.12"},
		},
	}
	nodes := parseMySQLReplicationStatus(`
attempt=1 node=mysql-00 ip=172.31.235.10 count=1 cluster=2 state=Synced
attempt=2 node=mysql-00 ip=172.31.235.10 count=1 cluster=3 state=Synced
attempt=2 node=mysql-01 ip=172.31.235.11 count=1 cluster=3 state=Synced
attempt=2 node=mysql-02 ip=172.31.235.12 count=1 cluster=3 state=Donor
attempt=3 node=mysql-02 ip=172.31.235.12 count=1 cluster=3 state=Synced
`, spec)
	if len(nodes) != 3 {
		t.Fatalf("nodes=%d: %#v", len(nodes), nodes)
	}
	if nodes[0].ID != "mysql-00" || nodes[0].Attempt != 2 || !nodes[0].Replicated {
		t.Fatalf("mysql-00 status not latest replicated: %#v", nodes[0])
	}
	if nodes[2].ID != "mysql-02" || nodes[2].Attempt != 3 || !nodes[2].Replicated {
		t.Fatalf("mysql-02 status not latest synced: %#v", nodes[2])
	}
	evidence := mysqlReplicationVerifyEvidence{Nodes: nodes, ExpectedReplicatedNodes: 3}
	for _, node := range nodes {
		if node.Replicated {
			evidence.ReplicatedNodes++
		}
	}
	if err := evaluateMySQLReplicationEvidence(spec, &evidence); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
}

func TestRun_MySQLReplicationVerifyUsesNATSTransport(t *testing.T) {
	root := t.TempDir()
	serverURL := startStackTestNATSServer(t)
	subject := "torque.lab.assign.mysql"
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	sshLog := filepath.Join(root, "ssh.log")
	writeExecutableForTest(t, filepath.Join(binDir, "ssh"), `#!/bin/sh
last=""
for arg in "$@"; do
  last="$arg"
done
if [ -n "${TORQUE_TEST_SSH_LOG:-}" ]; then
  printf '%s\n' "$last" >>"${TORQUE_TEST_SSH_LOG}"
fi
case "$last" in
  *SELECT*COUNT*) printf '1\n' ;;
  *wsrep_cluster_size*) printf 'wsrep_cluster_size\t2\n' ;;
  *wsrep_local_state_comment*) printf 'wsrep_local_state_comment\tSynced\n' ;;
  *) exit 0 ;;
esac
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TORQUE_TEST_SSH_LOG", sshLog)
	t.Setenv("TORQUE_NATS_URL", serverURL)
	ready := make(chan struct{})
	worker, err := natsworker.New(natsworker.Config{
		Server:                     serverURL,
		Subject:                    subject,
		Ready:                      ready,
		Timeout:                    2 * time.Second,
		Capabilities:               []string{NodeKindMySQLReplicationVerify},
		DisableCapabilityDiscovery: true,
		AgentID:                    "agent-mysql-verify",
		Tenant:                     "lab",
		TargetID:                   "host/mysql-verify",
		Hostname:                   "mysql-verify",
	})
	if err != nil {
		t.Fatalf("new nats worker: %v", err)
	}
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	workerErr := make(chan error, 1)
	go func() {
		workerErr <- worker.Run(workerCtx)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		workerCancel()
		t.Fatal("nats worker did not become ready")
	}

	interval := time.Millisecond
	requireSynced := true
	node := &ResolvedRelease{
		ID:   "mysql.replication.verify/mysql-verify",
		Kind: NodeKindMySQLReplicationVerify,
		Name: "mysql-verify",
		Dir:  root,
		MySQL: MySQLSpec{
			Transport:               "nats-mesh",
			Target:                  subject,
			ExpectedClusterSize:     2,
			ExpectedReplicatedNodes: 2,
			Database:                "torque_ops",
			ProbeTable:              "replication_probe",
			ProbeID:                 "probe-1",
			StatusPath:              filepath.Join(root, "mysql-status.txt"),
			StableAttempts:          1,
			StableInterval:          &interval,
			RequireSynced:           &requireSynced,
			Nodes: []MySQLNodeSpec{
				{ID: "mysql-00", Address: "10.0.0.10"},
				{ID: "mysql-01", Address: "10.0.0.11"},
			},
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		status, _ := os.ReadFile(filepath.Join(root, "mysql-status.txt"))
		sshCalls, _ := os.ReadFile(sshLog)
		runID, _ := LoadMostRecentRun(root)
		var artifactBody string
		if runID != "" {
			if audit, auditErr := GetRunAudit(context.Background(), RunAuditOptions{RootDir: root, RunID: runID, IncludeArtifacts: true}); auditErr == nil {
				for _, artifact := range audit.Artifacts {
					if artifact.Name == "mysql-replication-execute.json" || artifact.Name == "mysql-replication-verify.json" {
						artifactBody += "\n" + artifact.Name + "=" + artifact.Body
					}
				}
			}
		}
		t.Fatalf("Run apply: %v\nstderr=%s\nstatus=%s\nssh=%s\nartifacts=%s", err, errOut.String(), string(status), string(sshCalls), artifactBody)
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	found := false
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID == node.ID && artifact.Name == "mysql-replication-verify.json" {
			found = strings.Contains(artifact.Body, `"status": "succeeded"`) &&
				strings.Contains(artifact.Body, `"replicatedNodes": 2`) &&
				strings.Contains(artifact.Body, `"nats.request"`) &&
				strings.Contains(artifact.Body, "requiredCapability") &&
				strings.Contains(artifact.Body, "mysql.replication.verify") &&
				strings.Contains(artifact.Body, "nodeId") &&
				strings.Contains(artifact.Body, "runId") &&
				strings.Contains(artifact.Body, `"metadata"`) &&
				strings.Contains(artifact.Body, "agent-mysql-verify") &&
				strings.Contains(artifact.Body, "workerDecision") &&
				strings.Contains(artifact.Body, "executed")
			break
		}
	}
	if !found {
		t.Fatalf("missing nats-backed mysql replication evidence in %+v", audit.Artifacts)
	}
	workerCancel()
	select {
	case err := <-workerErr:
		if err != nil {
			t.Fatalf("nats worker error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nats worker did not stop")
	}
}

func TestRun_HostCommandFleetModeFansOutToReadyNATSAgents(t *testing.T) {
	root := t.TempDir()
	serverURL := startStackTestNATSServer(t)
	registryPath := filepath.Join(root, ".torque", "agent-registry.json")
	marker := filepath.Join(root, "fanout-marker.txt")
	stackYAML := fmt.Sprintf(`apiVersion: torque.dev/v1
kind: Stack
name: fleet-fanout
runner:
  mode: fleet
  readiness:
    source: store
    store: file
    storePath: %q
    tenant: lab
    selector:
      role: mysql
    requireAgents: true
    minReadyPercent: 100
    failureBudget: 0
    staleAfter: 45s
    onInsufficientReady: block
  fanout:
    maxParallel: 3
    maxFailed: 0
    minSucceededPercent: 100
    onPartialFailure: block
nodes:
  - kind: host.command.run
    name: write-marker
    host:
      transport: nats
      command: "printf 'fanout-hit\n' >> %s"
`, registryPath, marker)
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatalf("write stack: %v", err)
	}
	now := time.Now().UTC()
	agentIDs := []string{"agent-a", "agent-b", "agent-c"}
	for _, agentID := range agentIDs {
		writeFleetReadinessAgent(t, registryPath, agentID, heartbeat.StateReady, now, NodeKindHostCommandRun)
	}
	t.Setenv("TORQUE_NATS_URL", serverURL)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	errCh := make(chan error, len(agentIDs))
	for _, agentID := range agentIDs {
		ready := make(chan struct{})
		worker, err := natsworker.New(natsworker.Config{
			Server:                     serverURL,
			Subject:                    fleetNATSAssignmentSubject("lab", agentID),
			Ready:                      ready,
			Timeout:                    2 * time.Second,
			Capabilities:               []string{NodeKindHostCommandRun},
			DisableCapabilityDiscovery: true,
			AgentID:                    agentID,
			Tenant:                     "lab",
			TargetID:                   agentID,
			Hostname:                   agentID + ".test",
		})
		if err != nil {
			t.Fatalf("new worker %s: %v", agentID, err)
		}
		go func() {
			errCh <- worker.Run(workerCtx)
		}()
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			t.Fatalf("worker %s did not become ready", agentID)
		}
	}

	p := compileFleetReadinessStack(t, root)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	rawMarker, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if got := strings.Count(string(rawMarker), "fanout-hit"); got != 3 {
		t.Fatalf("marker hits = %d, want 3: %q", got, string(rawMarker))
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	var fanout fleetNATSFanoutReceipt
	for _, artifact := range audit.Artifacts {
		if artifact.Name == "host-command-fanout.json" {
			if err := json.Unmarshal([]byte(artifact.Body), &fanout); err != nil {
				t.Fatalf("parse fanout artifact: %v\n%s", err, artifact.Body)
			}
		}
	}
	if fanout.Status != "succeeded" || fanout.Summary.TargetCount != 3 || fanout.Summary.Succeeded != 3 || len(fanout.Results) != 3 {
		t.Fatalf("fanout artifact = %#v", fanout)
	}
	gotAgents := map[string]bool{}
	for _, result := range fanout.Results {
		gotAgents[result.Receipt.Metadata["agentId"]] = true
		if result.Receipt.Metadata["assignmentTargetId"] != result.TargetID || result.Receipt.Metadata["expectedAgentId"] != result.AgentID {
			t.Fatalf("assignment metadata mismatch for result %#v", result)
		}
	}
	for _, agentID := range agentIDs {
		if !gotAgents[agentID] {
			t.Fatalf("missing receipt for %s in %#v", agentID, fanout.Results)
		}
	}
	workerCancel()
	for range agentIDs {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("worker error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not stop")
		}
	}
}

func TestRun_HostCommandFleetModeJetStreamFanoutDeliversOfflineAssignment(t *testing.T) {
	root := t.TempDir()
	serverURL := startStackTestNATSJetStreamServer(t)
	registryPath := filepath.Join(root, ".torque", "agent-registry.json")
	marker := filepath.Join(root, "fanout-jetstream-marker.txt")
	assignmentStream := "TORQUE_ASSIGNMENTS_TEST"
	receiptStream := "TORQUE_RECEIPTS_TEST"
	stackYAML := fmt.Sprintf(`apiVersion: torque.dev/v1
kind: Stack
name: fleet-jetstream-fanout
runner:
  mode: fleet
  readiness:
    source: store
    store: file
    storePath: %q
    tenant: lab
    selector:
      role: mysql
    requireAgents: true
    minReadyPercent: 100
    failureBudget: 0
    staleAfter: 45s
    onInsufficientReady: block
  fanout:
    delivery: jetstream
    maxParallel: 1
    maxFailed: 0
    minSucceededPercent: 100
    onPartialFailure: block
nodes:
  - kind: host.command.run
    name: write-marker
    host:
      transport: nats
      timeout: 8s
      command: "printf 'jetstream-hit\n' >> %s"
`, registryPath, marker)
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatalf("write stack: %v", err)
	}
	agentID := "agent-js-fanout"
	writeFleetReadinessAgent(t, registryPath, agentID, heartbeat.StateReady, time.Now().UTC(), NodeKindHostCommandRun)
	t.Setenv("TORQUE_NATS_URL", serverURL)
	t.Setenv("TORQUE_NATS_ASSIGNMENT_STREAM", assignmentStream)
	t.Setenv("TORQUE_NATS_RECEIPT_STREAM", receiptStream)

	p := compileFleetReadinessStack(t, root)
	var out, errOut bytes.Buffer
	applyErr := make(chan error, 1)
	go func() {
		applyErr <- Run(context.Background(), RunOptions{
			Command:     "apply",
			Plan:        p,
			Concurrency: 1,
			Lock:        true,
		}, &out, &errOut)
	}()
	waitForStackTestStreamMessages(t, serverURL, assignmentStream, 1)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	ready := make(chan struct{})
	worker, err := natsworker.New(natsworker.Config{
		Server:                     serverURL,
		Subject:                    fleetNATSAssignmentSubject("lab", agentID),
		Delivery:                   natstransport.DeliveryJetStream,
		AssignmentStream:           assignmentStream,
		ReceiptStream:              receiptStream,
		Durable:                    "stack-js-fanout-worker",
		LedgerPath:                 filepath.Join(root, "agent-assignments.sqlite"),
		Ready:                      ready,
		Timeout:                    2 * time.Second,
		Capabilities:               []string{NodeKindHostCommandRun},
		DisableCapabilityDiscovery: true,
		AgentID:                    agentID,
		Tenant:                     "lab",
		TargetID:                   agentID,
		Hostname:                   agentID + ".test",
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	workerErr := make(chan error, 1)
	go func() {
		workerErr <- worker.Run(workerCtx)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not become ready")
	}
	select {
	case err := <-applyErr:
		if err != nil {
			t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stack apply did not finish")
	}
	rawMarker, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if got := strings.Count(string(rawMarker), "jetstream-hit"); got != 1 {
		t.Fatalf("marker hits = %d, want 1: %q", got, string(rawMarker))
	}
	audit := fleetReadinessAudit(t, root)
	var fanout fleetNATSFanoutReceipt
	for _, artifact := range audit.Artifacts {
		if artifact.Name == "host-command-fanout.json" {
			if err := json.Unmarshal([]byte(artifact.Body), &fanout); err != nil {
				t.Fatalf("parse fanout artifact: %v\n%s", err, artifact.Body)
			}
		}
	}
	if fanout.Status != "succeeded" || fanout.Policy.Delivery != RunnerFanoutDeliveryJetStream || fanout.Summary.Succeeded != 1 || len(fanout.Results) != 1 {
		t.Fatalf("fanout artifact = %#v", fanout)
	}
	result := fanout.Results[0]
	if result.Assignment == nil || result.AssignmentOffset == nil || result.AssignmentOffset.Sequence == 0 || result.ReceiptOffset == nil || result.ReceiptOffset.Sequence == 0 {
		t.Fatalf("missing JetStream assignment/receipt offsets: %#v", result)
	}
	if result.Receipt.Metadata["delivery"] != natstransport.DeliveryJetStream || result.Receipt.Metadata["workerDecision"] != "executed" {
		t.Fatalf("unexpected receipt metadata: %#v", result.Receipt.Metadata)
	}
	workerCancel()
	select {
	case err := <-workerErr:
		if err != nil {
			t.Fatalf("worker error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestRun_HostCommandFleetModeJetStreamFanoutAttachesSlotLease(t *testing.T) {
	root := t.TempDir()
	serverURL := startStackTestNATSJetStreamServer(t)
	registryPath := filepath.Join(root, ".torque", "agent-registry.json")
	slotLedgerPath := filepath.Join(root, ".torque", "fleet", "target-slot-ledger.sqlite")
	marker := filepath.Join(root, "fanout-jetstream-slot-marker.txt")
	assignmentStream := "TORQUE_ASSIGNMENTS_SLOT_TEST"
	receiptStream := "TORQUE_RECEIPTS_SLOT_TEST"
	stackYAML := fmt.Sprintf(`apiVersion: torque.dev/v1
kind: Stack
name: fleet-jetstream-slot-lease
runner:
  mode: fleet
  readiness:
    source: store
    store: file
    storePath: %q
    tenant: lab
    selector:
      role: mysql
    requireAgents: true
    minReadyPercent: 100
    failureBudget: 0
    staleAfter: 45s
    onInsufficientReady: block
  fanout:
    delivery: jetstream
    maxParallel: 1
    maxFailed: 0
    minSucceededPercent: 100
    onPartialFailure: block
    targetConcurrency:
      enabled: true
      requireAvailable: true
      maxPerTarget: 2
      leaseTTL: 20s
      ledger:
        enabled: true
        store: file
        storePath: %q
nodes:
  - kind: host.command.run
    name: write-slot-marker
    host:
      transport: nats
      timeout: 8s
      command: "printf 'slot-hit\n' >> %s"
`, registryPath, slotLedgerPath, marker)
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatalf("write stack: %v", err)
	}
	agentID := "agent-js-slot"
	writeFleetReadinessAgentWithWorkerSlots(t, registryPath, agentID, heartbeat.StateReady, time.Now().UTC(), heartbeat.Slots{Total: 2, InUse: 1}, NodeKindHostCommandRun)
	t.Setenv("TORQUE_NATS_URL", serverURL)
	t.Setenv("TORQUE_NATS_ASSIGNMENT_STREAM", assignmentStream)
	t.Setenv("TORQUE_NATS_RECEIPT_STREAM", receiptStream)

	p := compileFleetReadinessStack(t, root)
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	ready := make(chan struct{})
	worker, err := natsworker.New(natsworker.Config{
		Server:                     serverURL,
		Subject:                    fleetNATSAssignmentSubject("lab", agentID),
		Delivery:                   natstransport.DeliveryJetStream,
		AssignmentStream:           assignmentStream,
		ReceiptStream:              receiptStream,
		Durable:                    "stack-js-slot-worker",
		LedgerPath:                 filepath.Join(root, "agent-assignments.sqlite"),
		Ready:                      ready,
		Timeout:                    2 * time.Second,
		Capabilities:               []string{NodeKindHostCommandRun},
		DisableCapabilityDiscovery: true,
		AgentID:                    agentID,
		WorkerID:                   "slot-worker-a",
		Tenant:                     "lab",
		TargetID:                   agentID,
		Hostname:                   agentID + ".test",
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	workerErr := make(chan error, 1)
	go func() {
		workerErr <- worker.Run(workerCtx)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not become ready")
	}

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	if got := countFileSubstring(t, marker, "slot-hit"); got != 1 {
		t.Fatalf("marker hits = %d, want 1", got)
	}
	audit := fleetReadinessAudit(t, root)
	var fanout fleetNATSFanoutReceipt
	for _, artifact := range audit.Artifacts {
		if artifact.Name == "host-command-fanout.json" {
			if err := json.Unmarshal([]byte(artifact.Body), &fanout); err != nil {
				t.Fatalf("parse fanout artifact: %v\n%s", err, artifact.Body)
			}
		}
	}
	if fanout.Status != "succeeded" || !fanout.Policy.TargetConcurrency.Enabled || fanout.Summary.SlotLeases != 1 {
		t.Fatalf("fanout slot policy = %#v", fanout)
	}
	if len(fanout.Targets) != 1 || fanout.Targets[0].WorkerSlots.Total != 2 || fanout.Targets[0].WorkerSlotsAvailable != 1 || fanout.Targets[0].SlotLease == nil {
		t.Fatalf("fanout target slot evidence = %#v", fanout.Targets)
	}
	if fanout.Targets[0].SlotLease.Status != "released" || fanout.Targets[0].SlotLease.LedgerStore != "file" || fanout.Targets[0].SlotLease.LedgerTokenDigest == "" {
		t.Fatalf("fanout target ledger evidence = %#v", fanout.Targets[0].SlotLease)
	}
	if len(fanout.Results) != 1 {
		t.Fatalf("fanout results = %#v", fanout.Results)
	}
	result := fanout.Results[0]
	if result.Assignment == nil || result.SlotLease == nil || result.Assignment.SlotLeaseID == "" || result.Assignment.SlotLeaseID != result.SlotLease.ID {
		t.Fatalf("assignment slot lease missing: %#v", result)
	}
	if result.SlotLease.Status != "released" || result.SlotLease.ReleasedAt == "" {
		t.Fatalf("result slot lease was not released: %#v", result.SlotLease)
	}
	metadata := result.Receipt.Metadata
	if metadata["slotLeaseId"] != result.SlotLease.ID || metadata["slotLeaseTargetId"] != agentID || metadata["slotLeaseIndex"] != "1" || metadata["slotLeaseSlots"] != "1" {
		t.Fatalf("receipt slot lease metadata = %#v, lease=%#v", metadata, result.SlotLease)
	}
	workerCancel()
	select {
	case err := <-workerErr:
		if err != nil {
			t.Fatalf("worker error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestFleetNATSSlotLeaseRequiresAvailableWorkerSlot(t *testing.T) {
	exec := &customNodeExecutor{}
	_, err := exec.assignFleetNATSSlotLeases(context.Background(), nil, fleetNATSFanoutPolicy{
		TargetConcurrency: RunnerFanoutTargetConcurrencyResolved{
			Enabled:          true,
			RequireAvailable: true,
			MaxPerTarget:     2,
			LeaseTTL:         30 * time.Second,
		},
	}, "run-slot", "node-slot", []fleetNATSFanoutTarget{
		{
			agentID:     "agent-slot",
			targetID:    "host/slot",
			workerSlots: heartbeat.Slots{Total: 2, InUse: 2},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "no available worker slots") {
		t.Fatalf("expected no available worker slot error, got %v", err)
	}
}

func TestRun_HostCommandFleetModeJetStreamFanoutResumesReceiptOffset(t *testing.T) {
	root := t.TempDir()
	serverURL := startStackTestNATSJetStreamServer(t)
	registryPath := filepath.Join(root, ".torque", "agent-registry.json")
	marker := filepath.Join(root, "fanout-jetstream-resume-marker.txt")
	assignmentStream := "TORQUE_ASSIGNMENTS_RESUME_TEST"
	receiptStream := "TORQUE_RECEIPTS_RESUME_TEST"
	stackYAML := fmt.Sprintf(`apiVersion: torque.dev/v1
kind: Stack
name: fleet-jetstream-resume
runner:
  mode: fleet
  readiness:
    source: store
    store: file
    storePath: %q
    tenant: lab
    selector:
      role: mysql
    requireAgents: true
    minReadyPercent: 100
    failureBudget: 0
    staleAfter: 45s
    onInsufficientReady: block
  fanout:
    delivery: jetstream
    maxParallel: 1
    maxFailed: 0
    minSucceededPercent: 100
    onPartialFailure: block
nodes:
  - kind: host.command.run
    name: write-marker
    host:
      transport: nats
      timeout: 8s
      command: "printf 'resume-hit\n' >> %s"
`, registryPath, marker)
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatalf("write stack: %v", err)
	}
	agentID := "agent-js-resume"
	writeFleetReadinessAgent(t, registryPath, agentID, heartbeat.StateReady, time.Now().UTC(), NodeKindHostCommandRun)
	t.Setenv("TORQUE_NATS_URL", serverURL)
	t.Setenv("TORQUE_NATS_ASSIGNMENT_STREAM", assignmentStream)
	t.Setenv("TORQUE_NATS_RECEIPT_STREAM", receiptStream)

	p := compileFleetReadinessStack(t, root)
	workerCtx, workerCancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	worker, err := natsworker.New(natsworker.Config{
		Server:                     serverURL,
		Subject:                    fleetNATSAssignmentSubject("lab", agentID),
		Delivery:                   natstransport.DeliveryJetStream,
		AssignmentStream:           assignmentStream,
		ReceiptStream:              receiptStream,
		Durable:                    "stack-js-resume-worker",
		LedgerPath:                 filepath.Join(root, "agent-assignments.sqlite"),
		Ready:                      ready,
		Timeout:                    2 * time.Second,
		Capabilities:               []string{NodeKindHostCommandRun},
		DisableCapabilityDiscovery: true,
		AgentID:                    agentID,
		Tenant:                     "lab",
		TargetID:                   agentID,
		Hostname:                   agentID + ".test",
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	workerErr := make(chan error, 1)
	go func() {
		workerErr <- worker.Run(workerCtx)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		workerCancel()
		t.Fatal("worker did not become ready")
	}

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		workerCancel()
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	workerCancel()
	select {
	case err := <-workerErr:
		if err != nil {
			t.Fatalf("worker error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	if got := countFileSubstring(t, marker, "resume-hit"); got != 1 {
		t.Fatalf("marker hits after first run = %d, want 1", got)
	}
	firstRunID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	nodeID := "host.command.run/write-marker"
	store, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	checkpoints, err := store.ListReceiptOffsets(context.Background(), firstRunID, nodeID)
	if err != nil {
		_ = store.Close()
		t.Fatalf("ListReceiptOffsets: %v", err)
	}
	if len(checkpoints) != 1 || checkpoints[0].Offset == nil || checkpoints[0].Offset.Sequence == 0 {
		_ = store.Close()
		t.Fatalf("receipt checkpoints = %#v", checkpoints)
	}
	if _, err := store.db.ExecContext(context.Background(), `UPDATE torque_stack_nodes SET status = 'failed', error = 'simulated controller restart' WHERE run_id = ? AND node_id = ?`, firstRunID, nodeID); err != nil {
		_ = store.Close()
		t.Fatalf("mark node failed: %v", err)
	}
	if _, err := store.db.ExecContext(context.Background(), `UPDATE torque_stack_runs SET status = 'failed' WHERE run_id = ?`, firstRunID); err != nil {
		_ = store.Close()
		t.Fatalf("mark run failed: %v", err)
	}
	_ = store.Close()
	beforeAssignments := stackTestStreamMessageCount(t, serverURL, assignmentStream)

	loaded, err := LoadRun(root, firstRunID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	stepsByID, err := LoadRunNodeSteps(root, firstRunID)
	if err != nil {
		t.Fatalf("LoadRunNodeSteps: %v", err)
	}
	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), RunOptions{
		Command:           "apply",
		Plan:              loaded.Plan,
		Concurrency:       1,
		Lock:              false,
		ResumeFromRunID:   firstRunID,
		ResumeStatusByID:  loaded.StatusByID,
		ResumeAttemptByID: loaded.AttemptByID,
		ResumeStepsByID:   stepsByID,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run resume: %v\nstderr=%s", err, errOut.String())
	}
	if got := countFileSubstring(t, marker, "resume-hit"); got != 1 {
		t.Fatalf("marker hits after resume = %d, want 1", got)
	}
	afterAssignments := stackTestStreamMessageCount(t, serverURL, assignmentStream)
	if afterAssignments != beforeAssignments {
		t.Fatalf("resume published assignment messages: before=%d after=%d", beforeAssignments, afterAssignments)
	}
	audit := fleetReadinessAudit(t, root)
	var fanout fleetNATSFanoutReceipt
	for _, artifact := range audit.Artifacts {
		if artifact.Name == "host-command-fanout.json" {
			if err := json.Unmarshal([]byte(artifact.Body), &fanout); err != nil {
				t.Fatalf("parse fanout artifact: %v\n%s", err, artifact.Body)
			}
		}
	}
	if fanout.Status != "succeeded" || fanout.ReceiptRunID != firstRunID || fanout.ResumeFromRunID != firstRunID || len(fanout.Results) != 1 {
		t.Fatalf("resume fanout artifact = %#v", fanout)
	}
	result := fanout.Results[0]
	if result.ReceiptOffset == nil || result.ReceiptOffset.Sequence != checkpoints[0].Offset.Sequence {
		t.Fatalf("resume receipt offset = %#v want %#v", result.ReceiptOffset, checkpoints[0].Offset)
	}
	if result.Receipt.Metadata["receiptOffsetResumed"] != "true" || result.Receipt.Metadata["resumeFromRunId"] != firstRunID {
		t.Fatalf("resume metadata = %#v", result.Receipt.Metadata)
	}
}

func TestRun_KubernetesLifecycleSummaryArtifact(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "cert-renewal-state.json")
	renewLog := filepath.Join(root, "renew.log")
	maxInspectAge := 15 * time.Minute
	inspect := &ResolvedRelease{
		ID:        "k8s.cluster.inspect/cluster",
		Kind:      NodeKindK8sClusterInspect,
		Name:      "cluster",
		Dir:       root,
		Namespace: "default",
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{
				Transport:         "local",
				Target:            "local://localhost",
				Namespaces:        []string{"gitlab"},
				ConfigCommand:     `printf '%s\n' '{"clusters":[{"name":"lab","cluster":{"server":"https://127.0.0.1:6443"}}]}'`,
				APICommand:        `printf '%s\n' '{"serverVersion":{"gitVersion":"v1.30.4+k3s1","major":"1","minor":"30"}}'`,
				NodesCommand:      `printf '%s\n' '{"items":[{"metadata":{"name":"cp-1","labels":{"node-role.kubernetes.io/control-plane":"","k3s.io/hostname":"cp-1"}},"status":{"addresses":[{"type":"InternalIP","address":"10.0.0.10"}],"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.30.4+k3s1"}}},{"metadata":{"name":"worker-1","labels":{}},"status":{"addresses":[{"type":"InternalIP","address":"10.0.0.11"}],"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.30.4+k3s1"}}}]}'`,
				NamespacesCommand: `printf '%s\n' '{"items":[{"metadata":{"name":"kube-system"},"status":{"phase":"Active"}},{"metadata":{"name":"gitlab"},"status":{"phase":"Active"}}]}'`,
				PodsCommand:       `case "{{namespace}}" in kube-system) printf '%s\n' '{"items":[{"metadata":{"name":"coredns","namespace":"kube-system"},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}]}' ;; gitlab) printf '%s\n' '{"items":[{"metadata":{"name":"webservice","namespace":"gitlab"},"status":{"phase":"Running","containerStatuses":[{"ready":true},{"ready":true}]}}]}' ;; esac`,
			},
		},
	}
	certInspect := &ResolvedRelease{
		ID:        "k8s.cert.inspect/cert-inspect",
		Kind:      NodeKindK8sCertInspect,
		Name:      "cert-inspect",
		Dir:       root,
		Namespace: "default",
		Needs:     []string{"cluster"},
		Kubernetes: KubernetesSpec{
			Provider: "auto",
			Certificates: KubernetesCertSpec{
				TargetsFrom: KubernetesCertTargetsFromSpec{
					SourceNode:     "cluster",
					Roles:          []string{"control-plane", "worker"},
					Transport:      "local",
					TargetTemplate: "local://{{ .Name }}",
					InspectCommand: `printf '%s\n' '{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}'`,
				},
			},
		},
	}
	certRenew := &ResolvedRelease{
		ID:        "k8s.cert.renew/cert-renew",
		Kind:      NodeKindK8sCertRenew,
		Name:      "cert-renew",
		Dir:       root,
		Namespace: "default",
		Needs:     []string{"cert-inspect"},
		Kubernetes: KubernetesSpec{
			Provider: "auto",
			Certificates: KubernetesCertSpec{
				Force:              true,
				ForceOnceID:        "summary-test",
				StatePath:          statePath,
				HealthCheckCommand: "printf healthy",
				Order:              "control-plane-first",
				BatchSize:          1,
				Policy: KubernetesLifecyclePolicySpec{
					MaxUnavailable:           1,
					RequireFreshInspect:      true,
					MaxInspectAge:            &maxInspectAge,
					RequireHealthyInspect:    true,
					RequireSupportedProvider: true,
					MaintenanceWindow: KubernetesMaintenanceWindowSpec{
						Start:    "00:00",
						End:      "23:59",
						TimeZone: "UTC",
					},
					AppProbes: []KubernetesAppProbe{
						{ID: "gitlab-before-renew", Command: "printf 'GitLab ready'", Expect: "GitLab"},
					},
				},
				TargetsFrom: KubernetesCertTargetsFromSpec{
					SourceNode:     "cluster",
					Roles:          []string{"control-plane", "worker"},
					Transport:      "local",
					TargetTemplate: "local://{{ .Name }}",
					InspectCommand: `printf '%s\n' '{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}'`,
					RenewCommand:   "printf renewed >> " + shellQuoteForTest(renewLog),
				},
			},
		},
	}
	verify := &ResolvedRelease{
		ID:        "k8s.cluster.verify/verify",
		Kind:      NodeKindK8sClusterVerify,
		Name:      "verify",
		Dir:       root,
		Namespace: "default",
		Needs:     []string{"cert-renew"},
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{
				Transport:     "local",
				Target:        "local://localhost",
				MinReadyNodes: 2,
				Namespaces:    []string{"gitlab"},
				APICommand:    "printf api-ok",
				NodesCommand:  `printf '%s\n' '{"items":[{"metadata":{"name":"cp-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}},{"metadata":{"name":"worker-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}'`,
				PodsCommand:   `printf '%s\n' '{"items":[{"metadata":{"name":"webservice","namespace":"gitlab"},"status":{"phase":"Running","containerStatuses":[{"name":"web","ready":true},{"name":"sidekiq","ready":true}]}}]}'`,
				AppProbes: []KubernetesAppProbe{
					{ID: "gitlab-signin", Command: "printf '<title>Sign in - GitLab</title>'", Expect: "GitLab"},
				},
			},
		},
	}
	plan := planForTest(root, inspect, certInspect, certRenew, verify)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	var summary kubernetesLifecycleSummary
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID != verify.ID || artifact.Name != kubernetesLifecycleSummaryArtifact {
			continue
		}
		if strings.Contains(artifact.Body, `"stdout":`) || strings.Contains(artifact.Body, `"stderr":`) {
			t.Fatalf("summary copied raw transport payloads:\n%s", artifact.Body)
		}
		if err := json.Unmarshal([]byte(artifact.Body), &summary); err != nil {
			t.Fatalf("unmarshal summary: %v\n%s", err, artifact.Body)
		}
		break
	}
	var policyDecision kubernetesLifecyclePolicyDecision
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID != certRenew.ID || artifact.Name != kubernetesLifecyclePolicyDecisionArtifact {
			continue
		}
		if strings.Contains(artifact.Body, `"stdout":`) || strings.Contains(artifact.Body, `"stderr":`) {
			t.Fatalf("policy copied raw transport payloads:\n%s", artifact.Body)
		}
		if err := json.Unmarshal([]byte(artifact.Body), &policyDecision); err != nil {
			t.Fatalf("unmarshal policy: %v\n%s", err, artifact.Body)
		}
		break
	}
	if policyDecision.Kind != "KubernetesLifecyclePolicyDecision" || policyDecision.Status != "allowed" || len(policyDecision.AppProbes) != 1 || !policyDecision.AppProbes[0].Matched {
		t.Fatalf("unexpected lifecycle policy decision: %#v", policyDecision)
	}
	if summary.Kind != "KubernetesLifecycleSummary" {
		t.Fatalf("missing lifecycle summary artifact in %+v", audit.Artifacts)
	}
	if len(summary.SourceArtifacts) != 5 {
		t.Fatalf("source artifact count=%d summary=%#v", len(summary.SourceArtifacts), summary)
	}
	for _, source := range summary.SourceArtifacts {
		if !strings.HasPrefix(source.SHA256, "sha256:") {
			t.Fatalf("source artifact without digest: %#v", source)
		}
	}
	if summary.Inspect == nil || summary.Inspect.Provider.Distribution != "k3s" || summary.Inspect.Topology.TotalNodes != 2 || summary.Inspect.Topology.ControlPlaneNodes != 1 {
		t.Fatalf("unexpected inspect summary: %#v", summary.Inspect)
	}
	if summary.CertificateRenew == nil || summary.CertificateRenew.TargetsFrom == nil || summary.CertificateRenew.TargetsFrom.DerivedCount != 2 {
		t.Fatalf("unexpected cert renew summary: %#v", summary.CertificateRenew)
	}
	if summary.Policy == nil || summary.Policy.Status != "allowed" || summary.Policy.MaxUnavailable != 1 || len(summary.Policy.Checks) == 0 {
		t.Fatalf("unexpected lifecycle policy summary: %#v", summary.Policy)
	}
	if summary.CertificateRenew.TargetsFrom.SourceArtifactDigest == "" || summary.CertificateRenew.SourceArtifactDigest == "" {
		t.Fatalf("missing cert digest links: %#v", summary.CertificateRenew)
	}
	if len(summary.CertificateRenew.Targets) != 2 || summary.CertificateRenew.Targets[0].CheckpointStatus == "" || summary.CertificateRenew.Targets[1].CheckpointStatus == "" {
		t.Fatalf("missing checkpoint target summary: %#v", summary.CertificateRenew.Targets)
	}
	if summary.Verify == nil || summary.Verify.ReadyNodes != 2 || len(summary.Verify.AppProbes) != 1 || !summary.Verify.AppProbes[0].Matched {
		t.Fatalf("unexpected verify summary: %#v", summary.Verify)
	}
	if summary.ApplicationGate == nil || summary.ApplicationGate.Status != "passed" || len(summary.ApplicationGate.BeforeProbes) != 1 || len(summary.ApplicationGate.AfterProbes) != 1 {
		t.Fatalf("unexpected application gate summary: %#v", summary.ApplicationGate)
	}
	if summary.ApplicationGate.BeforeSourceArtifactDigest == "" || summary.ApplicationGate.AfterSourceArtifactDigest == "" {
		t.Fatalf("missing application gate digest links: %#v", summary.ApplicationGate)
	}
}

func TestRun_KubernetesLifecycleProviderMatrix(t *testing.T) {
	cases := []struct {
		name                string
		inspectDistribution string
		effectiveProvider   string
		fakeBinary          string
		customTargetsFrom   bool
	}{
		{name: "kubeadm", inspectDistribution: "kubeadm", effectiveProvider: "kubeadm", fakeBinary: "kubeadm"},
		{name: "k3s", inspectDistribution: "k3s", effectiveProvider: "k3s", fakeBinary: "k3s"},
		{name: "rke2", inspectDistribution: "rke2", effectiveProvider: "rke2", fakeBinary: "rke2"},
		{name: "custom", inspectDistribution: "unknown", effectiveProvider: "custom", customTargetsFrom: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			statePath := filepath.Join(root, "cert-renewal-state.json")
			renewLog := filepath.Join(root, "renew.log")
			if tc.fakeBinary != "" {
				fakeBin := filepath.Join(root, "bin")
				writeProviderMatrixFakeBinary(t, fakeBin, tc.fakeBinary)
				t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv("TORQUE_PROVIDER_MATRIX_LOG", renewLog)
			}
			maxInspectAge := 15 * time.Minute
			inspect := &ResolvedRelease{
				ID:        "k8s.cluster.inspect/" + tc.name + "-cluster-inspect",
				Kind:      NodeKindK8sClusterInspect,
				Name:      tc.name + "-cluster-inspect",
				Dir:       root,
				Namespace: "default",
				Kubernetes: KubernetesSpec{
					Cluster: KubernetesClusterSpec{
						Transport:         "local",
						Target:            "local://localhost",
						Namespaces:        []string{"app"},
						ConfigCommand:     providerMatrixJSONCommand(`{"clusters":[{"name":"provider-matrix","cluster":{"server":"https://127.0.0.1:6443"}}]}`),
						APICommand:        providerMatrixAPICommand(tc.name),
						NodesCommand:      providerMatrixNodesCommand(tc.name),
						NamespacesCommand: providerMatrixJSONCommand(`{"items":[{"metadata":{"name":"kube-system"},"status":{"phase":"Active"}},{"metadata":{"name":"app"},"status":{"phase":"Active"}}]}`),
						PodsCommand:       providerMatrixPodsCommand(tc.name),
					},
				},
			}
			certInspect := &ResolvedRelease{
				ID:        "k8s.cert.inspect/" + tc.name + "-cert-inspect",
				Kind:      NodeKindK8sCertInspect,
				Name:      tc.name + "-cert-inspect",
				Dir:       root,
				Namespace: "default",
				Needs:     []string{inspect.Name},
				Kubernetes: KubernetesSpec{
					Provider: "auto",
					Certificates: KubernetesCertSpec{
						TargetsFrom: providerMatrixTargetsFrom(inspect.Name, tc.effectiveProvider, renewLog, tc.customTargetsFrom),
					},
				},
			}
			certRenew := &ResolvedRelease{
				ID:        "k8s.cert.renew/" + tc.name + "-cert-renew",
				Kind:      NodeKindK8sCertRenew,
				Name:      tc.name + "-cert-renew",
				Dir:       root,
				Namespace: "default",
				Needs:     []string{certInspect.Name},
				Kubernetes: KubernetesSpec{
					Provider: "auto",
					Certificates: KubernetesCertSpec{
						Force:              true,
						ForceOnceID:        "provider-matrix-" + tc.name,
						StatePath:          statePath,
						HealthCheckCommand: "printf healthy",
						Order:              "control-plane-first",
						BatchSize:          1,
						Policy: KubernetesLifecyclePolicySpec{
							MaxUnavailable:           1,
							RequireFreshInspect:      true,
							MaxInspectAge:            &maxInspectAge,
							RequireHealthyInspect:    true,
							RequireSupportedProvider: true,
							AppProbes: []KubernetesAppProbe{
								{ID: "matrix-app-before-renew", Command: "printf 'app-ok'", Expect: "app-ok"},
							},
						},
						TargetsFrom: providerMatrixTargetsFrom(inspect.Name, tc.effectiveProvider, renewLog, tc.customTargetsFrom),
					},
				},
			}
			verify := &ResolvedRelease{
				ID:        "k8s.cluster.verify/" + tc.name + "-cluster-verify",
				Kind:      NodeKindK8sClusterVerify,
				Name:      tc.name + "-cluster-verify",
				Dir:       root,
				Namespace: "default",
				Needs:     []string{certRenew.Name},
				Kubernetes: KubernetesSpec{
					Cluster: KubernetesClusterSpec{
						Transport:        "local",
						Target:           "local://localhost",
						MinReadyNodes:    2,
						Namespaces:       []string{"app"},
						StableIterations: 1,
						StableInterval:   durationPtrCustom(0),
						APICommand:       "printf api-ok",
						NodesCommand:     providerMatrixNodesCommand(tc.name),
						PodsCommand:      providerMatrixPodsCommand(tc.name),
						AppProbes: []KubernetesAppProbe{
							{ID: "matrix-app", Command: "printf '<title>app-ok</title>'", Expect: "app-ok"},
						},
					},
				},
			}
			plan := planForTest(root, inspect, certInspect, certRenew, verify)
			var out, errOut bytes.Buffer
			if err := Run(context.Background(), RunOptions{
				Command:     "apply",
				Plan:        plan,
				Concurrency: 1,
				Lock:        true,
			}, &out, &errOut); err != nil {
				t.Fatalf("Run apply: %v\nstderr=%s", err, errOut.String())
			}
			if got := readTrimmedFile(t, renewLog); !strings.Contains(got, "renew-"+tc.effectiveProvider) {
				t.Fatalf("renew log for %s missing provider marker, got %q", tc.name, got)
			}
			runID, err := LoadMostRecentRun(root)
			if err != nil {
				t.Fatalf("LoadMostRecentRun: %v", err)
			}
			audit, err := GetRunAudit(context.Background(), RunAuditOptions{
				RootDir:          root,
				RunID:            runID,
				Verify:           true,
				IncludeArtifacts: true,
			})
			if err != nil {
				t.Fatalf("GetRunAudit: %v", err)
			}
			var summary kubernetesLifecycleSummary
			for _, artifact := range audit.Artifacts {
				if artifact.NodeID != verify.ID || artifact.Name != kubernetesLifecycleSummaryArtifact {
					continue
				}
				if err := json.Unmarshal([]byte(artifact.Body), &summary); err != nil {
					t.Fatalf("unmarshal summary: %v\n%s", err, artifact.Body)
				}
				break
			}
			if summary.Kind != "KubernetesLifecycleSummary" || summary.Status != "succeeded" {
				t.Fatalf("missing successful summary for %s: %#v", tc.name, summary)
			}
			if summary.Inspect == nil || summary.Inspect.Provider.Distribution != tc.inspectDistribution {
				t.Fatalf("unexpected inspect provider for %s: %#v", tc.name, summary.Inspect)
			}
			if summary.CertificateInspect == nil || summary.CertificateInspect.TargetsFrom == nil || summary.CertificateInspect.TargetsFrom.Provider != tc.effectiveProvider || summary.CertificateInspect.TargetsFrom.DerivedCount != 2 {
				t.Fatalf("unexpected cert inspect targetsFrom for %s: %#v", tc.name, summary.CertificateInspect)
			}
			if summary.Policy == nil || summary.Policy.Status != "allowed" || summary.Policy.Inspect == nil || summary.Policy.Inspect.EffectiveCertificateRenewal == nil {
				t.Fatalf("unexpected policy summary for %s: %#v", tc.name, summary.Policy)
			}
			if got := summary.Policy.Inspect.EffectiveCertificateRenewal.Provider; got != tc.effectiveProvider {
				t.Fatalf("effective provider for %s=%q", tc.name, got)
			}
			if !summary.Policy.Inspect.EffectiveCertificateRenewal.Supported {
				t.Fatalf("effective provider not supported for %s: %#v", tc.name, summary.Policy.Inspect.EffectiveCertificateRenewal)
			}
			if summary.CertificateRenew == nil || summary.CertificateRenew.TargetsFrom == nil || summary.CertificateRenew.TargetsFrom.Provider != tc.effectiveProvider {
				t.Fatalf("unexpected cert renew summary for %s: %#v", tc.name, summary.CertificateRenew)
			}
			if summary.Verify == nil || summary.Verify.ReadyNodes != 2 || len(summary.Verify.AppProbes) != 1 || !summary.Verify.AppProbes[0].Matched {
				t.Fatalf("unexpected verify summary for %s: %#v", tc.name, summary.Verify)
			}
		})
	}
}

func TestRun_KubernetesClusterVerifyFailsUnhealthyPods(t *testing.T) {
	root := t.TempDir()
	node := &ResolvedRelease{
		ID:        "k8s.cluster.verify/cluster",
		Kind:      NodeKindK8sClusterVerify,
		Name:      "cluster",
		Dir:       root,
		Namespace: "default",
		Kubernetes: KubernetesSpec{
			Cluster: KubernetesClusterSpec{
				Transport:     "local",
				Target:        "local://localhost",
				MinReadyNodes: 1,
				Namespaces:    []string{"gitlab"},
				APICommand:    "printf api-ok",
				NodesCommand:  `printf '%s\n' '{"items":[{"metadata":{"name":"node-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}'`,
				PodsCommand:   `printf '%s\n' '{"items":[{"metadata":{"name":"webservice","namespace":"gitlab"},"status":{"phase":"Running","containerStatuses":[{"name":"web","ready":false}]}}]}'`,
			},
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "unhealthy pods") {
		t.Fatalf("expected unhealthy pods error, got %v\nstderr=%s", err, errOut.String())
	}
}

func TestRun_KubernetesCertRenewCheckpointsAndSkipsCompletedIntent(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "cert-renewal-state.json")
	renewLog := filepath.Join(root, "renew.log")
	node := &ResolvedRelease{
		ID:        "k8s.cert.renew/certs",
		Kind:      NodeKindK8sCertRenew,
		Name:      "certs",
		Dir:       root,
		Namespace: "default",
		Kubernetes: KubernetesSpec{
			Provider: "custom",
			Certificates: KubernetesCertSpec{
				Force:              true,
				ForceOnceID:        "intent-1",
				StatePath:          statePath,
				HealthCheckCommand: "printf healthy",
				Order:              "control-plane-first",
				BatchSize:          2,
				Targets: []KubernetesCertTarget{
					{
						ID:             "cp-1",
						Role:           "control-plane",
						Transport:      "local",
						Target:         "local://localhost",
						InspectCommand: `printf '%s\n' '{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}'`,
						RenewCommand:   "printf renewed >> " + shellQuoteForTest(renewLog),
					},
				},
			},
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run first apply: %v\nstderr=%s", err, errOut.String())
	}
	if got := readTrimmedFile(t, renewLog); got != "renewed" {
		t.Fatalf("renew log=%q", got)
	}
	var state kubernetesCertTargetState
	rawState, err := os.ReadFile(kubernetesCertTargetStatePath(node.Kubernetes.Certificates, "cp-1"))
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if err := json.Unmarshal(rawState, &state); err != nil {
		t.Fatalf("unmarshal checkpoint: %v\n%s", err, string(rawState))
	}
	if state.Status != "succeeded" || state.Phase != "post-renew" || state.IntentDigest == "" || state.HealthDigest == "" {
		t.Fatalf("unexpected checkpoint: %#v", state)
	}

	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run second apply: %v\nstderr=%s", err, errOut.String())
	}
	if got := readTrimmedFile(t, renewLog); got != "renewed" {
		t.Fatalf("expected checkpoint skip to avoid second renewal, log=%q", got)
	}
}

func TestRun_KubernetesCertRenewBlocksWhenCheckpointHealthChanged(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "cert-renewal-state.json")
	renewLog := filepath.Join(root, "renew.log")
	node := &ResolvedRelease{
		ID:        "k8s.cert.renew/certs",
		Kind:      NodeKindK8sCertRenew,
		Name:      "certs",
		Dir:       root,
		Namespace: "default",
		Kubernetes: KubernetesSpec{
			Provider: "custom",
			Certificates: KubernetesCertSpec{
				Force:              true,
				ForceOnceID:        "intent-1",
				StatePath:          statePath,
				HealthCheckCommand: "printf new-health",
				Targets: []KubernetesCertTarget{
					{
						ID:             "cp-1",
						Transport:      "local",
						Target:         "local://localhost",
						InspectCommand: `printf '%s\n' '{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}'`,
						RenewCommand:   "printf renewed >> " + shellQuoteForTest(renewLog),
					},
				},
			},
		},
	}
	intent, _, err := ComputeEffectiveInputHash(root, node, true)
	if err != nil {
		t.Fatalf("ComputeEffectiveInputHash: %v", err)
	}
	node.EffectiveInputHash = intent
	oldDigest, err := hashJSONStable(struct {
		Stdout string `json:"stdout,omitempty"`
		Stderr string `json:"stderr,omitempty"`
	}{Stdout: "old-health"})
	if err != nil {
		t.Fatalf("hash old health: %v", err)
	}
	state := kubernetesCertTargetCheckpoint(
		&runNode{ResolvedRelease: node},
		kubernetesCertTargetEvidence{ID: "cp-1", IntentDigest: kubernetesCertTargetIntentDigest(&runNode{ResolvedRelease: node}, node.Kubernetes.Certificates.Targets[0])},
		"running",
		"pre-renew",
		oldDigest,
		digestString("inspect"),
		"",
	)
	if err := os.MkdirAll(filepath.Dir(kubernetesCertTargetStatePath(node.Kubernetes.Certificates, "cp-1")), 0o755); err != nil {
		t.Fatalf("mkdir checkpoint dir: %v", err)
	}
	rawState, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(kubernetesCertTargetStatePath(node.Kubernetes.Certificates, "cp-1"), rawState, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	err = Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "checkpoint blocked") {
		t.Fatalf("expected checkpoint blocked error, got %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(renewLog); !os.IsNotExist(err) {
		t.Fatalf("renew command ran despite changed health, stat err=%v", err)
	}
}

func TestRun_KubernetesLifecyclePolicyBlocksUnsafeBatch(t *testing.T) {
	root := t.TempDir()
	renewLog := filepath.Join(root, "renew.log")
	node := kubernetesLifecyclePolicyOverrideTestNode(root, renewLog)
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "lifecycle policy blocked") {
		t.Fatalf("expected lifecycle policy blocked error, got %v\nstderr=%s", err, errOut.String())
	}
	if _, err := os.Stat(renewLog); !os.IsNotExist(err) {
		t.Fatalf("renew command ran despite lifecycle policy block, stat err=%v", err)
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	var decision kubernetesLifecyclePolicyDecision
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID == node.ID && artifact.Name == kubernetesLifecyclePolicyDecisionArtifact {
			if err := json.Unmarshal([]byte(artifact.Body), &decision); err != nil {
				t.Fatalf("unmarshal policy decision: %v\n%s", err, artifact.Body)
			}
			break
		}
	}
	if decision.Status != "blocked" || decision.MaxUnavailable != 1 || !strings.Contains(decision.Message, "largest maintenance batch") {
		t.Fatalf("unexpected policy decision: %#v", decision)
	}
}

func TestRun_KubernetesLifecyclePolicyOverrideApprovesScopedBlock(t *testing.T) {
	root := t.TempDir()
	renewLog := filepath.Join(root, "renew.log")
	node := kubernetesLifecyclePolicyOverrideTestNode(root, renewLog)
	attachKubernetesLifecyclePolicyOverrideForTest(t, root, node, func(spec *KubernetesLifecyclePolicyOverrideSpec) {})
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:        "apply",
		Plan:           plan,
		Concurrency:    1,
		Lock:           true,
		PolicyOverride: true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run apply with policy override: %v\nstderr=%s", err, errOut.String())
	}
	if got := readTrimmedFile(t, renewLog); got != "cp-1cp-2" {
		t.Fatalf("renew log=%q", got)
	}
	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	var policy kubernetesLifecyclePolicyDecision
	var override kubernetesLifecyclePolicyOverrideDecision
	for _, artifact := range audit.Artifacts {
		switch {
		case artifact.NodeID == node.ID && artifact.Name == kubernetesLifecyclePolicyDecisionArtifact:
			if err := json.Unmarshal([]byte(artifact.Body), &policy); err != nil {
				t.Fatalf("unmarshal policy: %v\n%s", err, artifact.Body)
			}
		case artifact.NodeID == node.ID && artifact.Name == kubernetesLifecyclePolicyOverrideArtifact:
			if err := json.Unmarshal([]byte(artifact.Body), &override); err != nil {
				t.Fatalf("unmarshal override: %v\n%s", err, artifact.Body)
			}
		}
	}
	if policy.Status != "override-approved" || !strings.Contains(policy.Message, "CHG-123") {
		t.Fatalf("unexpected policy decision: %#v", policy)
	}
	if override.Status != "approved" || !override.RuntimeEnabled || override.RuntimeScope.TargetSetDigest == "" {
		t.Fatalf("unexpected override decision: %#v", override)
	}
}

func TestRun_KubernetesLifecyclePolicyOverrideRejectsInvalidApproval(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*KubernetesLifecyclePolicyOverrideSpec)
		want   string
	}{
		{
			name: "expired",
			mutate: func(spec *KubernetesLifecyclePolicyOverrideSpec) {
				spec.ExpiresAt = time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
			},
			want: "expired",
		},
		{
			name: "wrong_node",
			mutate: func(spec *KubernetesLifecyclePolicyOverrideSpec) {
				spec.Scope.NodeID = "k8s.cert.renew/other"
			},
			want: "scope.nodeId",
		},
		{
			name: "changed_intent",
			mutate: func(spec *KubernetesLifecyclePolicyOverrideSpec) {
				spec.Scope.IntentDigest = "sha256:changed"
			},
			want: "intent",
		},
		{
			name: "missing_reason",
			mutate: func(spec *KubernetesLifecyclePolicyOverrideSpec) {
				spec.Reason = ""
			},
			want: "reason",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			renewLog := filepath.Join(root, "renew.log")
			node := kubernetesLifecyclePolicyOverrideTestNode(root, renewLog)
			attachKubernetesLifecyclePolicyOverrideForTest(t, root, node, tc.mutate)
			plan := planForTest(root, node)
			var out, errOut bytes.Buffer
			err := Run(context.Background(), RunOptions{
				Command:        "apply",
				Plan:           plan,
				Concurrency:    1,
				Lock:           true,
				PolicyOverride: true,
			}, &out, &errOut)
			if err == nil || !strings.Contains(err.Error(), "lifecycle policy override rejected") {
				t.Fatalf("expected override rejection, got %v\nstderr=%s", err, errOut.String())
			}
			if _, err := os.Stat(renewLog); !os.IsNotExist(err) {
				t.Fatalf("renew command ran despite rejected override, stat err=%v", err)
			}
			runID, err := LoadMostRecentRun(root)
			if err != nil {
				t.Fatalf("LoadMostRecentRun: %v", err)
			}
			audit, err := GetRunAudit(context.Background(), RunAuditOptions{
				RootDir:          root,
				RunID:            runID,
				IncludeArtifacts: true,
			})
			if err != nil {
				t.Fatalf("GetRunAudit: %v", err)
			}
			var override kubernetesLifecyclePolicyOverrideDecision
			for _, artifact := range audit.Artifacts {
				if artifact.NodeID == node.ID && artifact.Name == kubernetesLifecyclePolicyOverrideArtifact {
					if err := json.Unmarshal([]byte(artifact.Body), &override); err != nil {
						t.Fatalf("unmarshal override: %v\n%s", err, artifact.Body)
					}
					break
				}
			}
			if override.Status != "rejected" || !strings.Contains(override.Message, tc.want) {
				t.Fatalf("unexpected override decision: %#v", override)
			}
		})
	}
}

func kubernetesLifecyclePolicyOverrideTestNode(root string, renewLog string) *ResolvedRelease {
	return &ResolvedRelease{
		ID:        "k8s.cert.renew/certs",
		Kind:      NodeKindK8sCertRenew,
		Name:      "certs",
		Dir:       root,
		Namespace: "default",
		Kubernetes: KubernetesSpec{
			Provider: "custom",
			Certificates: KubernetesCertSpec{
				Force:     true,
				BatchSize: 2,
				Policy: KubernetesLifecyclePolicySpec{
					MaxUnavailable: 1,
				},
				Targets: []KubernetesCertTarget{
					{
						ID:             "cp-1",
						Transport:      "local",
						Target:         "local://localhost",
						InspectCommand: `printf '%s\n' '{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}'`,
						RenewCommand:   "printf cp-1 >> " + shellQuoteForTest(renewLog),
					},
					{
						ID:             "cp-2",
						Transport:      "local",
						Target:         "local://localhost",
						InspectCommand: `printf '%s\n' '{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}'`,
						RenewCommand:   "printf cp-2 >> " + shellQuoteForTest(renewLog),
					},
				},
			},
		},
	}
}

func attachKubernetesLifecyclePolicyOverrideForTest(t *testing.T, root string, node *ResolvedRelease, mutate func(*KubernetesLifecyclePolicyOverrideSpec)) {
	t.Helper()
	intent, _, err := ComputeEffectiveInputHash(root, node, true)
	if err != nil {
		t.Fatalf("ComputeEffectiveInputHash: %v", err)
	}
	override := KubernetesLifecyclePolicyOverrideSpec{
		Reason:    "emergency maintenance window",
		ChangeID:  "CHG-123",
		Approver:  "sre@example.com",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		Scope: KubernetesLifecyclePolicyOverrideScopeSpec{
			NodeID:       node.ID,
			IntentDigest: intent,
			TargetIDs:    []string{"cp-1", "cp-2"},
		},
	}
	if mutate != nil {
		mutate(&override)
	}
	node.Kubernetes.Certificates.Policy.Override = override
}

func TestKubernetesCertHelpers(t *testing.T) {
	expiry, count := parseKubernetesCertExpiry(`{"items":[{"notAfter":"2031-01-01T00:00:00Z"},{"expiration":"2030-01-01T00:00:00Z"}]}`)
	if count != 2 || expiry.UTC().Format(time.RFC3339) != "2030-01-01T00:00:00Z" {
		t.Fatalf("json expiry=%s count=%d", expiry.UTC().Format(time.RFC3339), count)
	}
	expiry, count = parseKubernetesCertExpiry("CERT 2032-02-03T04:05:06Z")
	if count != 1 || expiry.UTC().Format(time.RFC3339) != "2032-02-03T04:05:06Z" {
		t.Fatalf("text expiry=%s count=%d", expiry.UTC().Format(time.RFC3339), count)
	}
	expiry, count = parseKubernetesCertExpiry("CERTIFICATE  Jun 10, 2035 11:11 UTC")
	if count != 1 || expiry.UTC().Format(time.RFC3339) != "2035-06-10T11:11:00Z" {
		t.Fatalf("human text expiry=%s count=%d", expiry.UTC().Format(time.RFC3339), count)
	}

	nested := nestedSSHCommand(KubernetesCertTarget{
		NodeAddress:      "root@10.0.0.10",
		NodeIdentityFile: "/tmp/lab_key",
		NodeSSHOptions:   "-p 2222",
	}, "echo ok")
	for _, want := range []string{"ssh", "root@10.0.0.10", "/tmp/lab_key", "-p", "2222", "echo ok"} {
		if !strings.Contains(nested, want) {
			t.Fatalf("nested ssh command %q missing %q", nested, want)
		}
	}

	kubeadm := kubernetesCertRenewCommand("kubeadm", KubernetesCertTarget{RestartCommand: "systemctl restart kubelet"}, KubernetesCertSpec{Services: []string{"apiserver"}})
	if !strings.Contains(kubeadm, "kubeadm certs renew") || strings.Contains(kubeadm, "--service") || !strings.Contains(kubeadm, "systemctl restart kubelet") {
		t.Fatalf("unexpected kubeadm renew command:\n%s", kubeadm)
	}
	k3s := kubernetesCertRenewCommand("k3s", KubernetesCertTarget{Service: "k3s"}, KubernetesCertSpec{Services: []string{"serving-kubelet.crt", "client-kube-proxy.crt"}})
	if !strings.Contains(k3s, "--service") || !strings.Contains(k3s, "client-kube-proxy.crt,serving-kubelet.crt") {
		t.Fatalf("unexpected k3s service flags:\n%s", k3s)
	}
	custom := kubernetesCertRenewCommand("custom", KubernetesCertTarget{RenewCommand: "renew", RestartCommand: "restart"}, KubernetesCertSpec{})
	if custom != "renew\nrestart" {
		t.Fatalf("custom command=%q", custom)
	}
	unsupported := kubernetesCertRenewCommand("custom", KubernetesCertTarget{}, KubernetesCertSpec{})
	if !strings.Contains(unsupported, "unsupported Kubernetes certificate provider") || !strings.Contains(unsupported, "exit 2") {
		t.Fatalf("unsupported command=%q", unsupported)
	}

	certs := KubernetesCertSpec{StatePath: "/var/lib/torque/cert-renewal-state.json"}
	stateA := kubernetesCertTargetStatePath(certs, "cp/1")
	stateB := kubernetesCertTargetStatePath(certs, "cp/2")
	if stateA == stateB || !strings.Contains(stateA, "cp_1") || !strings.Contains(stateB, "cp_2") {
		t.Fatalf("state paths not target-specific: %q %q", stateA, stateB)
	}

	batches := kubernetesCertTargetBatches([]kubernetesCertTargetRunner{
		{spec: KubernetesCertTarget{ID: "worker-1", Role: "worker"}},
		{spec: KubernetesCertTarget{ID: "cp-1", Role: "control-plane"}},
		{spec: KubernetesCertTarget{ID: "worker-2", Role: "worker"}},
	}, KubernetesCertSpec{Order: "control-plane-first", BatchSize: 2})
	if len(batches) != 2 || batches[0].Targets[0].spec.ID != "cp-1" || batches[0].Targets[1].spec.ID != "worker-1" || batches[1].Targets[0].spec.ID != "worker-2" {
		t.Fatalf("unexpected batches: %#v", batches)
	}
}

func TestRun_DBCutoverResumesWithoutSecondCommit(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "cutover.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE switches(value TEXT PRIMARY KEY);`); err != nil {
		t.Fatalf("create switches: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO switches(value) VALUES ('live');`); err != nil {
		t.Fatalf("seed switches: %v", err)
	}
	node := &ResolvedRelease{
		ID:        "local/default/cutover",
		Kind:      NodeKindDBCutover,
		Name:      "cutover",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "local"},
		Database: DatabaseSpec{
			Driver:              "sqlite",
			DSN:                 dbPath,
			MetadataTable:       "torque_cutover_state",
			CommitSQL:           `INSERT INTO switches(value) VALUES ('live');`,
			VerifySQL:           `SELECT COUNT(*) > 0 FROM switches WHERE value = 'live';`,
			FinalizeSQL:         `CREATE TABLE IF NOT EXISTS finalizations(value TEXT PRIMARY KEY); INSERT INTO finalizations(value) VALUES ('done');`,
			StabilizationWindow: durationPtrCustom(0),
		},
	}
	hash, _, err := ComputeEffectiveInputHash(root, node, true)
	if err != nil {
		t.Fatalf("ComputeEffectiveInputHashWithOptions: %v", err)
	}
	node.EffectiveInputHash = hash

	dialect, err := dialectFor("sqlite")
	if err != nil {
		t.Fatalf("dialectFor: %v", err)
	}
	if err := ensureCutoverTable(context.Background(), db, dialect, "torque_cutover_state"); err != nil {
		t.Fatalf("ensureCutoverTable: %v", err)
	}
	state := &cutoverState{
		ObjectID:     node.ID,
		CutoverEpoch: "epoch-1",
		Phase:        "commit",
		PhaseStatus:  "success",
		FenceToken:   "fence-1",
		CommitMarker: "commit-1",
		UpdatedAtNS:  time.Now().UTC().UnixNano(),
	}
	if err := upsertCutoverState(context.Background(), db, dialect, "torque_cutover_state", state); err != nil {
		t.Fatalf("upsertCutoverState: %v", err)
	}

	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, errOut.String())
	}

	var switchCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM switches WHERE value = 'live'`).Scan(&switchCount); err != nil {
		t.Fatalf("count switches: %v", err)
	}
	if switchCount != 1 {
		t.Fatalf("expected commit to stay single-shot, count=%d", switchCount)
	}
	var finalizeCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM finalizations WHERE value = 'done'`).Scan(&finalizeCount); err != nil {
		t.Fatalf("count finalizations: %v", err)
	}
	if finalizeCount != 1 {
		t.Fatalf("expected finalize to run once, count=%d", finalizeCount)
	}
}

func TestRun_DBCutover_MultiStatementSQLite(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "cutover.sqlite")
	node := &ResolvedRelease{
		ID:        "local/default/cutover",
		Kind:      NodeKindDBCutover,
		Name:      "cutover",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "local"},
		Database: DatabaseSpec{
			Driver:              "sqlite",
			DSN:                 dbPath,
			MetadataTable:       "torque_cutover_state",
			PrepareSQL:          `CREATE TABLE IF NOT EXISTS cutover_flags(name TEXT PRIMARY KEY, live INTEGER NOT NULL DEFAULT 0, verified INTEGER NOT NULL DEFAULT 0); INSERT INTO cutover_flags(name, live, verified) VALUES ('api;v1', 0, 0) ON CONFLICT(name) DO NOTHING;`,
			ArmSQL:              `UPDATE cutover_flags SET live = 0 WHERE name = 'api;v1';`,
			CommitSQL:           `UPDATE cutover_flags SET live = 1 WHERE name = 'api;v1';`,
			VerifySQL:           `SELECT live FROM cutover_flags WHERE name = 'api;v1';`,
			FinalizeSQL:         `UPDATE cutover_flags SET verified = 1 WHERE name = 'api;v1'; CREATE TABLE IF NOT EXISTS audit_log(entry TEXT PRIMARY KEY); INSERT INTO audit_log(entry) VALUES ('cutover complete') ON CONFLICT(entry) DO NOTHING;`,
			StabilizationWindow: durationPtrCustom(0),
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, errOut.String())
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	var state string
	if err := db.QueryRow(`SELECT CAST(live AS TEXT) || ',' || CAST(verified AS TEXT) FROM cutover_flags WHERE name = 'api;v1'`).Scan(&state); err != nil {
		t.Fatalf("query cutover_flags: %v", err)
	}
	if state != "1,1" {
		t.Fatalf("unexpected cutover state %q", state)
	}
	var auditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE entry = 'cutover complete'`).Scan(&auditCount); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("unexpected audit_count=%d", auditCount)
	}
}

func TestRun_DBBackfillResumesFromCheckpointSQLite(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "backfill.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE source_users(id INTEGER PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE shadow_users(id INTEGER PRIMARY KEY, name TEXT NOT NULL);
INSERT INTO source_users(id, name) VALUES (1, 'a'), (2, 'b'), (3, 'c'), (4, 'd'), (5, 'e');
INSERT INTO shadow_users(id, name) VALUES (1, 'a'), (2, 'b');
`); err != nil {
		t.Fatalf("seed sqlite: %v", err)
	}
	dialect, err := dialectFor("sqlite")
	if err != nil {
		t.Fatalf("dialectFor: %v", err)
	}
	if err := ensureBackfillTable(context.Background(), db, dialect, "torque_backfill_state"); err != nil {
		t.Fatalf("ensureBackfillTable: %v", err)
	}

	node := &ResolvedRelease{
		ID:        "local/default/backfill",
		Kind:      NodeKindDBBackfill,
		Name:      "backfill",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "local"},
		Database: DatabaseSpec{
			Driver:    "sqlite",
			DSN:       dbPath,
			VerifySQL: `SELECT (SELECT COUNT(*) FROM shadow_users) = (SELECT COUNT(*) FROM source_users), (SELECT COUNT(*) FROM shadow_users)`,
			Backfill: BackfillSpec{
				CheckpointTable: "torque_backfill_state",
				CheckpointKey:   "local/default/backfill",
				StartSQL:        `SELECT COALESCE(MIN(id), 1) - 1 FROM source_users`,
				EndSQL:          `SELECT COALESCE(MAX(id), 0) FROM source_users`,
				BatchSQL:        `INSERT INTO shadow_users(id, name) SELECT id, name FROM source_users WHERE id > {{.cursor_start}} AND id <= {{.cursor_end}} ON CONFLICT(id) DO NOTHING`,
				BatchSize:       2,
			},
		},
	}
	hash, _, err := ComputeEffectiveInputHashWithOptions(node, EffectiveInputHashOptions{
		StackRoot:        root,
		StackGitIdentity: &GitIdentity{Commit: "abc123", Dirty: false},
	})
	if err != nil {
		t.Fatalf("ComputeEffectiveInputHashWithOptions: %v", err)
	}
	node.EffectiveInputHash = hash
	if err := upsertBackfillState(context.Background(), db, dialect, "torque_backfill_state", &backfillState{
		ObjectID:         "local/default/backfill",
		IntentDigest:     "",
		CheckpointKey:    "local/default/backfill",
		StartCursor:      1,
		CurrentCursor:    2,
		EndCursor:        5,
		BatchSize:        2,
		BatchesCompleted: 1,
		PhaseStatus:      "running",
		UpdatedAtNS:      time.Now().UTC().UnixNano(),
	}); err != nil {
		t.Fatalf("upsertBackfillState: %v", err)
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, errOut.String())
	}
	var shadowCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM shadow_users`).Scan(&shadowCount); err != nil {
		t.Fatalf("count shadow_users: %v", err)
	}
	if shadowCount != 5 {
		t.Fatalf("unexpected shadow count %d", shadowCount)
	}
	var status string
	var currentCursor int64
	if err := db.QueryRow(`SELECT phase_status, current_cursor FROM torque_backfill_state WHERE object_id = 'local/default/backfill'`).Scan(&status, &currentCursor); err != nil {
		t.Fatalf("query backfill state: %v", err)
	}
	if status != "success" || currentCursor != 5 {
		t.Fatalf("unexpected backfill state status=%q current=%d", status, currentCursor)
	}
}

func TestRun_DBProgramArtifactsAppearInAuditAndExport(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "program.sqlite")
	nodes := dbProgramNodesForSQLite(root, dbPath)
	plan := planForTest(root, nodes...)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, errOut.String())
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	if len(audit.Artifacts) < 12 {
		t.Fatalf("expected artifacts for full db program, got %d", len(audit.Artifacts))
	}
	wantArtifacts := []string{
		"restore-point.json",
		"schema-expand.json",
		"backfill.json",
		"verify.json",
		"cutover.json",
		"schema-contract.json",
		"decision.json",
	}
	joined := make([]string, 0, len(audit.Artifacts))
	for _, artifact := range audit.Artifacts {
		joined = append(joined, artifact.NodeID+":"+artifact.Name)
	}
	for _, want := range wantArtifacts {
		found := false
		for _, got := range joined {
			if strings.HasSuffix(got, ":"+want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing artifact %s in %v", want, joined)
		}
	}

	bundlePath := filepath.Join(root, "export.tgz")
	if _, err := ExportRunBundle(context.Background(), root, runID, bundlePath); err != nil {
		t.Fatalf("ExportRunBundle: %v", err)
	}
	extracted, err := ExtractBundleToTempDir(bundlePath)
	if err != nil {
		t.Fatalf("ExtractBundleToTempDir: %v", err)
	}
	defer os.RemoveAll(extracted)

	exportDB, err := sql.Open("sqlite", filepath.Join(extracted, "state.sqlite"))
	if err != nil {
		t.Fatalf("open exported sqlite: %v", err)
	}
	defer exportDB.Close()
	var artifactCount int
	if err := exportDB.QueryRow(`SELECT COUNT(*) FROM torque_stack_run_artifacts WHERE run_id = ?`, runID).Scan(&artifactCount); err != nil {
		t.Fatalf("count exported artifacts: %v", err)
	}
	if artifactCount != len(audit.Artifacts) {
		t.Fatalf("exported artifact count=%d want=%d", artifactCount, len(audit.Artifacts))
	}
}

func TestSplitSQLStatements_QuotedSemicolons(t *testing.T) {
	stmts := splitSQLStatements(`
CREATE TABLE demo(v TEXT);
INSERT INTO demo(v) VALUES ('a;b');
-- preserve comments; ignore separator parsing inside text
UPDATE demo SET v = "c;d";
`)
	if len(stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d: %#v", len(stmts), stmts)
	}
	if stmts[1] != "INSERT INTO demo(v) VALUES ('a;b')" {
		t.Fatalf("unexpected statement %q", stmts[1])
	}
	if stmts[2] != "-- preserve comments; ignore separator parsing inside text\nUPDATE demo SET v = \"c;d\"" {
		t.Fatalf("unexpected statement %q", stmts[2])
	}
}

func planForTest(root string, nodes ...*ResolvedRelease) *Plan {
	p := &Plan{
		StackRoot: root,
		StackName: "test",
		Profile:   "",
		Nodes:     nodes,
		ByID:      map[string]*ResolvedRelease{},
		ByCluster: map[string][]*ResolvedRelease{},
	}
	for _, n := range nodes {
		p.ByID[n.ID] = n
		p.ByCluster[n.Cluster.Name] = append(p.ByCluster[n.Cluster.Name], n)
	}
	_ = assignExecutionGroups(p)
	return p
}

func shellQuoteForTest(path string) string {
	return "'" + filepath.ToSlash(path) + "'"
}

func shellQuoteStringForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func writeExecutableForTest(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func startStackTestNATSServer(t *testing.T) string {
	t.Helper()
	return startStackTestNATSServerWithArgs(t, nil)
}

func startStackTestNATSJetStreamServer(t *testing.T) string {
	t.Helper()
	return startStackTestNATSServerWithArgs(t, []string{"-js", "-sd", t.TempDir()})
}

func startStackTestNATSServerWithArgs(t *testing.T, extraArgs []string) string {
	t.Helper()
	binary, err := exec.LookPath("nats-server")
	if err != nil {
		t.Skip("nats-server binary not found")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve nats port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close reserved nats port: %v", err)
	}
	args := []string{"-a", "127.0.0.1", "-p", strconv.Itoa(port)}
	args = append(args, extraArgs...)
	cmd := exec.Command(binary, args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start nats-server: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})
	url := fmt.Sprintf("nats://127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := natsgo.Connect(url, natsgo.NoReconnect(), natsgo.Timeout(100*time.Millisecond))
		if err == nil {
			conn.Close()
			return url
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("nats-server did not become ready")
	}
	t.Fatalf("wait for nats-server: %v", lastErr)
	return ""
}

func waitForStackTestStreamMessages(t *testing.T, serverURL string, stream string, want uint64) {
	t.Helper()
	conn, err := natsgo.Connect(serverURL, natsgo.NoReconnect(), natsgo.Timeout(time.Second))
	if err != nil {
		t.Fatalf("connect NATS for stream wait: %v", err)
	}
	defer conn.Close()
	js, err := conn.JetStream(natsgo.MaxWait(time.Second))
	if err != nil {
		t.Fatalf("open JetStream for stream wait: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var lastState uint64
	for time.Now().Before(deadline) {
		info, err := js.StreamInfo(stream)
		if err == nil && info != nil {
			lastState = info.State.Msgs
			if lastState >= want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("stream %s messages = %d, want at least %d", stream, lastState, want)
}

func stackTestStreamMessageCount(t *testing.T, serverURL string, stream string) uint64 {
	t.Helper()
	conn, err := natsgo.Connect(serverURL, natsgo.NoReconnect(), natsgo.Timeout(time.Second))
	if err != nil {
		t.Fatalf("connect NATS for stream count: %v", err)
	}
	defer conn.Close()
	js, err := conn.JetStream(natsgo.MaxWait(time.Second))
	if err != nil {
		t.Fatalf("open JetStream for stream count: %v", err)
	}
	info, err := js.StreamInfo(stream)
	if err != nil {
		t.Fatalf("stream info %s: %v", stream, err)
	}
	if info == nil {
		return 0
	}
	return info.State.Msgs
}

func countFileSubstring(t *testing.T, path string, needle string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Count(string(raw), needle)
}

func providerMatrixJSONCommand(raw string) string {
	return "printf '%s\\n' " + shellQuoteStringForTest(raw)
}

func providerMatrixAPICommand(provider string) string {
	version := "v1.30.4"
	switch provider {
	case "k3s":
		version = "v1.30.4+k3s1"
	case "rke2":
		version = "v1.30.4+rke2r1"
	}
	return providerMatrixJSONCommand(`{"clientVersion":{"gitVersion":"v1.30.0"},"serverVersion":{"gitVersion":"` + version + `","major":"1","minor":"30","platform":"linux/amd64"}}`)
}

func providerMatrixNodesCommand(provider string) string {
	cpLabels := `"node-role.kubernetes.io/control-plane":""`
	kubeletVersion := "v1.30.4"
	switch provider {
	case "k3s":
		cpLabels += `,"k3s.io/hostname":"cp-1"`
		kubeletVersion = "v1.30.4+k3s1"
	case "rke2":
		cpLabels += `,"rke2.io/hostname":"cp-1"`
		kubeletVersion = "v1.30.4+rke2r1"
	}
	return providerMatrixJSONCommand(`{"items":[{"metadata":{"name":"cp-1","labels":{` + cpLabels + `}},"status":{"addresses":[{"type":"InternalIP","address":"10.0.0.10"}],"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"` + kubeletVersion + `","osImage":"Ubuntu","kernelVersion":"6.8.0","containerRuntimeVersion":"containerd://1.7.0"}}},{"metadata":{"name":"worker-1","labels":{}},"status":{"addresses":[{"type":"InternalIP","address":"10.0.0.11"}],"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"` + kubeletVersion + `","osImage":"Ubuntu","kernelVersion":"6.8.0","containerRuntimeVersion":"containerd://1.7.0"}}}]}`)
}

func providerMatrixPodsCommand(provider string) string {
	systemPods := `{"items":[{"metadata":{"name":"coredns","namespace":"kube-system"},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}]}`
	if provider == "kubeadm" {
		systemPods = `{"items":[{"metadata":{"name":"kube-apiserver-cp-1","namespace":"kube-system"},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}},{"metadata":{"name":"kube-controller-manager-cp-1","namespace":"kube-system"},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}},{"metadata":{"name":"kube-scheduler-cp-1","namespace":"kube-system"},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}]}`
	}
	appPods := `{"items":[{"metadata":{"name":"web","namespace":"app"},"status":{"phase":"Running","containerStatuses":[{"ready":true},{"ready":true}]}}]}`
	return `case "{{namespace}}" in kube-system) ` + providerMatrixJSONCommand(systemPods) + ` ;; app) ` + providerMatrixJSONCommand(appPods) + ` ;; *) ` + providerMatrixJSONCommand(`{"items":[]}`) + ` ;; esac`
}

func providerMatrixTargetsFrom(sourceNode string, provider string, renewLog string, custom bool) KubernetesCertTargetsFromSpec {
	spec := KubernetesCertTargetsFromSpec{
		SourceNode:       sourceNode,
		Roles:            []string{"control-plane", "worker"},
		Transport:        "local",
		TargetTemplate:   "local://{{ .Name }}",
		RestartCommand:   "printf 'restart-" + provider + "\\n' >> " + shellQuoteForTest(renewLog),
		NodeSSHOptions:   "",
		NodeIdentityFile: "",
	}
	if custom {
		spec.Provider = "custom"
		spec.InspectCommand = providerMatrixJSONCommand(`{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}`)
		spec.RenewCommand = "printf 'renew-custom\\n' >> " + shellQuoteForTest(renewLog)
	}
	return spec
}

func writeProviderMatrixFakeBinary(t *testing.T, dir string, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	path := filepath.Join(dir, name)
	script := `#!/bin/sh
set -eu
log="${TORQUE_PROVIDER_MATRIX_LOG:?}"
binary="$(basename "$0")"
case "${binary}:$*" in
  kubeadm:certs\ check-expiration*)
    printf '%s\n' '{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}'
    ;;
  kubeadm:certs\ renew*)
    printf '%s\n' "renew-kubeadm" >>"${log}"
    ;;
  k3s:certificate\ check*)
    printf '%s\n' '{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}'
    ;;
  k3s:certificate\ rotate*)
    printf '%s\n' "renew-k3s" >>"${log}"
    ;;
  rke2:certificate\ check*)
    printf '%s\n' '{"certificates":[{"notAfter":"2035-01-01T00:00:00Z"}]}'
    ;;
  rke2:certificate\ rotate*)
    printf '%s\n' "renew-rke2" >>"${log}"
    ;;
  *)
    printf 'unexpected provider matrix command: %s %s\n' "${binary}" "$*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake provider binary: %v", err)
	}
}

func readTrimmedFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(bytes.TrimSpace(raw))
}

func durationPtrCustom(v time.Duration) *time.Duration {
	return &v
}

func dbProgramNodesForSQLite(root string, dbPath string) []*ResolvedRelease {
	common := DatabaseSpec{
		Driver: "sqlite",
		DSN:    dbPath,
	}
	return []*ResolvedRelease{
		{
			ID:        "local/default/restore",
			Kind:      NodeKindDBRestorePoint,
			Name:      "restore",
			Dir:       root,
			Namespace: "default",
			Cluster:   ClusterTarget{Name: "local"},
			Database: DatabaseSpec{
				Driver:          common.Driver,
				DSN:             common.DSN,
				RestorePointSQL: `CREATE TABLE IF NOT EXISTS restore_points(marker TEXT PRIMARY KEY, created_at TEXT NOT NULL); INSERT INTO restore_points(marker, created_at) VALUES ('before-program', 'now') ON CONFLICT(marker) DO NOTHING`,
				VerifySQL:       `SELECT COUNT(*) > 0, (SELECT COUNT(*) FROM restore_points) FROM restore_points WHERE marker = 'before-program'`,
			},
		},
		{
			ID:        "local/default/expand",
			Kind:      NodeKindDBSchemaExpand,
			Name:      "expand",
			Dir:       root,
			Namespace: "default",
			Cluster:   ClusterTarget{Name: "local"},
			Needs:     []string{"restore"},
			Database: DatabaseSpec{
				Driver:    common.Driver,
				DSN:       common.DSN,
				ExpandSQL: `CREATE TABLE IF NOT EXISTS source_users(id INTEGER PRIMARY KEY, name TEXT NOT NULL); CREATE TABLE IF NOT EXISTS shadow_users(id INTEGER PRIMARY KEY, name TEXT NOT NULL); CREATE TABLE IF NOT EXISTS cutover_flags(name TEXT PRIMARY KEY, live INTEGER NOT NULL DEFAULT 0, verified INTEGER NOT NULL DEFAULT 0, contracted INTEGER NOT NULL DEFAULT 0); INSERT INTO source_users(id, name) VALUES (1, 'a'), (2, 'b'), (3, 'c'), (4, 'd'), (5, 'e') ON CONFLICT(id) DO NOTHING; INSERT INTO cutover_flags(name, live, verified, contracted) VALUES ('api', 0, 0, 0) ON CONFLICT(name) DO NOTHING`,
				VerifySQL: `SELECT EXISTS(SELECT 1 FROM source_users WHERE id = 5), (SELECT COUNT(*) FROM source_users)`,
			},
		},
		{
			ID:        "local/default/backfill",
			Kind:      NodeKindDBBackfill,
			Name:      "backfill",
			Dir:       root,
			Namespace: "default",
			Cluster:   ClusterTarget{Name: "local"},
			Needs:     []string{"expand"},
			Database: DatabaseSpec{
				Driver:    common.Driver,
				DSN:       common.DSN,
				VerifySQL: `SELECT (SELECT COUNT(*) FROM shadow_users) = (SELECT COUNT(*) FROM source_users), (SELECT COUNT(*) FROM shadow_users)`,
				Backfill: BackfillSpec{
					CheckpointTable: "torque_backfill_state",
					CheckpointKey:   "program",
					StartSQL:        `SELECT COALESCE(MIN(id), 1) - 1 FROM source_users`,
					EndSQL:          `SELECT COALESCE(MAX(id), 0) FROM source_users`,
					BatchSQL:        `INSERT INTO shadow_users(id, name) SELECT id, name FROM source_users WHERE id > {{.cursor_start}} AND id <= {{.cursor_end}} ON CONFLICT(id) DO NOTHING`,
					BatchSize:       2,
				},
			},
		},
		{
			ID:        "local/default/verify",
			Kind:      NodeKindDBVerify,
			Name:      "verify",
			Dir:       root,
			Namespace: "default",
			Cluster:   ClusterTarget{Name: "local"},
			Needs:     []string{"backfill"},
			Database: DatabaseSpec{
				Driver:    common.Driver,
				DSN:       common.DSN,
				VerifySQL: `SELECT (SELECT COUNT(*) FROM shadow_users) = (SELECT COUNT(*) FROM source_users), (SELECT COUNT(*) FROM shadow_users)`,
			},
		},
		{
			ID:        "local/default/cutover",
			Kind:      NodeKindDBCutover,
			Name:      "cutover",
			Dir:       root,
			Namespace: "default",
			Cluster:   ClusterTarget{Name: "local"},
			Needs:     []string{"verify"},
			Database: DatabaseSpec{
				Driver:              common.Driver,
				DSN:                 common.DSN,
				MetadataTable:       "torque_cutover_state",
				PrepareSQL:          `UPDATE cutover_flags SET live = 0, verified = 0 WHERE name = 'api'`,
				CommitSQL:           `UPDATE cutover_flags SET live = 1 WHERE name = 'api'`,
				VerifySQL:           `SELECT live, verified FROM cutover_flags WHERE name = 'api'`,
				FinalizeSQL:         `UPDATE cutover_flags SET verified = 1 WHERE name = 'api'`,
				StabilizationWindow: durationPtrCustom(0),
			},
		},
		{
			ID:        "local/default/contract",
			Kind:      NodeKindDBSchemaContract,
			Name:      "contract",
			Dir:       root,
			Namespace: "default",
			Cluster:   ClusterTarget{Name: "local"},
			Needs:     []string{"cutover"},
			Database: DatabaseSpec{
				Driver:      common.Driver,
				DSN:         common.DSN,
				ContractSQL: `UPDATE cutover_flags SET contracted = 1 WHERE name = 'api'; CREATE TABLE IF NOT EXISTS contract_log(entry TEXT PRIMARY KEY); INSERT INTO contract_log(entry) VALUES ('contract-complete') ON CONFLICT(entry) DO NOTHING`,
				VerifySQL:   `SELECT contracted, live, verified FROM cutover_flags WHERE name = 'api'`,
			},
		},
	}
}

func eligibleHostCommandOpsForTest(t *testing.T, root string, targetID string) *OpsPlanInputs {
	t.Helper()
	targetGraphPath := writeOpsPreflightTargetGraph(t, root, "role: web\n")
	factsPath := writeOpsPreflightFactsFile(t, root, targetID, "collected")
	policyPath := writeOpsPreflightPolicyFile(t, root, targetID, "allow")
	lockDir := filepath.Join(root, "ops-locks-"+strings.NewReplacer("/", "-", ":", "-").Replace(targetID))
	scope := "target/" + strings.TrimPrefix(targetID, "target/")
	if _, err := (locks.FileStore{Dir: lockDir}).Acquire(context.Background(), locks.AcquireRequest{
		Scope:     scope,
		TargetID:  targetID,
		Holder:    "test-operator",
		Operation: NodeKindHostCommandRun,
		TTL:       time.Minute,
	}); err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	return &OpsPlanInputs{
		APIVersion: "torque.dev/ops/plan-inputs/v1alpha1",
		Kind:       "OpsPlanInputs",
		TargetGraph: &OpsTargetGraphInput{
			Path:         targetGraphPath,
			Name:         "ops-host",
			SourceDigest: opsApplyPreflightFileDigest(t, targetGraphPath),
			Selection: OpsTargetSelectionInput{
				MatchedTargetIDs: []string{targetID},
			},
			Summary: OpsTargetGraphSummary{TargetCount: 1},
		},
		FactEvidence: []OpsFactEvidenceInput{
			{
				Source: factsPath,
				Kind:   "FactCollection",
				Digest: opsApplyPreflightFileDigest(t, factsPath),
				Targets: []OpsFactTargetInput{
					{TargetID: targetID, Status: "collected", Digest: "sha256:target-facts"},
				},
				Summary: OpsFactEvidenceSummary{
					Selected:  1,
					Targets:   1,
					Snapshots: 1,
					Collected: 1,
				},
			},
		},
		Locks: []OpsLockInput{
			{
				Source:   lockDir,
				Scope:    scope,
				Found:    true,
				TargetID: targetID,
				Status:   "held",
				Holder:   "test-operator",
			},
		},
		PolicyDecisions: []OpsPolicyDecisionInput{
			{
				Source:    policyPath,
				Digest:    opsApplyPreflightFileDigest(t, policyPath),
				Decision:  "allow",
				Reason:    "guarded policy satisfied",
				Operation: NodeKindHostCommandRun,
				TargetID:  targetID,
				Mutating:  true,
			},
		},
		Summary: OpsPlanInputSummary{
			TargetCount:     1,
			SelectedTargets: 1,
			FactEvidence:    1,
			FactSnapshots:   1,
			Locks:           1,
			PolicyDecisions: 1,
		},
	}
}

func auditHasArtifact(artifacts []RunArtifact, nodeID string, name string) bool {
	for _, artifact := range artifacts {
		if artifact.NodeID == nodeID && artifact.Name == name {
			return true
		}
	}
	return false
}

func auditArtifactBody(t *testing.T, artifacts []RunArtifact, nodeID string, name string) string {
	t.Helper()
	for _, artifact := range artifacts {
		if artifact.NodeID == nodeID && artifact.Name == name {
			return artifact.Body
		}
	}
	t.Fatalf("missing artifact %s for %s in %+v", name, nodeID, artifacts)
	return ""
}

func assertOpsAuditPassed(t *testing.T, audit *RunAudit, hostCommands int) {
	t.Helper()
	if audit.Ops == nil {
		t.Fatalf("missing ops audit")
	}
	if audit.Ops.Verification.Status != "passed" {
		t.Fatalf("ops verification = %s findings=%#v", audit.Ops.Verification.Status, audit.Ops.Findings)
	}
	if audit.Ops.Preflight == nil || audit.Ops.Preflight.Status != "eligible" || !audit.Ops.Preflight.EventSeen {
		t.Fatalf("ops preflight = %#v, want eligible with event", audit.Ops.Preflight)
	}
	if got := len(audit.Ops.HostCommands); got != hostCommands {
		t.Fatalf("host command audit count = %d, want %d: %#v", got, hostCommands, audit.Ops.HostCommands)
	}
	for _, item := range audit.Ops.HostCommands {
		if !item.ObservePresent || !item.PlanPresent || !item.ExecutePresent || !item.VerifyPresent {
			t.Fatalf("host command receipts incomplete: %#v", item)
		}
		if item.GuardMode != "ops" {
			t.Fatalf("host command guard mode = %q, want ops", item.GuardMode)
		}
		if !item.RedactionVerified {
			t.Fatalf("host command redaction not verified: %#v", item)
		}
	}
}

func opsAuditHasFinding(audit *RunAudit, code string) bool {
	if audit == nil || audit.Ops == nil {
		return false
	}
	for _, finding := range audit.Ops.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
