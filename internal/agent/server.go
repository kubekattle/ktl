// File: internal/agent/server.go
// Brief: Internal agent package implementation for 'server'.

// Package agent provides agent helpers.

package agent

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kubekattle/ktl/internal/logging"
	"github.com/kubekattle/ktl/internal/workflows/buildsvc"
	apiv1 "github.com/kubekattle/ktl/pkg/api/ktl/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// Config defines the runtime settings for the gRPC agent.
type Config struct {
	ListenAddr                string
	KubeconfigPath            string
	KubeContext               string
	AuthToken                 string
	HTTPListenAddr            string
	TLSCertFile               string
	TLSKeyFile                string
	TLSClientCAFile           string
	MirrorStore               string
	MirrorMaxSessions         int
	MirrorMaxFramesPerSession uint64
	MirrorMaxBytes            int64
	MirrorPruneInterval       time.Duration
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
	store, err := OpenMirrorStore(cfg.MirrorStore, MirrorStoreOptions{
		MaxSessions:         cfg.MirrorMaxSessions,
		MaxFramesPerSession: cfg.MirrorMaxFramesPerSession,
		MaxBytes:            cfg.MirrorMaxBytes,
		PruneInterval:       cfg.MirrorPruneInterval,
	})
	if err != nil {
		return nil, err
	}
	mirror := NewMirrorServer(WithMirrorStore(store))
	logSrv := &LogServer{Config: cfg, Logger: logger.WithName("logs"), Mirror: mirror}
	buildSrv := &BuildServer{Service: svc, Mirror: mirror, Logger: logger.WithName("build")}
	deploySrv := &DeployServer{Logger: logger.WithName("deploy"), Mirror: mirror}
	creds, err := serverCreds(cfg)
	if err != nil {
		return nil, err
	}
	grpcSrv := grpc.NewServer(
		grpc.Creds(creds),
		grpc.KeepaliveParams(keepalive.ServerParameters{}),
		grpc.UnaryInterceptor(authUnaryInterceptor(cfg.AuthToken)),
		grpc.StreamInterceptor(authStreamInterceptor(cfg.AuthToken)),
	)
	apiv1.RegisterLogServiceServer(grpcSrv, logSrv)
	apiv1.RegisterBuildServiceServer(grpcSrv, buildSrv)
	apiv1.RegisterDeployServiceServer(grpcSrv, deploySrv)
	apiv1.RegisterMirrorServiceServer(grpcSrv, mirror)
	apiv1.RegisterVerifyServiceServer(grpcSrv, newVerifyService(cfg, mirror))
	apiv1.RegisterAgentInfoServiceServer(grpcSrv, &infoService{})

	// Standard gRPC features that make automation (and AI agents) dramatically easier.
	// - Health: cheap readiness probe for orchestrators and clients.
	// - Reflection: enables grpcurl and dynamic clients to introspect the API.
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(grpcSrv, healthSrv)
	reflection.Register(grpcSrv)
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
	return s.Serve(ctx, ln)
}

// Serve starts the gRPC server on an existing listener.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	if s == nil || s.grpcSrv == nil || ln == nil {
		return nil
	}
	var httpSrv *http.Server
	var httpLn net.Listener
	if addr := strings.TrimSpace(s.cfg.HTTPListenAddr); addr != "" {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		httpLn = ln
		httpSrv = &http.Server{Handler: newHTTPGateway(s.cfg.AuthToken, s.mirror)}
		go func() {
			_ = httpSrv.Serve(httpLn)
		}()
	}
	go func() {
		<-ctx.Done()
		s.grpcSrv.GracefulStop()
		if httpSrv != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = httpSrv.Shutdown(shutdownCtx)
			cancel()
		}
	}()
	err := s.grpcSrv.Serve(ln)
	if httpSrv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = httpSrv.Shutdown(shutdownCtx)
		cancel()
		if httpLn != nil {
			_ = httpLn.Close()
		}
	}
	_ = s.mirror.Close()
	return err
}
