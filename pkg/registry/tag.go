package registry

import (
	"context"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

func CopyReference(ctx context.Context, src, dst string) error {
	return crane.Copy(src, dst, crane.WithContext(ctx), crane.WithAuthFromKeychain(authn.DefaultKeychain))
}
