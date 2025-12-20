package logging

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// New returns a controller-runtime logger configured with the given level string.
func New(level string) (logr.Logger, error) {
	lower := strings.ToLower(level)
	opts := crzap.Options{}
	var zapLevel zapcore.Level
	switch lower {
	case "debug":
		opts.Development = true
		zapLevel = zapcore.DebugLevel
	case "info", "":
		zapLevel = zapcore.InfoLevel
	case "warn", "warning":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		return logr.Logger{}, fmt.Errorf("unknown log level %q (expected debug, info, warn, or error)", level)
	}
	atomic := zap.NewAtomicLevelAt(zapLevel)
	opts.Level = &atomic
	logger := crzap.New(crzap.UseFlagOptions(&opts))
	return logger, nil
}
