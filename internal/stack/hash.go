// File: internal/stack/hash.go
// Brief: Effective input hashing for drift detection and resume safety.

package stack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/version"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
)

type EffectiveInputHashOptions struct {
	StackRoot             string
	IncludeValuesContents bool
	StackGitIdentity      *GitIdentity
}

func ComputeEffectiveInputHash(stackRoot string, n *ResolvedRelease, includeValuesContents bool) (string, *EffectiveInput, error) {
	return ComputeEffectiveInputHashWithOptions(n, EffectiveInputHashOptions{
		StackRoot:             stackRoot,
		IncludeValuesContents: includeValuesContents,
	})
}

func ComputeEffectiveInputHashWithOptions(n *ResolvedRelease, opts EffectiveInputHashOptions) (string, *EffectiveInput, error) {
	stackRoot := strings.TrimSpace(opts.StackRoot)
	if stackRoot == "" {
		stackRoot = "."
	}
	if n == nil {
		return "", nil, fmt.Errorf("nil node")
	}

	gid := GitIdentity{}
	if opts.StackGitIdentity != nil {
		gid = *opts.StackGitIdentity
	} else {
		var err error
		gid, err = GitIdentityForRoot(stackRoot)
		if err != nil {
			return "", nil, err
		}
	}

	values, err := digestValues(n.Values, opts.IncludeValuesContents)
	if err != nil {
		return "", nil, err
	}

	settings := cli.New()
	chartInput, err := digestChart(n.Chart, n.ChartVersion, settings)
	if err != nil {
		return "", nil, err
	}
	// If the config didn't pin a version and we resolved one, seal the version into the plan so
	// future runs don't accidentally pick up "latest".
	if strings.TrimSpace(n.ChartVersion) == "" && strings.TrimSpace(chartInput.ResolvedVersion) != "" && !isExistingPath(n.Chart) {
		n.ChartVersion = chartInput.ResolvedVersion
		chartInput.Version = n.ChartVersion
	}

	setDigest := digestSet(n.Set)
	clusterDigest := digestCluster(n)
	apply := digestApply(n)
	deleteInput := digestDelete(n)

	input := &EffectiveInput{
		APIVersion: "ktl.dev/stack-effective-input/v1",

		StackGitCommit: gid.Commit,
		StackGitDirty:  gid.Dirty,

		KtlVersion:   version.Version,
		KtlGitCommit: version.GitCommit,

		NodeID: n.ID,

		Chart: chartInput,

		Values: values,

		SetDigest:     setDigest,
		ClusterDigest: clusterDigest,

		Apply:  apply,
		Delete: deleteInput,
	}

	type effectiveInputHashV1 struct {
		APIVersion string `json:"apiVersion"`

		StackGitCommit string `json:"stackGitCommit,omitempty"`
		StackGitDirty  bool   `json:"stackGitDirty,omitempty"`

		KtlVersion   string `json:"ktlVersion,omitempty"`
		KtlGitCommit string `json:"ktlGitCommit,omitempty"`

		NodeID string `json:"nodeId"`

		ChartDigest          string `json:"chartDigest,omitempty"`
		ChartVersion         string `json:"chartVersion,omitempty"`
		ChartResolvedVersion string `json:"chartResolvedVersion,omitempty"`

		ValuesDigests []string `json:"valuesDigests,omitempty"`

		SetDigest     string `json:"setDigest,omitempty"`
		ClusterDigest string `json:"clusterDigest,omitempty"`

		Apply  EffectiveApplyInput  `json:"apply"`
		Delete EffectiveDeleteInput `json:"delete"`
	}

	valuesDigests := make([]string, 0, len(input.Values))
	for _, v := range input.Values {
		valuesDigests = append(valuesDigests, v.Digest)
	}

	hashInput := effectiveInputHashV1{
		APIVersion: "ktl.dev/stack-effective-input-hash/v1",

		StackGitCommit: input.StackGitCommit,
		StackGitDirty:  input.StackGitDirty,

		KtlVersion:   input.KtlVersion,
		KtlGitCommit: input.KtlGitCommit,

		NodeID: input.NodeID,

		ChartDigest:          input.Chart.Digest,
		ChartVersion:         input.Chart.Version,
		ChartResolvedVersion: input.Chart.ResolvedVersion,

		ValuesDigests: valuesDigests,

		SetDigest:     input.SetDigest,
		ClusterDigest: input.ClusterDigest,

		Apply:  input.Apply,
		Delete: input.Delete,
	}

	raw, err := json.Marshal(hashInput)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), input, nil
}

func isLocalPath(p string) bool {
	// Values in stack v1 are filesystem paths; keep an escape hatch anyway.
	return p != "" && !strings.Contains(p, "://")
}

func isExistingPath(p string) bool {
	pp := strings.TrimSpace(p)
	if pp == "" {
		return false
	}
	if _, err := os.Stat(pp); err == nil {
		return true
	}
	return false
}

func digestValues(paths []string, includeContents bool) ([]FileDigest, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]FileDigest, 0, len(paths))
	for _, p := range paths {
		d := FileDigest{Path: p}
		if includeContents && isLocalPath(p) {
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, fmt.Errorf("read values file %s: %w", p, err)
			}
			sum := sha256.Sum256(b)
			d.Digest = "sha256:" + hex.EncodeToString(sum[:])
		}
		out = append(out, d)
	}
	return out, nil
}

func digestChart(chartRef string, chartVersion string, settings *cli.EnvSettings) (EffectiveChartInput, error) {
	ref := strings.TrimSpace(chartRef)
	if ref == "" {
		return EffectiveChartInput{}, fmt.Errorf("chart ref is required")
	}
	v := strings.TrimSpace(chartVersion)

	chartPath := ref
	if !isExistingPath(ref) {
		cpo := action.ChartPathOptions{Version: v}
		located, err := cpo.LocateChart(ref, settings)
		if err != nil {
			return EffectiveChartInput{}, fmt.Errorf("locate chart %s: %w", ref, err)
		}
		chartPath = located
	}

	ch, err := loader.Load(chartPath)
	if err != nil {
		return EffectiveChartInput{}, fmt.Errorf("load chart %s: %w", chartPath, err)
	}

	resolvedVersion := ""
	if ch.Metadata != nil {
		resolvedVersion = strings.TrimSpace(ch.Metadata.Version)
	}

	return EffectiveChartInput{
		Ref:             ref,
		Version:         v,
		ResolvedVersion: resolvedVersion,
		Digest:          digestHelmChart(ch),
	}, nil
}

func digestHelmChart(ch *chart.Chart) string {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("ktl.stack-chart.v1")
	if ch == nil {
		return "sha256:" + hex.EncodeToString(h.Sum(nil))
	}
	if ch.Metadata != nil {
		write("name:" + ch.Metadata.Name)
		write("version:" + ch.Metadata.Version)
		write("apiVersion:" + ch.Metadata.APIVersion)
	}
	files := append([]*chart.File(nil), ch.Raw...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	for _, f := range files {
		sum := sha256.Sum256(f.Data)
		write(f.Name)
		write(hex.EncodeToString(sum[:]))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func digestSet(m map[string]string) string {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("ktl.stack-set.v1")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		write(k)
		write(m[k])
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func digestCluster(n *ResolvedRelease) string {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("ktl.stack-cluster.v1")
	write(n.Cluster.Name)
	write(n.Cluster.Context)
	write(n.Namespace)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func digestApply(n *ResolvedRelease) EffectiveApplyInput {
	timeout := 5 * time.Minute
	if n.Apply.Timeout != nil {
		timeout = *n.Apply.Timeout
	}
	wait := true
	if n.Apply.Wait != nil {
		wait = *n.Apply.Wait
	}
	atomic := true
	if n.Apply.Atomic != nil {
		atomic = *n.Apply.Atomic
	}

	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("ktl.stack-apply.v1")
	write(fmt.Sprintf("atomic=%t", atomic))
	write(fmt.Sprintf("wait=%t", wait))
	write("timeout=" + timeout.String())

	return EffectiveApplyInput{
		Atomic:  atomic,
		Wait:    wait,
		Timeout: timeout.String(),
		Digest:  "sha256:" + hex.EncodeToString(h.Sum(nil)),
	}
}

func digestDelete(n *ResolvedRelease) EffectiveDeleteInput {
	timeout := 5 * time.Minute
	if n.Delete.Timeout != nil {
		timeout = *n.Delete.Timeout
	}
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("ktl.stack-delete.v1")
	write("timeout=" + timeout.String())
	return EffectiveDeleteInput{
		Timeout: timeout.String(),
		Digest:  "sha256:" + hex.EncodeToString(h.Sum(nil)),
	}
}
