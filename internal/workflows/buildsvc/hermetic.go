package buildsvc

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	fromLineRE      = regexp.MustCompile(`(?i)^\s*from\s+([^\s]+)`)
	sha256DigestRE  = regexp.MustCompile(`@sha256:[a-f0-9]{64}$`)
	stagePrefixRE   = regexp.MustCompile(`(?i)^\s*--platform=[^\s]+\s+`)
	scratchImageRef = "scratch"
)

func validatePinnedBaseImagesWithOptions(dockerfilePath string, allowUnpinned bool) error {
	dockerfilePath = strings.TrimSpace(dockerfilePath)
	if dockerfilePath == "" {
		return fmt.Errorf("dockerfile path is required")
	}
	f, err := os.Open(dockerfilePath)
	if err != nil {
		return fmt.Errorf("open dockerfile %s: %w", dockerfilePath, err)
	}
	defer f.Close()

	var unpinned []string
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		m := fromLineRE.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		ref := strings.TrimSpace(m[1])
		ref = stagePrefixRE.ReplaceAllString(ref, "")
		if ref == "" || strings.EqualFold(ref, scratchImageRef) {
			continue
		}
		if sha256DigestRE.MatchString(ref) {
			continue
		}
		unpinned = append(unpinned, fmt.Sprintf("line %d: %s", lineNo, ref))
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read dockerfile %s: %w", dockerfilePath, err)
	}
	if len(unpinned) > 0 && !allowUnpinned {
		return fmt.Errorf("hermetic build requires pinned base-image digests (FROM ...@sha256:...); found unpinned references in %s: %s", dockerfilePath, strings.Join(unpinned, ", "))
	}
	return nil
}
