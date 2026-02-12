// File: internal/workflows/buildsvc/console_observer.go
// Brief: Internal buildsvc package implementation for 'console observer'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"fmt"
	"hash/fnv"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/kubekattle/ktl/internal/tailer"
)

type buildConsoleObserver struct {
	writer           io.Writer
	mu               sync.Mutex
	podPalette       []*color.Color
	containerPalette []*color.Color
	timestampColor   *color.Color
	level            int
}

func NewConsoleObserver(w io.Writer) tailer.LogObserver {
	return NewConsoleObserverWithLevel(w, "info")
}

func NewConsoleObserverWithLevel(w io.Writer, level string) tailer.LogObserver {
	if w == nil {
		return nil
	}
	return &buildConsoleObserver{
		writer:           w,
		podPalette:       tailer.DefaultColorPalette(),
		containerPalette: tailer.DefaultColorPalette(),
		timestampColor:   color.New(color.FgHiBlack),
		level:            parseLogLevel(level),
	}
}

func (o *buildConsoleObserver) ObserveLog(rec tailer.LogRecord) {
	if o == nil || o.writer == nil {
		return
	}
	line := o.render(rec)
	if line == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintln(o.writer, line)
}

func (o *buildConsoleObserver) render(rec tailer.LogRecord) string {
	if strings.EqualFold(rec.Namespace, "sandbox") || strings.EqualFold(rec.Namespace, "nsjail") {
		if o.level < logLevelDebug {
			return ""
		}
	}
	if strings.EqualFold(rec.Source, "heatmap") {
		if o.level < logLevelDebug {
			return ""
		}
	}
	if strings.EqualFold(rec.Source, "graph") {
		if o.level < logLevelDebug {
			return ""
		}
	}
	if !o.shouldPrint(rec) {
		return ""
	}
	payload := strings.TrimSpace(rec.Rendered)
	if payload == "" {
		payload = strings.TrimSpace(rec.Raw)
	}
	if payload == "" {
		return ""
	}

	ts := strings.TrimSpace(rec.FormattedTimestamp)
	if ts == "" {
		tsTime := rec.Timestamp
		if tsTime.IsZero() {
			tsTime = time.Now()
		}
		ts = tsTime.Local().Format("15:04:05")
	}
	timestampToken := fmt.Sprintf("[%s]", ts)

	podToken := strings.TrimSpace(rec.Pod)
	if podToken == "" {
		podToken = strings.TrimSpace(rec.Namespace)
	}
	containerTag := formatBuildContainerTag(rec.Container)

	if !color.NoColor {
		timestampToken = o.timestampColor.Sprint(timestampToken)
		podToken = o.colorizeBySeed(podToken, rec.Pod, o.podPalette)
		if containerTag != "" {
			containerSeed := rec.Pod + "/" + rec.Container
			containerTag = o.colorizeBySeed(containerTag, containerSeed, o.containerPalette)
		}
	}

	parts := []string{timestampToken, podToken}
	if containerTag != "" {
		parts = append(parts, containerTag)
	}
	parts = append(parts, payload)
	return strings.Join(filterEmpty(parts), " ")
}

const (
	logLevelError = 1
	logLevelWarn  = 2
	logLevelInfo  = 3
	logLevelDebug = 4
)

func parseLogLevel(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return logLevelDebug
	case "info", "":
		return logLevelInfo
	case "warn", "warning":
		return logLevelWarn
	case "error":
		return logLevelError
	default:
		return logLevelInfo
	}
}

func (o *buildConsoleObserver) shouldPrint(rec tailer.LogRecord) bool {
	if o == nil {
		return true
	}
	if o.level >= logLevelInfo {
		return true
	}
	// Use glyph severity when available.
	switch strings.TrimSpace(rec.SourceGlyph) {
	case "✖":
		return true
	case "⚠":
		return o.level >= logLevelWarn
	}
	container := strings.ToLower(strings.TrimSpace(rec.Container))
	// BuildKit log streams: keep stderr on warn/error.
	if container == "stderr" {
		return o.level >= logLevelWarn || o.level >= logLevelError
	}
	// Drop status/info spam at warn/error.
	if container == "status" || container == "info" {
		return false
	}
	// Diagnostics: show cache misses on warn, drop hits.
	if container == "diagnostic" {
		if o.level >= logLevelWarn && strings.Contains(strings.ToLower(rec.Rendered), "cache miss") {
			return true
		}
		return o.level >= logLevelDebug
	}
	return o.level >= logLevelWarn
}

func (o *buildConsoleObserver) colorizeBySeed(token, seed string, palette []*color.Color) string {
	if token == "" || len(palette) == 0 {
		return token
	}
	if seed == "" {
		seed = token
	}
	idx := paletteIndex(seed, len(palette))
	if idx < 0 || idx >= len(palette) {
		return token
	}
	return palette[idx].Sprint(token)
}

func formatBuildContainerTag(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("[%s]", trimmed)
}

func filterEmpty(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func paletteIndex(seed string, length int) int {
	if length == 0 {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(seed))
	return int(hasher.Sum32()) % length
}
