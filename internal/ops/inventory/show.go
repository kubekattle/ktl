package inventory

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ingresslabs/torque/internal/ops/targetgraph"
)

const (
	APIVersion = "torque.dev/ops/inventory/v1alpha1"
	ShowKind   = "InventoryShow"
)

var inventorySecretRefPattern = regexp.MustCompile(`secret://[^\s"',;]+`)

type ShowRequest struct {
	Selector map[string]string
	Groups   []string
	Limit    int
}

type ShowResult struct {
	APIVersion string                      `json:"apiVersion"`
	Kind       string                      `json:"kind"`
	GraphName  string                      `json:"graphName"`
	Summary    targetgraph.Summary         `json:"summary"`
	Selection  targetgraph.SelectionResult `json:"selection"`
	Targets    []TargetView                `json:"targets"`
}

type TargetView struct {
	ID                  string            `json:"id"`
	Type                string            `json:"type"`
	TransportRef        string            `json:"transportRef,omitempty"`
	Labels              map[string]string `json:"labels,omitempty"`
	Groups              []string          `json:"groups,omitempty"`
	FactsTTL            string            `json:"factsTtl,omitempty"`
	FactsDiscovery      string            `json:"factsDiscovery,omitempty"`
	PrivilegeProfile    string            `json:"privilegeProfile,omitempty"`
	LockScope           string            `json:"lockScope,omitempty"`
	AllowedCapabilities []string          `json:"allowedCapabilities,omitempty"`
}

func Show(graph *targetgraph.TargetGraph, request ShowRequest) (ShowResult, error) {
	if graph == nil {
		return ShowResult{}, fmt.Errorf("target graph is required")
	}
	selection, err := graph.ResolveSelection(targetgraph.SelectionRequest{
		Selector: request.Selector,
		Groups:   request.Groups,
		Limit:    request.Limit,
	})
	if err != nil {
		return ShowResult{}, err
	}
	byID := make(map[string]targetgraph.Target, len(graph.Targets))
	for _, target := range graph.Targets {
		byID[target.ID] = target
	}
	result := ShowResult{
		APIVersion: APIVersion,
		Kind:       ShowKind,
		GraphName:  graph.Metadata.Name,
		Summary:    graph.Summary(),
		Selection:  selection,
	}
	for _, targetID := range selection.MatchedTargetIDs {
		target, ok := byID[targetID]
		if !ok {
			return ShowResult{}, fmt.Errorf("selection referenced unknown target %q", targetID)
		}
		result.Targets = append(result.Targets, viewTarget(target))
	}
	return result, nil
}

func (r ShowResult) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func ParseSelector(values []string) (map[string]string, error) {
	selector := map[string]string{}
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("selector %q must be key=value", raw)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return nil, fmt.Errorf("selector %q must have non-empty key and value", raw)
		}
		selector[key] = value
	}
	if len(selector) == 0 {
		return nil, nil
	}
	return selector, nil
}

func FormatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(labels))
	for key, value := range labels {
		parts = append(parts, RedactSecretRefs(key)+"="+RedactSecretRefs(value))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func FormatList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	return strings.Join(out, ",")
}

func viewTarget(target targetgraph.Target) TargetView {
	return TargetView{
		ID:                  target.ID,
		Type:                target.Type,
		TransportRef:        RedactSecretRefs(target.TransportRef),
		Labels:              redactedLabels(target.Labels),
		Groups:              append([]string(nil), target.Groups...),
		FactsTTL:            RedactSecretRefs(target.Facts.TTL),
		FactsDiscovery:      RedactSecretRefs(target.Facts.Discovery),
		PrivilegeProfile:    RedactSecretRefs(target.PrivilegeProfile),
		LockScope:           RedactSecretRefs(target.LockScope),
		AllowedCapabilities: append([]string(nil), target.AllowedCapabilities...),
	}
}

func RedactSecretRefs(value string) string {
	return inventorySecretRefPattern.ReplaceAllString(value, "[REDACTED:secret-ref]")
}

func redactedLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		out[RedactSecretRefs(key)] = RedactSecretRefs(value)
	}
	return out
}

func redactedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, RedactSecretRefs(value))
	}
	return out
}
