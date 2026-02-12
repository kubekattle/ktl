package helpui

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kubekattle/ktl/internal/version"
	"github.com/spf13/cobra"
)

func TestHelpUIIndex_IncludesVersion(t *testing.T) {
	old := version.Version
	version.Version = "1.2.3"
	t.Cleanup(func() { version.Version = old })

	root := &cobra.Command{Use: "ktl"}
	srv := New(":0", root, logr.Discard())

	rr := httptest.NewRecorder()
	srv.handleIndex(rr, httptest.NewRequest("GET", "http://example/", nil))

	body := rr.Body.String()
	if !strings.Contains(body, "ktl 1.2.3") {
		t.Fatalf("expected HTML to include version, got: %q", body)
	}
}
