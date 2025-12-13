package main

import (
	"errors"
	"os"
	"path/filepath"
)

var composeDefaultFilenames = []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}

func resolveComposeFiles(files []string) ([]string, error) {
	if len(files) > 0 {
		return absolutePaths(files)
	}
	detected, err := findComposeFiles(".")
	if err != nil {
		return nil, err
	}
	if len(detected) == 0 {
		return nil, errors.New("no compose files specified and none found in the current directory")
	}
	return detected, nil
}

func findComposeFiles(base string) ([]string, error) {
	detected := make([]string, 0)
	for _, candidate := range composeDefaultFilenames {
		path := candidate
		if base != "" && base != "." {
			path = filepath.Join(base, candidate)
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		detected = append(detected, abs)
	}
	return detected, nil
}

func absolutePaths(paths []string) ([]string, error) {
	out := make([]string, len(paths))
	for i, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		out[i] = abs
	}
	return out, nil
}
