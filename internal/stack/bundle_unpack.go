package stack

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func extractTarGz(bundlePath string, dstDir string) error {
	f, err := os.Open(bundlePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr == nil {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimLeft(strings.TrimSpace(hdr.Name), "/")
		if name == "" {
			continue
		}
		if strings.Contains(name, "..") {
			return fmt.Errorf("invalid tar entry name %q", hdr.Name)
		}
		target := filepath.Join(dstDir, name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dstDir)+string(os.PathSeparator)) && filepath.Clean(target) != filepath.Clean(dstDir) {
			return fmt.Errorf("invalid tar entry path %q", hdr.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		tmp := target + ".tmp"
		out, err := os.Create(tmp)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			_ = os.Remove(tmp)
			return copyErr
		}
		if closeErr != nil {
			_ = os.Remove(tmp)
			return closeErr
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
}
