package helpui

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestBuildIndex_IncludesCommandsFlagsAndEnv(t *testing.T) {
	root := &cobra.Command{Use: "torque"}
	root.PersistentFlags().String("log-level", "info", "Log level")
	apply := &cobra.Command{Use: "apply", Short: "Apply chart", Example: "torque apply --chart ./chart --release foo"}
	apply.Flags().StringP("namespace", "n", "", "Namespace to deploy into")
	root.AddCommand(apply)

	index := BuildIndex(root, false)
	if len(index.Entries) == 0 {
		t.Fatalf("expected entries, got none")
	}
	assertHas := func(kind string, contains string) {
		t.Helper()
		for _, e := range index.Entries {
			if e.Kind != kind {
				continue
			}
			if e.Title == contains {
				return
			}
		}
		t.Fatalf("expected %s entry with title %q", kind, contains)
	}
	assertHas("command", "torque")
	assertHas("command", "torque apply")
	assertHas("env", "TORQUE_CONFIG")

	foundFlag := false
	for _, e := range index.Entries {
		if e.Kind == "flag" && e.Title == "-n, --namespace" {
			foundFlag = true
			break
		}
	}
	if !foundFlag {
		t.Fatalf("expected flag entry for -n/--namespace")
	}
}

func TestBuildIndex_DeduplicatesGlobalFlags(t *testing.T) {
	root := &cobra.Command{Use: "torque"}
	root.PersistentFlags().String("mirror-bus", "", "Publish mirror payloads to a shared gRPC bus")
	a := &cobra.Command{Use: "a"}
	a.Flags().String("foo", "", "Foo")
	b := &cobra.Command{Use: "b"}
	b.Flags().String("bar", "", "Bar")
	root.AddCommand(a, b)

	index := BuildIndex(root, false)
	count := 0
	for _, e := range index.Entries {
		if e.Kind == "flag" && e.Title == "--mirror-bus" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 --mirror-bus entry, got %d", count)
	}
}

func TestBuildIndex_HasUniqueEntryIDs(t *testing.T) {
	root := &cobra.Command{Use: "torque"}
	root.AddCommand(&cobra.Command{Use: "apply", Short: "Apply chart"})

	index := BuildIndex(root, false)
	seen := make(map[string]struct{}, len(index.Entries))
	for _, entry := range index.Entries {
		if entry.ID == "" {
			t.Fatalf("entry with empty ID: %+v", entry)
		}
		if _, ok := seen[entry.ID]; ok {
			t.Fatalf("duplicate index entry ID %q", entry.ID)
		}
		seen[entry.ID] = struct{}{}
	}
}

func TestBuildIndex_IncludesDemosDoc(t *testing.T) {
	root := &cobra.Command{Use: "torque"}

	index := BuildIndex(root, false)
	for _, entry := range index.Entries {
		if entry.ID != "doc:demos" {
			continue
		}
		if entry.Title != "Demos" {
			t.Fatalf("unexpected demos title %q", entry.Title)
		}
		if !strings.Contains(entry.Content, "Ship subcommand release flow") {
			t.Fatalf("expected demos content to include ship demo")
		}
		if !strings.Contains(entry.Content, "Complex DAG stack orchestration") {
			t.Fatalf("expected demos content to include DAG demo")
		}
		if !strings.Contains(entry.Content, "Helmer HTML plan reports") {
			t.Fatalf("expected demos content to include HTML plan report demo")
		}
		if !strings.Contains(entry.Content, "Kubernetes logs and evidence capture") {
			t.Fatalf("expected demos content to include logging demo")
		}
		if !strings.Contains(entry.Content, "Remote agent mirror sessions") {
			t.Fatalf("expected demos content to include remote mirror demo")
		}
		if !strings.Contains(entry.Content, "Capture explain drilldown") {
			t.Fatalf("expected demos content to include explain drilldown demo")
		}
		if !strings.Contains(entry.Content, "Secret-safe build and log evidence") {
			t.Fatalf("expected demos content to include secret-safe evidence demo")
		}
		if !strings.Contains(entry.Content, "Drift and plan comparison") {
			t.Fatalf("expected demos content to include drift comparison demo")
		}
		if !strings.Contains(entry.Content, "Stack resume and rerun failed") {
			t.Fatalf("expected demos content to include stack rerun demo")
		}
		if strings.Contains(entry.Content, "Build, plan, apply, and logs") {
			t.Fatalf("expected build/plan/apply/logs demo to be removed from demos content")
		}
		if strings.Contains(entry.Content, "Security and evidence gates") {
			t.Fatalf("expected security/evidence demo to be removed from demos content")
		}
		if strings.Contains(entry.Content, "torque compared with split tooling") {
			t.Fatalf("expected comparison demo to be removed from demos content")
		}
		return
	}
	t.Fatalf("expected demos doc in help index")
}

func TestBuildIndex_IncludesAgentAndCacheDocs(t *testing.T) {
	root := &cobra.Command{Use: "torque"}

	index := BuildIndex(root, false)
	want := map[string]string{
		"doc:grpc-agent":                  "-tls-client-ca",
		"doc:enterprise-agent-operations": "mTLS-First Remote Bridge",
		"doc:s3-build-cache":              "--s3-cache",
		"doc:apply-simulate":              "Live Apply Twin",
		"doc:guardian":                    "observe-only runtime proof",
		"doc:incident":                    "incident time machine",
		"doc:contract":                    "recurrence rules",
		"doc:proof-graph":                 "torque proof graph",
		"doc:fleet-control-plane":         "10,000 hosts",
	}
	for id, content := range want {
		found := false
		for _, entry := range index.Entries {
			if entry.ID != id {
				continue
			}
			found = true
			if !strings.Contains(entry.Content, content) {
				t.Fatalf("expected %s content to include %q", id, content)
			}
		}
		if !found {
			t.Fatalf("expected %s in help index", id)
		}
	}
}

func TestBuildIndex_ExcludesSpecDocs(t *testing.T) {
	root := &cobra.Command{Use: "torque"}

	index := BuildIndex(root, false)
	blocked := map[string]struct{}{
		"doc:mcp-server-spec":                {},
		"doc:secrets-verifier-evidence-spec": {},
		"doc:security-corpus-spec":           {},
	}
	for _, entry := range index.Entries {
		if _, ok := blocked[entry.ID]; ok {
			t.Fatalf("did not expect %s in help index", entry.ID)
		}
	}
}

func TestBuildIndex_IncludesArchitectureDiagramsDoc(t *testing.T) {
	root := &cobra.Command{Use: "torque"}

	index := BuildIndex(root, false)
	for _, entry := range index.Entries {
		if entry.ID != "doc:architecture-diagrams" {
			continue
		}
		if entry.Title != "Architecture Diagrams" {
			t.Fatalf("unexpected architecture diagrams title %q", entry.Title)
		}
		for _, want := range []string{
			"Secret-safe delivery path",
			"Verifier and agent safety matrix",
		} {
			if !strings.Contains(entry.Content, want) {
				t.Fatalf("expected architecture diagrams content to include %q", want)
			}
		}
		return
	}
	t.Fatalf("expected architecture diagrams doc in help index")
}
