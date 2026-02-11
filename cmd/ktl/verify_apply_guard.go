package main

import (
	"fmt"
	"strings"

	"github.com/example/ktl/internal/verify"
)

func enforceVerifiedDigest(reportPath string, renderedManifest string, release string, namespace string) error {
	rep, err := verify.LoadReport(reportPath)
	if err != nil {
		return fmt.Errorf("load verify report %s: %w", reportPath, err)
	}
	if rep == nil {
		return fmt.Errorf("verify report is required")
	}
	want := ""
	for _, in := range rep.Inputs {
		if strings.EqualFold(strings.TrimSpace(in.Kind), "chart") {
			want = strings.TrimSpace(in.RenderedSHA256)
			break
		}
	}
	if want == "" {
		return fmt.Errorf("verify report %s missing inputs.renderedSha256", reportPath)
	}
	got := verify.ManifestDigestSHA256(renderedManifest)
	if got != want {
		return fmt.Errorf("rendered manifest does not match verified digest (release=%s namespace=%s): got %s, want %s", release, namespace, got, want)
	}
	return nil
}
