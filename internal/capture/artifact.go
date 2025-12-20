// artifact.go defines the on-disk capture artifact format produced by 'ktl logs capture'.
package capture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PrepareArtifact returns a directory containing the capture contents. Call the
// returned cleanup when done to remove any temporary extraction dir.
func PrepareArtifact(path string) (string, func(), error) {
	dir, cleanup, err := prepareCaptureDir(path)
	if err != nil {
		return "", nil, err
	}
	if cleanup == nil {
		cleanup = func() {}
	}
	return dir, cleanup, nil
}

// LoadMetadata reads metadata.json from the capture artifact (file or directory).
func LoadMetadata(path string) (*Metadata, error) {
	dir, cleanup, err := PrepareArtifact(path)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	metaPath := filepath.Join(dir, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}
	if len(meta.Namespaces) > 1 {
		sort.Strings(meta.Namespaces)
	}
	return &meta, nil
}

// MetadataDiff captures differences between two capture metadata snapshots.
type MetadataDiff struct {
	LeftPath  string
	RightPath string
	Left      Metadata
	Right     Metadata

	FieldDiffs        []FieldDiff
	AddedNamespaces   []string
	RemovedNamespaces []string
}

// Empty returns true when no differences were detected.
func (m MetadataDiff) Empty() bool {
	return len(m.FieldDiffs) == 0 && len(m.AddedNamespaces) == 0 && len(m.RemovedNamespaces) == 0
}

// FieldDiff highlights a single metadata field change.
type FieldDiff struct {
	Name  string
	Left  string
	Right string
}

// CompareMetadataFiles loads both metadata files and returns a diff report.
func CompareMetadataFiles(leftPath, rightPath string) (*MetadataDiff, error) {
	left, err := LoadMetadata(leftPath)
	if err != nil {
		return nil, fmt.Errorf("load metadata %s: %w", leftPath, err)
	}
	right, err := LoadMetadata(rightPath)
	if err != nil {
		return nil, fmt.Errorf("load metadata %s: %w", rightPath, err)
	}
	diff := DiffMetadata(*left, *right)
	diff.LeftPath = leftPath
	diff.RightPath = rightPath
	return &diff, nil
}

// DiffMetadata computes field-wise differences between two metadata structures.
func DiffMetadata(left, right Metadata) MetadataDiff {
	diff := MetadataDiff{Left: left, Right: right}
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Session Name", fmtEmpty(left.SessionName), fmtEmpty(right.SessionName))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Started At", fmtTime(left.StartedAt), fmtTime(right.StartedAt))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Ended At", fmtTime(left.EndedAt), fmtTime(right.EndedAt))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Duration", fmtDuration(left.DurationSeconds), fmtDuration(right.DurationSeconds))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "All Namespaces", fmtBool(left.AllNamespaces), fmtBool(right.AllNamespaces))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Pod Query", fmtEmpty(left.PodQuery), fmtEmpty(right.PodQuery))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Tail Lines", fmtInt(left.TailLines), fmtInt(right.TailLines))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Since", fmtEmpty(left.Since), fmtEmpty(right.Since))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Context", fmtEmpty(left.Context), fmtEmpty(right.Context))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Kubeconfig", fmtEmpty(left.Kubeconfig), fmtEmpty(right.Kubeconfig))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Pod Count", fmtInt(int64(left.PodCount)), fmtInt(int64(right.PodCount)))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Events Enabled", fmtBool(left.EventsEnabled), fmtBool(right.EventsEnabled))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "Follow", fmtBool(left.Follow), fmtBool(right.Follow))
	diff.FieldDiffs = appendFieldDiff(diff.FieldDiffs, "SQLite", fmtEmpty(left.SQLitePath), fmtEmpty(right.SQLitePath))

	diff.FieldDiffs = filterNoChange(diff.FieldDiffs)
	diff.AddedNamespaces, diff.RemovedNamespaces = diffStringSets(left.Namespaces, right.Namespaces)
	return diff
}

func appendFieldDiff(in []FieldDiff, name, left, right string) []FieldDiff {
	if left == right {
		return in
	}
	return append(in, FieldDiff{Name: name, Left: left, Right: right})
}

func filterNoChange(diffs []FieldDiff) []FieldDiff {
	if len(diffs) == 0 {
		return diffs
	}
	out := diffs[:0]
	for _, d := range diffs {
		if d.Left == d.Right {
			continue
		}
		out = append(out, d)
	}
	return out
}

func diffStringSets(left, right []string) ([]string, []string) {
	leftSet := make(map[string]struct{}, len(left))
	for _, v := range left {
		leftSet[v] = struct{}{}
	}
	rightSet := make(map[string]struct{}, len(right))
	for _, v := range right {
		rightSet[v] = struct{}{}
	}
	var added, removed []string
	for v := range rightSet {
		if _, ok := leftSet[v]; !ok {
			added = append(added, v)
		}
	}
	for v := range leftSet {
		if _, ok := rightSet[v]; !ok {
			removed = append(removed, v)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func fmtEmpty(val string) string {
	if strings.TrimSpace(val) == "" {
		return "<empty>"
	}
	return val
}

func fmtBool(val bool) string {
	return strconv.FormatBool(val)
}

func fmtInt(val int64) string {
	return strconv.FormatInt(val, 10)
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "<zero>"
	}
	return t.UTC().Format(time.RFC3339)
}

func fmtDuration(seconds float64) string {
	if seconds == 0 {
		return "0s"
	}
	d := time.Duration(seconds * float64(time.Second))
	return d.String()
}
