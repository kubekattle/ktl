package deploy

import (
	"sort"
	"strings"
)

type BlockerSeverity string

const (
	BlockerFail BlockerSeverity = "fail"
	BlockerWarn BlockerSeverity = "warn"
)

type Blocker struct {
	Kind      string          `json:"kind"`
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	Reason    string          `json:"reason,omitempty"`
	Message   string          `json:"message,omitempty"`
	Severity  BlockerSeverity `json:"severity"`
}

func TopBlockers(rows []ResourceStatus, limit int) []Blocker {
	if limit <= 0 {
		limit = 6
	}
	var blockers []Blocker
	for _, rs := range rows {
		if rs.Kind == "" || rs.Name == "" {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(rs.Status))
		reason := strings.TrimSpace(rs.Reason)
		msg := strings.TrimSpace(rs.Message)
		if status == "" {
			continue
		}
		if status == "ready" || status == "succeeded" {
			continue
		}
		sev := BlockerWarn
		if status == "failed" {
			sev = BlockerFail
		}
		switch strings.ToLower(reason) {
		case "imagepullbackoff", "errimagepull", "crashloopbackoff", "createcontainerconfigerror", "unschedulable", "failedscheduling", "failedcreate", "progressdeadlineexceeded":
			sev = BlockerFail
		}
		blockers = append(blockers, Blocker{
			Kind:      rs.Kind,
			Namespace: rs.Namespace,
			Name:      rs.Name,
			Status:    rs.Status,
			Reason:    reason,
			Message:   msg,
			Severity:  sev,
		})
	}
	sort.Slice(blockers, func(i, j int) bool {
		a := blockers[i]
		b := blockers[j]
		if a.Severity != b.Severity {
			return a.Severity < b.Severity // fail < warn lexicographically
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	if len(blockers) > limit {
		return blockers[:limit]
	}
	return blockers
}
