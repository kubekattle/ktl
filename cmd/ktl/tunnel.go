package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type Tunnel struct {
	Name        string // "app" or "db"
	Target      string // "svc/foo" or "pod/bar"
	Namespace   string
	LocalPort   int
	RemotePort  int
	Status      string
	Error       error
	ReadyChan   chan struct{}
	StopChan    chan struct{}
	RetryCount  int
}

func newTunnelCommand(kubeconfig, kubeContext *string) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{
		Use:   "tunnel [SERVICE_OR_POD...]",
		Short: "Smart, resilient port-forwarding for multiple services",
		Long: `Auto-detects ports and manages multiple resilient tunnels.
Examples:
  ktl tunnel my-app              # Auto-detect port for service 'my-app'
  ktl tunnel db:5432             # Forward local 5432 to service 'db' (auto-detect remote)
  ktl tunnel 8080:app:80         # Explicit local:remote
  ktl tunnel app redis postgres  # Open 3 tunnels at once`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("at least one service or pod is required")
			}
			return runTunnel(cmd.Context(), kubeconfig, kubeContext, namespace, args)
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")
	return cmd
}

func runTunnel(ctx context.Context, kubeconfig, kubeContext *string, namespace string, targets []string) error {
	var kc, kctx string
	if kubeconfig != nil {
		kc = *kubeconfig
	}
	if kubeContext != nil {
		kctx = *kubeContext
	}

	kClient, err := kube.New(ctx, kc, kctx)
	if err != nil {
		return err
	}

	if namespace == "" {
		namespace = kClient.Namespace
		if namespace == "" {
			namespace = "default"
		}
	}

	tunnels := make([]*Tunnel, len(targets))
	for i, t := range targets {
		tunnels[i] = parseTarget(t, namespace)
	}

	// TUI Loop
	fmt.Print("\033[H\033[2J") // Clear screen
	printTable(tunnels)

	var wg sync.WaitGroup
	for _, t := range tunnels {
		wg.Add(1)
		go func(tun *Tunnel) {
			defer wg.Done()
			maintainTunnel(ctx, kClient, tun)
		}(t)
	}

	// Refresher loop
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				printTable(tunnels)
			}
		}
	}()

	wg.Wait()
	return nil
}

func maintainTunnel(ctx context.Context, kClient *kube.Client, t *Tunnel) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		t.Status = "Resolving..."
		podName, remotePort, err := resolveTarget(ctx, kClient, t)
		if err != nil {
			t.Status = "Error"
			t.Error = err
			time.Sleep(2 * time.Second)
			continue
		}
		t.RemotePort = remotePort

		if t.LocalPort == 0 {
			t.LocalPort = t.RemotePort // Default to same port
		}

		// Check if local port is available
		if !isPortAvailable(t.LocalPort) {
			t.Status = "Port Busy"
			t.Error = fmt.Errorf("local port %d is in use", t.LocalPort)
			time.Sleep(5 * time.Second)
			continue
		}

		t.Status = "Connecting..."
		t.Error = nil
		
		err = startPortForward(ctx, kClient, podName, t)
		if err != nil {
			t.Status = "Failed"
			t.Error = err
			t.RetryCount++
			time.Sleep(3 * time.Second)
		} else {
			// If we return cleanly, it means connection closed (e.g. pod died)
			t.Status = "Disconnected"
			t.RetryCount++
			time.Sleep(1 * time.Second)
		}
	}
}

func startPortForward(ctx context.Context, kClient *kube.Client, podName string, t *Tunnel) error {
	transport, upgrader, err := spdy.RoundTripperFor(kClient.RESTConfig)
	if err != nil {
		return err
	}

	req := kClient.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(t.Namespace).
		Name(podName).
		SubResource("portforward")

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	t.StopChan = make(chan struct{}, 1)
	t.ReadyChan = make(chan struct{})

	// Redirect output to discard to keep TUI clean
	pf, err := portforward.New(
		dialer,
		[]string{fmt.Sprintf("%d:%d", t.LocalPort, t.RemotePort)},
		t.StopChan,
		t.ReadyChan,
		io.Discard,
		io.Discard,
	)
	if err != nil {
		return err
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- pf.ForwardPorts()
	}()

	select {
	case <-t.ReadyChan:
		t.Status = "Active"
		t.Error = nil
		t.RetryCount = 0
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return nil
	}

	return <-errChan
}

func resolveTarget(ctx context.Context, kClient *kube.Client, t *Tunnel) (string, int, error) {
	// 1. Try as Service first
	svcName := t.Name
	// If it looks like pod/foo, skip service check
	if strings.HasPrefix(t.Target, "pod/") {
		return resolvePod(ctx, kClient, strings.TrimPrefix(t.Target, "pod/"), t)
	}

	svc, err := kClient.Clientset.CoreV1().Services(t.Namespace).Get(ctx, svcName, metav1.GetOptions{})
	if err == nil {
		// Found service
		if len(svc.Spec.Ports) == 0 {
			return "", 0, fmt.Errorf("service has no ports")
		}
		
		// Pick port
		port := int(svc.Spec.Ports[0].TargetPort.IntVal)
		if port == 0 {
			port = int(svc.Spec.Ports[0].Port)
		}
		// If user specified remote port (via input parsing logic which we simplified), use it. 
		// But here we just take the first one for now or match t.RemotePort if set.
		
		// Find backing pod
		selector := metav1.FormatLabelSelector(&metav1.LabelSelector{MatchLabels: svc.Spec.Selector})
		pods, err := kClient.Clientset.CoreV1().Pods(t.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil || len(pods.Items) == 0 {
			return "", 0, fmt.Errorf("no pods found for service %s", svcName)
		}
		// Pick first running pod
		for _, p := range pods.Items {
			if p.Status.Phase == corev1.PodRunning {
				return p.Name, port, nil
			}
		}
		return "", 0, fmt.Errorf("no running pods for service %s", svcName)
	}

	// 2. Try as Pod directly
	return resolvePod(ctx, kClient, t.Name, t)
}

func resolvePod(ctx context.Context, kClient *kube.Client, name string, t *Tunnel) (string, int, error) {
	pod, err := kClient.Clientset.CoreV1().Pods(t.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", 0, err
	}
	if len(pod.Spec.Containers) > 0 && len(pod.Spec.Containers[0].Ports) > 0 {
		return pod.Name, int(pod.Spec.Containers[0].Ports[0].ContainerPort), nil
	}
	return pod.Name, 80, nil // Fallback
}

func parseTarget(raw, ns string) *Tunnel {
	// formats: name, port:name, name:port, local:name:remote
	// Simplified: just name for now
	t := &Tunnel{
		Name:      raw,
		Target:    raw,
		Namespace: ns,
	}
	
	if strings.Contains(raw, ":") {
		parts := strings.Split(raw, ":")
		// db:5432 -> local=auto, name=db, remote=5432
		if len(parts) == 2 {
			// if first part is number -> 8080:app
			// if second part is number -> app:80
			// heuristic...
		}
	}
	return t
}

func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func printTable(tunnels []*Tunnel) {
	fmt.Print("\033[H\033[2J")
	color.New(color.FgCyan, color.Bold).Println(" KTL TUNNEL MANAGER ")
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("%-20s %-20s %-15s\n", "TARGET", "MAPPING", "STATUS")
	
	for _, t := range tunnels {
		statusColor := color.New(color.FgYellow)
		if t.Status == "Active" {
			statusColor = color.New(color.FgGreen)
		} else if strings.HasPrefix(t.Status, "Error") || strings.HasPrefix(t.Status, "Failed") {
			statusColor = color.New(color.FgRed)
		}
		
		mapping := fmt.Sprintf(":%d -> :%d", t.LocalPort, t.RemotePort)
		if t.LocalPort == 0 {
			mapping = "resolving..."
		}
		
		fmt.Printf("%-20s %-20s %s\n", 
			t.Name, 
			mapping,
			statusColor.Sprint(t.Status),
		)
		if t.Error != nil {
			color.New(color.FgRed).Printf("  └─ %v\n", t.Error)
		}
	}
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("Press Ctrl+C to stop all tunnels.")
}
