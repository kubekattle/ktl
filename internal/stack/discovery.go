// File: internal/stack/discovery.go
// Brief: Filesystem discovery of stack.yaml/release.yaml.

package stack

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	stackFileName   = "stack.yaml"
	releaseFileName = "release.yaml"
)

type discoveredRelease struct {
	Dir        string
	FromFile   *ReleaseFile
	FromInline *ReleaseSpec
}

type Universe struct {
	RootDir        string
	StackName      string
	DefaultProfile string

	Stacks   map[string]StackFile
	Releases []discoveredRelease
}

func Discover(root string) (*Universe, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	u := &Universe{
		RootDir: absRoot,
		Stacks:  map[string]StackFile{},
	}

	var rootStack *StackFile
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "bin" || name == "dist" {
				return fs.SkipDir
			}
			return nil
		}
		base := filepath.Base(path)
		switch base {
		case stackFileName:
			sf, err := readStackFile(path)
			if err != nil {
				return err
			}
			dir := filepath.Dir(path)
			u.Stacks[dir] = *sf
			if samePath(dir, absRoot) {
				rootStack = sf
				if strings.TrimSpace(sf.Name) != "" {
					u.StackName = sf.Name
				}
				if strings.TrimSpace(sf.DefaultProfile) != "" {
					u.DefaultProfile = sf.DefaultProfile
				}
			}
			for i := range sf.Releases {
				rel := sf.Releases[i]
				u.Releases = append(u.Releases, discoveredRelease{
					Dir:        dir,
					FromInline: &rel,
				})
			}
		case releaseFileName:
			rf, err := readReleaseFile(path)
			if err != nil {
				return err
			}
			u.Releases = append(u.Releases, discoveredRelease{
				Dir:      filepath.Dir(path),
				FromFile: rf,
			})
		default:
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if rootStack == nil {
		return nil, fmt.Errorf("no %s found at stack root %s", stackFileName, absRoot)
	}
	if strings.TrimSpace(u.StackName) == "" {
		u.StackName = filepath.Base(absRoot)
	}
	return u, nil
}

func readStackFile(path string) (*StackFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sf StackFile
	if err := yaml.Unmarshal(raw, &sf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// Allow "defaults-only" stack.yaml in subdirectories.
	if sf.Kind != "" && sf.Kind != "Stack" {
		return nil, fmt.Errorf("%s: kind must be Stack (got %q)", path, sf.Kind)
	}
	if sf.APIVersion != "" && sf.APIVersion != "ktl.dev/v1" {
		return nil, fmt.Errorf("%s: apiVersion must be ktl.dev/v1 (got %q)", path, sf.APIVersion)
	}
	for i := range sf.Releases {
		if strings.TrimSpace(sf.Releases[i].Name) == "" {
			return nil, fmt.Errorf("%s: releases[%d].name is required", path, i)
		}
		if strings.TrimSpace(sf.Releases[i].Chart) == "" {
			return nil, fmt.Errorf("%s: releases[%d].chart is required", path, i)
		}
	}
	return &sf, nil
}

func readReleaseFile(path string) (*ReleaseFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rf ReleaseFile
	if err := yaml.Unmarshal(raw, &rf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if rf.Kind != "" && rf.Kind != "Release" {
		return nil, fmt.Errorf("%s: kind must be Release (got %q)", path, rf.Kind)
	}
	if rf.APIVersion != "" && rf.APIVersion != "ktl.dev/v1" {
		return nil, fmt.Errorf("%s: apiVersion must be ktl.dev/v1 (got %q)", path, rf.APIVersion)
	}
	if strings.TrimSpace(rf.Name) == "" {
		return nil, errors.New(path + ": name is required")
	}
	if strings.TrimSpace(rf.Chart) == "" {
		return nil, errors.New(path + ": chart is required")
	}
	return &rf, nil
}

func samePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return filepath.Clean(aa) == filepath.Clean(bb)
}
