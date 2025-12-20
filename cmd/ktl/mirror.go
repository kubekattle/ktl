package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/caststream"
	"github.com/example/ktl/internal/castutil"
	"github.com/example/ktl/internal/grpcutil"
	"github.com/example/ktl/internal/logging"
	apiv1 "github.com/example/ktl/pkg/api/v1"
)

func newMirrorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mirror",
		Short: "Bridge mirror bus sessions to HTML viewers",
	}
	cmd.AddCommand(newMirrorProxyCommand())
	return cmd
}

func newMirrorProxyCommand() *cobra.Command {
	var (
		busAddr    string
		sessionID  string
		listenAddr string
		mode       string
		clusterTag string
	)
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Subscribe to a mirror session and host the HTML viewer",
		RunE: func(cmd *cobra.Command, args []string) error {
			if busAddr == "" {
				return fmt.Errorf("--bus is required")
			}
			if sessionID == "" {
				return fmt.Errorf("--session is required")
			}
			ctx := cmd.Context()
			conn, err := grpcutil.Dial(ctx, busAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				return err
			}
			defer conn.Close()
			client := apiv1.NewMirrorServiceClient(conn)
			stream, err := client.Subscribe(ctx, &apiv1.MirrorSubscribeRequest{SessionId: sessionID, Replay: true})
			if err != nil {
				return err
			}
			logger, err := logging.New("info")
			if err != nil {
				return err
			}
			var opts []caststream.Option
			if mode == "deploy" {
				opts = append(opts, caststream.WithDeployUI())
			} else {
				opts = append(opts, caststream.WithoutClusterInfo())
			}
			srv := caststream.New(listenAddr, caststream.ModeWeb, clusterTag, logger.WithName("mirror-proxy"), opts...)
			if err := castutil.StartCastServer(ctx, srv, "mirror proxy", logger.WithName("mirror-proxy"), cmd.ErrOrStderr()); err != nil {
				return err
			}
			for {
				frame, err := stream.Recv()
				if err != nil {
					return err
				}
				if log := frame.GetLog(); log != nil {
					srv.ObserveLog(convert.FromProtoLogLine(log))
					continue
				}
				if build := frame.GetBuild(); build != nil {
					if log := build.GetLog(); log != nil {
						srv.ObserveLog(convert.FromProtoLogLine(log))
					}
					continue
				}
			}
		},
	}
	cmd.Flags().StringVar(&busAddr, "bus", "", "Mirror bus gRPC address (host:port)")
	cmd.Flags().StringVar(&sessionID, "session", "", "Mirror session identifier")
	cmd.Flags().StringVar(&listenAddr, "listen", ":0", "Address to bind the HTML viewer")
	cmd.Flags().StringVar(&mode, "mode", "logs", "Viewer mode: logs or deploy")
	cmd.Flags().StringVar(&clusterTag, "title", "ktl mirror", "Title displayed in the viewer")
	return cmd
}
