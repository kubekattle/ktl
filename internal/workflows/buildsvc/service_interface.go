// File: internal/workflows/buildsvc/service_interface.go
// Brief: Internal buildsvc package implementation for 'service interface'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import "context"

// Service exposes the build workflow entrypoint.
type Service interface {
	Run(ctx context.Context, opts Options) (*Result, error)
}
