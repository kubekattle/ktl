package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
)

type ActionDescriptor struct {
	Release   string
	Chart     string
	Version   string
	Namespace string
	DryRun    bool
	Diff      bool
	Destroy   bool
}

func DescribeDeployAction(desc ActionDescriptor) string {
	ns := strings.TrimSpace(desc.Namespace)
	if ns == "" {
		ns = "default"
	}
	target := strings.TrimSpace(desc.Chart)
	version := strings.TrimSpace(desc.Version)
	if target == "" {
		target = strings.TrimSpace(desc.Release)
	}
	if target == "" {
		target = "release"
	}
	if version != "" {
		target = fmt.Sprintf("%s %s", target, version)
	}
	var verb string
	switch {
	case desc.Destroy:
		verb = "Destroying"
	case desc.Diff:
		verb = "Diffing"
	case desc.DryRun:
		verb = "Rendering"
	default:
		verb = "Deploying"
	}
	return fmt.Sprintf("%s %s into ns/%s", verb, target, ns)
}

func FetchLatestReleaseManifest(actionCfg *action.Configuration, releaseName string) (string, string) {
	if actionCfg == nil || strings.TrimSpace(releaseName) == "" {
		return "", "release name missing"
	}
	getAction := action.NewGet(actionCfg)
	if rel, err := getAction.Run(releaseName); err == nil && rel != nil && strings.TrimSpace(rel.Manifest) != "" {
		return rel.Manifest, "from helm get"
	}
	historyAction := action.NewHistory(actionCfg)
	historyAction.Max = 20
	revisions, err := historyAction.Run(releaseName)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return "", "release not found (no deployed release or history)"
		}
		return "", fmt.Sprintf("unable to read release history: %v", err)
	}
	for i := len(revisions) - 1; i >= 0; i-- {
		if revisions[i] != nil && strings.TrimSpace(revisions[i].Manifest) != "" {
			return revisions[i].Manifest, "from latest release history"
		}
	}
	return "", "release history has no manifest"
}

type CaptureFileHash struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Error  string `json:"error,omitempty"`
}

func HashFiles(paths []string) []CaptureFileHash {
	out := make([]CaptureFileHash, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		h := CaptureFileHash{Path: p}
		info, err := os.Stat(p)
		if err != nil {
			h.Error = err.Error()
			out = append(out, h)
			continue
		}
		h.Size = info.Size()
		data, err := os.ReadFile(p)
		if err != nil {
			h.Error = err.Error()
			out = append(out, h)
			continue
		}
		sum := sha256.Sum256(data)
		h.SHA256 = hex.EncodeToString(sum[:])
		out = append(out, h)
	}
	return out
}

func ReleaseHistoryBreadcrumbs(actionCfg *action.Configuration, releaseName string, limit int) ([]HistoryBreadcrumb, *HistoryBreadcrumb, error) {
	if actionCfg == nil || strings.TrimSpace(releaseName) == "" || limit <= 0 {
		return nil, nil, nil
	}
	historyAction := action.NewHistory(actionCfg)
	fetchLimit := limit * 3
	if fetchLimit < limit {
		fetchLimit = limit
	}
	if fetchLimit < 10 {
		fetchLimit = 10
	}
	historyAction.Max = fetchLimit
	revisions, err := historyAction.Run(releaseName)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var breadcrumbs []HistoryBreadcrumb
	var lastSuccessful *HistoryBreadcrumb
	for i := len(revisions) - 1; i >= 0; i-- {
		crumb, ok := BreadcrumbFromRelease(revisions[i])
		if !ok {
			continue
		}
		if lastSuccessful == nil && strings.EqualFold(crumb.Status, release.StatusDeployed.String()) {
			c := crumb
			lastSuccessful = &c
		}
		if len(breadcrumbs) < limit {
			breadcrumbs = append(breadcrumbs, crumb)
		}
	}
	return breadcrumbs, lastSuccessful, nil
}

func BreadcrumbFromRelease(rel *release.Release) (HistoryBreadcrumb, bool) {
	if rel == nil {
		return HistoryBreadcrumb{}, false
	}
	crumb := HistoryBreadcrumb{
		Revision: rel.Version,
		Status:   "",
	}
	if rel.Info != nil {
		if rel.Info.Status != "" {
			crumb.Status = rel.Info.Status.String()
		}
		if desc := strings.TrimSpace(rel.Info.Description); desc != "" {
			crumb.Description = desc
		}
		if !rel.Info.LastDeployed.IsZero() {
			crumb.DeployedAt = rel.Info.LastDeployed.UTC().Format(time.RFC3339Nano)
		}
	}
	if crumb.Status == "" && rel.Info != nil {
		crumb.Status = rel.Info.Status.String()
	}
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		crumb.Chart = rel.Chart.Metadata.Name
		crumb.Version = rel.Chart.Metadata.Version
		crumb.AppVersion = rel.Chart.Metadata.AppVersion
	}
	if crumb.Status == "" && rel.Info != nil {
		crumb.Status = rel.Info.Status.String()
	}
	if crumb.Revision == 0 && crumb.Chart == "" && crumb.Status == "" {
		return HistoryBreadcrumb{}, false
	}
	return crumb, true
}

func PrependBreadcrumb(history []HistoryBreadcrumb, crumb HistoryBreadcrumb, limit int) []HistoryBreadcrumb {
	if limit <= 0 {
		return CloneBreadcrumbs(history)
	}
	out := make([]HistoryBreadcrumb, 0, limit)
	out = append(out, crumb)
	for _, existing := range history {
		if len(out) >= limit {
			break
		}
		if existing.Revision == crumb.Revision {
			continue
		}
		out = append(out, existing)
	}
	return out
}

func CloneBreadcrumbs(history []HistoryBreadcrumb) []HistoryBreadcrumb {
	if len(history) == 0 {
		return nil
	}
	out := make([]HistoryBreadcrumb, len(history))
	copy(out, history)
	return out
}

func CloneBreadcrumbPointer(crumb *HistoryBreadcrumb) *HistoryBreadcrumb {
	if crumb == nil {
		return nil
	}
	c := *crumb
	return &c
}

func IsSuccessfulStatus(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return false
	}
	if status == "succeeded" || status == "success" || status == release.StatusDeployed.String() {
		return true
	}
	return false
}
