package stack

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadRun_SqliteOverridesStackRootAfterMove(t *testing.T) {
	root1 := t.TempDir()
	writeMinimalStackFixture(t, root1, "move-test")

	u, err := Discover(root1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}

	exec := &fakeExecutor{sleepOn: "", sleep: 0}
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 2,
		Lock:        true,
		LockTTL:     200 * time.Millisecond,
		Executor:    exec,
	}, &out, &errOut); err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, errOut.String())
	}

	// Move/copy to a new directory and prove LoadRun uses the new location.
	root2 := t.TempDir()
	copyDir(t, root1, root2)

	runRoot, err := LoadMostRecentRun(root2)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRun(runRoot)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(loaded.Plan.StackRoot) != filepath.Clean(root2) {
		t.Fatalf("expected stackRoot=%s got %s", root2, loaded.Plan.StackRoot)
	}
}

func TestDisasterRecovery_KillAndReplayDoesNotCorruptSQLite(t *testing.T) {
	if os.Getenv("KTL_STACK_TEST_HELPER") == "1" {
		helperMain(t)
		return
	}

	root := t.TempDir()
	writeMinimalStackFixture(t, root, "crash-test")

	// Start a helper process that will block on node 2, then we'll SIGKILL it.
	cmd := exec.Command(os.Args[0], "-test.run", t.Name(), "-test.v")
	cmd.Env = append(os.Environ(),
		"KTL_STACK_TEST_HELPER=1",
		"KTL_STACK_TEST_ROOT="+root,
	)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	// Wait until we see NODE_RUNNING for node2 in sqlite (or time out).
	wantNode := "c1/ns/app2"
	deadline := time.Now().Add(15 * time.Second)
	for {
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("timeout waiting for node running; stderr:\n%s", stderr.String())
		}
		runRoot, err := LoadMostRecentRun(root)
		if err == nil {
			runID := filepath.Base(runRoot)
			s, err := openStackStateStore(root, true)
			if err == nil {
				evs, _, _ := s.TailEvents(context.Background(), runID, 200)
				_ = s.Close()
				for _, ev := range evs {
					if ev.Type == "NODE_RUNNING" && ev.NodeID == wantNode {
						goto kill
					}
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

kill:
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	// Verify sqlite is readable after an unclean stop.
	runRoot, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatal(err)
	}
	runID := filepath.Base(runRoot)
	s, err := openStackStateStore(root, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.GetRunSummary(context.Background(), runID)
	_ = s.Close()
	if err != nil {
		t.Fatalf("read summary after kill: %v", err)
	}

	// Let the lock TTL expire, then replay the run and ensure it completes.
	time.Sleep(300 * time.Millisecond)

	u, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Lock:        true,
		LockTTL:     200 * time.Millisecond,
		Executor:    &fakeExecutor{},
	}, &out, &errOut); err != nil {
		t.Fatalf("replay run: %v\nstderr:\n%s", err, errOut.String())
	}
}

func TestSQLite_ConcurrentReadDuringWrite(t *testing.T) {
	root := t.TempDir()
	writeMinimalStackFixture(t, root, "concurrent-read")

	u, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Block on app2 to keep the writer active.
	runDone := make(chan error, 1)
	go func() {
		var out, errOut bytes.Buffer
		runDone <- Run(ctx, RunOptions{
			Command:     "apply",
			Plan:        p,
			Concurrency: 1,
			Lock:        true,
			LockTTL:     2 * time.Second,
			Executor:    &fakeExecutor{sleepOn: "app2", sleep: 2 * time.Second},
		}, &out, &errOut)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runRoot, err := LoadMostRecentRun(root)
		if err == nil {
			runID := filepath.Base(runRoot)
			s, err := openStackStateStore(root, true)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = s.TailEvents(context.Background(), runID, 50)
			_ = s.Close()
			if err != nil {
				t.Fatalf("tail events during write: %v", err)
			}
		}
		time.Sleep(25 * time.Millisecond)
	}

	cancel()
	<-runDone
}

type fakeExecutor struct {
	sleepOn string
	sleep   time.Duration
}

func (f *fakeExecutor) RunNode(ctx context.Context, node *runNode, command string) error {
	_ = command
	if f.sleepOn != "" && node.Name == f.sleepOn {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.sleep):
		}
	}
	return nil
}

func helperMain(t *testing.T) {
	root := os.Getenv("KTL_STACK_TEST_ROOT")
	if strings.TrimSpace(root) == "" {
		t.Fatal("missing KTL_STACK_TEST_ROOT")
	}

	u, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Block on app2 long enough for the parent to kill us mid-run.
	exec := &fakeExecutor{sleepOn: "app2", sleep: 30 * time.Second}
	var out, errOut bytes.Buffer
	_ = Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Lock:        true,
		LockTTL:     200 * time.Millisecond,
		Executor:    exec,
	}, &out, &errOut)

	// If we weren't killed (e.g. running under Windows), fail loudly.
	if runtime.GOOS == "windows" {
		return
	}
	t.Fatalf("helper unexpectedly returned (expected to be killed); stderr:\n%s", errOut.String())
}

func writeMinimalStackFixture(t *testing.T, root string, name string) {
	t.Helper()

	chartDir := filepath.Join(root, "charts", "cm")
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("name: cm\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte("fixture: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tpl := `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name | quote }}
data:
  fixture: {{ .Values.fixture | quote }}
`
	if err := os.WriteFile(filepath.Join(chartDir, "templates", "configmap.yaml"), []byte(tpl), 0o644); err != nil {
		t.Fatal(err)
	}

	stackYAML := fmt.Sprintf(`apiVersion: ktl.dev/v1
kind: Stack
name: %s
defaults:
  cluster: { name: c1 }
  namespace: ns
releases:
  - name: app1
    chart: ./charts/cm
    set: { fixture: "app1" }
  - name: app2
    chart: ./charts/cm
    needs: [app1]
    set: { fixture: "app2" }
  - name: app3
    chart: ./charts/cm
    needs: [app2]
    set: { fixture: "app3" }
`, name)
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyDir(t *testing.T, src string, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFilterByNodeStatus_PrunesMissingNeeds(t *testing.T) {
	p := &Plan{
		StackRoot: "/tmp",
		StackName: "x",
		Nodes: []*ResolvedRelease{
			{ID: "c/ns/a", Name: "a", Cluster: ClusterTarget{Name: "c"}, Namespace: "ns"},
			{ID: "c/ns/b", Name: "b", Cluster: ClusterTarget{Name: "c"}, Namespace: "ns", Needs: []string{"a"}},
		},
		ByID: map[string]*ResolvedRelease{},
		ByCluster: map[string][]*ResolvedRelease{
			"c": {
				{ID: "c/ns/a", Name: "a", Cluster: ClusterTarget{Name: "c"}, Namespace: "ns"},
				{ID: "c/ns/b", Name: "b", Cluster: ClusterTarget{Name: "c"}, Namespace: "ns", Needs: []string{"a"}},
			},
		},
	}
	status := map[string]string{"c/ns/a": "succeeded", "c/ns/b": "failed"}
	out := FilterByNodeStatus(p, status, []string{"failed"})
	if out == nil || len(out.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %+v", out)
	}
	if got := out.Nodes[0].Name; got != "b" {
		t.Fatalf("expected b, got %s", got)
	}
	if len(out.Nodes[0].Needs) != 0 {
		t.Fatalf("expected needs pruned, got %v", out.Nodes[0].Needs)
	}
}

var _ NodeExecutor = (*fakeExecutor)(nil)
