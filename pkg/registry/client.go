package registry

import "context"

// Client exposes registry recording and publishing helpers.
type Client interface {
	RecordBuild(tags []string, layoutPath string) error
	PushReference(ctx context.Context, reference string, opts PushOptions) error
	PushRepository(ctx context.Context, repository string, opts PushOptions) error
}

// NewClient returns a Client backed by the default registry helpers.
func NewClient() Client {
	return defaultClient{}
}

var defaultRegistryClient = NewClient()

type defaultClient struct{}

func (defaultClient) RecordBuild(tags []string, layoutPath string) error {
	return recordBuild(tags, layoutPath)
}

func (defaultClient) PushReference(ctx context.Context, reference string, opts PushOptions) error {
	return pushReference(ctx, reference, opts)
}

func (defaultClient) PushRepository(ctx context.Context, repository string, opts PushOptions) error {
	return pushRepository(ctx, repository, opts)
}
