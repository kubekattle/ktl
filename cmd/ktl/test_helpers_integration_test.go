//go:build integration

package main

import (
	"path/filepath"
	"runtime"
)

var _, intTestFilePath, _, _ = runtime.Caller(0)
var intTestRepoRoot = filepath.Clean(filepath.Join(filepath.Dir(intTestFilePath), "..", ".."))

func repoTestdata(parts ...string) string {
	base := append([]string{intTestRepoRoot, "testdata"}, parts...)
	return filepath.Join(base...)
}

func repoScript(parts ...string) string {
	base := append([]string{intTestRepoRoot, "scripts"}, parts...)
	return filepath.Join(base...)
}
