package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SealedPlanErrorKind string

const (
	SealedPlanErrAttestationPlanHashMismatch SealedPlanErrorKind = "attestation_plan_hash_mismatch"
	SealedPlanErrBundleDigestMismatch        SealedPlanErrorKind = "bundle_digest_mismatch"
	SealedPlanErrPlanHashMismatch            SealedPlanErrorKind = "plan_hash_mismatch"
	SealedPlanErrBundlePlanHashMismatch      SealedPlanErrorKind = "bundle_plan_hash_mismatch"
	SealedPlanErrInputHashMismatch           SealedPlanErrorKind = "input_hash_mismatch"
)

type SealedPlanError struct {
	Kind   SealedPlanErrorKind
	NodeID string
	Want   string
	Got    string
}

func (e *SealedPlanError) Error() string {
	switch e.Kind {
	case SealedPlanErrAttestationPlanHashMismatch:
		return fmt.Sprintf("attestation planHash mismatch (%s != %s)", e.Got, e.Want)
	case SealedPlanErrBundleDigestMismatch:
		return fmt.Sprintf("attestation bundle digest mismatch (%s != %s)", e.Got, e.Want)
	case SealedPlanErrPlanHashMismatch:
		return fmt.Sprintf("sealed plan hash mismatch (%s != %s)", e.Got, e.Want)
	case SealedPlanErrBundlePlanHashMismatch:
		return fmt.Sprintf("bundle planHash mismatch (%s != %s)", e.Got, e.Want)
	case SealedPlanErrInputHashMismatch:
		return fmt.Sprintf("%s inputs mismatch (want %s got %s)", e.NodeID, e.Want, e.Got)
	default:
		return "sealed plan error"
	}
}

type LoadSealedPlanOptions struct {
	// StateStoreRoot is written into Plan.StackRoot so the run uses the current stack root
	// for state storage, even when inputs are loaded from a sealed artifact.
	StateStoreRoot string

	// Exactly one of SealedDir or BundlePath must be set.
	SealedDir    string
	BundlePath   string
	VerifyBundle bool

	RequireSigned bool
	TrustedPubKey []byte
}

// LoadSealedPlan loads a run plan (plan.json + inputs.tar.gz) either from a directory produced by
// `ktl stack seal` or from a portable .tgz bundle.
//
// The returned cleanup must be called to remove any temporary extraction directories.
func LoadSealedPlan(ctx context.Context, opts LoadSealedPlanOptions) (*Plan, func(), error) {
	sealedDir := strings.TrimSpace(opts.SealedDir)
	bundlePath := strings.TrimSpace(opts.BundlePath)
	if sealedDir != "" && bundlePath != "" {
		return nil, nil, fmt.Errorf("cannot combine --sealed-dir and --from-bundle")
	}
	if sealedDir == "" && bundlePath == "" {
		return nil, nil, fmt.Errorf("expected --sealed-dir or --from-bundle")
	}

	var cleanupFuncs []func()
	dir := sealedDir
	if dir == "" {
		if opts.VerifyBundle {
			if err := VerifyBundleIntegrity(bundlePath); err != nil {
				return nil, nil, err
			}
		}
		if opts.RequireSigned {
			if _, err := VerifyBundle(bundlePath, opts.TrustedPubKey); err != nil {
				return nil, nil, err
			}
		}

		tmp, err := ExtractBundleToTempDir(bundlePath)
		if err != nil {
			return nil, nil, err
		}
		cleanupFuncs = append(cleanupFuncs, func() { _ = os.RemoveAll(tmp) })
		dir = tmp
	}

	rp, err := readRunPlanFile(filepath.Join(dir, "plan.json"))
	if err != nil {
		return nil, nil, err
	}

	wantPlanHash := strings.TrimSpace(rp.PlanHash)
	bundleFile := "inputs.tar.gz"
	attPath := filepath.Join(dir, "attestation.json")
	if _, err := os.Stat(attPath); err == nil {
		att, err := readSealAttestation(attPath)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(att.PlanHash) != "" {
			if wantPlanHash != "" && att.PlanHash != wantPlanHash {
				return nil, nil, &SealedPlanError{Kind: SealedPlanErrAttestationPlanHashMismatch, Want: wantPlanHash, Got: att.PlanHash}
			}
			if wantPlanHash == "" {
				wantPlanHash = att.PlanHash
			}
		}
		if strings.TrimSpace(att.InputsBundle) != "" {
			bundleFile = strings.TrimSpace(att.InputsBundle)
		}
		if strings.TrimSpace(att.InputsBundleSH) != "" {
			sum, err := sha256File(filepath.Join(dir, bundleFile))
			if err != nil {
				return nil, nil, err
			}
			got := "sha256:" + sum
			if got != strings.TrimSpace(att.InputsBundleSH) {
				return nil, nil, &SealedPlanError{Kind: SealedPlanErrBundleDigestMismatch, Want: strings.TrimSpace(att.InputsBundleSH), Got: got}
			}
		}
	}

	gotPlanHash, err := ComputeRunPlanHash(rp)
	if err != nil {
		return nil, nil, err
	}
	if wantPlanHash != "" && gotPlanHash != wantPlanHash {
		return nil, nil, &SealedPlanError{Kind: SealedPlanErrPlanHashMismatch, Want: wantPlanHash, Got: gotPlanHash}
	}

	p, err := PlanFromRunPlan(rp)
	if err != nil {
		return nil, nil, err
	}

	tmpDir, err := os.MkdirTemp("", "ktl-stack-inputs-*")
	if err != nil {
		return nil, nil, err
	}
	cleanupFuncs = append(cleanupFuncs, func() { _ = os.RemoveAll(tmpDir) })

	manifest, err := ExtractInputBundle(ctx, filepath.Join(dir, bundleFile), tmpDir)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(manifest.PlanHash) != "" && wantPlanHash != "" && manifest.PlanHash != wantPlanHash {
		return nil, nil, &SealedPlanError{Kind: SealedPlanErrBundlePlanHashMismatch, Want: wantPlanHash, Got: manifest.PlanHash}
	}
	if err := ApplyInputBundleToPlan(p, tmpDir, manifest); err != nil {
		return nil, nil, err
	}

	gid := &GitIdentity{Commit: rp.StackGitCommit, Dirty: rp.StackGitDirty}
	for _, n := range p.Nodes {
		got, _, err := ComputeEffectiveInputHashWithOptions(n, EffectiveInputHashOptions{
			StackRoot:             tmpDir,
			IncludeValuesContents: true,
			StackGitIdentity:      gid,
		})
		if err != nil {
			return nil, nil, err
		}
		want := strings.TrimSpace(n.EffectiveInputHash)
		if want != "" && got != want {
			return nil, nil, &SealedPlanError{Kind: SealedPlanErrInputHashMismatch, NodeID: n.ID, Want: want, Got: got}
		}
	}

	p.StackRoot = strings.TrimSpace(opts.StateStoreRoot)

	var cleanup func()
	if len(cleanupFuncs) > 0 {
		cleanup = func() {
			for i := len(cleanupFuncs) - 1; i >= 0; i-- {
				cleanupFuncs[i]()
			}
		}
	}
	return p, cleanup, nil
}

func readRunPlanFile(path string) (*RunPlan, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rp RunPlan
	if err := json.Unmarshal(raw, &rp); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &rp, nil
}

type sealAttestationFile struct {
	PlanHash       string `json:"planHash"`
	InputsBundle   string `json:"inputsBundle,omitempty"`
	InputsBundleSH string `json:"inputsBundleDigest,omitempty"`
}

func readSealAttestation(path string) (*sealAttestationFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a sealAttestationFile
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &a, nil
}
