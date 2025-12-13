package registry

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ImageRecord struct {
	Reference  string    `json:"reference"`
	LayoutPath string    `json:"layoutPath"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func RecordLayout(reference, layoutPath string) error {
	if reference == "" {
		return errors.New("reference is required")
	}
	if layoutPath == "" {
		return errors.New("layout path is required")
	}
	infoPath := filepath.Join(layoutPath, "index.json")
	if _, err := os.Stat(infoPath); err != nil {
		return fmt.Errorf("%s is not a valid OCI layout: %w", layoutPath, err)
	}
	absLayout, err := filepath.Abs(layoutPath)
	if err != nil {
		return err
	}
	rec := ImageRecord{Reference: reference, LayoutPath: absLayout, UpdatedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	dir, err := recordsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf("%s.tmp", encodeReference(reference)))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	target := filepath.Join(dir, fmt.Sprintf("%s.json", encodeReference(reference)))
	if err := os.Rename(tmp, target); err != nil {
		return err
	}
	return nil
}

func ResolveLayout(reference string) (ImageRecord, error) {
	dir, err := recordsDir()
	if err != nil {
		return ImageRecord{}, err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s.json", encodeReference(reference)))
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ImageRecord{}, fmt.Errorf("no cached build for %s; run ktl build first", reference)
		}
		return ImageRecord{}, err
	}
	var rec ImageRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return ImageRecord{}, err
	}
	if _, err := os.Stat(filepath.Join(rec.LayoutPath, "index.json")); err != nil {
		return ImageRecord{}, fmt.Errorf("cached OCI layout for %s is invalid: %w", reference, err)
	}
	return rec, nil
}

func ListRepository(repository string) ([]ImageRecord, error) {
	if repository == "" {
		return nil, errors.New("repository is required")
	}
	records, err := readAllRecords()
	if err != nil {
		return nil, err
	}
	prefix := repository + ":"
	matches := make([]ImageRecord, 0)
	for _, rec := range records {
		if strings.HasPrefix(rec.Reference, prefix) {
			matches = append(matches, rec)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Reference == matches[j].Reference {
			return matches[i].UpdatedAt.After(matches[j].UpdatedAt)
		}
		return matches[i].Reference < matches[j].Reference
	})
	if len(matches) == 0 {
		return nil, fmt.Errorf("no cached tags found for %s", repository)
	}
	return matches, nil
}

func RecordBuild(tags []string, layoutPath string) error {
	return defaultRegistryClient.RecordBuild(tags, layoutPath)
}

func recordBuild(tags []string, layoutPath string) error {
	if len(tags) == 0 || layoutPath == "" {
		return nil
	}
	for _, tag := range tags {
		if err := RecordLayout(tag, layoutPath); err != nil {
			return err
		}
	}
	return nil
}

func recordsDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "ktl", "images"), nil
}

func encodeReference(ref string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(ref))
}

func readAllRecords() ([]ImageRecord, error) {
	dir, err := recordsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]ImageRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var rec ImageRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}
