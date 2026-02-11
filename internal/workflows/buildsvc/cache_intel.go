package buildsvc

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/pkg/buildkit"
	"github.com/moby/buildkit/client"
	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
	digest "github.com/opencontainers/go-digest"
)

type cacheIntelCollector struct {
	contextAbs     string
	dockerfilePath string
	cacheDir       string
	topN           int

	previous  *cacheIntelInputsSnapshot
	current   *cacheIntelInputsSnapshot
	prevGraph *cacheIntelSolveGraph

	mu       chan struct{}
	vertices map[string]*cacheIntelVertex
	ociDir   string
}

type cacheIntelVertex struct {
	id          string
	name        string
	cached      bool
	startedAt   *time.Time
	completedAt *time.Time
	err         string
	bytesTotal  int64
	inputs      []string
}

type cacheIntelSolveGraph struct {
	Version    int                     `json:"version"`
	TakenAtUTC time.Time               `json:"takenAtUtc"`
	Vertices   []cacheIntelSolveVertex `json:"vertices"`
}

type cacheIntelSolveVertex struct {
	Digest         string     `json:"digest"`
	Name           string     `json:"name,omitempty"`
	Inputs         []string   `json:"inputs,omitempty"`
	Cached         bool       `json:"cached,omitempty"`
	Error          string     `json:"error,omitempty"`
	BytesTotal     int64      `json:"bytesTotal,omitempty"`
	StartedAtUTC   *time.Time `json:"startedAtUtc,omitempty"`
	CompletedAtUTC *time.Time `json:"completedAtUtc,omitempty"`
}

type cacheIntelInputsSnapshot struct {
	Version             int               `json:"version"`
	TakenAtUTC          time.Time         `json:"takenAtUtc"`
	ContextAbs          string            `json:"contextAbs"`
	DockerfileRel       string            `json:"dockerfileRel"`
	DockerfileSHA       string            `json:"dockerfileSha256"`
	DockerignoreSHA     string            `json:"dockerignoreSha256,omitempty"`
	BuildArgSHA         map[string]string `json:"buildArgSha256,omitempty"`
	SecretIDs           []string          `json:"secretIds,omitempty"`
	FileSHA             map[string]string `json:"fileSha256,omitempty"`
	FileBytes           map[string]int64  `json:"fileBytes,omitempty"`
	BroadContextCopy    bool              `json:"broadContextCopy,omitempty"`
	SecretMounts        int               `json:"secretMounts,omitempty"`
	SSHMounts           int               `json:"sshMounts,omitempty"`
	ContextMetaSHA      string            `json:"contextMetaSha256,omitempty"`
	ContextTopFileSHA   map[string]string `json:"contextTopFileSha256,omitempty"`
	ContextTopFileBytes map[string]int64  `json:"contextTopFileBytes,omitempty"`
}

type cacheIntelDiff struct {
	BuildArgsChanged    []cacheIntelChangedValue `json:"buildArgsChanged,omitempty"`
	SecretsChanged      bool                     `json:"secretsChanged,omitempty"`
	DockerfileChanged   bool                     `json:"dockerfileChanged,omitempty"`
	DockerignoreChanged bool                     `json:"dockerignoreChanged,omitempty"`
	FilesChanged        []cacheIntelChangedValue `json:"filesChanged,omitempty"`
	BroadContextCopy    bool                     `json:"broadContextCopy,omitempty"`
	SecretMounts        int                      `json:"secretMounts,omitempty"`
	SSHMounts           int                      `json:"sshMounts,omitempty"`
}

type cacheIntelChangedValue struct {
	Key    string `json:"key"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

func newCacheIntelCollector(ctx context.Context, contextAbs, dockerfileAbs, cacheDir string, topN int, buildArgs map[string]string, secretIDs []string) (*cacheIntelCollector, error) {
	if topN <= 0 {
		topN = 10
	}
	relDockerfile := dockerfileAbs
	if rel, err := filepath.Rel(contextAbs, dockerfileAbs); err == nil {
		relDockerfile = rel
	}

	c := &cacheIntelCollector{
		contextAbs:     contextAbs,
		dockerfilePath: dockerfileAbs,
		cacheDir:       cacheDir,
		topN:           topN,
		mu:             make(chan struct{}, 1),
		vertices:       map[string]*cacheIntelVertex{},
	}
	c.mu <- struct{}{}

	prev, _ := c.loadPreviousSnapshot(ctx, relDockerfile)
	c.previous = prev
	prevGraph, _ := c.loadPreviousGraph(ctx, relDockerfile)
	c.prevGraph = prevGraph

	cur, _ := buildCacheIntelInputsSnapshot(ctx, contextAbs, dockerfileAbs, relDockerfile, buildArgs, secretIDs)
	c.current = cur
	_ = c.writeCurrentSnapshot(ctx)
	return c, nil
}

func (c *cacheIntelCollector) HandleStatus(status *client.SolveStatus) {
	if c == nil || status == nil {
		return
	}
	<-c.mu
	defer func() { c.mu <- struct{}{} }()

	now := time.Now()
	for _, vertex := range status.Vertexes {
		if vertex == nil {
			continue
		}
		key := vertex.Digest.String()
		if strings.TrimSpace(key) == "" {
			key = digest.FromString(vertex.Name).String()
		}
		st := c.vertexForLocked(key)
		if name := strings.TrimSpace(vertex.Name); name != "" {
			st.name = name
		}
		if vertex.Cached {
			st.cached = true
		}
		if vertex.Started != nil {
			t := *vertex.Started
			st.startedAt = &t
		}
		if vertex.Completed != nil {
			t := *vertex.Completed
			st.completedAt = &t
		}
		if vertex.Error != "" {
			st.err = vertex.Error
		}
		if len(vertex.Inputs) > 0 {
			inputs := make([]string, 0, len(vertex.Inputs))
			for _, in := range vertex.Inputs {
				s := strings.TrimSpace(in.String())
				if s == "" {
					continue
				}
				inputs = append(inputs, s)
			}
			if len(inputs) > 0 {
				sort.Strings(inputs)
				st.inputs = inputs
			}
		}
		if st.startedAt == nil && st.completedAt != nil {
			st.startedAt = st.completedAt
		}
		if st.startedAt == nil && st.completedAt == nil {
			st.startedAt = &now
		}
	}
	for _, vs := range status.Statuses {
		key := vs.Vertex.String()
		if strings.TrimSpace(key) == "" {
			continue
		}
		st := c.vertexForLocked(key)
		if name := strings.TrimSpace(vs.Name); name != "" {
			st.name = name
		}
		if vs.Total > st.bytesTotal {
			st.bytesTotal = vs.Total
		}
	}
}

func (c *cacheIntelCollector) attachOCILayoutDir(ociDir string) {
	if c == nil {
		return
	}
	ociDir = strings.TrimSpace(ociDir)
	if ociDir == "" {
		return
	}
	<-c.mu
	c.ociDir = ociDir
	c.mu <- struct{}{}
}

func (c *cacheIntelCollector) vertexForLocked(key string) *cacheIntelVertex {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "unknown"
	}
	if v, ok := c.vertices[key]; ok {
		return v
	}
	v := &cacheIntelVertex{id: key}
	c.vertices[key] = v
	return v
}

func (c *cacheIntelCollector) report() cacheIntelReport {
	out := cacheIntelReport{
		TopN:   10,
		NowUTC: time.Now().UTC(),
	}
	if c == nil {
		return out
	}
	out.ContextAbs = c.contextAbs
	out.Dockerfile = c.dockerfilePath
	out.TopN = c.topN
	<-c.mu
	vertices := make([]cacheIntelVertex, 0, len(c.vertices))
	for _, v := range c.vertices {
		if v == nil {
			continue
		}
		vertices = append(vertices, *v)
	}
	cur := c.current
	prev := c.previous
	ociDir := c.ociDir
	prevGraph := c.prevGraph
	c.mu <- struct{}{}

	out.InputsPrevious = prev
	out.InputsCurrent = cur
	out.Diff = diffCacheIntelInputs(prev, cur, c.topN)

	for _, v := range vertices {
		if v.cached {
			out.CacheHits++
		} else if v.completedAt != nil && v.err == "" {
			out.CacheMisses++
		}
	}

	out.Vertices = vertices
	curGraph := buildSolveGraph(vertices)
	out.CacheKeyDiffs = diffSolveGraphs(prevGraph, &curGraph, out.TopN)
	if cur != nil {
		_ = c.writeCurrentGraph(context.Background(), cur.DockerfileRel, curGraph)
	}
	if ociDir != "" {
		if layers, err := buildkit.TopOCILayers(ociDir, out.TopN); err == nil {
			out.Layers = make([]cacheIntelLayer, 0, len(layers))
			for _, layer := range layers {
				out.Layers = append(out.Layers, cacheIntelLayer{
					ImageDigest: layer.ImageDigest,
					Digest:      layer.Digest,
					Size:        layer.Size,
					MediaType:   layer.MediaType,
				})
			}
		}
	}
	return out
}

type cacheIntelReport struct {
	NowUTC     time.Time `json:"nowUtc"`
	ContextAbs string    `json:"contextAbs"`
	Dockerfile string    `json:"dockerfile"`
	TopN       int       `json:"topN"`

	CacheHits   int `json:"cacheHits"`
	CacheMisses int `json:"cacheMisses"`

	InputsPrevious *cacheIntelInputsSnapshot `json:"inputsPrevious,omitempty"`
	InputsCurrent  *cacheIntelInputsSnapshot `json:"inputsCurrent,omitempty"`
	Diff           cacheIntelDiff            `json:"diff"`

	Vertices      []cacheIntelVertex       `json:"vertices,omitempty"`
	Layers        []cacheIntelLayer        `json:"layers,omitempty"`
	CacheKeyDiffs []cacheIntelCacheKeyDiff `json:"cacheKeyDiffs,omitempty"`
}

type cacheIntelLayer struct {
	ImageDigest string `json:"imageDigest,omitempty"`
	Digest      string `json:"digest"`
	Size        int64  `json:"size"`
	MediaType   string `json:"mediaType,omitempty"`
}

type cacheIntelCacheKeyDiff struct {
	Name          string   `json:"name"`
	PrevDigest    string   `json:"prevDigest,omitempty"`
	CurDigest     string   `json:"curDigest,omitempty"`
	Type          string   `json:"type"` // definition_changed | cache_evicted | new_step | removed_step
	UpstreamNames []string `json:"upstreamNames,omitempty"`
}

func (r cacheIntelReport) writeJSON(w io.Writer) error {
	if w == nil {
		return nil
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func (r cacheIntelReport) writeHuman(w io.Writer) {
	if w == nil {
		return
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Cache intelligence")
	fmt.Fprintf(w, "  Cache hits: %d  Cache misses: %d\n", r.CacheHits, r.CacheMisses)

	if len(r.Diff.BuildArgsChanged) > 0 || r.Diff.SecretsChanged || r.Diff.DockerfileChanged || r.Diff.DockerignoreChanged || len(r.Diff.FilesChanged) > 0 {
		fmt.Fprintln(w, "  Inputs changed since last build:")
		for _, c := range r.Diff.BuildArgsChanged {
			fmt.Fprintf(w, "    ARG %s %s → %s\n", c.Key, shortHash(c.Before), shortHash(c.After))
		}
		if r.Diff.SecretsChanged {
			fmt.Fprintln(w, "    Secrets: changed")
		}
		if r.Diff.DockerfileChanged {
			fmt.Fprintln(w, "    Dockerfile: changed")
		}
		if r.Diff.DockerignoreChanged {
			fmt.Fprintln(w, "    .dockerignore: changed")
		}
		for _, c := range r.Diff.FilesChanged {
			size := int64(0)
			if r.InputsCurrent != nil {
				size = r.InputsCurrent.FileBytes[c.Key]
			}
			if size > 0 {
				fmt.Fprintf(w, "    %s (%s) %s → %s\n", c.Key, formatBytes(size), shortHash(c.Before), shortHash(c.After))
			} else {
				fmt.Fprintf(w, "    %s %s → %s\n", c.Key, shortHash(c.Before), shortHash(c.After))
			}
		}
	}
	if r.Diff.BroadContextCopy {
		fmt.Fprintln(w, "  Note: Dockerfile contains COPY/ADD of '.' (broad context copy); file attribution is best-effort.")
	}
	if r.Diff.SecretMounts > 0 || r.Diff.SSHMounts > 0 {
		fmt.Fprintf(w, "  Note: RUN mounts detected: secret=%d ssh=%d (may reduce cacheability).\n", r.Diff.SecretMounts, r.Diff.SSHMounts)
	}
	if r.InputsPrevious != nil && r.InputsCurrent != nil && r.InputsCurrent.BroadContextCopy && r.InputsPrevious.ContextMetaSHA != "" && r.InputsCurrent.ContextMetaSHA != "" && r.InputsPrevious.ContextMetaSHA != r.InputsCurrent.ContextMetaSHA {
		fmt.Fprintln(w, "  Broad context fingerprint changed (COPY .):")
		changes := diffStringMap(r.InputsPrevious.ContextTopFileSHA, r.InputsCurrent.ContextTopFileSHA, min(r.TopN, 10))
		for _, c := range changes {
			size := int64(0)
			if r.InputsCurrent != nil {
				size = r.InputsCurrent.ContextTopFileBytes[c.Key]
			}
			if size > 0 {
				fmt.Fprintf(w, "    %s (%s) %s → %s\n", c.Key, formatBytes(size), shortHash(c.Before), shortHash(c.After))
			} else {
				fmt.Fprintf(w, "    %s %s → %s\n", c.Key, shortHash(c.Before), shortHash(c.After))
			}
		}
	}

	misses := r.cacheMissVertices()
	if len(misses) > 0 {
		diffByName := map[string]cacheIntelCacheKeyDiff{}
		for _, d := range r.CacheKeyDiffs {
			if strings.TrimSpace(d.Name) == "" {
				continue
			}
			diffByName[strings.TrimSpace(d.Name)] = d
		}
		fmt.Fprintln(w, "  Cache-missed steps (best-effort reasons):")
		for i, v := range misses {
			if i >= r.TopN {
				break
			}
			reason := classifyCacheMiss(v, r.Diff)
			if d, ok := diffByName[strings.TrimSpace(v.name)]; ok {
				switch d.Type {
				case "definition_changed":
					if len(d.UpstreamNames) > 0 {
						reason = fmt.Sprintf("cache key changed vs last run (upstream: %s)", strings.Join(d.UpstreamNames, ", "))
					} else {
						reason = "cache key changed vs last run"
					}
				case "cache_evicted":
					reason = "cache key stable but result missing (cache evicted/pruned or cache import missing)"
				case "new_step":
					reason = "new step (no previous cache key)"
				}
			}
			dur := vertexDuration(v).Round(time.Millisecond)
			if dur < 0 {
				dur = 0
			}
			fmt.Fprintf(w, "    - %s (%s): %s\n", strings.TrimSpace(v.name), dur, reason)
		}
	}

	slow := r.topByDuration()
	if len(slow) > 0 {
		fmt.Fprintln(w, "  Slowest steps:")
		for i, v := range slow {
			if i >= min(r.TopN, 5) {
				break
			}
			dur := vertexDuration(v).Round(time.Millisecond)
			if dur < 0 {
				dur = 0
			}
			cacheTag := "miss"
			if v.cached {
				cacheTag = "hit"
			}
			fmt.Fprintf(w, "    - %s (%s, cache %s)\n", strings.TrimSpace(v.name), dur, cacheTag)
		}
	}

	ioSteps := r.topByBytes()
	if len(ioSteps) > 0 {
		fmt.Fprintln(w, "  Largest I/O steps (bytes from progress totals):")
		for i, v := range ioSteps {
			if i >= min(r.TopN, 5) {
				break
			}
			cacheTag := "miss"
			if v.cached {
				cacheTag = "hit"
			}
			fmt.Fprintf(w, "    - %s (%s, cache %s)\n", strings.TrimSpace(v.name), formatBytes(v.bytesTotal), cacheTag)
		}
	}

	if len(r.Layers) > 0 {
		fmt.Fprintln(w, "  Largest final layers (from OCI layout):")
		for i, layer := range r.Layers {
			if i >= min(r.TopN, 5) {
				break
			}
			if layer.ImageDigest != "" {
				fmt.Fprintf(w, "    - %s (%s) image %s\n", layer.Digest, formatBytes(layer.Size), shortHash(layer.ImageDigest))
			} else {
				fmt.Fprintf(w, "    - %s (%s)\n", layer.Digest, formatBytes(layer.Size))
			}
		}
	}
}

func (r cacheIntelReport) cacheMissVertices() []cacheIntelVertex {
	out := make([]cacheIntelVertex, 0)
	for _, v := range r.Vertices {
		if v.err != "" {
			continue
		}
		if v.completedAt == nil {
			continue
		}
		if v.cached {
			continue
		}
		if strings.TrimSpace(v.name) == "" {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		return vertexDuration(out[i]) > vertexDuration(out[j])
	})
	return out
}

func (r cacheIntelReport) topByDuration() []cacheIntelVertex {
	out := append([]cacheIntelVertex(nil), r.Vertices...)
	sort.Slice(out, func(i, j int) bool {
		return vertexDuration(out[i]) > vertexDuration(out[j])
	})
	return out
}

func (r cacheIntelReport) topByBytes() []cacheIntelVertex {
	out := make([]cacheIntelVertex, 0, len(r.Vertices))
	for _, v := range r.Vertices {
		if v.bytesTotal <= 0 {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].bytesTotal > out[j].bytesTotal
	})
	return out
}

func vertexDuration(v cacheIntelVertex) time.Duration {
	var started, completed time.Time
	if v.startedAt != nil {
		started = *v.startedAt
	}
	if v.completedAt != nil {
		completed = *v.completedAt
	}
	if !started.IsZero() && !completed.IsZero() {
		return completed.Sub(started)
	}
	return 0
}

func classifyCacheMiss(v cacheIntelVertex, diff cacheIntelDiff) string {
	name := strings.ToUpper(strings.TrimSpace(v.name))
	switch {
	case strings.Contains(name, "LOAD BUILD CONTEXT"):
		if len(diff.FilesChanged) > 0 || diff.DockerignoreChanged {
			return "build context changed"
		}
		return "no reusable cache found"
	case strings.HasPrefix(name, "ARG ") || strings.Contains(name, "\nARG "):
		if len(diff.BuildArgsChanged) > 0 {
			return "build args changed"
		}
		return "ARG-related cache key changed"
	case strings.HasPrefix(name, "COPY ") || strings.HasPrefix(name, "ADD "):
		if len(diff.FilesChanged) > 0 || diff.DockerfileChanged || diff.DockerignoreChanged {
			return "source files or Dockerfile changed"
		}
		return "COPY/ADD invalidated by upstream change"
	case strings.HasPrefix(name, "RUN "):
		if strings.Contains(name, "TYPE=SECRET") || strings.Contains(name, "SECRET") {
			return "secret mount present (may be non-cacheable or changed)"
		}
		return "RUN invalidated by upstream change"
	case strings.HasPrefix(name, "FROM "):
		return "base image changed or cache evicted"
	default:
		if len(diff.BuildArgsChanged) > 0 || len(diff.FilesChanged) > 0 || diff.DockerfileChanged || diff.SecretsChanged {
			return "inputs changed"
		}
		return "no reusable cache found"
	}
}

func diffCacheIntelInputs(prev, cur *cacheIntelInputsSnapshot, topN int) cacheIntelDiff {
	var out cacheIntelDiff
	if prev == nil || cur == nil {
		return out
	}
	out.BroadContextCopy = cur.BroadContextCopy
	out.SecretMounts = cur.SecretMounts
	out.SSHMounts = cur.SSHMounts
	if cur.BroadContextCopy && prev.ContextMetaSHA != "" && cur.ContextMetaSHA != "" && prev.ContextMetaSHA != cur.ContextMetaSHA {
		out.DockerignoreChanged = out.DockerignoreChanged || (prev.DockerignoreSHA != "" && cur.DockerignoreSHA != "" && prev.DockerignoreSHA != cur.DockerignoreSHA)
	}
	if prev.DockerfileSHA != "" && cur.DockerfileSHA != "" && prev.DockerfileSHA != cur.DockerfileSHA {
		out.DockerfileChanged = true
	}
	if prev.DockerignoreSHA != "" && cur.DockerignoreSHA != "" && prev.DockerignoreSHA != cur.DockerignoreSHA {
		out.DockerignoreChanged = true
	}

	out.BuildArgsChanged = diffStringMap(prev.BuildArgSHA, cur.BuildArgSHA, topN)
	if !stringSliceEqual(prev.SecretIDs, cur.SecretIDs) {
		out.SecretsChanged = true
	}
	out.FilesChanged = diffFileMap(prev, cur, topN)
	return out
}

func diffStringMap(before, after map[string]string, topN int) []cacheIntelChangedValue {
	keys := map[string]struct{}{}
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range after {
		keys[k] = struct{}{}
	}
	changed := make([]cacheIntelChangedValue, 0)
	for k := range keys {
		b := before[k]
		a := after[k]
		if b == a {
			continue
		}
		changed = append(changed, cacheIntelChangedValue{Key: k, Before: b, After: a})
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Key < changed[j].Key })
	if topN > 0 && len(changed) > topN {
		return changed[:topN]
	}
	return changed
}

func diffFileMap(prev, cur *cacheIntelInputsSnapshot, topN int) []cacheIntelChangedValue {
	before := map[string]string{}
	after := map[string]string{}
	if prev != nil {
		before = prev.FileSHA
	}
	if cur != nil {
		after = cur.FileSHA
	}
	changed := diffStringMap(before, after, 0)
	sort.Slice(changed, func(i, j int) bool {
		a := int64(0)
		b := int64(0)
		if cur != nil {
			a = cur.FileBytes[changed[i].Key]
			b = cur.FileBytes[changed[j].Key]
		}
		if a == b {
			return changed[i].Key < changed[j].Key
		}
		return a > b
	})
	if topN > 0 && len(changed) > topN {
		return changed[:topN]
	}
	return changed
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pathSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	err = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		i, err := d.Info()
		if err != nil {
			return err
		}
		total += i.Size()
		return nil
	})
	return total, err
}

func (c *cacheIntelCollector) snapshotPath(relDockerfile string) string {
	key := sha256.Sum256([]byte(strings.ToLower(c.contextAbs) + "\n" + strings.ToLower(relDockerfile)))
	dir := filepath.Join(c.cacheDir, "ktl-cache-intel")
	return filepath.Join(dir, hex.EncodeToString(key[:])+".json")
}

func (c *cacheIntelCollector) graphPath(relDockerfile string) string {
	base := c.snapshotPath(relDockerfile)
	ext := filepath.Ext(base)
	if ext == "" {
		return base + "-graph.json"
	}
	return strings.TrimSuffix(base, ext) + "-graph" + ext
}

func (c *cacheIntelCollector) loadPreviousSnapshot(ctx context.Context, relDockerfile string) (*cacheIntelInputsSnapshot, error) {
	_ = ctx
	if c.cacheDir == "" {
		return nil, errors.New("cache dir empty")
	}
	path := c.snapshotPath(relDockerfile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap cacheIntelInputsSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	if snap.Version == 0 {
		snap.Version = 1
	}
	return &snap, nil
}

func (c *cacheIntelCollector) loadPreviousGraph(ctx context.Context, relDockerfile string) (*cacheIntelSolveGraph, error) {
	_ = ctx
	if c.cacheDir == "" {
		return nil, errors.New("cache dir empty")
	}
	path := c.graphPath(relDockerfile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var g cacheIntelSolveGraph
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, err
	}
	if g.Version == 0 {
		g.Version = 1
	}
	return &g, nil
}

func (c *cacheIntelCollector) writeCurrentSnapshot(ctx context.Context) error {
	_ = ctx
	if c.cacheDir == "" || c.current == nil {
		return nil
	}
	dir := filepath.Dir(c.snapshotPath(c.current.DockerfileRel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := c.snapshotPath(c.current.DockerfileRel)
	payload, err := json.MarshalIndent(c.current, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func (c *cacheIntelCollector) writeCurrentGraph(ctx context.Context, relDockerfile string, graph cacheIntelSolveGraph) error {
	_ = ctx
	if c.cacheDir == "" {
		return nil
	}
	graph.Version = 1
	if graph.TakenAtUTC.IsZero() {
		graph.TakenAtUTC = time.Now().UTC()
	}
	path := c.graphPath(relDockerfile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func buildCacheIntelInputsSnapshot(ctx context.Context, contextAbs, dockerfileAbs, dockerfileRel string, buildArgs map[string]string, secretIDs []string) (*cacheIntelInputsSnapshot, error) {
	_ = ctx
	if contextAbs == "" || dockerfileAbs == "" {
		return nil, errors.New("missing paths")
	}
	snap := &cacheIntelInputsSnapshot{
		Version:             1,
		TakenAtUTC:          time.Now().UTC(),
		ContextAbs:          contextAbs,
		DockerfileRel:       dockerfileRel,
		BuildArgSHA:         map[string]string{},
		FileSHA:             map[string]string{},
		FileBytes:           map[string]int64{},
		ContextTopFileSHA:   map[string]string{},
		ContextTopFileBytes: map[string]int64{},
	}
	dfHash, err := sha256File(dockerfileAbs)
	if err == nil {
		snap.DockerfileSHA = dfHash
	}
	dockerignorePath := filepath.Join(contextAbs, ".dockerignore")
	if h, err := sha256File(dockerignorePath); err == nil {
		snap.DockerignoreSHA = h
	}

	keys := make([]string, 0, len(buildArgs))
	for k := range buildArgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		snap.BuildArgSHA[k] = sha256String(buildArgs[k])
	}

	secretIDs = append([]string(nil), secretIDs...)
	for i := range secretIDs {
		secretIDs[i] = strings.TrimSpace(secretIDs[i])
	}
	secretIDs = filterNonEmpty(secretIDs)
	sort.Strings(secretIDs)
	snap.SecretIDs = secretIDs

	paths, broadCopy, secretMounts, sshMounts := referencedBuildContextPaths(contextAbs, dockerfileAbs)
	snap.BroadContextCopy = broadCopy
	snap.SecretMounts = secretMounts
	snap.SSHMounts = sshMounts
	for _, p := range paths {
		abs := filepath.Join(contextAbs, p)
		h, err := sha256Path(abs)
		if err != nil {
			continue
		}
		snap.FileSHA[p] = h
		if size, err := pathSize(abs); err == nil {
			snap.FileBytes[p] = size
		}
	}
	if broadCopy {
		meta, top, err := snapshotBroadContext(contextAbs, filepath.Join(contextAbs, ".dockerignore"), 50)
		if err == nil {
			snap.ContextMetaSHA = meta
			for _, entry := range top {
				snap.ContextTopFileSHA[entry.Path] = entry.SHA
				snap.ContextTopFileBytes[entry.Path] = entry.Bytes
			}
		}
	}
	return snap, nil
}

func referencedBuildContextPaths(contextAbs, dockerfileAbs string) ([]string, bool, int, int) {
	raw, err := os.ReadFile(dockerfileAbs)
	if err != nil {
		return nil, false, 0, 0
	}
	refs, broadCopy, secretMounts, sshMounts := parseDockerfileRefs(string(raw))
	expanded := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
			continue
		}
		ref = strings.TrimPrefix(ref, "./")
		ref = filepath.Clean(ref)
		if strings.HasPrefix(ref, "..") || filepath.IsAbs(ref) {
			continue
		}
		if strings.ContainsAny(ref, "*?[") {
			matches, _ := filepath.Glob(filepath.Join(contextAbs, ref))
			for _, match := range matches {
				rel, err := filepath.Rel(contextAbs, match)
				if err != nil {
					continue
				}
				rel = filepath.ToSlash(rel)
				if rel == "" || strings.HasPrefix(rel, "..") {
					continue
				}
				if _, ok := seen[rel]; ok {
					continue
				}
				seen[rel] = struct{}{}
				expanded = append(expanded, rel)
			}
			continue
		}
		ref = filepath.ToSlash(ref)
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		expanded = append(expanded, ref)
	}
	sort.Strings(expanded)
	return expanded, broadCopy, secretMounts, sshMounts
}

func parseDockerfileCopyAddSources(dockerfile string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(dockerfile))
	var logical string
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if idx := strings.Index(trimmed, "#"); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
		if strings.HasSuffix(trimmed, "\\") {
			logical += strings.TrimSpace(strings.TrimSuffix(trimmed, "\\")) + " "
			continue
		}
		logical += trimmed
		instr, rest, ok := splitDockerfileInstruction(logical)
		logical = ""
		if !ok {
			continue
		}
		switch strings.ToUpper(instr) {
		case "COPY", "ADD":
			out = append(out, extractCopyAddSources(rest)...)
		}
	}
	return out
}

func parseDockerfileRefs(dockerfile string) ([]string, bool, int, int) {
	refs := parseDockerfileCopyAddSources(dockerfile)

	broadCopy := false
	for _, r := range refs {
		switch strings.TrimSpace(r) {
		case ".", "./":
			broadCopy = true
		}
	}

	secretMounts := 0
	sshMounts := 0
	sc := bufio.NewScanner(strings.NewReader(dockerfile))
	var logical string
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if idx := strings.Index(trimmed, "#"); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
		if strings.HasSuffix(trimmed, "\\") {
			logical += strings.TrimSpace(strings.TrimSuffix(trimmed, "\\")) + " "
			continue
		}
		logical += trimmed
		instr, rest, ok := splitDockerfileInstruction(logical)
		logical = ""
		if !ok {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(instr)) != "RUN" {
			continue
		}
		upper := strings.ToUpper(rest)
		if strings.Contains(upper, "TYPE=SECRET") {
			secretMounts++
		}
		if strings.Contains(upper, "TYPE=SSH") {
			sshMounts++
		}
	}

	return refs, broadCopy, secretMounts, sshMounts
}

func splitDockerfileInstruction(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	instr := fields[0]
	rest := strings.TrimSpace(line[len(instr):])
	return instr, rest, true
}

func extractCopyAddSources(rest string) []string {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil
	}
	fields := splitDockerfileArgs(rest)
	filtered := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.HasPrefix(f, "--") {
			continue
		}
		filtered = append(filtered, f)
	}
	if len(filtered) < 2 {
		return nil
	}
	return filtered[:len(filtered)-1]
}

func splitDockerfileArgs(s string) []string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(s), &arr); err == nil {
			return arr
		}
	}
	var out []string
	var cur strings.Builder
	inQuote := rune(0)
	escape := false
	for _, r := range s {
		if escape {
			cur.WriteRune(r)
			escape = false
			continue
		}
		if r == '\\' {
			escape = true
			continue
		}
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
				continue
			}
			cur.WriteRune(r)
			continue
		}
		if r == '"' || r == '\'' {
			inQuote = r
			continue
		}
		if r == ' ' || r == '\t' {
			if cur.Len() == 0 {
				continue
			}
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

func sha256String(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha256Path(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return sha256File(path)
	}
	h := sha256.New()
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		fh, err := sha256File(p)
		if err != nil {
			return err
		}
		_, _ = h.Write([]byte(fh))
		_, _ = h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func filterNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func shortHash(full string) string {
	full = strings.TrimSpace(full)
	if len(full) <= 12 {
		return full
	}
	return full[:12]
}

func formatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 6 {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}[exp]
	if value >= 10 {
		return fmt.Sprintf("%.0f%s", value, suffix)
	}
	return fmt.Sprintf("%.1f%s", value, suffix)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ buildkit.ProgressObserver = (*cacheIntelCollector)(nil)

type contextFileSnapshot struct {
	Path  string
	Bytes int64
	SHA   string
}

func snapshotBroadContext(contextAbs, dockerignorePath string, topFiles int) (string, []contextFileSnapshot, error) {
	if topFiles <= 0 {
		topFiles = 50
	}

	patterns := []string{}
	if raw, err := os.ReadFile(dockerignorePath); err == nil {
		if p, err := ignorefile.ReadAll(strings.NewReader(string(raw))); err == nil {
			patterns = p
		}
	}
	matcher, err := patternmatcher.New(patterns)
	if err != nil {
		return "", nil, err
	}

	type fileEntry struct {
		rel   string
		size  int64
		mtime int64
	}
	files := make([]fileEntry, 0, 2048)
	err = filepath.WalkDir(contextAbs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "dist" || name == "bin" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(contextAbs, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "" || strings.HasPrefix(rel, "..") {
			return nil
		}
		ignored, err := matcher.MatchesOrParentMatches(rel)
		if err == nil && ignored {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, fileEntry{rel: rel, size: info.Size(), mtime: info.ModTime().UnixNano()})
		return nil
	})
	if err != nil {
		return "", nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].rel == files[j].rel {
			return files[i].size > files[j].size
		}
		return files[i].rel < files[j].rel
	})
	metaHasher := sha256.New()
	for _, f := range files {
		_, _ = metaHasher.Write([]byte(f.rel))
		_, _ = metaHasher.Write([]byte{0})
		_, _ = metaHasher.Write([]byte(fmt.Sprintf("%d", f.size)))
		_, _ = metaHasher.Write([]byte{0})
		_, _ = metaHasher.Write([]byte(fmt.Sprintf("%d", f.mtime)))
		_, _ = metaHasher.Write([]byte{0})
	}
	meta := hex.EncodeToString(metaHasher.Sum(nil))

	sort.Slice(files, func(i, j int) bool {
		if files[i].size == files[j].size {
			return files[i].rel < files[j].rel
		}
		return files[i].size > files[j].size
	})
	if len(files) > topFiles {
		files = files[:topFiles]
	}

	out := make([]contextFileSnapshot, 0, len(files))
	for _, f := range files {
		abs := filepath.Join(contextAbs, filepath.FromSlash(f.rel))
		h, err := sha256File(abs)
		if err != nil {
			continue
		}
		out = append(out, contextFileSnapshot{Path: f.rel, Bytes: f.size, SHA: h})
	}
	return meta, out, nil
}

func buildSolveGraph(vertices []cacheIntelVertex) cacheIntelSolveGraph {
	g := cacheIntelSolveGraph{
		Version:    1,
		TakenAtUTC: time.Now().UTC(),
		Vertices:   make([]cacheIntelSolveVertex, 0, len(vertices)),
	}
	for _, v := range vertices {
		name := strings.TrimSpace(v.name)
		inputs := append([]string(nil), v.inputs...)
		sort.Strings(inputs)
		start := (*time.Time)(nil)
		complete := (*time.Time)(nil)
		if v.startedAt != nil {
			t := v.startedAt.UTC()
			start = &t
		}
		if v.completedAt != nil {
			t := v.completedAt.UTC()
			complete = &t
		}
		g.Vertices = append(g.Vertices, cacheIntelSolveVertex{
			Digest:         strings.TrimSpace(v.id),
			Name:           name,
			Inputs:         inputs,
			Cached:         v.cached,
			Error:          strings.TrimSpace(v.err),
			BytesTotal:     v.bytesTotal,
			StartedAtUTC:   start,
			CompletedAtUTC: complete,
		})
	}
	sort.Slice(g.Vertices, func(i, j int) bool {
		if g.Vertices[i].Name == g.Vertices[j].Name {
			return g.Vertices[i].Digest < g.Vertices[j].Digest
		}
		return g.Vertices[i].Name < g.Vertices[j].Name
	})
	return g
}

func diffSolveGraphs(prev, cur *cacheIntelSolveGraph, topN int) []cacheIntelCacheKeyDiff {
	if cur == nil {
		return nil
	}
	prevByName := map[string]cacheIntelSolveVertex{}
	prevByDigest := map[string]cacheIntelSolveVertex{}
	if prev != nil {
		for _, v := range prev.Vertices {
			name := strings.TrimSpace(v.Name)
			if name != "" {
				prevByName[name] = v
			}
			if d := strings.TrimSpace(v.Digest); d != "" {
				prevByDigest[d] = v
			}
		}
	}
	curByName := map[string]cacheIntelSolveVertex{}
	curByDigest := map[string]cacheIntelSolveVertex{}
	for _, v := range cur.Vertices {
		name := strings.TrimSpace(v.Name)
		if name != "" {
			curByName[name] = v
		}
		if d := strings.TrimSpace(v.Digest); d != "" {
			curByDigest[d] = v
		}
	}

	names := make([]string, 0, len(curByName))
	for name := range curByName {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]cacheIntelCacheKeyDiff, 0)
	for _, name := range names {
		curV := curByName[name]
		prevV, ok := prevByName[name]
		if !ok {
			if !curV.Cached {
				out = append(out, cacheIntelCacheKeyDiff{
					Name:      name,
					CurDigest: curV.Digest,
					Type:      "new_step",
				})
			}
			continue
		}
		if strings.TrimSpace(prevV.Digest) != strings.TrimSpace(curV.Digest) {
			upstreams := upstreamChanges(prevV, curV, prevByDigest, curByDigest)
			out = append(out, cacheIntelCacheKeyDiff{
				Name:          name,
				PrevDigest:    prevV.Digest,
				CurDigest:     curV.Digest,
				Type:          "definition_changed",
				UpstreamNames: upstreams,
			})
			continue
		}
		if prevV.Cached && !curV.Cached {
			out = append(out, cacheIntelCacheKeyDiff{
				Name:       name,
				PrevDigest: prevV.Digest,
				CurDigest:  curV.Digest,
				Type:       "cache_evicted",
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Type == out[j].Type {
			return out[i].Name < out[j].Name
		}
		return out[i].Type < out[j].Type
	})
	if topN > 0 && len(out) > topN {
		return out[:topN]
	}
	return out
}

func upstreamChanges(prev, cur cacheIntelSolveVertex, prevByDigest, curByDigest map[string]cacheIntelSolveVertex) []string {
	prevSet := map[string]struct{}{}
	for _, in := range prev.Inputs {
		in = strings.TrimSpace(in)
		if in == "" {
			continue
		}
		prevSet[in] = struct{}{}
	}
	curSet := map[string]struct{}{}
	for _, in := range cur.Inputs {
		in = strings.TrimSpace(in)
		if in == "" {
			continue
		}
		curSet[in] = struct{}{}
	}

	changedDigests := make([]string, 0)
	for d := range curSet {
		if _, ok := prevSet[d]; !ok {
			changedDigests = append(changedDigests, d)
		}
	}
	for d := range prevSet {
		if _, ok := curSet[d]; !ok {
			changedDigests = append(changedDigests, d)
		}
	}
	sort.Strings(changedDigests)

	names := make([]string, 0, len(changedDigests))
	seen := map[string]struct{}{}
	for _, d := range changedDigests {
		if v, ok := curByDigest[d]; ok && strings.TrimSpace(v.Name) != "" {
			n := strings.TrimSpace(v.Name)
			if _, dup := seen[n]; !dup {
				seen[n] = struct{}{}
				names = append(names, n)
			}
			continue
		}
		if v, ok := prevByDigest[d]; ok && strings.TrimSpace(v.Name) != "" {
			n := strings.TrimSpace(v.Name)
			if _, dup := seen[n]; !dup {
				seen[n] = struct{}{}
				names = append(names, n)
			}
		}
	}
	sort.Strings(names)
	if len(names) > 5 {
		names = names[:5]
	}
	return names
}
