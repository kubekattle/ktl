package analyze

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Snippet represents a piece of source code related to a log error
type Snippet struct {
	File    string
	Line    int
	Content string // Contextual lines around the error
}

// Common stack trace patterns
var (
	// Go: "\t/path/to/file.go:123 +0x123"
	goTraceRe = regexp.MustCompile(`\s+([a-zA-Z0-9_\-\./\\]+\.go):(\d+)`)
	// Python: "File "/path/to/file.py", line 123, in module"
	pyTraceRe = regexp.MustCompile(`File "([a-zA-Z0-9_\-\./\\]+\.py)", line (\d+)`)
	// Node/JS: "at Function.execute (/path/to/file.js:123:45)"
	jsTraceRe = regexp.MustCompile(`\((/[a-zA-Z0-9_\-\./\\]+\.[jt]s):(\d+):\d+\)`)
	// Java: "at com.example.MyClass.method(MyClass.java:123)" - tough to map to file without source root, skipping for now
)

func FindSourceSnippets(logs string) []Snippet {
	var snippets []Snippet
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(logs))
	for scanner.Scan() {
		line := scanner.Text()
		
		// Try Go
		if matches := goTraceRe.FindStringSubmatch(line); len(matches) == 3 {
			s := tryExtractSnippet(matches[1], matches[2])
			if s != nil && !seen[s.File+strconv.Itoa(s.Line)] {
				snippets = append(snippets, *s)
				seen[s.File+strconv.Itoa(s.Line)] = true
			}
		}
		// Try Python
		if matches := pyTraceRe.FindStringSubmatch(line); len(matches) == 3 {
			s := tryExtractSnippet(matches[1], matches[2])
			if s != nil && !seen[s.File+strconv.Itoa(s.Line)] {
				snippets = append(snippets, *s)
				seen[s.File+strconv.Itoa(s.Line)] = true
			}
		}
		// Try JS
		if matches := jsTraceRe.FindStringSubmatch(line); len(matches) == 3 {
			s := tryExtractSnippet(matches[1], matches[2])
			if s != nil && !seen[s.File+strconv.Itoa(s.Line)] {
				snippets = append(snippets, *s)
				seen[s.File+strconv.Itoa(s.Line)] = true
			}
		}
	}
	return snippets
}

func tryExtractSnippet(pathStr, lineStr string) *Snippet {
	lineNum, err := strconv.Atoi(lineStr)
	if err != nil {
		return nil
	}

	// The path in logs might be absolute (e.g. /app/main.go) or relative.
	// We need to find it in the current workspace.
	// Strategy: Take the basename and search for it.
	filename := filepath.Base(pathStr)
	
	// Fast search in current directory (recursive)
	// Limiting depth for performance
	foundPath := findFileInCwd(filename)
	if foundPath == "" {
		return nil
	}

	content, err := readLinesAround(foundPath, lineNum, 3) // +/- 3 lines
	if err != nil {
		return nil
	}

	return &Snippet{
		File:    foundPath,
		Line:    lineNum,
		Content: content,
	}
}

func findFileInCwd(targetName string) string {
	cwd, _ := os.Getwd()
	var match string
	
	// Walk current directory
	_ = filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// Skip vendor, node_modules, .git
			if info.Name() == "vendor" || info.Name() == "node_modules" || info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == targetName {
			match = path
			return io.EOF // Stop search
		}
		return nil
	})
	
	return match
}

func readLinesAround(path string, targetLine, radius int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	current := 0
	start := targetLine - radius
	end := targetLine + radius

	for scanner.Scan() {
		current++
		if current >= start && current <= end {
			prefix := "  "
			if current == targetLine {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s%d: %s", prefix, current, scanner.Text()))
		}
		if current > end {
			break
		}
	}
	
	if len(lines) == 0 {
		return "", fmt.Errorf("line not found")
	}
	
	return strings.Join(lines, "\n"), nil
}
