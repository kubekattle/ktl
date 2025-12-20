package buildsvc

import "context"

// Service exposes the build workflow entrypoint.
type Service interface {
	Run(ctx context.Context, opts Options) (*Result, error)
}
