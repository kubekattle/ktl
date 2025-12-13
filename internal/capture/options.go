// options.go collects and normalizes CLI flags used by capture sessions.
package capture

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Options controls capture session behavior.
type Options struct {
	Duration       time.Duration
	OutputPath     string
	SessionName    string
	SQLite         bool
	AttachDescribe bool
}

// NewOptions returns capture options seeded with defaults.
func NewOptions() *Options {
	return &Options{
		Duration: 5 * time.Minute,
	}
}

// AddFlags installs capture-specific flags on the provided command.
func (o *Options) AddFlags(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&o.Duration, "duration", o.Duration, "Length of the capture window (e.g. 2m, 30s)")
	cmd.Flags().StringVar(&o.OutputPath, "capture-output", o.OutputPath, "Path to the resulting .tar.gz artifact (defaults to ./ktl-capture-<timestamp>.tar.gz)")
	cmd.Flags().StringVar(&o.SessionName, "session-name", o.SessionName, "Optional friendly name recorded inside the capture metadata")
	cmd.Flags().BoolVar(&o.SQLite, "capture-sqlite", o.SQLite, "Also persist logs into logs.sqlite inside the capture archive")
	cmd.Flags().BoolVar(&o.AttachDescribe, "attach-describe", o.AttachDescribe, "Capture kubectl describe-style summaries for observed pods")
}

// Validate ensures capture options are coherent.
func (o *Options) Validate() error {
	if o.Duration <= 0 {
		return fmt.Errorf("--duration must be greater than zero")
	}
	if strings.TrimSpace(o.OutputPath) != "" {
		o.OutputPath = filepath.Clean(o.OutputPath)
	}
	return nil
}

// ResolveOutputPath picks the final artifact path, falling back to a timestamped filename.
func (o *Options) ResolveOutputPath(start time.Time) string {
	if strings.TrimSpace(o.OutputPath) != "" {
		return o.OutputPath
	}
	name := fmt.Sprintf("ktl-capture-%s.tar.gz", start.UTC().Format("20060102-150405"))
	return name
}
