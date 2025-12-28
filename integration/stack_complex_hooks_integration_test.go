package integration_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestKtlStackComplexHooksE2E(t *testing.T) {
	requireCommand(t, "kubectl")
	requireCommand(t, "helm")

	hookServer := newHookServer(t)
	stateDir := t.TempDir()
	releaseA := "hooky-a-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	releaseB := "hooky-b-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	// Use the pre-provisioned namespace from the integration bootstrap fixtures to avoid
	// requiring cluster-scoped namespace create/delete permissions.
	namespace := testNamespace
	nodeIDA := "default/" + namespace + "/" + releaseA
	nodeIDB := "default/" + namespace + "/" + releaseB

	stackRoot := prepareComplexHooksStack(t, complexHooksStackConfig{
		HookServerURL: hookServer.URL,
		StateDir:      stateDir,
		Namespace:     namespace,
		RunID:         strconv.FormatInt(time.Now().UnixNano(), 10),
		ReleaseA:      releaseA,
		ReleaseB:      releaseB,
		ChartPath:     filepath.Join(repoRoot, "testdata", "charts", "ktl-stack-hooks-e2e"),
	})

	t.Cleanup(func() {
		_, _, _ = execKtl(2*time.Minute, "stack", "--config", stackRoot, "delete", "--output", "json", "--yes")
		_ = exec.Command("kubectl", kubectlArgs("delete", "job", "-n", namespace, "-l", "ktl.dev/e2e=true", "--ignore-not-found=true")...).Run()
	})

	out1 := runKtl(t, 10*time.Minute,
		"stack", "--config", stackRoot, "apply",
		"--output", "json",
		"--yes",
		"--cache-apply",
	)
	events1 := decodeJSONEvents(t, out1)
	assertRunSucceeded(t, events1)
	assertHookSucceeded(t, events1, "", "pre-apply", "stack-pre-flaky", 1)
	assertHookSucceeded(t, events1, "", "pre-apply", "stack-pre-http", 1)
	assertHookSucceeded(t, events1, "", "post-apply", "stack-post-http-success", 1)
	assertHookSucceeded(t, events1, nodeIDA, "pre-apply", "rel-pre-script", 1)
	assertHookSucceeded(t, events1, nodeIDB, "pre-apply", "rel-pre-script", 1)
	assertHookSucceeded(t, events1, nodeIDA, "post-apply", "rel-post-http", 1)
	assertHookSucceeded(t, events1, nodeIDB, "post-apply", "rel-post-http", 1)
	assertHookRetryLogged(t, events1, "stack-pre-flaky")

	waitForHookJob(t, namespace, releaseA+"-hook-pre-install")
	waitForHookJob(t, namespace, releaseA+"-hook-post-install")
	waitForHookJob(t, namespace, releaseB+"-hook-pre-install")
	waitForHookJob(t, namespace, releaseB+"-hook-post-install")

	assertApplyCacheHasHooks(t, stackRoot, namespace, releaseA)
	assertApplyCacheHasHooks(t, stackRoot, namespace, releaseB)

	out2 := runKtl(t, 10*time.Minute,
		"stack", "--config", stackRoot, "apply",
		"--output", "json",
		"--yes",
		"--cache-apply",
	)
	events2 := decodeJSONEvents(t, out2)
	assertRunSucceeded(t, events2)
	assertNoApplyCacheNoopSkip(t, events2)

	waitForHookJob(t, namespace, releaseA+"-hook-pre-upgrade")
	waitForHookJob(t, namespace, releaseA+"-hook-post-upgrade")
	waitForHookJob(t, namespace, releaseB+"-hook-pre-upgrade")
	waitForHookJob(t, namespace, releaseB+"-hook-post-upgrade")

	out3 := runKtl(t, 10*time.Minute,
		"stack", "--config", stackRoot, "delete",
		"--output", "json",
		"--yes",
	)
	events3 := decodeJSONEvents(t, out3)
	assertRunSucceeded(t, events3)
	assertHookSucceeded(t, events3, "", "pre-delete", "stack-predelete-emit", 1)
	assertHookSucceeded(t, events3, "", "post-delete", "stack-postdelete-http", 1)
	assertHookSucceeded(t, events3, nodeIDA, "pre-delete", "rel-predelete-emit", 1)
	assertHookSucceeded(t, events3, nodeIDA, "post-delete", "rel-postdelete-http", 1)
	assertHookSucceeded(t, events3, nodeIDB, "pre-delete", "rel-predelete-emit", 1)
	assertHookSucceeded(t, events3, nodeIDB, "post-delete", "rel-postdelete-http", 1)

	waitForHookJob(t, namespace, releaseA+"-hook-pre-delete")
	waitForHookJob(t, namespace, releaseA+"-hook-post-delete")
	waitForHookJob(t, namespace, releaseB+"-hook-pre-delete")
	waitForHookJob(t, namespace, releaseB+"-hook-post-delete")

	hookServer.AssertCalled(t, "/stack-pre")
	hookServer.AssertCalled(t, "/stack-post")
	hookServer.AssertCalled(t, "/stack-post-delete")
	hookServer.AssertCalled(t, "/rel-post")
	hookServer.AssertCalled(t, "/rel-post-delete")
}

type complexHooksStackConfig struct {
	HookServerURL string
	StateDir      string
	Namespace     string
	RunID         string
	ReleaseA      string
	ReleaseB      string
	ChartPath     string
}

func prepareComplexHooksStack(t *testing.T, cfg complexHooksStackConfig) string {
	t.Helper()
	src := filepath.Join(repoRoot, "testdata", "stack", "complex-hooks")
	dst := filepath.Join(t.TempDir(), "stack")
	if err := copyDir(dst, src); err != nil {
		t.Fatalf("copy stack fixture: %v", err)
	}

	patchFile := func(path string, replacements map[string]string) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		s := string(raw)
		for from, to := range replacements {
			s = strings.ReplaceAll(s, from, to)
		}
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	repls := map[string]string{
		"__KTL_TEST_HOOK_SERVER__": cfg.HookServerURL,
		"__KTL_TEST_STATE_DIR__":   cfg.StateDir,
		"__KTL_TEST_NAMESPACE__":   cfg.Namespace,
		"__KTL_TEST_RUN_ID__":      cfg.RunID,
		"__KTL_TEST_RELEASE_A__":   cfg.ReleaseA,
		"__KTL_TEST_RELEASE_B__":   cfg.ReleaseB,
		"__KTL_TEST_CHART__":       cfg.ChartPath,
	}
	patchFile(filepath.Join(dst, "stack.yaml"), repls)
	patchFile(filepath.Join(dst, "hooks", "ns.yaml"), repls)
	patchFile(filepath.Join(dst, "hooks", "preapply-configmap.yaml"), repls)

	return dst
}

type hookServer struct {
	URL string

	mu       sync.Mutex
	requests []hookRequest
}

type hookRequest struct {
	Path    string
	Method  string
	Headers http.Header
	Body    string
}

func newHookServer(t *testing.T) *hookServer {
	t.Helper()
	hs := &hookServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		_ = r.Body.Close()

		hs.mu.Lock()
		hs.requests = append(hs.requests, hookRequest{
			Path:    r.URL.Path,
			Method:  r.Method,
			Headers: r.Header.Clone(),
			Body:    string(body),
		})
		hs.mu.Unlock()

		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	hs.URL = srv.URL
	return hs
}

func (s *hookServer) AssertCalled(t *testing.T, path string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, req := range s.requests {
		if req.Path == path {
			return
		}
	}
	var got []string
	for _, req := range s.requests {
		got = append(got, req.Path)
	}
	t.Fatalf("expected hook server path %q, got %v", path, got)
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("missing required command %q", name)
	}
}

func waitForHookJob(t *testing.T, namespace, name string) {
	t.Helper()
	runKubectl(t, "wait", "--for=condition=complete", "job/"+name, "-n", namespace, "--timeout=90s")
}

type jsonRunEvent struct {
	Type    string         `json:"type"`
	NodeID  string         `json:"nodeId,omitempty"`
	Message string         `json:"message,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

func decodeJSONEvents(t *testing.T, stdout string) []jsonRunEvent {
	t.Helper()
	var events []jsonRunEvent
	dec := json.NewDecoder(strings.NewReader(stdout))
	for {
		var ev jsonRunEvent
		if err := dec.Decode(&ev); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode event: %v\nstdout:\n%s", err, stdout)
		}
		events = append(events, ev)
	}
	if len(events) == 0 {
		t.Fatalf("expected at least 1 run event, got 0")
	}
	return events
}

func assertRunSucceeded(t *testing.T, events []jsonRunEvent) {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Type != "RUN_COMPLETED" {
			continue
		}
		if status, _ := ev.Fields["status"].(string); status == "succeeded" {
			return
		}
		t.Fatalf("expected run succeeded, got: type=%s message=%q fields=%v", ev.Type, ev.Message, ev.Fields)
	}
	t.Fatalf("missing RUN_COMPLETED event")
}

func assertHookSucceeded(t *testing.T, events []jsonRunEvent, nodeID, phase, hook string, want int) {
	t.Helper()
	got := 0
	for _, ev := range events {
		if ev.Type != "HOOK_SUCCEEDED" {
			continue
		}
		if strings.TrimSpace(nodeID) != strings.TrimSpace(ev.NodeID) {
			continue
		}
		if ev.Fields == nil {
			continue
		}
		if strings.TrimSpace(phase) != strings.TrimSpace(asString(ev.Fields["phase"])) {
			continue
		}
		if strings.TrimSpace(hook) != strings.TrimSpace(asString(ev.Fields["hook"])) {
			continue
		}
		got++
	}
	if got != want {
		t.Fatalf("expected %d HOOK_SUCCEEDED for node=%q phase=%q hook=%q, got %d", want, nodeID, phase, hook, got)
	}
}

func assertHookRetryLogged(t *testing.T, events []jsonRunEvent, hook string) {
	t.Helper()
	for _, ev := range events {
		if ev.Type != "NODE_LOG" {
			continue
		}
		if !strings.Contains(ev.Message, "hook pre-apply "+hook+" failed (attempt 1/2)") {
			continue
		}
		return
	}
	t.Fatalf("expected NODE_LOG retry message for hook %q", hook)
}

func assertNoApplyCacheNoopSkip(t *testing.T, events []jsonRunEvent) {
	t.Helper()
	for _, ev := range events {
		if ev.Type != "PHASE_COMPLETED" || ev.Fields == nil {
			continue
		}
		if asString(ev.Fields["phase"]) != "post-hooks" {
			continue
		}
		if asString(ev.Fields["status"]) != "skipped" {
			continue
		}
		if strings.Contains(asString(ev.Fields["message"]), "Apply cache: no-op") {
			t.Fatalf("unexpected apply-cache skip: %v", ev)
		}
	}
}

func assertApplyCacheHasHooks(t *testing.T, stackRoot, namespace, releaseName string) {
	t.Helper()
	dbPath := filepath.Join(stackRoot, ".ktl", "stack", "state.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite %s: %v", dbPath, err)
	}
	defer db.Close()

	var hasHooks int
	row := db.QueryRow(`SELECT has_hooks FROM ktl_stack_apply_cache WHERE namespace = ? AND release_name = ? AND command = 'apply' LIMIT 1`, namespace, releaseName)
	if err := row.Scan(&hasHooks); err != nil {
		t.Fatalf("scan apply_cache row (ns=%s release=%s): %v", namespace, releaseName, err)
	}
	if hasHooks != 1 {
		t.Fatalf("expected apply_cache.has_hooks=1 for ns=%s release=%s, got %d", namespace, releaseName, hasHooks)
	}
}

func asString(v any) string {
	switch vv := v.(type) {
	case string:
		return vv
	case []byte:
		return string(vv)
	default:
		return ""
	}
}

func copyDir(dst, src string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		outPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(outPath, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Preserve read bits; keep it simple for fixtures.
		mode := os.FileMode(0o644)
		if fi, statErr := os.Stat(path); statErr == nil {
			mode = fi.Mode() & 0o777
		}
		return os.WriteFile(outPath, raw, mode)
	})
}

func (e jsonRunEvent) String() string {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(e)
	return strings.TrimSpace(buf.String())
}
