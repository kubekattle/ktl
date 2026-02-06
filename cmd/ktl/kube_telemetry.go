package main

import (
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"

	"github.com/example/ktl/internal/kube"
)

func attachKubeTelemetry(settings *cli.EnvSettings, client *kube.Client) {
	if settings == nil || client == nil || client.APIStats == nil {
		return
	}
	if flags, ok := settings.RESTClientGetter().(*genericclioptions.ConfigFlags); ok && flags != nil {
		wrap := flags.WrapConfigFn
		flags.WrapConfigFn = func(cfg *rest.Config) *rest.Config {
			if wrap != nil {
				cfg = wrap(cfg)
			}
			kube.AttachAPITelemetry(cfg, client.APIStats)
			return cfg
		}
	}
}
