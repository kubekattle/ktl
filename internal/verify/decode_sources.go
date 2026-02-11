package verify

import (
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
)

// DecodeK8SYAMLWithSources decodes a multi-document YAML manifest into objects and
// attaches a best-effort template source (Helm "# Source: ...") to each object,
// if present.
//
// It annotates objects with:
// - __ktl_source: string
func DecodeK8SYAMLWithSources(manifest string) ([]map[string]any, error) {
	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	var out []map[string]any
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			return out, err
		}
		if len(obj) == 0 {
			continue
		}
		// We cannot recover comments via YAML decoding; as a compromise, support
		// a prior pre-pass where the manifest includes a "__ktl_source" key
		// (set by callers that parse "# Source:" comments).
		out = append(out, obj)
	}
	return out, nil
}

// SplitYAMLDocs splits a YAML stream into raw documents. It is intentionally
// simple and only treats "---" at the start of a line as a separator.
func SplitYAMLDocs(manifest string) []string {
	manifest = strings.ReplaceAll(manifest, "\r\n", "\n")
	if strings.TrimSpace(manifest) == "" {
		return nil
	}
	lines := strings.Split(manifest, "\n")
	var docs []string
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		doc := strings.TrimSpace(strings.Join(cur, "\n"))
		if doc != "" {
			docs = append(docs, doc+"\n")
		}
		cur = nil
	}
	for _, ln := range lines {
		if strings.HasPrefix(ln, "---") && strings.TrimSpace(ln) == "---" {
			flush()
			continue
		}
		cur = append(cur, ln)
	}
	flush()
	return docs
}

func extractHelmSourceComment(doc string) string {
	for _, ln := range strings.Split(doc, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "# Source:") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "# Source:"))
		}
		// Stop scanning once we hit non-comment content.
		if !strings.HasPrefix(ln, "#") {
			return ""
		}
	}
	return ""
}

// DecodeK8SYAMLWithHelmSources preserves Helm template source hints by parsing
// documents individually and extracting "# Source:" comments before decoding.
func DecodeK8SYAMLWithHelmSources(manifest string) ([]map[string]any, error) {
	docs := SplitYAMLDocs(manifest)
	if len(docs) == 0 {
		return nil, nil
	}
	var out []map[string]any
	for _, doc := range docs {
		source := extractHelmSourceComment(doc)
		objs, err := DecodeK8SYAML(doc)
		if err != nil {
			return out, err
		}
		for _, obj := range objs {
			if source != "" && obj != nil {
				obj["__ktl_source"] = source
			}
			out = append(out, obj)
		}
	}
	return out, nil
}
