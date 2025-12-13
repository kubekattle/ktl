// diff.go renders the HTML diff/plan view for deploy/report output.
package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/apparchive"
	"github.com/pmezard/go-difflib/difflib"
	"sigs.k8s.io/yaml"
)

// ArchiveSource describes an archive path and optional snapshot selector.
type ArchiveSource struct {
	Path     string
	Snapshot string
}

// ArchiveDiff encapsulates drift between two archive snapshots.
type ArchiveDiff struct {
	Left      ArchiveSide
	Right     ArchiveSide
	Summary   DiffSummary
	Resources []ResourceDiff
}

// ArchiveSide captures metadata for one side of the diff.
type ArchiveSide struct {
	Path          string
	Snapshot      string
	CreatedAt     string
	Metadata      map[string]string
	ManifestCount int
}

// DiffSummary aggregates change counts.
type DiffSummary struct {
	Added   int
	Removed int
	Changed int
}

// ResourceDiff captures per-resource drift data.
type ResourceDiff struct {
	Key             string
	Kind            string
	Namespace       string
	Name            string
	Change          string
	Diff            string
	Highlights      []DiffHighlight
	RollbackCommand string
}

// DiffHighlight surfaces a focused field-level change.
type DiffHighlight struct {
	Label  string
	Before string
	After  string
	Change string
}

// BuildArchiveDiff loads two archives and produces a structured diff.
func BuildArchiveDiff(leftSrc, rightSrc ArchiveSource) (*ArchiveDiff, error) {
	if strings.TrimSpace(leftSrc.Path) == "" || strings.TrimSpace(rightSrc.Path) == "" {
		return nil, fmt.Errorf("both archive paths are required for diff")
	}
	leftSide, leftMap, err := loadArchiveSide(leftSrc)
	if err != nil {
		return nil, fmt.Errorf("load left archive: %w", err)
	}
	rightSide, rightMap, err := loadArchiveSide(rightSrc)
	if err != nil {
		return nil, fmt.Errorf("load right archive: %w", err)
	}

	diff := &ArchiveDiff{
		Left:  leftSide,
		Right: rightSide,
	}

	seen := make(map[string]struct{})
	for key := range leftMap {
		seen[key] = struct{}{}
	}
	for key := range rightMap {
		seen[key] = struct{}{}
	}

	resourceDiffs := make([]ResourceDiff, 0, len(seen))
	for key := range seen {
		leftRec, leftOK := leftMap[key]
		rightRec, rightOK := rightMap[key]
		change := ""
		switch {
		case leftOK && !rightOK:
			change = "removed"
			diff.Summary.Removed++
		case !leftOK && rightOK:
			change = "added"
			diff.Summary.Added++
		default:
			if manifestsEqual(leftRec.Body, rightRec.Body) {
				continue
			}
			change = "changed"
			diff.Summary.Changed++
		}
		resourceDiffs = append(resourceDiffs, buildResourceDiff(key, change, leftRec, rightRec))
	}

	sort.Slice(resourceDiffs, func(i, j int) bool {
		return resourceDiffs[i].Key < resourceDiffs[j].Key
	})
	diff.Resources = resourceDiffs
	return diff, nil
}

func loadArchiveSide(src ArchiveSource) (ArchiveSide, map[string]apparchive.ManifestRecord, error) {
	reader, err := apparchive.NewReader(src.Path)
	if err != nil {
		return ArchiveSide{}, nil, err
	}
	defer reader.Close()
	snapshot, err := reader.ResolveSnapshot(src.Snapshot)
	if err != nil {
		return ArchiveSide{}, nil, err
	}
	manifests, err := reader.ReadManifests(snapshot)
	if err != nil {
		return ArchiveSide{}, nil, err
	}
	side := ArchiveSide{
		Path:          src.Path,
		Snapshot:      snapshot.Name,
		CreatedAt:     snapshot.CreatedAt.Format(time.RFC3339),
		Metadata:      snapshot.Metadata,
		ManifestCount: len(manifests),
	}
	manifestMap := make(map[string]apparchive.ManifestRecord, len(manifests))
	for _, manifest := range manifests {
		key := strings.ToLower(fmt.Sprintf("%s/%s/%s", manifest.Kind, manifest.Namespace, manifest.Name))
		manifestMap[key] = manifest
	}
	return side, manifestMap, nil
}

func buildResourceDiff(key, change string, left, right apparchive.ManifestRecord) ResourceDiff {
	diff := ResourceDiff{
		Key:       key,
		Change:    change,
		Kind:      coalesce(left.Kind, right.Kind),
		Namespace: coalesce(left.Namespace, right.Namespace),
		Name:      coalesce(left.Name, right.Name),
	}
	leftBody := left.Body
	rightBody := right.Body
	diff.Diff = unified(leftBody, rightBody, diff.Kind, diff.Name)
	diff.Highlights = buildHighlights(change, left.Kind, right.Kind, leftBody, rightBody)
	diff.RollbackCommand = rollbackCommand(change, left, diff.Namespace)
	return diff
}

func rollbackCommand(change string, left apparchive.ManifestRecord, namespace string) string {
	switch change {
	case "added":
		return fmt.Sprintf("kubectl delete %s %s -n %s", strings.ToLower(left.Kind), left.Name, namespace)
	default:
		body := left.Body
		if strings.TrimSpace(body) == "" {
			return ""
		}
		return fmt.Sprintf("kubectl apply -n %s -f - <<'KTL_MANIFEST'\n%s\nKTL_MANIFEST", namespace, body)
	}
}

func manifestsEqual(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func unified(a, b, kind, name string) string {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(a),
		B:        difflib.SplitLines(b),
		FromFile: fmt.Sprintf("left/%s", name),
		ToFile:   fmt.Sprintf("right/%s", name),
		Context:  3,
	}
	out, _ := difflib.GetUnifiedDiffString(diff)
	if len(out) > 16000 {
		return out[:16000] + "\n…truncated…"
	}
	return out
}

func buildHighlights(change, leftKind, rightKind, leftBody, rightBody string) []DiffHighlight {
	var highlights []DiffHighlight
	if change == "added" {
		highlights = append(highlights, DiffHighlight{
			Label:  "Resource",
			Before: "∅ (absent)",
			After:  "present",
			Change: "added",
		})
		return highlights
	}
	if change == "removed" {
		highlights = append(highlights, DiffHighlight{
			Label:  "Resource",
			Before: "present",
			After:  "∅ (removed)",
			Change: "removed",
		})
		return highlights
	}
	leftObj := parseYAML(leftBody)
	rightObj := parseYAML(rightBody)
	kind := strings.ToLower(coalesce(leftKind, rightKind))
	highlights = append(highlights, workloadHighlights(kind, leftObj, rightObj)...)
	highlights = append(highlights, rbacHighlights(kind, leftObj, rightObj)...)
	highlights = append(highlights, crdHighlights(kind, leftObj, rightObj)...)
	return highlights
}

func workloadHighlights(kind string, leftObj, rightObj map[string]interface{}) []DiffHighlight {
	switch kind {
	case "deployment", "statefulset", "daemonset", "job", "cronjob", "pod":
	default:
		return nil
	}
	leftSpec := extractPodSpec(kind, leftObj)
	rightSpec := extractPodSpec(kind, rightObj)
	if leftSpec == nil && rightSpec == nil {
		return nil
	}
	leftContainers := collectContainers(leftSpec)
	rightContainers := collectContainers(rightSpec)
	highlights := make([]DiffHighlight, 0)
	seen := setUnionKeys(leftContainers, rightContainers)
	for ctr := range seen {
		lc := leftContainers[ctr]
		rc := rightContainers[ctr]
		if lc.image != rc.image {
			highlights = append(highlights, DiffHighlight{
				Label:  fmt.Sprintf("Image · %s", ctr),
				Before: truncateValue(lc.image),
				After:  truncateValue(rc.image),
				Change: "image",
			})
		}
		envSeen := setUnionKeys(lc.env, rc.env)
		for env := range envSeen {
			lv := lc.env[env]
			rv := rc.env[env]
			if lv == rv {
				continue
			}
			highlights = append(highlights, DiffHighlight{
				Label:  fmt.Sprintf("Env · %s/%s", ctr, env),
				Before: truncateValue(emptyFallback(lv, "∅")),
				After:  truncateValue(emptyFallback(rv, "∅")),
				Change: "env",
			})
		}
	}
	return highlights
}

func rbacHighlights(kind string, leftObj, rightObj map[string]interface{}) []DiffHighlight {
	switch kind {
	case "role", "clusterrole":
		leftRules := toYAMLString(getField(leftObj, "rules"))
		rightRules := toYAMLString(getField(rightObj, "rules"))
		if leftRules == rightRules {
			return nil
		}
		return []DiffHighlight{{
			Label:  "RBAC Rules",
			Before: truncateValue(emptyFallback(leftRules, "∅")),
			After:  truncateValue(emptyFallback(rightRules, "∅")),
			Change: "rbac",
		}}
	case "rolebinding", "clusterrolebinding":
		leftSubjects := toYAMLString(getField(leftObj, "subjects"))
		rightSubjects := toYAMLString(getField(rightObj, "subjects"))
		leftRoleRef := toYAMLString(getField(leftObj, "roleRef"))
		rightRoleRef := toYAMLString(getField(rightObj, "roleRef"))
		var highlights []DiffHighlight
		if leftRoleRef != rightRoleRef {
			highlights = append(highlights, DiffHighlight{
				Label:  "RoleRef",
				Before: truncateValue(emptyFallback(leftRoleRef, "∅")),
				After:  truncateValue(emptyFallback(rightRoleRef, "∅")),
				Change: "rbac",
			})
		}
		if leftSubjects != rightSubjects {
			highlights = append(highlights, DiffHighlight{
				Label:  "Subjects",
				Before: truncateValue(emptyFallback(leftSubjects, "∅")),
				After:  truncateValue(emptyFallback(rightSubjects, "∅")),
				Change: "rbac",
			})
		}
		return highlights
	default:
		return nil
	}
}

func crdHighlights(kind string, leftObj, rightObj map[string]interface{}) []DiffHighlight {
	if kind != "customresourcedefinition" {
		return nil
	}
	leftVersions := toYAMLString(nestedField(leftObj, "spec", "versions"))
	rightVersions := toYAMLString(nestedField(rightObj, "spec", "versions"))
	if leftVersions == rightVersions {
		return nil
	}
	return []DiffHighlight{{
		Label:  "CRD Versions",
		Before: truncateValue(emptyFallback(leftVersions, "∅")),
		After:  truncateValue(emptyFallback(rightVersions, "∅")),
		Change: "crd",
	}}
}

func parseYAML(doc string) map[string]interface{} {
	if strings.TrimSpace(doc) == "" {
		return nil
	}
	var obj map[string]interface{}
	if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
		return nil
	}
	return obj
}

func extractPodSpec(kind string, obj map[string]interface{}) map[string]interface{} {
	if obj == nil {
		return nil
	}
	spec, _ := obj["spec"].(map[string]interface{})
	switch kind {
	case "pod":
		return spec
	case "deployment", "replicaset", "statefulset", "daemonset", "job":
		return nestedSpec(spec, "template", "spec")
	case "cronjob":
		return nestedSpec(spec, "jobTemplate", "spec", "template", "spec")
	default:
		return nestedSpec(spec, "template", "spec")
	}
}

func nestedSpec(spec map[string]interface{}, fields ...string) map[string]interface{} {
	current := spec
	for _, field := range fields {
		next, _ := current[field].(map[string]interface{})
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

type containerData struct {
	image string
	env   map[string]string
}

func collectContainers(spec map[string]interface{}) map[string]containerData {
	result := map[string]containerData{}
	if spec == nil {
		return result
	}
	for _, section := range []string{"containers", "initContainers"} {
		items, _ := spec[section].([]interface{})
		for _, item := range items {
			container, _ := item.(map[string]interface{})
			if container == nil {
				continue
			}
			name, _ := container["name"].(string)
			if name == "" {
				continue
			}
			data := result[name]
			data.image, _ = container["image"].(string)
			if data.env == nil {
				data.env = map[string]string{}
			}
			envEntries, _ := container["env"].([]interface{})
			for _, entry := range envEntries {
				e, _ := entry.(map[string]interface{})
				if e == nil {
					continue
				}
				key, _ := e["name"].(string)
				if key == "" {
					continue
				}
				if val, ok := e["value"].(string); ok {
					data.env[key] = val
					continue
				}
				if vf, ok := e["valueFrom"].(map[string]interface{}); ok {
					data.env[key] = summarizeValueFrom(vf)
				}
			}
			result[name] = data
		}
	}
	return result
}

func summarizeValueFrom(v map[string]interface{}) string {
	switch {
	case v["secretKeyRef"] != nil:
		ref, _ := v["secretKeyRef"].(map[string]interface{})
		return fmt.Sprintf("secret:%s/%s", ref["name"], ref["key"])
	case v["configMapKeyRef"] != nil:
		ref, _ := v["configMapKeyRef"].(map[string]interface{})
		return fmt.Sprintf("configMap:%s/%s", ref["name"], ref["key"])
	case v["fieldRef"] != nil:
		ref, _ := v["fieldRef"].(map[string]interface{})
		return fmt.Sprintf("field:%s", ref["fieldPath"])
	}
	return "valueFrom"
}

func setUnionKeys[K comparable, V any](left map[K]V, right map[K]V) map[K]struct{} {
	out := make(map[K]struct{})
	for k := range left {
		out[k] = struct{}{}
	}
	for k := range right {
		out[k] = struct{}{}
	}
	return out
}

func truncateValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if len(value) > 120 {
		return value[:117] + "…"
	}
	return value
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func getField(obj map[string]interface{}, field string) interface{} {
	if obj == nil {
		return nil
	}
	return obj[field]
}

func nestedField(obj map[string]interface{}, fields ...string) interface{} {
	current := interface{}(obj)
	for _, field := range fields {
		nextMap, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = nextMap[field]
	}
	return current
}

func toYAMLString(value interface{}) string {
	if value == nil {
		return ""
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func coalesce(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
