package hostfacts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	localtransport "github.com/ingresslabs/torque/internal/ops/transport/local"
	sshtransport "github.com/ingresslabs/torque/internal/ops/transport/ssh"
)

func TestCollectBuildsSnapshotAndDigest(t *testing.T) {
	observedAt := time.Date(2026, 5, 23, 14, 30, 0, 0, time.UTC)
	target := &fakeTransport{
		digest: "sha256:target",
		outputs: map[string]string{
			"fact.osRelease": osReleaseFixture(),
			"fact.kernel":    "Linux\n6.8.0-test\nx86_64\n",
			"fact.packages":  "manager=dpkg\npackage=base-files\npackage=curl\ncount=2\n",
			"fact.services":  "manager=systemd\nservice=ssh.service,running\ncount=1\nrunning=1\n",
			"fact.users":     "user=root,/bin/bash\nuser=nobody,/usr/sbin/nologin\ncount=2\nloginShells=1\n",
			"fact.disks":     "Filesystem Type 1024-blocks Used Available Capacity Mounted on\n/dev/vda1 ext4 1000 200 800 20% /\n",
			"fact.network":   "hostname=lab\naddr=eth0,inet,192.0.2.10/24\naddr=lo,inet,127.0.0.1/8\n",
		},
	}
	snapshot, err := Collect(context.Background(), target, CollectRequest{
		TargetID:   "host/lab-ssh",
		ObservedAt: observedAt,
		TTL:        30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if snapshot.APIVersion != APIVersion || snapshot.Kind != Kind {
		t.Fatalf("snapshot type = %s/%s", snapshot.APIVersion, snapshot.Kind)
	}
	if snapshot.TargetID != "host/lab-ssh" || snapshot.TargetDigest != "sha256:target" {
		t.Fatalf("target fields = %q/%q", snapshot.TargetID, snapshot.TargetDigest)
	}
	if snapshot.ObservedAt != "2026-05-23T14:30:00Z" || snapshot.ExpiresAt != "2026-05-23T15:00:00Z" {
		t.Fatalf("time fields = %q/%q", snapshot.ObservedAt, snapshot.ExpiresAt)
	}
	if snapshot.OS.ID != "ubuntu" || snapshot.OS.PrettyName != "Ubuntu 24.04 LTS" {
		t.Fatalf("OS = %#v", snapshot.OS)
	}
	if snapshot.Kernel.Release != "6.8.0-test" || snapshot.Kernel.Machine != "x86_64" {
		t.Fatalf("Kernel = %#v", snapshot.Kernel)
	}
	if snapshot.Packages.Manager != "dpkg" || snapshot.Packages.Count != 2 {
		t.Fatalf("Packages = %#v", snapshot.Packages)
	}
	if snapshot.Services.RunningCount != 1 {
		t.Fatalf("Services = %#v", snapshot.Services)
	}
	if snapshot.Users.Count != 2 || snapshot.Users.LoginShellCount != 1 {
		t.Fatalf("Users = %#v", snapshot.Users)
	}
	if snapshot.Disks.Count != 1 || snapshot.Disks.Filesystems[0].Mountpoint != "/" {
		t.Fatalf("Disks = %#v", snapshot.Disks)
	}
	if snapshot.Network.AddressCount != 2 || snapshot.Network.InterfaceCount != 2 {
		t.Fatalf("Network = %#v", snapshot.Network)
	}
	if snapshot.Digest == "" || snapshot.Digest != snapshot.StableDigest() {
		t.Fatalf("Digest = %q, StableDigest = %q", snapshot.Digest, snapshot.StableDigest())
	}
	if len(snapshot.CommandReceipts) != 7 {
		t.Fatalf("CommandReceipts length = %d, want 7", len(snapshot.CommandReceipts))
	}
}

func TestCollectRejectsFailedFactCommand(t *testing.T) {
	target := &fakeTransport{
		digest: "sha256:target",
		outputs: map[string]string{
			"fact.osRelease": osReleaseFixture(),
		},
		failures: map[string]bool{"fact.kernel": true},
	}
	_, err := Collect(context.Background(), target, CollectRequest{TargetID: "host/lab", TTL: time.Minute})
	if err == nil {
		t.Fatal("Collect() error = nil, want failed command")
	}
	if !strings.Contains(err.Error(), "fact.kernel failed") {
		t.Fatalf("Collect() error = %v, want fact.kernel detail", err)
	}
}

func TestSnapshotMarshalDoesNotLeakRedactedReceipts(t *testing.T) {
	target := &fakeTransport{
		digest: "sha256:target",
		outputs: map[string]string{
			"fact.osRelease": osReleaseFixture(),
			"fact.kernel":    "Linux\n6.8.0-test\nx86_64\n",
			"fact.packages":  "manager=dpkg\npackage=base-files\ncount=1\n",
			"fact.services":  "manager=systemd\ncount=0\nrunning=0\n",
			"fact.users":     "count=0\nloginShells=0\n",
			"fact.disks":     "Filesystem Type 1024-blocks Used Available Capacity Mounted on\n",
			"fact.network":   "hostname=lab\n",
		},
		redactValues: []string{"top-secret"},
	}
	snapshot, err := Collect(context.Background(), target, CollectRequest{TargetID: "host/lab", TTL: time.Minute})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if strings.Contains(string(raw), "top-secret") || strings.Contains(string(raw), "secret://") {
		t.Fatalf("snapshot leaked secret material: %s", raw)
	}
}

func TestFileCacheFreshnessAndStaleBlock(t *testing.T) {
	observedAt := time.Date(2026, 5, 23, 14, 30, 0, 0, time.UTC)
	target := fullFakeTransport()
	snapshot, err := Collect(context.Background(), target, CollectRequest{
		TargetID:   "host/lab-ssh",
		ObservedAt: observedAt,
		TTL:        10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	cache := FileCache{Dir: t.TempDir()}
	entry, err := cache.Store(snapshot, observedAt)
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	if entry.SnapshotDigest != snapshot.Digest {
		t.Fatalf("entry snapshot digest = %q, want %q", entry.SnapshotDigest, snapshot.Digest)
	}
	loaded, found, err := cache.Load("host/lab-ssh")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !found {
		t.Fatal("Load() found = false, want true")
	}
	if loaded.Snapshot.Digest != snapshot.Digest {
		t.Fatalf("loaded digest = %q, want %q", loaded.Snapshot.Digest, snapshot.Digest)
	}
	fresh := EvaluateFreshness(&loaded.Snapshot, "sha256:target", observedAt.Add(5*time.Second))
	if fresh.Status != "fresh" || fresh.Decision != "allow" || fresh.Blocked {
		t.Fatalf("fresh decision = %#v", fresh)
	}
	stale := EvaluateFreshness(&loaded.Snapshot, "sha256:target", observedAt.Add(11*time.Second))
	if stale.Status != "stale" || stale.Decision != "block" || !stale.Blocked || stale.StaleByMillis <= 0 {
		t.Fatalf("stale decision = %#v", stale)
	}
}

func TestResolveUsesCacheBlocksStaleAndRefreshes(t *testing.T) {
	observedAt := time.Date(2026, 5, 23, 14, 30, 0, 0, time.UTC)
	target := fullFakeTransport()
	cacheDir := t.TempDir()
	request := CollectRequest{
		TargetID:   "host/lab-ssh",
		ObservedAt: observedAt,
		TTL:        10 * time.Second,
	}

	initial, err := Resolve(context.Background(), target, request, ResolveOptions{
		CacheDir: cacheDir,
		Refresh:  true,
		Now:      observedAt,
	})
	if err != nil {
		t.Fatalf("initial Resolve() error = %v", err)
	}
	if initial.Source != "refresh" || !initial.Refreshed || initial.PreviousDecision == nil || initial.PreviousDecision.Status != "missing" {
		t.Fatalf("initial resolution = %#v", initial)
	}
	runsAfterInitial := target.runs

	cached, err := Resolve(context.Background(), target, CollectRequest{TargetID: "host/lab-ssh"}, ResolveOptions{
		CacheDir: cacheDir,
		Now:      observedAt.Add(5 * time.Second),
	})
	if err != nil {
		t.Fatalf("cached Resolve() error = %v", err)
	}
	if cached.Source != "cache" || cached.Refreshed || cached.Decision.Status != "fresh" {
		t.Fatalf("cached resolution = %#v", cached)
	}
	if target.runs != runsAfterInitial {
		t.Fatalf("cache hit ran transport commands: got %d, want %d", target.runs, runsAfterInitial)
	}

	stale, err := Resolve(context.Background(), target, CollectRequest{TargetID: "host/lab-ssh"}, ResolveOptions{
		CacheDir: cacheDir,
		Now:      observedAt.Add(11 * time.Second),
	})
	if err != nil {
		t.Fatalf("stale Resolve() error = %v", err)
	}
	if stale.Decision.Status != "stale" || stale.Decision.Decision != "block" || !stale.Decision.Blocked {
		t.Fatalf("stale resolution = %#v", stale)
	}
	if target.runs != runsAfterInitial {
		t.Fatalf("stale block ran transport commands: got %d, want %d", target.runs, runsAfterInitial)
	}

	refreshAt := observedAt.Add(12 * time.Second)
	refreshed, err := Resolve(context.Background(), target, CollectRequest{
		TargetID: "host/lab-ssh",
		TTL:      10 * time.Second,
	}, ResolveOptions{
		CacheDir: cacheDir,
		Refresh:  true,
		Now:      refreshAt,
	})
	if err != nil {
		t.Fatalf("refresh Resolve() error = %v", err)
	}
	if refreshed.Source != "refresh" || !refreshed.Refreshed || refreshed.PreviousDecision.Status != "stale" || refreshed.Decision.Status != "fresh" {
		t.Fatalf("refresh resolution = %#v", refreshed)
	}
	if refreshed.Snapshot.ObservedAt != refreshAt.Format(time.RFC3339) {
		t.Fatalf("refreshed observedAt = %q, want %q", refreshed.Snapshot.ObservedAt, refreshAt.Format(time.RFC3339))
	}
	if target.runs <= runsAfterInitial {
		t.Fatalf("refresh did not run transport commands")
	}
}

func TestEvaluateFreshnessBlocksTargetDigestMismatch(t *testing.T) {
	observedAt := time.Date(2026, 5, 23, 14, 30, 0, 0, time.UTC)
	target := fullFakeTransport()
	snapshot, err := Collect(context.Background(), target, CollectRequest{
		TargetID:   "host/lab-ssh",
		ObservedAt: observedAt,
		TTL:        time.Minute,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	decision := EvaluateFreshness(snapshot, "sha256:other-target", observedAt.Add(time.Second))
	if decision.Status != "target-digest-mismatch" || decision.Decision != "block" || !decision.Blocked {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestParsersHandleMissingManagers(t *testing.T) {
	if got := parsePackages("manager=unknown\ncount=0\n"); got.Manager != "unknown" || got.Count != 0 {
		t.Fatalf("parsePackages = %#v", got)
	}
	if got := parseServices("manager=unknown\ncount=0\nrunning=0\n"); got.Manager != "unknown" || got.RunningCount != 0 {
		t.Fatalf("parseServices = %#v", got)
	}
	if got := parseNetwork("hostname=lab\n"); got.Hostname != "lab" || got.AddressCount != 0 {
		t.Fatalf("parseNetwork = %#v", got)
	}
}

func TestE2EEnvCollectHostFacts(t *testing.T) {
	output := os.Getenv("TORQUE_OPS_FACT_HOST_E2E_OUTPUT")
	target := os.Getenv("TORQUE_OPS_FACT_HOST_E2E_TARGET")
	if output == "" && target == "" {
		t.Skip("set TORQUE_OPS_FACT_HOST_E2E_TARGET and TORQUE_OPS_FACT_HOST_E2E_OUTPUT to run the host fact E2E proof")
	}
	if output == "" || target == "" {
		t.Fatal("TORQUE_OPS_FACT_HOST_E2E_TARGET and TORQUE_OPS_FACT_HOST_E2E_OUTPUT must be set together")
	}

	canary := os.Getenv("TORQUE_OPS_FACT_HOST_E2E_CANARY")
	if canary == "" {
		canary = "torque-redaction-canary-e2e"
	}
	ttl := 15 * time.Minute
	if rawTTL := os.Getenv("TORQUE_OPS_FACT_HOST_E2E_TTL"); rawTTL != "" {
		parsed, err := time.ParseDuration(rawTTL)
		if err != nil {
			t.Fatalf("parse TTL: %v", err)
		}
		ttl = parsed
	}

	factTarget, err := e2eTransport(target, canary)
	if err != nil {
		t.Fatalf("build transport: %v", err)
	}
	snapshot, err := Collect(context.Background(), factTarget, CollectRequest{
		TargetID: "host/lab-ssh",
		TTL:      ttl,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	errors := validateSnapshot(snapshot)
	status := "succeeded"
	if len(errors) > 0 {
		status = "failed"
	}
	doc := map[string]any{
		"apiVersion": "torque.dev/e2e/v1",
		"kind":       "OpsHostFactCollectionProof",
		"status":     status,
		"snapshot":   snapshot,
		"errors":     errors,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal proof: %v", err)
	}
	if strings.Contains(string(raw), canary) || strings.Contains(string(raw), "secret://") {
		t.Fatal("host fact proof leaked secret material")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(output, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	if len(errors) > 0 {
		t.Fatalf("host fact E2E failed: %s", strings.Join(errors, "; "))
	}
}

func TestE2EEnvHostFactCacheStaleness(t *testing.T) {
	output := os.Getenv("TORQUE_OPS_FACT_CACHE_E2E_OUTPUT")
	target := os.Getenv("TORQUE_OPS_FACT_CACHE_E2E_TARGET")
	cacheDir := os.Getenv("TORQUE_OPS_FACT_CACHE_E2E_DIR")
	if output == "" && target == "" && cacheDir == "" {
		t.Skip("set TORQUE_OPS_FACT_CACHE_E2E_TARGET, TORQUE_OPS_FACT_CACHE_E2E_OUTPUT, and TORQUE_OPS_FACT_CACHE_E2E_DIR to run the cache E2E proof")
	}
	if output == "" || target == "" || cacheDir == "" {
		t.Fatal("TORQUE_OPS_FACT_CACHE_E2E_TARGET, TORQUE_OPS_FACT_CACHE_E2E_OUTPUT, and TORQUE_OPS_FACT_CACHE_E2E_DIR must be set together")
	}

	canary := os.Getenv("TORQUE_OPS_FACT_CACHE_E2E_CANARY")
	if canary == "" {
		canary = "torque-redaction-canary-cache-e2e"
	}
	ttl := 2 * time.Second
	if rawTTL := os.Getenv("TORQUE_OPS_FACT_CACHE_E2E_TTL"); rawTTL != "" {
		parsed, err := time.ParseDuration(rawTTL)
		if err != nil {
			t.Fatalf("parse TTL: %v", err)
		}
		ttl = parsed
	}
	base := time.Now().UTC().Truncate(time.Second)
	factTarget, err := e2eTransport(target, canary)
	if err != nil {
		t.Fatalf("build transport: %v", err)
	}
	request := CollectRequest{
		TargetID:   "host/lab-ssh",
		ObservedAt: base,
		TTL:        ttl,
	}
	initial, err := Resolve(context.Background(), factTarget, request, ResolveOptions{
		CacheDir: cacheDir,
		Refresh:  true,
		Now:      base,
	})
	if err != nil {
		t.Fatalf("initial Resolve() error = %v", err)
	}
	fresh, err := Resolve(context.Background(), factTarget, CollectRequest{TargetID: "host/lab-ssh"}, ResolveOptions{
		CacheDir: cacheDir,
		Now:      base.Add(ttl / 2),
	})
	if err != nil {
		t.Fatalf("fresh Resolve() error = %v", err)
	}
	staleAt := base.Add(ttl + time.Second)
	stale, err := Resolve(context.Background(), factTarget, CollectRequest{TargetID: "host/lab-ssh"}, ResolveOptions{
		CacheDir: cacheDir,
		Now:      staleAt,
	})
	if err != nil {
		t.Fatalf("stale Resolve() error = %v", err)
	}
	refreshed, err := Resolve(context.Background(), factTarget, CollectRequest{
		TargetID: "host/lab-ssh",
		TTL:      ttl,
	}, ResolveOptions{
		CacheDir: cacheDir,
		Refresh:  true,
		Now:      staleAt.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("refresh Resolve() error = %v", err)
	}

	errors := validateCacheProof(initial, fresh, stale, refreshed)
	status := "succeeded"
	if len(errors) > 0 {
		status = "failed"
	}
	doc := map[string]any{
		"apiVersion": "torque.dev/e2e/v1",
		"kind":       "OpsHostFactCacheStalenessProof",
		"status":     status,
		"ttl":        ttl.String(),
		"initial":    summarizeResolution(initial),
		"fresh":      summarizeResolution(fresh),
		"stale":      summarizeResolution(stale),
		"refresh":    summarizeResolution(refreshed),
		"errors":     errors,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal proof: %v", err)
	}
	if strings.Contains(string(raw), canary) || strings.Contains(string(raw), "secret://") {
		t.Fatal("host fact cache proof leaked secret material")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(output, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	if len(errors) > 0 {
		t.Fatalf("host fact cache E2E failed: %s", strings.Join(errors, "; "))
	}
}

func e2eTransport(target, canary string) (Transport, error) {
	switch {
	case strings.HasPrefix(target, "ssh://"):
		return sshtransport.New(sshtransport.Config{
			Target:       target,
			IdentityFile: firstNonEmpty(os.Getenv("TORQUE_OPS_FACT_HOST_E2E_IDENTITY"), os.Getenv("TORQUE_LAB_SSH_IDENTITY")),
			ExtraArgs:    strings.Fields(firstNonEmpty(os.Getenv("TORQUE_OPS_FACT_HOST_E2E_SSH_OPTS"), os.Getenv("TORQUE_LAB_SSH_OPTS"))),
			Timeout:      20 * time.Second,
			RedactValues: []string{canary},
		})
	case strings.HasPrefix(target, "local://") || target == "localhost":
		return localtransport.New(localtransport.Config{
			Target:       target,
			Timeout:      20 * time.Second,
			RedactValues: []string{canary},
		})
	default:
		return nil, fmt.Errorf("unsupported target %q", target)
	}
}

func validateCacheProof(initial, fresh, stale, refreshed *ResolveResult) []string {
	var errors []string
	if initial == nil || fresh == nil || stale == nil || refreshed == nil {
		return []string{"missing one or more cache resolution phases"}
	}
	if initial.Source != "refresh" || !initial.Refreshed || initial.PreviousDecision == nil || initial.PreviousDecision.Status != "missing" {
		errors = append(errors, "initial missing-cache refresh did not record expected evidence")
	}
	if initial.Decision.Status != "fresh" || initial.Snapshot == nil || initial.Snapshot.Digest == "" {
		errors = append(errors, "initial refresh did not produce fresh cached facts")
	}
	if fresh.Source != "cache" || fresh.Refreshed || fresh.Decision.Status != "fresh" || fresh.Decision.Blocked {
		errors = append(errors, "fresh cache hit did not allow without refresh")
	}
	if stale.Source != "cache" || stale.Decision.Status != "stale" || stale.Decision.Decision != "block" || !stale.Decision.Blocked {
		errors = append(errors, "stale cache did not block the plan")
	}
	if refreshed.Source != "refresh" || !refreshed.Refreshed || refreshed.PreviousDecision == nil || refreshed.PreviousDecision.Status != "stale" {
		errors = append(errors, "stale refresh did not record previous stale decision")
	}
	if refreshed.Decision.Status != "fresh" || refreshed.Decision.Blocked || refreshed.Snapshot == nil || refreshed.Snapshot.Digest == "" {
		errors = append(errors, "refresh did not produce fresh facts")
	}
	return errors
}

func summarizeResolution(result *ResolveResult) map[string]any {
	if result == nil {
		return map[string]any{"status": "missing"}
	}
	summary := map[string]any{
		"source":         result.Source,
		"refreshed":      result.Refreshed,
		"cachePath":      result.CachePath,
		"decision":       result.Decision,
		"snapshotDigest": "",
		"targetDigest":   "",
	}
	if result.PreviousDecision != nil {
		summary["previousDecision"] = result.PreviousDecision
	}
	if result.Snapshot != nil {
		summary["snapshotDigest"] = result.Snapshot.Digest
		summary["targetDigest"] = result.Snapshot.TargetDigest
		summary["observedAt"] = result.Snapshot.ObservedAt
		summary["expiresAt"] = result.Snapshot.ExpiresAt
		summary["commandReceiptCount"] = len(result.Snapshot.CommandReceipts)
	}
	return summary
}

func validateSnapshot(snapshot *Snapshot) []string {
	var errors []string
	if snapshot.APIVersion != APIVersion || snapshot.Kind != Kind {
		errors = append(errors, "snapshot type mismatch")
	}
	if snapshot.TargetDigest == "" {
		errors = append(errors, "target digest missing")
	}
	if snapshot.Digest == "" || snapshot.Digest != snapshot.StableDigest() {
		errors = append(errors, "stable digest missing or mismatched")
	}
	if snapshot.TTL == "" || snapshot.ObservedAt == "" || snapshot.ExpiresAt == "" {
		errors = append(errors, "ttl or observation window missing")
	}
	if snapshot.Kernel.Name == "" || snapshot.Kernel.Release == "" {
		errors = append(errors, "kernel facts missing")
	}
	if snapshot.OS.ID == "" && snapshot.OS.Name == "" && snapshot.OS.PrettyName == "" {
		errors = append(errors, "os facts missing")
	}
	if snapshot.Packages.Manager == "" {
		errors = append(errors, "package manager facts missing")
	}
	if snapshot.Services.Manager == "" {
		errors = append(errors, "service manager facts missing")
	}
	if snapshot.Users.Count < 1 {
		errors = append(errors, "user facts missing")
	}
	if snapshot.Disks.Count < 1 {
		errors = append(errors, "disk facts missing")
	}
	if snapshot.Network.Hostname == "" {
		errors = append(errors, "network hostname missing")
	}
	if len(snapshot.CommandReceipts) != 7 {
		errors = append(errors, "command receipt count mismatch")
	}
	for _, receipt := range snapshot.CommandReceipts {
		if receipt.Status != "succeeded" {
			errors = append(errors, "command receipt failed: "+receipt.Operation)
		}
	}
	return errors
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func osReleaseFixture() string {
	return "ID=ubuntu\nNAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04 LTS\"\n"
}

func fullFakeTransport() *fakeTransport {
	return &fakeTransport{
		digest: "sha256:target",
		outputs: map[string]string{
			"fact.osRelease": osReleaseFixture(),
			"fact.kernel":    "Linux\n6.8.0-test\nx86_64\n",
			"fact.packages":  "manager=dpkg\npackage=base-files\npackage=curl\ncount=2\n",
			"fact.services":  "manager=systemd\nservice=ssh.service,running\ncount=1\nrunning=1\n",
			"fact.users":     "user=root,/bin/bash\nuser=nobody,/usr/sbin/nologin\ncount=2\nloginShells=1\n",
			"fact.disks":     "Filesystem Type 1024-blocks Used Available Capacity Mounted on\n/dev/vda1 ext4 1000 200 800 20% /\n",
			"fact.network":   "hostname=lab\naddr=eth0,inet,192.0.2.10/24\naddr=lo,inet,127.0.0.1/8\n",
		},
	}
}

type fakeTransport struct {
	digest       string
	outputs      map[string]string
	failures     map[string]bool
	redactValues []string
	index        int
	runs         int
}

func (t *fakeTransport) TargetDigest() string {
	return t.digest
}

func (t *fakeTransport) Run(_ context.Context, command string) transport.OperationResult {
	commands := factCommands()
	name := "unknown"
	if len(commands) > 0 {
		name = commands[t.index%len(commands)].name
	}
	t.index++
	t.runs++
	redactor := transport.NewRedactor(t.redactValues)
	stdout := t.outputs[name]
	if name == "fact.osRelease" && t.redactValues != nil {
		stdout += "TOKEN=top-secret\nSECRET=secret://ops/test#value\n"
	}
	result := transport.OperationResult{
		Operation:    "run",
		Status:       "succeeded",
		TargetDigest: t.digest,
		Command:      redactor.RedactArgs([]string{"fake", command}),
		Stdout:       redactor.RedactString(stdout),
		ExitCode:     0,
	}
	if t.failures[name] {
		result.Status = "failed"
		result.ExitCode = 1
		result.Stderr = "failed"
	}
	return result
}
