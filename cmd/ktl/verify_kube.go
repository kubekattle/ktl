package main

import (
	"context"
	"fmt"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/verify"
)

func collectNamespacedObjects(ctx context.Context, client *kube.Client, namespace string) ([]map[string]any, error) {
	if client == nil {
		return nil, fmt.Errorf("kube client is required")
	}
	return verify.CollectNamespacedObjects(ctx, client, namespace)
}
