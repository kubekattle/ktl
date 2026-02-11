package agent

import (
	"context"

	"github.com/example/ktl/internal/version"
	apiv1 "github.com/example/ktl/pkg/api/ktl/api/v1"
)

type infoService struct {
	apiv1.UnimplementedAgentInfoServiceServer
}

func (s *infoService) GetInfo(_ context.Context, _ *apiv1.AgentInfoRequest) (*apiv1.AgentInfo, error) {
	info := version.Get()
	return &apiv1.AgentInfo{
		Version:      info.Version,
		GitCommit:    info.GitCommit,
		GitTreeState: info.GitTreeState,
		BuildDate:    info.BuildDate,
		GoVersion:    info.GoVersion,
		Platform:     info.Platform,
	}, nil
}
