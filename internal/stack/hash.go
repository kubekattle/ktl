// File: internal/stack/hash.go
// Brief: Effective input hashing for drift detection and resume safety.

package stack

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/example/ktl/internal/version"
)

func ComputeEffectiveInputHash(n *ResolvedRelease, includeValuesContents bool) (string, error) {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("ktl.stack-run.v1")
	write("ktl:" + version.Version)
	write("git:" + version.GitCommit)
	write(n.ID)
	write(n.Chart)
	for _, vf := range n.Values {
		write(vf)
		if includeValuesContents && isLocalPath(vf) {
			b, err := os.ReadFile(vf)
			if err != nil {
				return "", fmt.Errorf("read values file %s: %w", vf, err)
			}
			sum := sha256.Sum256(b)
			write("values-sha256:" + hex.EncodeToString(sum[:]))
		}
	}
	keys := make([]string, 0, len(n.Set))
	for k := range n.Set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		write(k)
		write(n.Set[k])
	}
	write(n.Cluster.Name)
	write(n.Cluster.Kubeconfig)
	write(n.Cluster.Context)
	write(n.Namespace)
	sum := hex.EncodeToString(h.Sum(nil))
	return "sha256:" + sum, nil
}

func isLocalPath(p string) bool {
	// Values in stack v1 are filesystem paths; keep an escape hatch anyway.
	return p != "" && !strings.Contains(p, "://")
}
