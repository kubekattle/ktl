// File: internal/kube/client.go
// Brief: Internal kube package implementation for 'client'.

// client.go constructs Kubernetes clientsets/dynamic clients used across ktl.
package kube

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mitchellh/go-homedir"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Client bundles the Kubernetes clients used throughout the application.
type Client struct {
	RESTConfig *rest.Config
	Clientset  kubernetes.Interface
	Dynamic    dynamic.Interface
	Metrics    metricsclient.Interface
	RESTMapper *restmapper.DeferredDiscoveryRESTMapper
	Namespace  string
}

// New builds a Kubernetes client configuration honoring the provided kubeconfig path and context.
func New(ctx context.Context, kubeconfigPath, contextName string) (*Client, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		expanded, err := homedir.Expand(kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("expand kubeconfig path: %w", err)
		}
		loadingRules.Precedence = []string{filepath.Clean(expanded)}
	}

	overrides := &clientcmd.ConfigOverrides{ClusterInfo: api.Cluster{Server: ""}}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	namespace, _, err := clientConfig.Namespace()
	if err != nil {
		return nil, fmt.Errorf("resolve default namespace: %w", err)
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build rest config: %w", err)
	}
	rest.SetDefaultWarningHandler(rest.NoWarnings{})

	// Aggressive defaults for snappy startup.
	restConfig.Timeout = 30 * time.Second
	restConfig.QPS = 50
	restConfig.Burst = 100

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create typed client: %w", err)
	}

	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient))

	metrics, err := metricsclient.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create metrics client: %w", err)
	}

	return &Client{
		RESTConfig: restConfig,
		Clientset:  clientset,
		Dynamic:    dyn,
		Metrics:    metrics,
		RESTMapper: mapper,
		Namespace:  namespace,
	}, nil
}
