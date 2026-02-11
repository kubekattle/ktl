//go:build integration && linux
// +build integration,linux

// File: cmd/ktl/test_helpers_test.go
// Brief: CLI command wiring and implementation for 'test helpers'.

package main

import (
	"path/filepath"
	"runtime"
)

var _, testFilePath, _, _ = runtime.Caller(0)
var testRepoRoot = filepath.Clean(filepath.Join(filepath.Dir(testFilePath), "..", ".."))

func repoTestdata(parts ...string) string {
	base := append([]string{testRepoRoot, "testdata"}, parts...)
	return filepath.Join(base...)
}
