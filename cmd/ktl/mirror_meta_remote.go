package main

import (
	"context"
	"strings"
	"time"

	apiv1 "github.com/example/ktl/pkg/api/ktl/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func trySetRemoteMirrorSessionMeta(ctx context.Context, conn *grpc.ClientConn, sessionID string, meta *apiv1.MirrorSessionMeta, tags map[string]string) {
	if conn == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if meta == nil && len(tags) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rpcCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client := apiv1.NewMirrorServiceClient(conn)
	_, err := client.SetSessionMeta(rpcCtx, &apiv1.MirrorSetSessionMetaRequest{
		SessionId: sessionID,
		Meta:      meta,
		Tags:      tags,
	})
	if err == nil {
		return
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
		return
	}
}
