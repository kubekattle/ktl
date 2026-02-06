package verify

import (
	"path/filepath"
	"runtime"
)

var _, verifyTestFile, _, _ = runtime.Caller(0)
var verifyRepoRoot = filepath.Clean(filepath.Join(filepath.Dir(verifyTestFile), "..", ".."))

func verifyTestdata(parts ...string) string {
	base := append([]string{verifyRepoRoot}, parts...)
	return filepath.Join(base...)
}
