// collect.go queries Kubernetes objects and builds the network readiness summary.
package networkstatus

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Options controls namespace scoping.
type Options struct {
	Namespaces       []string
	AllNamespaces    bool
	DefaultNamespace string
}

// Summary aggregates ingress, service, and gateway statuses.
type Summary struct {
	Ingresses []IngressStatus
	Services  []ServiceStatus
	Gateways  []GatewayStatus
}

type IngressStatus struct {
	Namespace    string
	Name         string
	Class        string
	Hosts        []string
	LoadBalancer []string
	ServiceRefs  []string
	TLS          []TLSSecretStatus
	Ready        bool
}

type TLSSecretStatus struct {
	Secret string
	Found  bool
}

type ServiceStatus struct {
	Namespace      string
	Name           string
	Type           corev1.ServiceType
	Ports          []string
	LoadBalancerIP []string
	ExternalIPs    []string
	ReadyEndpoints int
	NotReady       int
}

type endpointStats struct {
	ready    int
	notReady int
}

type GatewayStatus struct {
	Namespace string
	Name      string
	Class     string
	Addresses []string
	Ready     bool
	Message   string
}

// Collect builds the summary for the provided namespaces.
func Collect(ctx context.Context, client kubernetes.Interface, dyn dynamic.Interface, opts Options) (*Summary, error) {
	namespaces, err := resolveNamespaces(ctx, client, opts)
	if err != nil {
		return nil, err
	}
	var ingStatuses []IngressStatus
	var svcStatuses []ServiceStatus
	var gwStatuses []GatewayStatus

	secretCache := make(map[string]map[string]bool)

	for _, ns := range namespaces {
		ingList, err := client.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list ingresses in %s: %w", ns, err)
		}
		for _, ing := range ingList.Items {
			ingStatuses = append(ingStatuses, summarizeIngress(ctx, client, ing, secretCache))
		}

		svcList, err := client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list services in %s: %w", ns, err)
		}
		endpointStats, err := fetchEndpointStats(ctx, client, ns)
		if err != nil {
			legacyStats, legacyErr := fetchLegacyEndpointStats(ctx, client, ns)
			if legacyErr != nil {
				return nil, fmt.Errorf("list endpoints in %s: %v (fallback: %w)", ns, err, legacyErr)
			}
			endpointStats = legacyStats
		}
		for _, svc := range svcList.Items {
			svcStatuses = append(svcStatuses, summarizeService(svc, endpointStats[svc.Name]))
		}
		if dyn != nil {
			gws, err := listGateways(ctx, dyn, ns)
			if err == nil {
				gwStatuses = append(gwStatuses, gws...)
			}
		}
	}

	sort.Slice(ingStatuses, func(i, j int) bool {
		if ingStatuses[i].Namespace == ingStatuses[j].Namespace {
			return ingStatuses[i].Name < ingStatuses[j].Name
		}
		return ingStatuses[i].Namespace < ingStatuses[j].Namespace
	})
	sort.Slice(svcStatuses, func(i, j int) bool {
		if svcStatuses[i].Namespace == svcStatuses[j].Namespace {
			return svcStatuses[i].Name < svcStatuses[j].Name
		}
		return svcStatuses[i].Namespace < svcStatuses[j].Namespace
	})
	sort.Slice(gwStatuses, func(i, j int) bool {
		if gwStatuses[i].Namespace == gwStatuses[j].Namespace {
			return gwStatuses[i].Name < gwStatuses[j].Name
		}
		return gwStatuses[i].Namespace < gwStatuses[j].Namespace
	})

	return &Summary{
		Ingresses: ingStatuses,
		Services:  svcStatuses,
		Gateways:  gwStatuses,
	}, nil
}

func resolveNamespaces(ctx context.Context, client kubernetes.Interface, opts Options) ([]string, error) {
	if opts.AllNamespaces {
		nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces: %w", err)
		}
		names := make([]string, 0, len(nsList.Items))
		for _, ns := range nsList.Items {
			names = append(names, ns.Name)
		}
		return names, nil
	}
	if len(opts.Namespaces) > 0 {
		var names []string
		for _, ns := range opts.Namespaces {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				names = append(names, ns)
			}
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("no namespaces specified")
		}
		return names, nil
	}
	if opts.DefaultNamespace != "" {
		return []string{opts.DefaultNamespace}, nil
	}
	return []string{"default"}, nil
}

func summarizeIngress(ctx context.Context, client kubernetes.Interface, ing networkingv1.Ingress, cache map[string]map[string]bool) IngressStatus {
	hosts := make([]string, 0, len(ing.Spec.Rules))
	serviceRefs := map[string]struct{}{}
	for _, rule := range ing.Spec.Rules {
		if rule.Host != "" {
			hosts = append(hosts, rule.Host)
		}
		if rule.HTTP != nil {
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service != nil {
					svc := fmt.Sprintf("%s/%s", ing.Namespace, path.Backend.Service.Name)
					serviceRefs[svc] = struct{}{}
				}
			}
		}
	}
	for _, tls := range ing.Spec.TLS {
		for _, host := range tls.Hosts {
			if host != "" && !contains(hosts, host) {
				hosts = append(hosts, host)
			}
		}
	}
	var lb []string
	for _, entry := range ing.Status.LoadBalancer.Ingress {
		if entry.Hostname != "" {
			lb = append(lb, entry.Hostname)
		} else if entry.IP != "" {
			lb = append(lb, entry.IP)
		}
	}
	tlsStatuses := make([]TLSSecretStatus, 0, len(ing.Spec.TLS))
	for _, tls := range ing.Spec.TLS {
		found := secretExists(ctx, client, ing.Namespace, tls.SecretName, cache)
		tlsStatuses = append(tlsStatuses, TLSSecretStatus{
			Secret: tls.SecretName,
			Found:  found,
		})
	}
	services := make([]string, 0, len(serviceRefs))
	for svc := range serviceRefs {
		services = append(services, svc)
	}
	sort.Strings(services)
	class := ""
	if ing.Spec.IngressClassName != nil {
		class = *ing.Spec.IngressClassName
	}
	return IngressStatus{
		Namespace:    ing.Namespace,
		Name:         ing.Name,
		Class:        class,
		Hosts:        hosts,
		LoadBalancer: lb,
		ServiceRefs:  services,
		TLS:          tlsStatuses,
		Ready:        len(lb) > 0,
	}
}

func secretExists(ctx context.Context, client kubernetes.Interface, namespace, name string, cache map[string]map[string]bool) bool {
	if name == "" {
		return false
	}
	if cache[namespace] == nil {
		cache[namespace] = map[string]bool{}
	}
	if result, ok := cache[namespace][name]; ok {
		return result
	}
	_, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	found := err == nil
	cache[namespace][name] = found
	return found
}

func summarizeService(svc corev1.Service, stats endpointStats) ServiceStatus {
	ports := make([]string, 0, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		ports = append(ports, fmt.Sprintf("%s:%d/%s", port.Name, port.Port, port.Protocol))
	}
	lbIPs := []string{}
	for _, entry := range svc.Status.LoadBalancer.Ingress {
		if entry.Hostname != "" {
			lbIPs = append(lbIPs, entry.Hostname)
		} else if entry.IP != "" {
			lbIPs = append(lbIPs, entry.IP)
		}
	}
	ready := stats.ready
	notReady := stats.notReady
	return ServiceStatus{
		Namespace:      svc.Namespace,
		Name:           svc.Name,
		Type:           svc.Spec.Type,
		Ports:          ports,
		LoadBalancerIP: lbIPs,
		ExternalIPs:    append([]string{}, svc.Spec.ExternalIPs...),
		ReadyEndpoints: ready,
		NotReady:       notReady,
	}
}

var gatewayGVR = schema.GroupVersionResource{
	Group:    "gateway.networking.k8s.io",
	Version:  "v1",
	Resource: "gateways",
}

func fetchEndpointStats(ctx context.Context, client kubernetes.Interface, namespace string) (map[string]endpointStats, error) {
	stats := make(map[string]endpointStats)
	slices, err := client.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, slice := range slices.Items {
		serviceName, ok := slice.Labels[discoveryv1.LabelServiceName]
		if !ok || serviceName == "" {
			continue
		}
		stat := stats[serviceName]
		for _, endpoint := range slice.Endpoints {
			ready := endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready
			if ready {
				stat.ready++
			} else {
				stat.notReady++
			}
		}
		stats[serviceName] = stat
	}
	return stats, nil
}

func fetchLegacyEndpointStats(ctx context.Context, client kubernetes.Interface, namespace string) (map[string]endpointStats, error) {
	stats := make(map[string]endpointStats)
	eps, err := client.CoreV1().Endpoints(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, ep := range eps.Items {
		stat := stats[ep.Name]
		for _, subset := range ep.Subsets {
			stat.ready += len(subset.Addresses)
			stat.notReady += len(subset.NotReadyAddresses)
		}
		stats[ep.Name] = stat
	}
	return stats, nil
}

func listGateways(ctx context.Context, dyn dynamic.Interface, namespace string) ([]GatewayStatus, error) {
	var statuses []GatewayStatus
	list, err := dyn.Resource(gatewayGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range list.Items {
		statuses = append(statuses, summarizeGateway(item))
	}
	return statuses, nil
}

func summarizeGateway(obj unstructured.Unstructured) GatewayStatus {
	ns := obj.GetNamespace()
	name := obj.GetName()
	class, _, _ := unstructured.NestedString(obj.Object, "spec", "gatewayClassName")
	addressValues := []string{}
	if addresses, found, _ := unstructured.NestedSlice(obj.Object, "status", "addresses"); found {
		for _, addr := range addresses {
			if addrMap, ok := addr.(map[string]interface{}); ok {
				if val, ok := addrMap["value"].(string); ok && val != "" {
					addressValues = append(addressValues, val)
				}
			}
		}
	}
	ready := false
	message := ""
	if conditions, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions"); found {
		for _, cond := range conditions {
			if condMap, ok := cond.(map[string]interface{}); ok {
				if condType, _ := condMap["type"].(string); condType == "Programmed" {
					if status, _ := condMap["status"].(string); status == "True" {
						ready = true
					}
					if msg, _ := condMap["message"].(string); msg != "" {
						message = msg
					}
				}
			}
		}
	}
	return GatewayStatus{
		Namespace: ns,
		Name:      name,
		Class:     class,
		Addresses: addressValues,
		Ready:     ready,
		Message:   message,
	}
}

func contains(list []string, needle string) bool {
	for _, item := range list {
		if item == needle {
			return true
		}
	}
	return false
}
