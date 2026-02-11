// File: internal/grpcutil/dial.go
// Brief: Internal grpcutil package implementation for 'dial'.

// Package grpcutil provides grpcutil helpers.

package grpcutil

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

// Dial creates a gRPC ClientConn and blocks until it is Ready (or ctx expires).
//
// It uses grpc.NewClient under the hood to avoid deprecated grpc.DialContext.
func Dial(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	target = strings.TrimSpace(target)
	if target != "" && !strings.Contains(target, "://") {
		// Preserve grpc.DialContext's default "passthrough" resolver behavior.
		target = "passthrough:///" + target
	}

	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, err
	}

	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return conn, nil
		}
		if state == connectivity.Shutdown {
			_ = conn.Close()
			return nil, fmt.Errorf("grpc connection shutdown")
		}
		if !conn.WaitForStateChange(ctx, state) {
			_ = conn.Close()
			return nil, ctx.Err()
		}
	}
}
