package grpcutil

import (
	"context"
	"strings"

	"google.golang.org/grpc"
)

type bearerTokenCreds struct {
	token string
}

func (b bearerTokenCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	token := strings.TrimSpace(b.token)
	if token == "" {
		return nil, nil
	}
	return map[string]string{"authorization": "Bearer " + token}, nil
}

func (b bearerTokenCreds) RequireTransportSecurity() bool {
	// ktl-agent runs insecure gRPC by default today; keep token auth usable in
	// local/dev clusters without forcing TLS.
	return false
}

// WithBearerToken returns a DialOption that attaches `authorization: Bearer <token>`
// metadata to all RPCs on the connection.
func WithBearerToken(token string) grpc.DialOption {
	return grpc.WithPerRPCCredentials(bearerTokenCreds{token: token})
}
