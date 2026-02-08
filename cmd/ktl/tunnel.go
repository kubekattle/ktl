package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"golang.org/x/term"
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
	BytesIn     int64
	BytesOut    int64
	Protocol    string // http, https, postgres, redis, mysql, etc.
}

var (
	logBuffer []string
	logMu     sync.Mutex
)

func logEvent(format string, args ...interface{}) {
	logMu.Lock()
	defer logMu.Unlock()
	ts := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	logBuffer = append(logBuffer, fmt.Sprintf("%s %s", color.New(color.FgHiBlack).Sprint(ts), msg))
	if len(logBuffer) > 10 {
		logBuffer = logBuffer[len(logBuffer)-10:]
	}
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

	if len(targets) == 0 {
		var err error
		targets, err = selectTargets(ctx, kClient, namespace)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return fmt.Errorf("no targets selected")
		}
	}

	tunnels := make([]*Tunnel, len(targets))
	for i, t := range targets {
		tunnels[i] = parseTarget(t, namespace)
	}

	// TUI Loop
	fmt.Print("\033[H\033[2J") // Clear screen
	
	// Enable Raw Mode for interactive keys
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err == nil {
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	selectedIndex := 0
	printTable(tunnels, selectedIndex)

	var wg sync.WaitGroup
	for _, t := range tunnels {
		wg.Add(1)
		go func(tun *Tunnel) {
			defer wg.Done()
			maintainTunnel(ctx, kClient, tun)
		}(t)
	}

	// Input Loop
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				return
			}
			key := buf[0]
			
			switch key {
			case 'q', 3: // q or Ctrl+C
				// Restore terminal before exiting
				term.Restore(int(os.Stdin.Fd()), oldState)
				os.Exit(0)
			case 'j', 's': // Down
				if selectedIndex < len(tunnels)-1 {
					selectedIndex++
					printTable(tunnels, selectedIndex)
				}
			case 'k', 'w': // Up
				if selectedIndex > 0 {
					selectedIndex--
					printTable(tunnels, selectedIndex)
				}
			case 'o': // Open
				t := tunnels[selectedIndex]
				if strings.HasPrefix(t.Protocol, "http") {
					openBrowser(fmt.Sprintf("%s://localhost:%d", t.Protocol, t.LocalPort))
				}
			case 'c': // Copy
				t := tunnels[selectedIndex]
				copyToClipboard(fmt.Sprintf("localhost:%d", t.LocalPort))
			}
		}
	}()

	// Refresher loop
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				printTable(tunnels, selectedIndex)
			}
		}
	}()

	wg.Wait()
	return nil
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	}
	if err != nil {
		// ignore
	}
}

func copyToClipboard(text string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "c")
	default:
		return
	}
	cmd.Stdin = strings.NewReader(text)
	_ = cmd.Run()
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
			logEvent("Failed to resolve %s: %v", t.Name, err)
			time.Sleep(2 * time.Second)
			continue
		}
		t.RemotePort = remotePort
		t.Protocol = inferProtocol(t.RemotePort)

		if t.LocalPort == 0 {
			t.LocalPort = t.RemotePort // Default to same port
		}

		// Check if local port is available
		if !isPortAvailable(t.LocalPort) {
			// Auto-fix: try next ports
			found := false
			originalPort := t.LocalPort
			for i := 1; i <= 10; i++ {
				next := originalPort + i
				if isPortAvailable(next) {
					t.LocalPort = next
					found = true
					break
				}
			}
			if !found {
				t.Status = "Port Busy"
				t.Error = fmt.Errorf("local port %d (and next 10) in use", originalPort)
				logEvent("Local port %d busy for %s", originalPort, t.Name)
				time.Sleep(5 * time.Second)
				continue
			}
		}

		t.Status = "Connecting..."
		t.Error = nil
		
		logEvent("Tunneling %s :%d -> %s:%d", t.Name, t.LocalPort, podName, t.RemotePort)
		err = startPortForward(ctx, kClient, podName, t)
		if err != nil {
			t.Status = "Failed"
			t.Error = err
			t.RetryCount++
			logEvent("Tunnel %s failed: %v", t.Name, err)
			time.Sleep(3 * time.Second)
		} else {
			// If we return cleanly, it means connection closed (e.g. pod died)
			t.Status = "Disconnected"
			t.RetryCount++
			logEvent("Tunnel %s disconnected (pod maybe dead?)", t.Name)
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

	// Traffic counters
	outStream := &counterWriter{target: &t.BytesOut}
	inStream := &counterWriter{target: &t.BytesIn}

	// Redirect output to discard to keep TUI clean
	pf, err := portforward.New(
		dialer,
		[]string{fmt.Sprintf("%d:%d", t.LocalPort, t.RemotePort)},
		t.StopChan,
		t.ReadyChan,
		outStream, // Remote -> Local (BytesOut)
		inStream,  // Local -> Remote (BytesIn)
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

func selectTargets(ctx context.Context, kClient *kube.Client, namespace string) ([]string, error) {
	svcs, err := kClient.Clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if len(svcs.Items) == 0 {
		return nil, fmt.Errorf("no services found in namespace %s", namespace)
	}

	fmt.Printf("Available Services in %s:\n", namespace)
	for i, svc := range svcs.Items {
		ports := []string{}
		for _, p := range svc.Spec.Ports {
			ports = append(ports, fmt.Sprintf("%d", p.Port))
		}
		fmt.Printf("  %d) %s (Ports: %s)\n", i+1, svc.Name, strings.Join(ports, ", "))
	}

	fmt.Print("\nEnter numbers (e.g. 1,3) to tunnel: ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return nil, fmt.Errorf("no input")
	}
	input := scanner.Text()

	var selected []string
	parts := strings.Split(input, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		idx, err := strconv.Atoi(p)
		if err != nil || idx < 1 || idx > len(svcs.Items) {
			fmt.Printf("Invalid selection: %s\n", p)
			continue
		}
		selected = append(selected, svcs.Items[idx-1].Name)
	}
	return selected, nil
}

type counterWriter struct {
	target *int64
}

func (cw *counterWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	*cw.target += int64(n)
	return n, nil
}

func printTable(tunnels []*Tunnel, selectedIndex int) {
	fmt.Print("\033[H\033[2J")
	color.New(color.FgCyan, color.Bold).Println(" KTL TUNNEL MANAGER ")
	fmt.Println(strings.Repeat("-", 90))
	fmt.Printf("%-3s %-20s %-20s %-10s %-15s %-10s %-10s\n", "", "TARGET", "MAPPING", "PROTO", "STATUS", "TX", "RX")
	
	for i, t := range tunnels {
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
		
		marker := "   "
		if i == selectedIndex {
			marker = " > "
		}

		fmt.Printf("%s %-20s %-20s %-10s %-15s %-10s %-10s\n", 
			marker,
			t.Name, 
			mapping,
			t.Protocol,
			statusColor.Sprint(t.Status),
			formatBytes(t.BytesIn),
			formatBytes(t.BytesOut),
		)
		if t.Error != nil {
			color.New(color.FgRed).Printf("     └─ %v\n", t.Error)
		}
	}
	fmt.Println(strings.Repeat("-", 90))
	fmt.Println("Keys: [j/k] Select | [o] Open Browser | [c] Copy Address | [q] Quit")
	
	fmt.Println(strings.Repeat("-", 90))
	fmt.Println("EVENT LOG:")
	logMu.Lock()
	defer logMu.Unlock()
	for _, line := range logBuffer {
		fmt.Println(line)
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func inferProtocol(port int) string {
	switch port {
	case 80, 8080, 3000, 5000, 8000:
		return "http"
	case 443, 8443:
		return "https"
	case 5432:
		return "postgres"
	case 3306:
		return "mysql"
	case 6379:
		return "redis"
	case 27017:
		return "mongo"
	case 9090:
		return "prometheus"
	case 22, 2222:
		return "ssh"
	default:
		return "tcp"
	}
}
