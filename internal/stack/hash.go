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

	"github.com/kubekattle/ktl/internal/version"
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
	verify := digestVerify(n)

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
		Verify: verify,
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
		Verify EffectiveVerifyInput `json:"verify"`
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
		Verify: input.Verify,
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
	createNamespace := false
	if n.Apply.CreateNamespace != nil {
		createNamespace = *n.Apply.CreateNamespace
	}

	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("ktl.stack-apply.v1")
	write(fmt.Sprintf("atomic=%t", atomic))
	write(fmt.Sprintf("wait=%t", wait))
	write(fmt.Sprintf("createNamespace=%t", createNamespace))
	write("timeout=" + timeout.String())

	return EffectiveApplyInput{
		Atomic:          atomic,
		Wait:            wait,
		CreateNamespace: createNamespace,
		Timeout:         timeout.String(),
		Digest:          "sha256:" + hex.EncodeToString(h.Sum(nil)),
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

func digestVerify(n *ResolvedRelease) EffectiveVerifyInput {
	enabled := false
	if n.Verify.Enabled != nil {
		enabled = *n.Verify.Enabled
	}
	failOnWarnings := true
	if n.Verify.FailOnWarnings != nil {
		failOnWarnings = *n.Verify.FailOnWarnings
	}
	warnOnly := false
	if n.Verify.WarnOnly != nil {
		warnOnly = *n.Verify.WarnOnly
	}
	window := 15 * time.Minute
	if n.Verify.EventsWindow != nil && *n.Verify.EventsWindow > 0 {
		window = *n.Verify.EventsWindow
	}
	timeout := 2 * time.Minute
	if n.Verify.Timeout != nil && *n.Verify.Timeout > 0 {
		timeout = *n.Verify.Timeout
	}

	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("ktl.stack-verify.v1")
	write(fmt.Sprintf("enabled=%t", enabled))
	write(fmt.Sprintf("failOnWarnings=%t", failOnWarnings))
	write(fmt.Sprintf("warnOnly=%t", warnOnly))
	write("eventsWindow=" + window.String())
	write("timeout=" + timeout.String())
	if len(n.Verify.DenyReasons) > 0 {
		for _, r := range n.Verify.DenyReasons {
			write("denyReason=" + strings.ToLower(strings.TrimSpace(r)))
		}
	}
	if len(n.Verify.AllowReasons) > 0 {
		for _, r := range n.Verify.AllowReasons {
			write("allowReason=" + strings.ToLower(strings.TrimSpace(r)))
		}
	}
	if len(n.Verify.RequireConditions) > 0 {
		// Keep deterministic order.
		reqs := append([]VerifyConditionRequirement(nil), n.Verify.RequireConditions...)
		sort.Slice(reqs, func(i, j int) bool {
			a := strings.ToLower(reqs[i].Group) + "/" + strings.ToLower(reqs[i].Kind) + "/" + strings.ToLower(reqs[i].ConditionType)
			b := strings.ToLower(reqs[j].Group) + "/" + strings.ToLower(reqs[j].Kind) + "/" + strings.ToLower(reqs[j].ConditionType)
			if a != b {
				return a < b
			}
			return strings.ToLower(reqs[i].RequireStatus) < strings.ToLower(reqs[j].RequireStatus)
		})
		for _, r := range reqs {
			write("req=" + strings.ToLower(strings.TrimSpace(r.Group)) + "/" + strings.ToLower(strings.TrimSpace(r.Kind)))
			write("cond=" + strings.ToLower(strings.TrimSpace(r.ConditionType)) + "=" + strings.ToLower(strings.TrimSpace(r.RequireStatus)))
			write(fmt.Sprintf("allowMissing=%t", r.AllowMissing))
		}
	}

	return EffectiveVerifyInput{
		Enabled:        enabled,
		FailOnWarnings: failOnWarnings,
		WarnOnly:       warnOnly,
		EventsWindow:   window.String(),
		Timeout:        timeout.String(),
		DenyReasons:    append([]string(nil), n.Verify.DenyReasons...),
		AllowReasons:   append([]string(nil), n.Verify.AllowReasons...),
		RequireConditions: func() []VerifyConditionRequirement {
			if len(n.Verify.RequireConditions) == 0 {
				return nil
			}
			return append([]VerifyConditionRequirement(nil), n.Verify.RequireConditions...)
		}(),
		Digest: "sha256:" + hex.EncodeToString(h.Sum(nil)),
	}
}
