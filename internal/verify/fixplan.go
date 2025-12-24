package verify

import (
	"fmt"
	"sort"
	"strings"
)

type FixChange struct {
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
	RuleID    string `json:"ruleId,omitempty"`
	Title     string `json:"title,omitempty"`
	PatchYAML string `json:"patchYaml,omitempty"`
}

func BuildFixPlan(findings []Finding) []FixChange {
	bySubject := map[string][]Finding{}
	for _, f := range findings {
		key := subjectKey(f.Subject)
		bySubject[key] = append(bySubject[key], f)
	}
	var keys []string
	for k := range bySubject {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []FixChange
	for _, k := range keys {
		list := bySubject[k]
		sort.Slice(list, func(i, j int) bool { return list[i].RuleID < list[j].RuleID })
		sub := list[0].Subject
		for _, f := range list {
			if patch := patchSuggestion(sub, f.RuleID); patch != "" {
				out = append(out, FixChange{
					Kind:      sub.Kind,
					Namespace: sub.Namespace,
					Name:      sub.Name,
					RuleID:    f.RuleID,
					Title:     fixTitle(f.RuleID),
					PatchYAML: patch,
				})
			}
		}
	}
	return out
}

func RenderFixPlanText(changes []FixChange) string {
	if len(changes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nFix plan (suggested patches):\n")
	for _, c := range changes {
		target := fmt.Sprintf("%s/%s", c.Kind, c.Name)
		if strings.TrimSpace(c.Namespace) != "" {
			target = fmt.Sprintf("%s/%s/%s", c.Kind, c.Namespace, c.Name)
		}
		b.WriteString(fmt.Sprintf("\n- %s (%s)\n", target, c.RuleID))
		if c.Title != "" {
			b.WriteString("  " + c.Title + "\n")
		}
		lines := strings.Split(strings.TrimSpace(c.PatchYAML), "\n")
		for _, line := range lines {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

func subjectKey(s Subject) string {
	return strings.Join([]string{strings.TrimSpace(s.Kind), strings.TrimSpace(s.Namespace), strings.TrimSpace(s.Name)}, "/")
}

func fixTitle(ruleID string) string {
	switch ruleID {
	case "k8s/service_account_token_automount_not_disabled":
		return "Disable automount of ServiceAccount token on the Pod spec"
	case "k8s/net_raw_capabilities_not_being_dropped":
		return "Drop NET_RAW (or ALL) Linux capabilities for containers"
	case "k8s/pod_or_container_without_security_context":
		return "Add a baseline securityContext for pods/containers"
	case "k8s/memory_limits_not_defined":
		return "Define memory requests/limits for containers"
	default:
		return ""
	}
}

func patchSuggestion(sub Subject, ruleID string) string {
	kind := strings.TrimSpace(sub.Kind)
	name := strings.TrimSpace(sub.Name)
	ns := strings.TrimSpace(sub.Namespace)
	if kind == "" || name == "" {
		return ""
	}
	header := fmt.Sprintf("apiVersion: v1\nkind: %s\nmetadata:\n  name: %s\n", kind, name)
	if ns != "" {
		header += "  namespace: " + ns + "\n"
	}
	switch ruleID {
	case "k8s/service_account_token_automount_not_disabled":
		return header + "spec:\n  template:\n    spec:\n      automountServiceAccountToken: false\n"
	case "k8s/net_raw_capabilities_not_being_dropped":
		return header + "spec:\n  template:\n    spec:\n      containers:\n        - name: <container>\n          securityContext:\n            capabilities:\n              drop: [\"ALL\"]\n"
	case "k8s/pod_or_container_without_security_context":
		return header + "spec:\n  template:\n    spec:\n      securityContext:\n        runAsNonRoot: true\n      containers:\n        - name: <container>\n          securityContext:\n            allowPrivilegeEscalation: false\n            readOnlyRootFilesystem: true\n            capabilities:\n              drop: [\"ALL\"]\n"
	case "k8s/memory_limits_not_defined":
		return header + "spec:\n  template:\n    spec:\n      containers:\n        - name: <container>\n          resources:\n            requests:\n              memory: 128Mi\n            limits:\n              memory: 256Mi\n"
	default:
		return ""
	}
}
