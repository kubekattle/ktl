package secrets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Mode string

const (
	ModeWarn  Mode = "warn"
	ModeBlock Mode = "block"
	ModeOff   Mode = "off"
)

type Severity string

const (
	SeverityWarn  Severity = "warn"
	SeverityBlock Severity = "block"
)

type Source string

const (
	SourceBuildArg Source = "build-arg"
	SourceOCI      Source = "oci-layer"
)

type Finding struct {
	Severity Severity `json:"severity"`
	Source   Source   `json:"source"`
	Rule     string   `json:"rule"`
	Message  string   `json:"message"`
	Key      string   `json:"key,omitempty"`
	Location string   `json:"location,omitempty"`
	Match    string   `json:"match,omitempty"`
}

type Report struct {
	Mode        Mode      `json:"mode"`
	Passed      bool      `json:"passed"`
	Blocked     bool      `json:"blocked"`
	Findings    []Finding `json:"findings,omitempty"`
	EvaluatedAt time.Time `json:"evaluatedAt"`
}

func DefaultReportPath(attestDir string) string {
	attestDir = strings.TrimSpace(attestDir)
	if attestDir == "" {
		return ""
	}
	return filepath.Join(attestDir, "ktl-secrets-report.json")
}

func WriteReport(path string, report *Report) error {
	path = strings.TrimSpace(path)
	if path == "" || report == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}
