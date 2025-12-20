// File: internal/api/convert/deploy.go
// Brief: Internal convert package implementation for 'deploy'.

// Package convert provides convert helpers.

package convert

import (
	"encoding/json"
	"time"

	"github.com/example/ktl/internal/deploy"
	apiv1 "github.com/example/ktl/pkg/api/v1"
)

// DeployApplyConfig mirrors the CLI flags needed to run a deploy apply.
type DeployApplyConfig struct {
	ReleaseName     string
	Chart           string
	Namespace       string
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
	KubeConfigPath  string
	KubeContext     string
}

// DeployDestroyConfig mirrors the deploy destroy flags.
type DeployDestroyConfig struct {
	Release        string
	Namespace      string
	Timeout        time.Duration
	Wait           bool
	KeepHistory    bool
	DryRun         bool
	Force          bool
	DisableHooks   bool
	KubeConfigPath string
	KubeContext    string
}

// DeployEventToProto serializes a StreamEvent into the protobuf envelope.
func DeployEventToProto(evt deploy.StreamEvent) (*apiv1.DeployEvent, error) {
	data, err := json.Marshal(evt)
	if err != nil {
		return nil, err
	}
	return &apiv1.DeployEvent{Json: string(data)}, nil
}

// DeployEventFromProto decodes a protobuf deploy event payload.
func DeployEventFromProto(msg *apiv1.DeployEvent) (deploy.StreamEvent, error) {
	var evt deploy.StreamEvent
	if msg == nil || msg.GetJson() == "" {
		return evt, nil
	}
	if err := json.Unmarshal([]byte(msg.GetJson()), &evt); err != nil {
		return deploy.StreamEvent{}, err
	}
	return evt, nil
}

// DeployApplyToProto converts the config into the protobuf options.
func DeployApplyToProto(cfg DeployApplyConfig) *apiv1.DeployApplyOptions {
	return &apiv1.DeployApplyOptions{
		Release:         cfg.ReleaseName,
		Chart:           cfg.Chart,
		Namespace:       cfg.Namespace,
		Version:         cfg.Version,
		ValuesFiles:     append([]string(nil), cfg.ValuesFiles...),
		SetValues:       append([]string(nil), cfg.SetValues...),
		SetStringValues: append([]string(nil), cfg.SetStringValues...),
		SetFileValues:   append([]string(nil), cfg.SetFileValues...),
		TimeoutSeconds:  int64(cfg.Timeout / time.Second),
		Wait:            cfg.Wait,
		Atomic:          cfg.Atomic,
		UpgradeOnly:     cfg.UpgradeOnly,
		CreateNamespace: cfg.CreateNamespace,
		DryRun:          cfg.DryRun,
		Diff:            cfg.Diff,
		KubeContext:     cfg.KubeContext,
		KubeconfigPath:  cfg.KubeConfigPath,
	}
}

// DeployApplyFromProto converts protobuf options into the config struct.
func DeployApplyFromProto(pb *apiv1.DeployApplyOptions) DeployApplyConfig {
	if pb == nil {
		return DeployApplyConfig{}
	}
	timeout := time.Duration(pb.GetTimeoutSeconds()) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return DeployApplyConfig{
		ReleaseName:     pb.GetRelease(),
		Chart:           pb.GetChart(),
		Namespace:       pb.GetNamespace(),
		Version:         pb.GetVersion(),
		ValuesFiles:     append([]string(nil), pb.GetValuesFiles()...),
		SetValues:       append([]string(nil), pb.GetSetValues()...),
		SetStringValues: append([]string(nil), pb.GetSetStringValues()...),
		SetFileValues:   append([]string(nil), pb.GetSetFileValues()...),
		Timeout:         timeout,
		Wait:            pb.GetWait(),
		Atomic:          pb.GetAtomic(),
		UpgradeOnly:     pb.GetUpgradeOnly(),
		CreateNamespace: pb.GetCreateNamespace(),
		DryRun:          pb.GetDryRun(),
		Diff:            pb.GetDiff(),
		KubeConfigPath:  pb.GetKubeconfigPath(),
		KubeContext:     pb.GetKubeContext(),
	}
}

// DeployDestroyToProto converts destroy config into protobuf form.
func DeployDestroyToProto(cfg DeployDestroyConfig) *apiv1.DeployDestroyOptions {
	return &apiv1.DeployDestroyOptions{
		Release:        cfg.Release,
		Namespace:      cfg.Namespace,
		TimeoutSeconds: int64(cfg.Timeout / time.Second),
		Wait:           cfg.Wait,
		KeepHistory:    cfg.KeepHistory,
		DryRun:         cfg.DryRun,
		Force:          cfg.Force,
		DisableHooks:   cfg.DisableHooks,
		KubeContext:    cfg.KubeContext,
		KubeconfigPath: cfg.KubeConfigPath,
	}
}

// DeployDestroyFromProto converts protobuf destroy options into config.
func DeployDestroyFromProto(pb *apiv1.DeployDestroyOptions) DeployDestroyConfig {
	if pb == nil {
		return DeployDestroyConfig{}
	}
	timeout := time.Duration(pb.GetTimeoutSeconds()) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return DeployDestroyConfig{
		Release:        pb.GetRelease(),
		Namespace:      pb.GetNamespace(),
		Timeout:        timeout,
		Wait:           pb.GetWait(),
		KeepHistory:    pb.GetKeepHistory(),
		DryRun:         pb.GetDryRun(),
		Force:          pb.GetForce(),
		DisableHooks:   pb.GetDisableHooks(),
		KubeConfigPath: pb.GetKubeconfigPath(),
		KubeContext:    pb.GetKubeContext(),
	}
}
