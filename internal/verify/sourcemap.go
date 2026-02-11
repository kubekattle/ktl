package verify

import (
	"strings"

	"gopkg.in/yaml.v3"
)

type docSlice struct {
	Text      string
	StartLine int // 1-based line in the original stream where this doc begins
}

// splitYAMLDocsWithLineOffsets is like SplitYAMLDocs but keeps the starting line
// for each document so we can compute absolute line numbers for findings.
func splitYAMLDocsWithLineOffsets(manifest string) []docSlice {
	manifest = strings.ReplaceAll(manifest, "\r\n", "\n")
	if strings.TrimSpace(manifest) == "" {
		return nil
	}
	lines := strings.Split(manifest, "\n")
	var out []docSlice
	var cur []string
	startLine := 1
	flush := func() {
		if len(cur) == 0 {
			return
		}
		doc := strings.TrimSpace(strings.Join(cur, "\n"))
		if doc != "" {
			out = append(out, docSlice{Text: doc + "\n", StartLine: startLine})
		}
		cur = nil
	}

	for i, ln := range lines {
		// YAML doc boundary: line is exactly '---'
		if strings.HasPrefix(ln, "---") && strings.TrimSpace(ln) == "---" {
			flush()
			// next content line is the start of the next document
			startLine = i + 2
			continue
		}
		cur = append(cur, ln)
	}
	flush()
	return out
}

type parsedDoc struct {
	HelmSource string
	StartLine  int
	Node       *yaml.Node
	Kind       string
	Namespace  string
	Name       string
}

func parseManifestDocsForSourceMap(renderedManifest string) map[string]parsedDoc {
	docs := splitYAMLDocsWithLineOffsets(renderedManifest)
	if len(docs) == 0 {
		return nil
	}
	out := map[string]parsedDoc{}
	for _, d := range docs {
		helmSource := extractHelmSourceComment(d.Text)

		var n yaml.Node
		if err := yaml.Unmarshal([]byte(d.Text), &n); err != nil {
			continue
		}
		root := &n
		if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
			root = root.Content[0]
		}
		if root == nil || root.Kind != yaml.MappingNode {
			continue
		}
		kind := strings.TrimSpace(lookupScalar(root, "kind"))
		name := strings.TrimSpace(lookupScalar(root, "metadata", "name"))
		ns := strings.TrimSpace(lookupScalar(root, "metadata", "namespace"))
		if kind == "" || name == "" {
			continue
		}
		key := subjectKey(Subject{Kind: kind, Namespace: ns, Name: name})
		if _, ok := out[key]; ok {
			continue
		}
		out[key] = parsedDoc{
			HelmSource: helmSource,
			StartLine:  d.StartLine,
			Node:       root,
			Kind:       kind,
			Namespace:  ns,
			Name:       name,
		}
	}
	return out
}

func lookupScalar(root *yaml.Node, path ...string) string {
	n := lookupNode(root, path...)
	if n == nil {
		return ""
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	default:
		return ""
	}
}

func lookupNode(root *yaml.Node, path ...string) *yaml.Node {
	cur := root
	for _, p := range path {
		if cur == nil {
			return nil
		}
		switch cur.Kind {
		case yaml.MappingNode:
			var next *yaml.Node
			for i := 0; i+1 < len(cur.Content); i += 2 {
				k := cur.Content[i]
				v := cur.Content[i+1]
				if k != nil && k.Value == p {
					next = v
					break
				}
			}
			cur = next
		default:
			return nil
		}
	}
	return cur
}

func lineForFieldPath(root *yaml.Node, fieldPath string) int {
	// Best-effort: if the exact path doesn't exist (common when a rule fires on a
	// missing field), walk up the path until we find an anchor node.
	fieldPath = strings.TrimSpace(fieldPath)
	fieldPath = strings.TrimPrefix(fieldPath, ".")
	if fieldPath == "" || root == nil {
		return 0
	}
	parts := strings.Split(fieldPath, ".")
	for i := len(parts); i >= 1; i-- {
		p := strings.Join(parts[:i], ".")
		if ln := lineForFieldPathExact(root, p); ln > 0 {
			return ln
		}
	}
	return 0
}

func lineForFieldPathExact(root *yaml.Node, fieldPath string) int {
	fieldPath = strings.TrimSpace(fieldPath)
	if fieldPath == "" || root == nil {
		return 0
	}
	fieldPath = strings.TrimPrefix(fieldPath, ".")
	parts := strings.Split(fieldPath, ".")
	cur := root
	for _, p := range parts {
		if cur == nil {
			return 0
		}
		if p == "" {
			continue
		}

		// numeric segment = sequence index
		if isDigits(p) {
			if cur.Kind != yaml.SequenceNode {
				return 0
			}
			idx := atoiSmall(p)
			if idx < 0 || idx >= len(cur.Content) {
				return 0
			}
			cur = cur.Content[idx]
			continue
		}

		if cur.Kind != yaml.MappingNode {
			return 0
		}
		var next *yaml.Node
		for i := 0; i+1 < len(cur.Content); i += 2 {
			k := cur.Content[i]
			v := cur.Content[i+1]
			if k != nil && k.Value == p {
				next = v
				break
			}
		}
		cur = next
	}
	if cur != nil && cur.Line > 0 {
		return cur.Line
	}
	return 0
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func atoiSmall(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
		if n > 1_000_000 {
			return -1
		}
	}
	return n
}

// AnnotateFindingsWithRenderedSource sets Finding.Path and Finding.Line based on
// the rendered manifest's YAML node positions. It does not overwrite existing
// Path/Line values.
//
// renderedPath should be a real file path when possible so SARIF consumers can
// open the artifact and jump to the line.
func AnnotateFindingsWithRenderedSource(renderedPath string, renderedManifest string, findings []Finding) []Finding {
	if strings.TrimSpace(renderedManifest) == "" || len(findings) == 0 {
		return findings
	}
	docs := parseManifestDocsForSourceMap(renderedManifest)
	if len(docs) == 0 {
		return findings
	}

	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		updated := f
		key := subjectKey(f.Subject)
		doc, ok := docs[key]
		if ok {
			// Prefer linking to an actual rendered file if we have one; otherwise
			// fall back to the Helm template source for display only.
			if strings.TrimSpace(updated.Path) == "" {
				if strings.TrimSpace(renderedPath) != "" {
					updated.Path = strings.TrimSpace(renderedPath)
				} else if strings.TrimSpace(doc.HelmSource) != "" {
					updated.Path = strings.TrimSpace(doc.HelmSource)
				}
			}
			if updated.Line <= 0 {
				fp := strings.TrimSpace(updated.FieldPath)
				// Policy findings often populate Location with a real field path.
				if fp == "" && !strings.Contains(updated.Location, "{{") {
					fp = strings.TrimSpace(updated.Location)
				}
				if fp != "" {
					if ln := lineForFieldPath(doc.Node, fp); ln > 0 {
						updated.Line = (doc.StartLine - 1) + ln
					}
				}
			}
		}
		out = append(out, updated)
	}
	return out
}
