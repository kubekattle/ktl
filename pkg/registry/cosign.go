package registry

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type CosignSignOptions struct {
	KeyRef      string
	RekorURL    string
	TLogUpload  *bool
	Output      io.Writer
	ErrorOutput io.Writer
}

func CosignSign(ctx context.Context, reference string, opts CosignSignOptions) error {
	_, err := exec.LookPath("cosign")
	if err != nil {
		return fmt.Errorf("cosign binary not found in PATH: %w", err)
	}
	args := []string{"sign", "--yes"}
	if opts.KeyRef != "" {
		args = append(args, "--key", opts.KeyRef)
	}
	if opts.RekorURL != "" {
		args = append(args, "--rekor-url", opts.RekorURL)
	}
	if opts.TLogUpload != nil {
		if *opts.TLogUpload {
			args = append(args, "--tlog-upload=true")
		} else {
			args = append(args, "--tlog-upload=false")
		}
	}
	args = append(args, reference)

	cmd := exec.CommandContext(ctx, "cosign", args...)
	cmd.Env = os.Environ()
	if opts.Output != nil {
		cmd.Stdout = opts.Output
	} else {
		cmd.Stdout = os.Stdout
	}
	if opts.ErrorOutput != nil {
		cmd.Stderr = opts.ErrorOutput
	} else {
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}
