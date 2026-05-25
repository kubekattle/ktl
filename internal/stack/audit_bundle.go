package stack

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func prepareAuditBundleRoot(bundlePath string, verifyBundle bool) (string, string, func(), error) {
	bundlePath = strings.TrimSpace(bundlePath)
	if bundlePath == "" {
		return "", "", nil, fmt.Errorf("bundle path is required")
	}
	if verifyBundle {
		if err := VerifyBundleIntegrity(bundlePath); err != nil {
			return "", "", nil, fmt.Errorf("verify bundle: %w", err)
		}
	}

	tmp, err := ExtractBundleToTempDir(bundlePath)
	if err != nil {
		return "", "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	manifest, err := readRunBundleManifest(filepath.Join(tmp, "manifest.json"))
	if err != nil {
		cleanup()
		return "", "", nil, err
	}
	if err := installAuditBundleState(tmp); err != nil {
		cleanup()
		return "", "", nil, err
	}
	return tmp, strings.TrimSpace(manifest.RunID), cleanup, nil
}

func readRunBundleManifest(path string) (*RunBundleManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest.json: %w", err)
	}
	var manifest RunBundleManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest.json: %w", err)
	}
	if strings.TrimSpace(manifest.RunID) == "" {
		return nil, fmt.Errorf("manifest.json is missing runId")
	}
	return &manifest, nil
}

func installAuditBundleState(root string) error {
	dst := filepath.Join(root, stackStateSQLiteRelPath)
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	src := filepath.Join(root, "state.sqlite")
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("read state.sqlite: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	return copyFileForAuditBundle(src, dst)
}

func copyFileForAuditBundle(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return closeErr
	}
	return nil
}
