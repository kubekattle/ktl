package convert

import (
	"time"

	apiv1 "github.com/example/ktl/pkg/api/v1"
)

// DriftWatchConfig captures the CLI drift watch flags in a reusable form.
type DriftWatchConfig struct {
	Namespaces    []string
	AllNamespaces bool
	Interval      time.Duration
	History       int
	Iterations    int
	KubeConfig    string
	KubeContext   string
}

// DriftToProto converts drift watch options into protobuf form.
func DriftToProto(cfg DriftWatchConfig) *apiv1.DriftWatchRequest {
	return &apiv1.DriftWatchRequest{
		Namespaces:      append([]string(nil), cfg.Namespaces...),
		AllNamespaces:   cfg.AllNamespaces,
		IntervalSeconds: int64(cfg.Interval / time.Second),
		History:         int32(cfg.History),
		Iterations:      int32(cfg.Iterations),
		KubeContext:     cfg.KubeContext,
		KubeconfigPath:  cfg.KubeConfig,
	}
}

// DriftFromProto converts protobuf drift watch options into the config struct.
func DriftFromProto(pb *apiv1.DriftWatchRequest) DriftWatchConfig {
	if pb == nil {
		return DriftWatchConfig{}
	}
	interval := time.Duration(pb.GetIntervalSeconds()) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	history := int(pb.GetHistory())
	if history <= 0 {
		history = 20
	}
	iterations := int(pb.GetIterations())
	return DriftWatchConfig{
		Namespaces:    append([]string(nil), pb.GetNamespaces()...),
		AllNamespaces: pb.GetAllNamespaces(),
		Interval:      interval,
		History:       history,
		Iterations:    iterations,
		KubeConfig:    pb.GetKubeconfigPath(),
		KubeContext:   pb.GetKubeContext(),
	}
}
