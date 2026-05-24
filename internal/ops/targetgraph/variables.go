package targetgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type VariableResolutionRequest struct {
	TargetID    string         `json:"targetId"`
	Environment map[string]any `json:"environment,omitempty"`
	CLI         map[string]any `json:"cli,omitempty"`
}

type VariableResolution struct {
	TargetID       string                       `json:"targetId"`
	Values         map[string]ResolvedVariable  `json:"values"`
	Provenance     []VariableAssignment         `json:"provenance"`
	LayerOrder     []VariableLayerSource        `json:"layerOrder"`
	Redaction      VariableRedactionSummary     `json:"redaction"`
	Precedence     []string                     `json:"precedence"`
	OverriddenKeys map[string][]VariableSource  `json:"overriddenKeys,omitempty"`
	FinalSources   map[string]VariableSource    `json:"finalSources"`
	SourceCounts   map[string]int               `json:"sourceCounts"`
	SecretRefs     map[string]SecretReferenceID `json:"secretRefs,omitempty"`
}

type ResolvedVariable struct {
	Key             string           `json:"key"`
	Value           any              `json:"value"`
	Source          VariableSource   `json:"source"`
	Redacted        bool             `json:"redacted"`
	SecretRef       bool             `json:"secretRef"`
	SecretRefDigest string           `json:"secretRefDigest,omitempty"`
	ValueDigest     string           `json:"valueDigest"`
	History         []VariableSource `json:"history,omitempty"`
	ActualValue     any              `json:"-"`
}

type VariableAssignment struct {
	Key             string         `json:"key"`
	Value           any            `json:"value"`
	Source          VariableSource `json:"source"`
	Precedence      int            `json:"precedence"`
	Overridden      bool           `json:"overridden"`
	Redacted        bool           `json:"redacted"`
	SecretRef       bool           `json:"secretRef"`
	SecretRefDigest string         `json:"secretRefDigest,omitempty"`
	ValueDigest     string         `json:"valueDigest"`
	ActualValue     any            `json:"-"`
}

type VariableLayerSource struct {
	Source     VariableSource `json:"source"`
	Precedence int            `json:"precedence"`
	KeyCount   int            `json:"keyCount"`
}

type VariableSource struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	LayerID  string `json:"layerId,omitempty"`
	TargetID string `json:"targetId,omitempty"`
	GroupID  string `json:"groupId,omitempty"`
}

type VariableRedactionSummary struct {
	RedactedAssignments int `json:"redactedAssignments"`
	SecretRefCount      int `json:"secretRefCount"`
	SensitiveKeyCount   int `json:"sensitiveKeyCount"`
}

type SecretReferenceID struct {
	Digest string `json:"digest"`
	Kind   string `json:"kind"`
}

func (g TargetGraph) ResolveVariables(req VariableResolutionRequest) (VariableResolution, error) {
	target, ok := g.targetByID(req.TargetID)
	if !ok {
		return VariableResolution{}, fmt.Errorf("unknown target %q", req.TargetID)
	}

	resolution := VariableResolution{
		TargetID:       req.TargetID,
		Values:         map[string]ResolvedVariable{},
		Precedence:     []string{"graph", "group", "target", "environment", "cli"},
		OverriddenKeys: map[string][]VariableSource{},
		FinalSources:   map[string]VariableSource{},
		SourceCounts:   map[string]int{},
		SecretRefs:     map[string]SecretReferenceID{},
	}
	assignmentsByKey := map[string][]int{}
	precedence := 0

	addLayer := func(source VariableSource, values map[string]any) {
		if len(values) == 0 {
			return
		}
		precedence++
		resolution.LayerOrder = append(resolution.LayerOrder, VariableLayerSource{
			Source:     source,
			Precedence: precedence,
			KeyCount:   len(values),
		})
		resolution.SourceCounts[source.Type] += len(values)
		keys := sortedMapKeys(values)
		for _, key := range keys {
			actual := values[key]
			redactedValue, redacted, secretRef, sensitiveKey, secretDigest := redactVariableValue(key, actual)
			if redacted {
				resolution.Redaction.RedactedAssignments++
			}
			if secretRef {
				resolution.Redaction.SecretRefCount++
				resolution.SecretRefs[key] = SecretReferenceID{Digest: secretDigest, Kind: "secret-ref"}
			}
			if sensitiveKey {
				resolution.Redaction.SensitiveKeyCount++
			}
			assignment := VariableAssignment{
				Key:             key,
				Value:           redactedValue,
				Source:          source,
				Precedence:      precedence,
				Redacted:        redacted,
				SecretRef:       secretRef,
				SecretRefDigest: secretDigest,
				ValueDigest:     digestAny(actual),
				ActualValue:     actual,
			}
			assignmentsByKey[key] = append(assignmentsByKey[key], len(resolution.Provenance))
			resolution.Provenance = append(resolution.Provenance, assignment)
		}
	}

	for _, layer := range g.Variables {
		addLayer(VariableSource{Type: "graph", ID: layer.ID, LayerID: layer.ID}, layer.Values)
	}
	for _, group := range g.groupsForTarget(target) {
		for _, layer := range group.Variables {
			addLayer(VariableSource{Type: "group", ID: group.ID + "/" + layer.ID, LayerID: layer.ID, GroupID: group.ID}, layer.Values)
		}
	}
	for _, layer := range target.Variables {
		addLayer(VariableSource{Type: "target", ID: target.ID + "/" + layer.ID, LayerID: layer.ID, TargetID: target.ID}, layer.Values)
	}
	addLayer(VariableSource{Type: "environment", ID: "environment"}, req.Environment)
	addLayer(VariableSource{Type: "cli", ID: "cli"}, req.CLI)

	for key, indexes := range assignmentsByKey {
		for i, index := range indexes {
			if i < len(indexes)-1 {
				resolution.Provenance[index].Overridden = true
				resolution.OverriddenKeys[key] = append(resolution.OverriddenKeys[key], resolution.Provenance[index].Source)
			}
		}
		final := resolution.Provenance[indexes[len(indexes)-1]]
		history := make([]VariableSource, 0, len(indexes))
		for _, index := range indexes {
			history = append(history, resolution.Provenance[index].Source)
		}
		resolution.Values[key] = ResolvedVariable{
			Key:             key,
			Value:           final.Value,
			Source:          final.Source,
			Redacted:        final.Redacted,
			SecretRef:       final.SecretRef,
			SecretRefDigest: final.SecretRefDigest,
			ValueDigest:     final.ValueDigest,
			History:         history,
			ActualValue:     final.ActualValue,
		}
		resolution.FinalSources[key] = final.Source
	}
	if len(resolution.OverriddenKeys) == 0 {
		resolution.OverriddenKeys = nil
	}
	if len(resolution.SecretRefs) == 0 {
		resolution.SecretRefs = nil
	}
	return resolution, nil
}

func (g TargetGraph) groupsForTarget(target Target) []Group {
	var out []Group
	seen := map[string]struct{}{}
	targetGroupRefs := map[string]struct{}{}
	for _, groupID := range target.Groups {
		targetGroupRefs[groupID] = struct{}{}
	}
	for _, group := range g.Groups {
		_, explicitTargetGroup := targetGroupRefs[group.ID]
		if explicitTargetGroup || stringInSlice(target.ID, group.Targets) || (len(group.Selector) > 0 && LabelsMatch(target.Labels, group.Selector)) {
			if _, exists := seen[group.ID]; !exists {
				out = append(out, group)
				seen[group.ID] = struct{}{}
			}
		}
	}
	return out
}

func (g TargetGraph) targetByID(targetID string) (Target, bool) {
	for _, target := range g.Targets {
		if target.ID == targetID {
			return target, true
		}
	}
	return Target{}, false
}

func redactVariableValue(key string, value any) (any, bool, bool, bool, string) {
	if isSensitiveKey(key) {
		return "[REDACTED:sensitive-key]", true, containsSecretRef(value), true, secretRefDigest(value)
	}
	redacted, didRedact, secretRef := redactValue(value)
	return redacted, didRedact, secretRef, false, secretRefDigest(value)
}

func redactValue(value any) (any, bool, bool) {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, "secret://") {
			return "[REDACTED:secret-ref]", true, true
		}
		return typed, false, false
	case map[string]any:
		out := make(map[string]any, len(typed))
		redacted := false
		secretRef := false
		for _, key := range sortedMapKeys(typed) {
			next, nextRedacted, nextSecretRef := redactVariableValueForNestedKey(key, typed[key])
			out[key] = next
			redacted = redacted || nextRedacted
			secretRef = secretRef || nextSecretRef
		}
		return out, redacted, secretRef
	case []any:
		out := make([]any, len(typed))
		redacted := false
		secretRef := false
		for i, item := range typed {
			next, nextRedacted, nextSecretRef := redactValue(item)
			out[i] = next
			redacted = redacted || nextRedacted
			secretRef = secretRef || nextSecretRef
		}
		return out, redacted, secretRef
	default:
		return value, false, false
	}
}

func redactVariableValueForNestedKey(key string, value any) (any, bool, bool) {
	if isSensitiveKey(key) {
		return "[REDACTED:sensitive-key]", true, containsSecretRef(value)
	}
	return redactValue(value)
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "password") ||
		strings.Contains(lower, "passwd") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "privatekey")
}

func containsSecretRef(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.HasPrefix(typed, "secret://")
	case map[string]any:
		for _, item := range typed {
			if containsSecretRef(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsSecretRef(item) {
				return true
			}
		}
	}
	return false
}

func secretRefDigest(value any) string {
	refs := collectSecretRefs(value, nil)
	if len(refs) == 0 {
		return ""
	}
	sort.Strings(refs)
	hash := sha256.Sum256([]byte(strings.Join(refs, "\n")))
	return hex.EncodeToString(hash[:])
}

func collectSecretRefs(value any, refs []string) []string {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, "secret://") {
			refs = append(refs, typed)
		}
	case map[string]any:
		for _, key := range sortedMapKeys(typed) {
			refs = collectSecretRefs(typed[key], refs)
		}
	case []any:
		for _, item := range typed {
			refs = collectSecretRefs(item, refs)
		}
	}
	return refs
}

func digestAny(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		raw = []byte(fmt.Sprintf("%v", value))
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringInSlice(needle string, haystack []string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}
