package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func DefaultReportPath(attestDir string) string {
	attestDir = strings.TrimSpace(attestDir)
	if attestDir == "" {
		return ""
	}
	return filepath.Join(attestDir, "ktl-policy-report.json")
}

func WriteReport(path string, report *Report) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if report == nil {
		return fmt.Errorf("report is nil")
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
