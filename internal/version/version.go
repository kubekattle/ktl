package version

import (
	"fmt"
	"runtime"
)

// These values are overridden at build time via -ldflags "-X ...".
var (
	Version      = "dev"
	GitCommit    = "unknown"
	GitTreeState = "unknown" // clean|dirty|unknown
	BuildDate    = "unknown" // RFC3339 UTC preferred
)

type Info struct {
	Version      string
	GitCommit    string
	GitTreeState string
	BuildDate    string
	GoVersion    string
	Platform     string
}

func Get() Info {
	return Info{
		Version:      Version,
		GitCommit:    GitCommit,
		GitTreeState: GitTreeState,
		BuildDate:    BuildDate,
		GoVersion:    runtime.Version(),
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}
