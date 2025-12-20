// File: internal/agent/server.go
// Brief: Internal agent package implementation for 'server'.

// Package agent provides agent helpers.

package agent

import (
	"context"
	"net"

	"github.com/example/ktl/internal/logging"
	"github.com/example/ktl/internal/workflows/buildsvc"
	apiv1 "github.com/example/ktl/pkg/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Config defines the runtime settings for the gRPC agent.
type Config struct {
	ListenAddr     string
	KubeconfigPath string
	KubeContext    string
}

// Server wraps the gRPC agent server state.
type Server struct {
	cfg     Config
	build   buildsvc.Service
	mirror  *MirrorServer
	logs    *LogServer
	grpcSrv *grpc.Server
}

// New constructs a Server with default dependencies.
func New(cfg Config, svc buildsvc.Service) (*Server, error) {
	if svc == nil {
		svc = buildsvc.New(buildsvc.Dependencies{})
	}
	logger, err := logging.New("info")
	if err != nil {
		return nil, err
	}
	mirror := NewMirrorServer()
	logSrv := &LogServer{Config: cfg, Logger: logger.WithName("logs"), Mirror: mirror}
	buildSrv := &BuildServer{Service: svc, Mirror: mirror, Logger: logger.WithName("build")}
	deploySrv := &DeployServer{Logger: logger.WithName("deploy")}
	captureSrv := &CaptureServer{}
	driftSrv := &DriftServer{}
	grpcSrv := grpc.NewServer(grpc.Creds(insecure.NewCredentials()), grpc.KeepaliveParams(keepalive.ServerParameters{}))
	apiv1.RegisterLogServiceServer(grpcSrv, logSrv)
	apiv1.RegisterBuildServiceServer(grpcSrv, buildSrv)
	apiv1.RegisterDeployServiceServer(grpcSrv, deploySrv)
	apiv1.RegisterCaptureServiceServer(grpcSrv, captureSrv)
	apiv1.RegisterDriftServiceServer(grpcSrv, driftSrv)
	apiv1.RegisterMirrorServiceServer(grpcSrv, mirror)
	return &Server{cfg: cfg, build: svc, mirror: mirror, logs: logSrv, grpcSrv: grpcSrv}, nil
}

// Run starts the gRPC server.
func (s *Server) Run(ctx context.Context) error {
	if s == nil || s.grpcSrv == nil {
		return nil
	}
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		s.grpcSrv.GracefulStop()
	}()
	return s.grpcSrv.Serve(ln)
}
