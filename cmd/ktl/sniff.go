// sniff.go implements the packet-capture helpers ('ktl analyze traffic'), launching tcpdump sidecars and streaming pcap data.
package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/sniff"
	"github.com/example/ktl/internal/sniffcast"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"
)

const defaultSnifferImage = "docker.io/corfr/tcpdump:latest"

func newAnalyzeCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze cluster signals and runtime behavior",
	}
	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Namespace used as default for analyze targets (defaults to kube context)")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	cmd.AddCommand(
		newAnalyzeTrafficCommand(&namespace, kubeconfig, kubeContext),
		newAnalyzeSyscallsCommand(&namespace, kubeconfig, kubeContext),
	)
	return cmd
}

func newAnalyzeTrafficCommand(namespace *string, kubeconfig *string, kubeContext *string) *cobra.Command {
	type snifferInstance struct {
		spec   sniffTarget
		helper string
	}
	var (
		targetArgs     []string
		iface          string
		filter         string
		packetCount    int
		snapLen        int
		absoluteTime   bool
		betweenOnly    bool
		snifferImage   string
		pullPolicyFlag string
		privileged     bool
		startupTimeout time.Duration
		presetNames    []string
		uiAddr         string
		wsListenAddr   string
	)

	iface = "any"
	absoluteTime = true
	snifferImage = defaultSnifferImage
	pullPolicyFlag = string(corev1.PullIfNotPresent)
	privileged = true
	startupTimeout = 45 * time.Second

	cmd := &cobra.Command{
		Use:   "traffic",
		Short: "Inject tcpdump helpers via ephemeral containers and stream captures",
		Long: `ktl analyze traffic injects a purpose-built ephemeral container (with tcpdump + NET_ADMIN) into each target pod, then streams the capture back to your terminal.
Targets are provided with --target in the form [namespace/]<pod>[:container]. Examples:
  ktl analyze traffic --target checkout-0
  ktl analyze traffic --namespace payments --target checkout-0:proxy --filter "port 443"
  ktl analyze traffic --target payments/checkout-0 --target payments/checkout-1 --between`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(targetArgs) == 0 {
				return fmt.Errorf("at least one --target is required (format: [namespace/]<pod>[:container])")
			}
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}

			resolvedNS := ""
			if namespace != nil {
				resolvedNS = *namespace
			}
			if resolvedNS == "" {
				resolvedNS = kubeClient.Namespace
			}
			if resolvedNS == "" {
				resolvedNS = "default"
			}

			targetSpecs, err := buildSniffTargets(ctx, kubeClient, targetArgs, resolvedNS)
			if err != nil {
				return err
			}
			if betweenOnly {
				if len(targetSpecs) != 2 {
					return fmt.Errorf("--between requires exactly two --target values")
				}
				if err := applyBetweenFilters(targetSpecs); err != nil {
					return err
				}
			}

			pullPolicy := corev1.PullPolicy(pullPolicyFlag)
			if err := validatePullPolicy(pullPolicy); err != nil {
				return err
			}

			presetExpr, err := buildPresetFilter(presetNames)
			if err != nil {
				return err
			}
			filterExpr := combineFilters(filter, presetExpr)

			injector := sniff.NewInjector(kubeClient, sniff.InjectorOptions{
				Image:          snifferImage,
				PullPolicy:     pullPolicy,
				Privileged:     privileged,
				StartupTimeout: startupTimeout,
			})
			streamTargets := make([]sniff.Target, 0, len(targetSpecs))
			var instances []snifferInstance
			defer func() {
				if len(instances) == 0 {
					return
				}
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				for _, inst := range instances {
					if err := injector.Shutdown(cleanupCtx, inst.spec.Namespace, inst.spec.Pod, inst.helper); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to stop sniffer %s/%s:%s: %v\n", inst.spec.Namespace, inst.spec.Pod, inst.helper, err)
					}
				}
			}()
			for _, spec := range targetSpecs {
				containerName, err := injector.Ensure(ctx, sniff.PodRef{
					Namespace:       spec.Namespace,
					Pod:             spec.Pod,
					TargetContainer: spec.Container,
				})
				if err != nil {
					return err
				}
				target := sniff.Target{
					Namespace: spec.Namespace,
					Pod:       spec.Pod,
					Container: containerName,
					Label:     fmt.Sprintf("%s/%s:%s", spec.Namespace, spec.Pod, spec.Container),
					Filter:    spec.Filter,
				}
				streamTargets = append(streamTargets, target)
				instances = append(instances, snifferInstance{spec: spec, helper: containerName})
			}

			contextName := ""
			if kubeContext != nil {
				contextName = *kubeContext
			}
			clusterInfo := describeClusterLabel(kubeClient, contextName)
			observer := buildTrafficObservers(ctx, cmd, clusterInfo, uiAddr, wsListenAddr)

			options := sniff.StreamOptions{
				Interface:    iface,
				SnapLen:      snapLen,
				PacketCount:  packetCount,
				AbsoluteTime: absoluteTime,
				GlobalFilter: filterExpr,
				Targets:      streamTargets,
				Stdout:       cmd.OutOrStdout(),
				Stderr:       cmd.ErrOrStderr(),
				Observer:     observer,
			}
			if err := sniff.Stream(ctx, kubeClient, options); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&targetArgs, "target", nil, "Pod target in the form [namespace/]<pod>[:container]; repeat to attach to multiple pods")
	cmd.Flags().StringVar(&iface, "interface", iface, "Interface name passed to tcpdump -i")
	cmd.Flags().StringVar(&filter, "filter", "", "Additional BPF filter applied to every tcpdump invocation")
	cmd.Flags().StringSliceVar(&presetNames, "bpf", nil, "Named BPF presets to include (dns, service-mesh, handshake); repeat to combine")
	cmd.Flags().IntVar(&packetCount, "count", 0, "Stop after capturing this many packets per target (0 = unlimited)")
	cmd.Flags().IntVar(&snapLen, "snaplen", 0, "Snap length passed to tcpdump -s (0 = full packet)")
	cmd.Flags().BoolVar(&absoluteTime, "absolute-time", absoluteTime, "Render tcpdump timestamps with -tttt (disable for relative time)")
	cmd.Flags().BoolVar(&betweenOnly, "between", false, "Automatically filter traffic between the two provided targets")
	cmd.Flags().StringVar(&snifferImage, "image", snifferImage, "Container image that provides tcpdump inside the sniffer ephemeral container")
	cmd.Flags().StringVar(&pullPolicyFlag, "image-pull-policy", pullPolicyFlag, "Image pull policy for the sniffer container (Always, IfNotPresent, Never)")
	cmd.Flags().BoolVar(&privileged, "privileged", privileged, "Run the sniffer container in privileged mode (adds NET_ADMIN/NET_RAW caps)")
	cmd.Flags().DurationVar(&startupTimeout, "startup-timeout", startupTimeout, "How long to wait for the sniffer container to become ready")
	cmd.Flags().StringVar(&uiAddr, "ui", "", "Serve a live HTML view of captured traffic at this address (e.g. :8081)")
	if flag := cmd.Flags().Lookup("ui"); flag != nil {
		flag.NoOptDefVal = ":8081"
	}
	cmd.Flags().StringVar(&wsListenAddr, "ws-listen", "", "Serve a raw WebSocket feed of captured traffic at this address (e.g. :9091)")

	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "analyze traffic Flags")
	return cmd
}

type sniffTarget struct {
	Namespace string
	Pod       string
	Container string
	Filter    string
	PodIP     string
}

func buildSniffTargets(ctx context.Context, client *kube.Client, rawTargets []string, defaultNamespace string) ([]sniffTarget, error) {
	var specs []sniffTarget
	for _, raw := range rawTargets {
		spec, err := parseTargetArg(raw, defaultNamespace)
		if err != nil {
			return nil, err
		}
		pod, err := client.Clientset.CoreV1().Pods(spec.Namespace).Get(ctx, spec.Pod, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get pod %s/%s: %w", spec.Namespace, spec.Pod, err)
		}
		container := spec.Container
		if container == "" {
			if len(pod.Spec.Containers) == 0 {
				return nil, fmt.Errorf("pod %s/%s has no containers", spec.Namespace, spec.Pod)
			}
			container = pod.Spec.Containers[0].Name
		} else if !containerExists(pod, container) {
			return nil, fmt.Errorf("container %q not found in pod %s/%s", container, spec.Namespace, spec.Pod)
		}
		specs = append(specs, sniffTarget{
			Namespace: spec.Namespace,
			Pod:       spec.Pod,
			Container: container,
			PodIP:     strings.TrimSpace(pod.Status.PodIP),
		})
	}
	return specs, nil
}

func applyBetweenFilters(specs []sniffTarget) error {
	if len(specs) != 2 {
		return fmt.Errorf("--between requires exactly two targets")
	}
	if specs[0].PodIP == "" || specs[1].PodIP == "" {
		return fmt.Errorf("both pods must have an assigned IP address to use --between")
	}
	specs[0].Filter = fmt.Sprintf("host %s and host %s", specs[0].PodIP, specs[1].PodIP)
	specs[1].Filter = fmt.Sprintf("host %s and host %s", specs[1].PodIP, specs[0].PodIP)
	return nil
}

func parseTargetArg(raw, defaultNamespace string) (sniffTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return sniffTarget{}, fmt.Errorf("empty target value")
	}
	var container string
	namePart := raw
	if idx := strings.Index(raw, ":"); idx >= 0 {
		namePart = raw[:idx]
		container = raw[idx+1:]
	}

	ns := defaultNamespace
	pod := namePart
	if slash := strings.Index(namePart, "/"); slash >= 0 {
		ns = namePart[:slash]
		pod = namePart[slash+1:]
	}
	ns = strings.TrimSpace(ns)
	pod = strings.TrimSpace(pod)
	container = strings.TrimSpace(container)
	if ns == "" {
		return sniffTarget{}, fmt.Errorf("namespace could not be determined for target %q", raw)
	}
	if pod == "" {
		return sniffTarget{}, fmt.Errorf("pod name missing in target %q", raw)
	}
	return sniffTarget{
		Namespace: ns,
		Pod:       pod,
		Container: container,
	}, nil
}

type observerList struct {
	items []sniff.Observer
}

func (o observerList) ObserveTraffic(record sniff.Record) {
	for _, obs := range o.items {
		if obs == nil {
			continue
		}
		obs.ObserveTraffic(record)
	}
}

func buildTrafficObservers(ctx context.Context, cmd *cobra.Command, clusterInfo, uiAddr, wsAddr string) sniff.Observer {
	logger := ctrl.Log.WithName("trafficcast")
	var observers []sniff.Observer
	startServer := func(addr string, mode sniffcast.Mode, label string) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return
		}
		srv := sniffcast.New(addr, mode, clusterInfo, logger.WithName(label))
		go func() {
			if err := srv.Run(ctx); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: traffic %s server stopped: %v\n", label, err)
			}
		}()
		fmt.Fprintf(cmd.ErrOrStderr(), "Serving ktl traffic %s view on %s\n", label, addr)
		observers = append(observers, srv)
	}
	startServer(uiAddr, sniffcast.ModeWeb, "ui")
	startServer(wsAddr, sniffcast.ModeWS, "ws")
	if len(observers) == 0 {
		return nil
	}
	return observerList{items: observers}
}

func containerExists(pod *corev1.Pod, container string) bool {
	for _, c := range pod.Spec.Containers {
		if c.Name == container {
			return true
		}
	}
	return false
}

func validatePullPolicy(policy corev1.PullPolicy) error {
	switch policy {
	case corev1.PullIfNotPresent, corev1.PullAlways, corev1.PullNever:
		return nil
	default:
		return fmt.Errorf("invalid --image-pull-policy value %q (use Always, IfNotPresent, or Never)", policy)
	}
}

var bpfPresetLibrary = map[string]string{
	"dns":               "port 53 or port 5353",
	"service-mesh":      "port 15021 or port 15090 or port 15032",
	"handshake":         "(tcp[13] & 0x02 != 0) or (tcp[13] & 0x10 != 0)",
	"http":              "tcp port 80 or tcp port 8080",
	"https":             "tcp port 443",
	"grpc":              "tcp port 50051 or tcp port 50052",
	"postgres":          "tcp port 5432",
	"mysql":             "tcp port 3306",
	"redis":             "tcp port 6379",
	"kafka":             "tcp port 9092",
	"ssh":               "tcp port 22",
	"kube-api":          "tcp port 6443",
	"node-metrics":      "tcp port 10255 or tcp port 10250",
	"health-checks":     "tcp port 15020 or tcp port 10256",
	"ingress":           "tcp port 80 or tcp port 443 or tcp port 8443",
	"nodeport":          "(tcp portrange 30000-32767) or (udp portrange 30000-32767)",
	"control-plane":     "tcp port 2380 or tcp port 2379",
	"etcd":              "tcp port 2379 or tcp port 2380",
	"istio-mtls":        "port 15012 or port 15017",
	"otel-collector":    "tcp port 4317 or tcp port 55680",
	"prometheus-scrape": "tcp port 9090 or tcp port 9091",
}

func buildPresetFilter(names []string) (string, error) {
	if len(names) == 0 {
		return "", nil
	}
	var clauses []string
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		expr, ok := bpfPresetLibrary[name]
		if !ok {
			return "", fmt.Errorf("unknown --bpf preset %q (supported: %s)", raw, supportedPresetList())
		}
		clauses = append(clauses, fmt.Sprintf("(%s)", expr))
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return strings.Join(clauses, " or "), nil
}

func supportedPresetList() string {
	var keys []string
	for name := range bpfPresetLibrary {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func combineFilters(global, preset string) string {
	global = strings.TrimSpace(global)
	preset = strings.TrimSpace(preset)
	switch {
	case global == "" && preset == "":
		return ""
	case global == "":
		return preset
	case preset == "":
		return global
	default:
		return fmt.Sprintf("(%s) and (%s)", global, preset)
	}
}
