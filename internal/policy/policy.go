package policy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Bundle struct {
	Dir  string
	Data map[string]any
}

const maxPolicyBytes = 25 << 20 // 25 MiB

func LoadBundle(ctx context.Context, ref string) (*Bundle, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("policy ref is required")
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return loadBundleFromURL(ctx, ref)
	}
	return loadBundleFromPath(ref)
}

func loadBundleFromPath(path string) (*Bundle, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("policy path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat policy path: %w", err)
	}
	if info.IsDir() {
		data, derr := readBundleData(filepath.Join(path, "data.json"))
		if derr != nil {
			return nil, derr
		}
		return &Bundle{Dir: path, Data: data}, nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".tar", ".tgz", ".gz":
		return unpackTarball(path)
	default:
		return nil, fmt.Errorf("unsupported policy bundle file %s (want directory or .tar/.tgz)", path)
	}
}

func loadBundleFromURL(ctx context.Context, url string) (*Bundle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("fetch policy bundle: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPolicyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPolicyBytes {
		return nil, fmt.Errorf("policy bundle too large (>%d bytes)", maxPolicyBytes)
	}
	tmp, err := os.MkdirTemp("", "ktl-policy-*")
	if err != nil {
		return nil, err
	}
	if err := untarBytes(tmp, body); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	data, derr := readBundleData(filepath.Join(tmp, "data.json"))
	if derr != nil {
		_ = os.RemoveAll(tmp)
		return nil, derr
	}
	return &Bundle{Dir: tmp, Data: data}, nil
}

func unpackTarball(path string) (*Bundle, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "ktl-policy-*")
	if err != nil {
		return nil, err
	}
	if err := untarBytes(tmp, raw); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	data, derr := readBundleData(filepath.Join(tmp, "data.json"))
	if derr != nil {
		_ = os.RemoveAll(tmp)
		return nil, derr
	}
	return &Bundle{Dir: tmp, Data: data}, nil
}

func untarBytes(dest string, payload []byte) error {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return errors.New("empty policy bundle")
	}
	r := io.Reader(bytes.NewReader(payload))
	if bytes.HasPrefix(payload, []byte{0x1f, 0x8b}) {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(strings.TrimPrefix(h.Name, "/"))
		if name == "." || name == "" {
			continue
		}
		if strings.Contains(name, "..") {
			return fmt.Errorf("invalid tar entry %q", h.Name)
		}
		outPath := filepath.Join(dest, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(outPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.CopyN(f, tr, maxPolicyBytes); err != nil && !errors.Is(err, io.EOF) {
				_ = f.Close()
				return err
			}
			_ = f.Close()
		default:
			continue
		}
	}
	return nil
}

func readBundleData(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse data.json: %w", err)
	}
	return out, nil
}
