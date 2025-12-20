// File: internal/api/convert/capture.go
// Brief: Internal convert package implementation for 'capture'.

// Package convert provides convert helpers.

package convert

import (
	"time"

	apiv1 "github.com/example/ktl/pkg/api/v1"
)

// CaptureConfig represents the non-log capture options shared across binaries.
type CaptureConfig struct {
	Duration       time.Duration
	OutputName     string
	SQLite         bool
	AttachDescribe bool
	SessionName    string
}

// CaptureToProto converts capture options into protobuf form.
func CaptureToProto(cfg CaptureConfig) *apiv1.CaptureOptions {
	return &apiv1.CaptureOptions{
		DurationSeconds: int64(cfg.Duration / time.Second),
		OutputName:      cfg.OutputName,
		Sqlite:          cfg.SQLite,
		AttachDescribe:  cfg.AttachDescribe,
		SessionName:     cfg.SessionName,
	}
}

// CaptureFromProto converts protobuf capture options into the config struct.
func CaptureFromProto(pb *apiv1.CaptureOptions) CaptureConfig {
	if pb == nil {
		return CaptureConfig{}
	}
	duration := time.Duration(pb.GetDurationSeconds()) * time.Second
	if duration <= 0 {
		duration = 5 * time.Minute
	}
	return CaptureConfig{
		Duration:       duration,
		OutputName:     pb.GetOutputName(),
		SQLite:         pb.GetSqlite(),
		AttachDescribe: pb.GetAttachDescribe(),
		SessionName:    pb.GetSessionName(),
	}
}
