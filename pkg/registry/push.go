package registry

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type PushOptions struct {
	Sign   bool
	Output io.Writer
}

func PushReference(ctx context.Context, reference string, opts PushOptions) error {
	return defaultRegistryClient.PushReference(ctx, reference, opts)
}

func PushRepository(ctx context.Context, repository string, opts PushOptions) error {
	return defaultRegistryClient.PushRepository(ctx, repository, opts)
}

func pushReference(ctx context.Context, reference string, opts PushOptions) error {
	rec, err := ResolveLayout(reference)
	if err != nil {
		return err
	}
	ref, err := name.ParseReference(reference)
	if err != nil {
		return err
	}
	if opts.Output != nil {
		fmt.Fprintf(opts.Output, "Pushing %s from %s\n", reference, rec.LayoutPath)
	}
	if err := pushLayout(ctx, rec.LayoutPath, ref); err != nil {
		return err
	}
	if opts.Sign {
		if err := signWithCosign(ctx, reference); err != nil {
			return err
		}
	}
	return nil
}

func pushRepository(ctx context.Context, repository string, opts PushOptions) error {
	records, err := ListRepository(repository)
	if err != nil {
		return err
	}
	for _, rec := range records {
		if err := pushReference(ctx, rec.Reference, opts); err != nil {
			return err
		}
	}
	return nil
}

func pushLayout(ctx context.Context, layoutPath string, ref name.Reference) error {
	lp, err := layout.FromPath(layoutPath)
	if err != nil {
		return fmt.Errorf("open OCI layout: %w", err)
	}
	idx, err := lp.ImageIndex()
	if err != nil {
		return fmt.Errorf("load OCI index: %w", err)
	}
	return remote.WriteIndex(ref, idx, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
}

func signWithCosign(ctx context.Context, reference string) error {
	_, err := exec.LookPath("cosign")
	if err != nil {
		return fmt.Errorf("cosign binary not found in PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, "cosign", "sign", "--yes", reference)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}
