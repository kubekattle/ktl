package stack

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
)

type InputBundleManifest struct {
	APIVersion string `json:"apiVersion"`
	CreatedAt  string `json:"createdAt"`
	PlanHash   string `json:"planHash,omitempty"`

	Nodes []InputBundleNode `json:"nodes"`
}

type InputBundleNode struct {
	ID       string             `json:"id"`
	ChartDir string             `json:"chartDir"`
	Values   []InputBundleValue `json:"values,omitempty"`
}

type InputBundleValue struct {
	OriginalPath string `json:"originalPath,omitempty"`
	BundlePath   string `json:"bundlePath"`
	Digest       string `json:"digest,omitempty"`
}

// WriteInputBundle writes a portable .tar.gz containing the Helm chart contents and values files
// required by the provided nodes. The returned digest is for the compressed bundle bytes.
func WriteInputBundle(ctx context.Context, outPath string, planHash string, nodes []*ResolvedRelease) (*InputBundleManifest, string, error) {
	outPath = strings.TrimSpace(outPath)
	if outPath == "" {
		return nil, "", fmt.Errorf("bundle path is required")
	}
	tmp := outPath + ".tmp"

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, "", err
	}

	f, err := os.Create(tmp)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = f.Close() }()

	hash := sha256.New()
	mw := io.MultiWriter(f, hash)
	gw := gzip.NewWriter(mw)
	gw.Header.ModTime = time.Unix(0, 0).UTC()
	gw.Header.OS = 255 // unknown; keep stable across hosts
	tw := tar.NewWriter(gw)

	now := time.Now().UTC()
	manifest := &InputBundleManifest{
		APIVersion: "ktl.dev/stack-input-bundle/v1",
		CreatedAt:  now.Format(time.RFC3339Nano),
		PlanHash:   strings.TrimSpace(planHash),
	}

	// Deterministic order.
	sorted := append([]*ResolvedRelease(nil), nodes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	settings := cli.New()
	for _, n := range sorted {
		if err := ctx.Err(); err != nil {
			_ = tw.Close()
			_ = gw.Close()
			_ = f.Close()
			_ = os.Remove(tmp)
			return nil, "", err
		}
		nodeKey := bundleNodeKey(n.ID)
		chartDir := path.Join("nodes", nodeKey, "chart")
		valuesDir := path.Join("nodes", nodeKey, "values")

		ch, err := loadHelmChartForBundle(n.Chart, n.ChartVersion, settings)
		if err != nil {
			_ = tw.Close()
			_ = gw.Close()
			_ = f.Close()
			_ = os.Remove(tmp)
			return nil, "", err
		}
		if err := writeChartToTar(tw, chartDir, ch); err != nil {
			_ = tw.Close()
			_ = gw.Close()
			_ = f.Close()
			_ = os.Remove(tmp)
			return nil, "", err
		}

		nodeEntry := InputBundleNode{
			ID:       n.ID,
			ChartDir: chartDir,
		}

		for i, vp := range n.Values {
			b, err := os.ReadFile(vp)
			if err != nil {
				_ = tw.Close()
				_ = gw.Close()
				_ = f.Close()
				_ = os.Remove(tmp)
				return nil, "", fmt.Errorf("read values file %s: %w", vp, err)
			}
			sum := sha256.Sum256(b)
			dst := path.Join(valuesDir, fmt.Sprintf("%02d_%s", i, filepath.Base(vp)))
			if err := writeFileToTar(tw, dst, b); err != nil {
				_ = tw.Close()
				_ = gw.Close()
				_ = f.Close()
				_ = os.Remove(tmp)
				return nil, "", err
			}
			nodeEntry.Values = append(nodeEntry.Values, InputBundleValue{
				OriginalPath: vp,
				BundlePath:   dst,
				Digest:       "sha256:" + hex.EncodeToString(sum[:]),
			})
		}
		manifest.Nodes = append(manifest.Nodes, nodeEntry)
	}

	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		_ = tw.Close()
		_ = gw.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, "", err
	}
	if err := writeFileToTar(tw, "manifest.json", rawManifest); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, "", err
	}

	if err := tw.Close(); err != nil {
		_ = gw.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, "", err
	}
	if err := gw.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, "", err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return nil, "", err
	}
	if err := os.Rename(tmp, outPath); err != nil {
		_ = os.Remove(tmp)
		return nil, "", err
	}
	return manifest, "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func ExtractInputBundle(ctx context.Context, bundlePath string, dstDir string) (*InputBundleManifest, error) {
	bundlePath = strings.TrimSpace(bundlePath)
	dstDir = strings.TrimSpace(dstDir)
	if bundlePath == "" || dstDir == "" {
		return nil, fmt.Errorf("bundlePath and dstDir are required")
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, err
	}

	f, err := os.Open(bundlePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(hdr.Name)
		if name == "" {
			continue
		}
		clean := path.Clean(name)
		if clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || strings.HasPrefix(clean, "/") {
			return nil, fmt.Errorf("invalid bundle path %q", name)
		}
		target := filepath.Join(dstDir, filepath.FromSlash(clean))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, err
			}
			out, err := os.Create(target)
			if err != nil {
				return nil, err
			}
			if _, err := io.CopyN(out, tr, hdr.Size); err != nil {
				_ = out.Close()
				return nil, err
			}
			if err := out.Close(); err != nil {
				return nil, err
			}
		default:
			// ignore other types
		}
	}

	raw, err := os.ReadFile(filepath.Join(dstDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m InputBundleManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func ApplyInputBundleToPlan(p *Plan, bundleRoot string, manifest *InputBundleManifest) error {
	if p == nil || manifest == nil {
		return nil
	}
	bundleRoot = strings.TrimSpace(bundleRoot)
	if bundleRoot == "" {
		return fmt.Errorf("bundleRoot is required")
	}
	byID := map[string]InputBundleNode{}
	for _, n := range manifest.Nodes {
		byID[n.ID] = n
	}
	for _, n := range p.Nodes {
		entry, ok := byID[n.ID]
		if !ok {
			return fmt.Errorf("bundle missing node %s", n.ID)
		}
		n.Chart = filepath.Join(bundleRoot, filepath.FromSlash(entry.ChartDir))
		var vals []string
		for _, v := range entry.Values {
			vals = append(vals, filepath.Join(bundleRoot, filepath.FromSlash(v.BundlePath)))
		}
		n.Values = vals
	}
	return nil
}

func bundleNodeKey(nodeID string) string {
	s := strings.TrimSpace(nodeID)
	s = strings.ReplaceAll(s, "/", "__")
	s = strings.ReplaceAll(s, string(os.PathSeparator), "__")
	if s == "" {
		return "node"
	}
	return s
}

func loadHelmChartForBundle(ref string, version string, settings *cli.EnvSettings) (*chart.Chart, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("chart ref is required")
	}
	v := strings.TrimSpace(version)
	chartPath := ref
	if !isExistingPath(ref) {
		cpo := action.ChartPathOptions{Version: v}
		located, err := cpo.LocateChart(ref, settings)
		if err != nil {
			return nil, fmt.Errorf("locate chart %s: %w", ref, err)
		}
		chartPath = located
	}
	ch, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("load chart %s: %w", chartPath, err)
	}
	return ch, nil
}

func writeChartToTar(tw *tar.Writer, chartDir string, ch *chart.Chart) error {
	if tw == nil || ch == nil {
		return nil
	}
	files := append([]*chart.File(nil), ch.Raw...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	for _, f := range files {
		dst := path.Join(chartDir, f.Name)
		if err := writeFileToTar(tw, dst, f.Data); err != nil {
			return err
		}
	}
	return nil
}

func writeFileToTar(tw *tar.Writer, name string, data []byte) error {
	if tw == nil {
		return nil
	}
	clean := path.Clean(strings.TrimSpace(name))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || strings.HasPrefix(clean, "/") {
		return fmt.Errorf("invalid tar path %q", name)
	}
	hdr := &tar.Header{
		Name:    clean,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: time.Unix(0, 0).UTC(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}
