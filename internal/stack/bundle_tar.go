package stack

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type tarFile struct {
	Name string
	Path string
	Mode int64
}

func writeDeterministicTarGz(dstPath string, files []tarFile) error {
	if strings.TrimSpace(dstPath) == "" {
		return fmt.Errorf("bundle output path is required")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	tmp := dstPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	gw := gzip.NewWriter(f)
	gw.Name = filepath.Base(dstPath)
	gw.ModTime = time.Unix(0, 0).UTC()
	tw := tar.NewWriter(gw)

	defer func() {
		_ = tw.Close()
		_ = gw.Close()
	}()

	for _, tf := range files {
		name := strings.TrimLeft(strings.TrimSpace(tf.Name), "/")
		if name == "" {
			return fmt.Errorf("empty tar entry name for %s", tf.Path)
		}
		info, err := os.Stat(tf.Path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("tar entry %s is a directory", tf.Path)
		}
		mode := info.Mode().Perm()
		if tf.Mode != 0 {
			mode = os.FileMode(tf.Mode)
		}
		hdr := &tar.Header{
			Name:     name,
			Mode:     int64(mode),
			Size:     info.Size(),
			ModTime:  time.Unix(0, 0).UTC(),
			Uid:      0,
			Gid:      0,
			Uname:    "",
			Gname:    "",
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		src, err := os.Open(tf.Path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, src)
		_ = src.Close()
		if copyErr != nil {
			return copyErr
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dstPath)
}
