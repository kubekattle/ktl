package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveComposeFilesAutoDetect(t *testing.T) {
	dir := repoTestdata("build", "compose")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	files, err := resolveComposeFiles(nil)
	if err != nil {
		t.Fatalf("resolveComposeFiles returned error: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("expected at least one compose file")
	}
	for _, file := range files {
		if filepath.Ext(file) != ".yml" && filepath.Ext(file) != ".yaml" {
			t.Fatalf("unexpected compose file extension: %s", file)
		}
	}
}
