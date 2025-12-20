// File: internal/capture/graph.go
// Brief: Internal capture package implementation for 'graph'.

// graph.go maintains the informer graph (pods, nodes, events) referenced during capture sessions.
package capture

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	appinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/example/ktl/internal/config"
)

type graph struct {
	factory        informers.SharedInformerFactory
	podInformer    coreinformers.PodInformer
	nodeInformer   coreinformers.NodeInformer
	rsInformer     appinformers.ReplicaSetInformer
	deployInformer appinformers.DeploymentInformer
	logger         logr.Logger
	nodeSynced     bool
	nodeMu         sync.RWMutex
}

func newGraph(client kubernetes.Interface, opts *config.Options, logger logr.Logger) (*graph, error) {
	namespaceScope := metav1.NamespaceAll
	if !opts.AllNamespaces && len(opts.Namespaces) == 1 {
		namespaceScope = opts.Namespaces[0]
	}
	sharedOpts := []informers.SharedInformerOption{
		informers.WithNamespace(namespaceScope),
	}
	if opts.LabelSelector != "" || opts.FieldSelector != "" {
		sharedOpts = append(sharedOpts, informers.WithTweakListOptions(func(lo *metav1.ListOptions) {
			if opts.LabelSelector != "" {
				lo.LabelSelector = opts.LabelSelector
			}
			if opts.FieldSelector != "" {
				lo.FieldSelector = opts.FieldSelector
			}
		}))
	}
	factory := informers.NewSharedInformerFactoryWithOptions(client, 0, sharedOpts...)
	return &graph{
		factory:        factory,
		podInformer:    factory.Core().V1().Pods(),
		nodeInformer:   factory.Core().V1().Nodes(),
		rsInformer:     factory.Apps().V1().ReplicaSets(),
		deployInformer: factory.Apps().V1().Deployments(),
		logger:         logger.WithName("graph"),
	}, nil
}

func (g *graph) start(ctx context.Context, syncTimeout time.Duration) bool {
	g.logger.V(1).Info("starting capture graph informers")
	go g.factory.Start(ctx.Done())
	synced := []cache.InformerSynced{
		g.podInformer.Informer().HasSynced,
		g.rsInformer.Informer().HasSynced,
		g.deployInformer.Informer().HasSynced,
	}

	syncStop := make(chan struct{})
	syncCtx, syncCancel := context.WithCancel(context.Background())
	defer syncCancel()
	syncCause := make(chan string, 1)
	go func() {
		timer := time.NewTimer(syncTimeout)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			syncCause <- "context"
			close(syncStop)
		case <-timer.C:
			syncCause <- "timeout"
			close(syncStop)
		case <-syncCtx.Done():
			return
		}
	}()

	initialSynced := cache.WaitForCacheSync(syncStop, synced...)
	syncCancel()
	if !initialSynced {
		cause := "unknown"
		select {
		case cause = <-syncCause:
		default:
		}
		if cause == "context" {
			g.logger.V(1).Info("capture graph informers did not sync before capture ended; continuing without enrichment")
		} else {
			g.logger.V(1).Info("capture graph informers did not sync within startup window; continuing without enrichment", "timeout", syncTimeout.String())
		}
		return false
	}
	if g.nodeInformer != nil {
		go func() {
			if cache.WaitForCacheSync(ctx.Done(), g.nodeInformer.Informer().HasSynced) {
				g.nodeMu.Lock()
				g.nodeSynced = true
				g.nodeMu.Unlock()
				g.logger.V(1).Info("node informer synced")
			} else {
				g.logger.V(1).Info("node informer failed to sync; node metadata will be skipped")
			}
		}()
	}
	g.logger.V(1).Info("capture graph informers synced")
	return true
}

func (g *graph) getPod(namespace, name string) (*corev1.Pod, error) {
	pod, err := g.podInformer.Lister().Pods(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return pod.DeepCopy(), nil
}

func (g *graph) getNode(name string) (*corev1.Node, error) {
	if g.nodeInformer == nil {
		return nil, fmt.Errorf("node informer disabled")
	}
	g.nodeMu.RLock()
	synced := g.nodeSynced
	g.nodeMu.RUnlock()
	if !synced {
		return nil, fmt.Errorf("node informer not synced")
	}
	node, err := g.nodeInformer.Lister().Get(name)
	if err != nil {
		return nil, err
	}
	return node.DeepCopy(), nil
}

func (g *graph) buildOwnerChain(pod *corev1.Pod) []OwnerSnapshot {
	if pod == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var chain []OwnerSnapshot
	for _, ref := range pod.OwnerReferences {
		g.appendOwner(ref, pod.Namespace, &chain, seen)
	}
	return chain
}

func (g *graph) appendOwner(ref metav1.OwnerReference, namespace string, chain *[]OwnerSnapshot, seen map[string]struct{}) {
	key := fmt.Sprintf("%s/%s", ref.Kind, ref.Name)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	owner := OwnerSnapshot{
		Kind: ref.Kind,
		Name: ref.Name,
		UID:  string(ref.UID),
	}
	switch ref.Kind {
	case "ReplicaSet":
		if rs, err := g.rsInformer.Lister().ReplicaSets(namespace).Get(ref.Name); err == nil {
			owner.Hash = rs.Labels["pod-template-hash"]
			owner.Revision = rs.Annotations["deployment.kubernetes.io/revision"]
			*chain = append(*chain, owner)
			for _, parent := range rs.OwnerReferences {
				g.appendOwner(parent, namespace, chain, seen)
			}
			return
		}
	case "Deployment":
		if deploy, err := g.deployInformer.Lister().Deployments(namespace).Get(ref.Name); err == nil {
			owner.Revision = deploy.Annotations["deployment.kubernetes.io/revision"]
		}
	}
	*chain = append(*chain, owner)
}
