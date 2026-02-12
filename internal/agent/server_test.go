package agent

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/kubekattle/ktl/internal/grpcutil"
	"github.com/kubekattle/ktl/internal/workflows/buildsvc"
	apiv1 "github.com/kubekattle/ktl/pkg/api/ktl/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestAgentInfoAuth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := New(Config{AuthToken: "secret"}, buildsvc.New(buildsvc.Dependencies{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- srv.Serve(ctx, ln)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Fatalf("agent server did not stop in time")
		}
	})

	addr := ln.Addr().String()

	// No token -> unauthenticated.
	{
		dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelDial()
		conn, err := grpcutil.Dial(dialCtx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("dial (no token): %v", err)
		}
		defer conn.Close()

		client := apiv1.NewAgentInfoServiceClient(conn)
		_, err = client.GetInfo(dialCtx, &apiv1.AgentInfoRequest{})
		if err == nil {
			t.Fatalf("expected unauthenticated error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok {
			t.Fatalf("expected gRPC status error, got %T: %v", err, err)
		}
		if st.Code() != codes.Unauthenticated {
			t.Fatalf("expected Unauthenticated, got %s: %v", st.Code(), err)
		}
	}

	// Correct token -> success.
	{
		dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelDial()
		conn, err := grpcutil.Dial(dialCtx, addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpcutil.WithBearerToken("secret"),
		)
		if err != nil {
			t.Fatalf("dial (token): %v", err)
		}
		defer conn.Close()

		client := apiv1.NewAgentInfoServiceClient(conn)
		info, err := client.GetInfo(dialCtx, &apiv1.AgentInfoRequest{})
		if err != nil {
			t.Fatalf("GetInfo: %v", err)
		}
		if info.GetVersion() == "" {
			t.Fatalf("expected version, got empty: %+v", info)
		}
		if info.GetPlatform() == "" {
			t.Fatalf("expected platform, got empty: %+v", info)
		}
	}
}
