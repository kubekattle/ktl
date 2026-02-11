package agent

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func authUnaryInterceptor(token string) grpc.UnaryServerInterceptor {
	expected := strings.TrimSpace(token)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if expected != "" {
			if err := requireAuth(ctx, expected); err != nil {
				return nil, err
			}
		}
		return handler(ctx, req)
	}
}

func authStreamInterceptor(token string) grpc.StreamServerInterceptor {
	expected := strings.TrimSpace(token)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if expected != "" {
			if err := requireAuth(ss.Context(), expected); err != nil {
				return err
			}
		}
		return handler(srv, ss)
	}
}

func requireAuth(ctx context.Context, expected string) error {
	md, _ := metadata.FromIncomingContext(ctx)
	if md == nil {
		return status.Error(codes.Unauthenticated, "missing authentication token")
	}

	// Accept either `authorization: Bearer <token>` or `x-ktl-token: <token>`.
	raw := firstNonEmpty(md.Get("authorization"))
	if raw == "" {
		raw = firstNonEmpty(md.Get("x-ktl-token"))
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return status.Error(codes.Unauthenticated, "missing authentication token")
	}

	normalized := raw
	if len(normalized) >= 7 && strings.EqualFold(normalized[:7], "bearer ") {
		normalized = strings.TrimSpace(normalized[7:])
	}
	if normalized == "" {
		return status.Error(codes.Unauthenticated, "missing authentication token")
	}
	if subtle.ConstantTimeCompare([]byte(normalized), []byte(expected)) != 1 {
		return status.Error(codes.Unauthenticated, "invalid authentication token")
	}
	return nil
}

func firstNonEmpty(vals []string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}
