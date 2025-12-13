// save.go snapshots container images into tarballs for packaging/offline workflows.
package images

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// SaverOptions configure how images are downloaded and archived.
type SaverOptions struct {
	Keychain authn.Keychain
}

// Saver downloads container images and writes them to a single archive file.
type Saver struct {
	keychain authn.Keychain
}

// NewSaver creates a new Saver with the provided options.
func NewSaver(opts SaverOptions) *Saver {
	keychain := opts.Keychain
	if keychain == nil {
		keychain = authn.DefaultKeychain
	}
	return &Saver{keychain: keychain}
}

// Save pulls every image reference and stores them in docker-archive format at outputPath.
func (s *Saver) Save(ctx context.Context, refs []string, outputPath string) error {
	if len(refs) == 0 {
		return fmt.Errorf("no images to package")
	}
	if err := ensureDir(filepath.Dir(outputPath)); err != nil {
		return err
	}
	images := make(map[string]v1.Image, len(refs))
	for _, ref := range refs {
		img, err := crane.Pull(ref, crane.WithContext(ctx), crane.WithAuthFromKeychain(s.keychain))
		if err != nil {
			return fmt.Errorf("pull %s: %w", ref, err)
		}
		images[ref] = img
	}
	if err := crane.MultiSave(images, outputPath, crane.WithContext(ctx), crane.WithAuthFromKeychain(s.keychain)); err != nil {
		return fmt.Errorf("save archive: %w", err)
	}
	return nil
}

func ensureDir(dir string) error {
	if dir == "" || dir == "." || dir == string(filepath.Separator) {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
