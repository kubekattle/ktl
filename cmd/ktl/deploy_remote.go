// File: cmd/ktl/deploy_remote.go
// Brief: CLI command wiring and implementation for 'deploy remote'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/grpcutil"
	apiv1 "github.com/example/ktl/pkg/api/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type remoteDeployApplyArgs struct {
	Chart           string
	Release         string
	Namespace       *string
	Version         string
	ValuesFiles     []string
	SetValues       []string
	SetStringValues []string
	SetFileValues   []string
	Timeout         time.Duration
	Wait            bool
	Atomic          bool
	UpgradeOnly     bool
	CreateNamespace bool
	DryRun          bool
	Diff            bool
	KubeConfig      *string
	KubeContext     *string
	RemoteAddr      string
}

type remoteDeployDestroyArgs struct {
	Release      string
	Namespace    *string
	Timeout      time.Duration
	Wait         bool
	KeepHistory  bool
	DryRun       bool
	Force        bool
	DisableHooks bool
	KubeConfig   *string
	KubeContext  *string
	RemoteAddr   string
}

func runRemoteDeployApply(cmd *cobra.Command, args remoteDeployApplyArgs) error {
	ctx := cmd.Context()
	conn, err := grpcutil.Dial(ctx, args.RemoteAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	client := apiv1.NewDeployServiceClient(conn)

	cfg := convert.DeployApplyConfig{
		ReleaseName:     args.Release,
		Chart:           args.Chart,
		Namespace:       derefString(args.Namespace),
		Version:         args.Version,
		ValuesFiles:     append([]string(nil), args.ValuesFiles...),
		SetValues:       append([]string(nil), args.SetValues...),
		SetStringValues: append([]string(nil), args.SetStringValues...),
		SetFileValues:   append([]string(nil), args.SetFileValues...),
		Timeout:         args.Timeout,
		Wait:            args.Wait,
		Atomic:          args.Atomic,
		UpgradeOnly:     args.UpgradeOnly,
		CreateNamespace: args.CreateNamespace,
		DryRun:          args.DryRun,
		Diff:            args.Diff,
		KubeConfigPath:  derefString(args.KubeConfig),
		KubeContext:     derefString(args.KubeContext),
	}

	stream, err := client.Apply(ctx, &apiv1.DeployApplyRequest{Options: convert.DeployApplyToProto(cfg)})
	if err != nil {
		return err
	}
	return consumeDeployStream(cmd, stream)
}

func runRemoteDeployDestroy(cmd *cobra.Command, args remoteDeployDestroyArgs) error {
	ctx := cmd.Context()
	conn, err := grpcutil.Dial(ctx, args.RemoteAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	client := apiv1.NewDeployServiceClient(conn)

	cfg := convert.DeployDestroyConfig{
		Release:        args.Release,
		Namespace:      derefString(args.Namespace),
		Timeout:        args.Timeout,
		Wait:           args.Wait,
		KeepHistory:    args.KeepHistory,
		DryRun:         args.DryRun,
		Force:          args.Force,
		DisableHooks:   args.DisableHooks,
		KubeConfigPath: derefString(args.KubeConfig),
		KubeContext:    derefString(args.KubeContext),
	}
	stream, err := client.Destroy(ctx, &apiv1.DeployDestroyRequest{Options: convert.DeployDestroyToProto(cfg)})
	if err != nil {
		return err
	}
	return consumeDeployStream(cmd, stream)
}

func consumeDeployStream(cmd *cobra.Command, stream interface {
	Recv() (*apiv1.DeployEvent, error)
}) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	for {
		evt, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		payload, err := convert.DeployEventFromProto(evt)
		if err != nil {
			fmt.Fprintf(errOut, "Warning: failed to decode deploy event: %v\n", err)
			continue
		}
		renderDeployEvent(out, errOut, payload)
	}
}

func renderDeployEvent(out io.Writer, errOut io.Writer, evt deploy.StreamEvent) {
	switch evt.Kind {
	case deploy.StreamEventPhase:
		if evt.Phase != nil {
			fmt.Fprintf(errOut, "[%s] phase %s -> %s %s\n", strings.TrimSpace(evt.Timestamp), strings.TrimSpace(evt.Phase.Name), strings.TrimSpace(evt.Phase.Status), strings.TrimSpace(evt.Phase.Message))
		}
	case deploy.StreamEventLog:
		if evt.Log != nil {
			fmt.Fprintf(errOut, "[%s] %s\n", evt.Log.Level, evt.Log.Message)
		}
	case deploy.StreamEventSummary:
		if evt.Summary != nil {
			fmt.Fprintf(out, "Release %s (%s): %s\n", evt.Summary.Release, evt.Summary.Namespace, evt.Summary.Status)
			if evt.Summary.Error != "" {
				fmt.Fprintf(out, "Error: %s\n", evt.Summary.Error)
			}
		}
	default:
		// ignore other event kinds for remote output
	}
}

func derefString(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
