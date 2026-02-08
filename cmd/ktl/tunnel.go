package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/stack"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
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
	Protocol    string `json:"protocol"`
	ListenAddr  string `json:"listenAddr"`
	KubeContext string `json:"kubeContext"`
	Health      string `json:"health"`
}

var (
	logBuffer []string
	logMu     sync.Mutex
	tunnels   []*Tunnel
	tunnelsMu sync.RWMutex
	
	clientCache = make(map[string]*kube.Client)
	clientMu    sync.Mutex
)

func getClient(ctx context.Context, kubeconfig, kubeContext string) (*kube.Client, error) {
	clientMu.Lock()
	defer clientMu.Unlock()
	
	key := fmt.Sprintf("%s|%s", kubeconfig, kubeContext)
	if c, ok := clientCache[key]; ok {
		return c, nil
	}
	
	c, err := kube.New(ctx, kubeconfig, kubeContext)
	if err != nil {
		return nil, err
	}
	clientCache[key] = c
	return c, nil
}

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
	var share bool
	var deps bool
	var hosts bool
	var execCmd string
	var envFrom string
	var web bool
	var stackConfig string
	cmd := &cobra.Command{
		Use:   "tunnel [SERVICE_OR_POD...]",
		Short: "Smart, resilient port-forwarding for multiple services",
		Long: `Auto-detects ports and manages multiple resilient tunnels.
Examples:
  ktl tunnel app --share         # Listen on 0.0.0.0 (share with LAN)
  ktl tunnel app --deps          # Also tunnel to app's dependencies (from stack.yaml)
  ktl tunnel app --hosts         # Add 'app.local' to /etc/hosts (requires sudo)
  ktl tunnel db --exec "npm run migrate"  # Run script when tunnel is ready
  ktl tunnel db --env-from deployment/app --exec "go run ." # Run local app with remote env
  ktl tunnel app --web           # Start web dashboard`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTunnel(cmd.Context(), kubeconfig, kubeContext, namespace, args, share, deps, hosts, execCmd, envFrom, web, stackConfig)
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")
	cmd.Flags().BoolVar(&share, "share", false, "Listen on all interfaces (0.0.0.0) to share with LAN")
	cmd.Flags().BoolVar(&deps, "deps", false, "Recursively tunnel to dependencies defined in stack.yaml")
	cmd.Flags().BoolVar(&hosts, "hosts", false, "Update /etc/hosts with .local aliases (requires sudo)")
	cmd.Flags().StringVar(&execCmd, "exec", "", "Command to run when all tunnels are active")
	cmd.Flags().StringVar(&envFrom, "env-from", "", "Fetch environment variables from workload (e.g. deployment/app)")
	cmd.Flags().BoolVar(&web, "web", false, "Start web dashboard on port 4545")
	cmd.Flags().StringVar(&stackConfig, "config", "", "Path to stack.yaml (used with --deps)")
	
	cmd.AddCommand(newTunnelSaveCommand())
	cmd.AddCommand(newTunnelListCommand())
	
	return cmd
}

func newTunnelSaveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "save NAME TARGET...",
		Short: "Save a tunnel profile",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			targets := args[1:]
			return saveTunnelProfile(name, targets)
		},
	}
}

func newTunnelListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved tunnel profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := loadTunnelProfiles()
			if err != nil {
				return err
			}
			if len(profiles) == 0 {
				fmt.Println("No saved profiles.")
				return nil
			}
			fmt.Println("Saved Profiles:")
			for name, targets := range profiles {
				fmt.Printf("  - %s: %s\n", name, strings.Join(targets, " "))
			}
			return nil
		},
	}
}

func runTunnel(ctx context.Context, kubeconfig, kubeContext *string, namespace string, targets []string, share bool, deps bool, hosts bool, execCmd string, envFrom string, web bool, stackConfig string) error {
	// Check for profile expansion
	if len(targets) == 1 {
		profiles, _ := loadTunnelProfiles()
		if expanded, ok := profiles[targets[0]]; ok {
			fmt.Printf("Loaded profile '%s': %s\n", targets[0], strings.Join(expanded, ", "))
			targets = expanded
		}
	}

	if deps {
		var err error
		targets, err = expandDependencies(ctx, targets, stackConfig)
		if err != nil {
			return fmt.Errorf("failed to expand dependencies: %w", err)
		}
		fmt.Printf("Expanded targets with dependencies: %s\n", strings.Join(targets, ", "))
	}

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

	// Fetch Env if requested
	var fetchedEnv []string
	if envFrom != "" {
		fmt.Printf("Fetching environment from %s...\n", envFrom)
		var err error
		fetchedEnv, err = kube.FetchWorkloadEnv(ctx, kClient, namespace, envFrom)
		if err != nil {
			return fmt.Errorf("failed to fetch env: %w", err)
		}
		fmt.Printf("Loaded %d environment variables.\n", len(fetchedEnv))
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

	tunnelsMu.Lock()
	tunnels = make([]*Tunnel, len(targets))
	for i, t := range targets {
		tunnels[i] = parseTarget(t, namespace)
		if share {
			tunnels[i].ListenAddr = "0.0.0.0"
		} else {
			tunnels[i].ListenAddr = "127.0.0.1"
		}
	}
	tunnelsMu.Unlock()

	if hosts {
		if err := updateHostsFile(tunnels); err != nil {
			fmt.Printf("Warning: failed to update /etc/hosts: %v\n", err)
			fmt.Println("Try running with sudo if you want DNS aliases.")
			time.Sleep(2 * time.Second)
		} else {
			defer restoreHostsFile()
		}
	}

	// TUI Loop
	fmt.Print("\033[H\033[2J") // Clear screen
	
	// Enable Raw Mode for interactive keys
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err == nil {
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	selectedIndex := 0
	if !web {
		printTable(tunnels, selectedIndex)
	} else {
		fmt.Println("Web Dashboard enabled at http://localhost:4545")
		go startWebServer(4545)
	}

	var wg sync.WaitGroup
	for _, t := range tunnels {
		wg.Add(1)
		go func(tun *Tunnel) {
			defer wg.Done()
			maintainTunnel(ctx, kClient, kc, tun)
		}(t)
	}

	// Input Loop
	if !web {
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
					tunnelsMu.RLock()
					if selectedIndex < len(tunnels)-1 {
						selectedIndex++
						printTable(tunnels, selectedIndex)
					}
					tunnelsMu.RUnlock()
				case 'k', 'w': // Up
					if selectedIndex > 0 {
						selectedIndex--
						tunnelsMu.RLock()
						printTable(tunnels, selectedIndex)
						tunnelsMu.RUnlock()
					}
				case 'o': // Open
					tunnelsMu.RLock()
					t := tunnels[selectedIndex]
					tunnelsMu.RUnlock()
					if strings.HasPrefix(t.Protocol, "http") {
						openBrowser(fmt.Sprintf("%s://localhost:%d", t.Protocol, t.LocalPort))
					}
				case 'c': // Copy
					tunnelsMu.RLock()
					t := tunnels[selectedIndex]
					tunnelsMu.RUnlock()
					copyToClipboard(fmt.Sprintf("localhost:%d", t.LocalPort))
				}
			}
		}()
	} else {
		// Just handle Ctrl+C signal
		go func() {
			c := make(chan os.Signal, 1)
			_ = c
			// signal.Notify(c, os.Interrupt) ... (cobra handles this usually but let's be safe if we want custom cleanup)
			// For now, assume user just hits Ctrl+C and it kills process.
			// But we need to keep main loop alive.
		}()
	}

	// Refresher loop
	executed := false
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !web {
					tunnelsMu.RLock()
					printTable(tunnels, selectedIndex)
					tunnelsMu.RUnlock()
				}
				
				// Check for exec
				if execCmd != "" && !executed {
					allReady := true
					for _, t := range tunnels {
						if t.Status != "Active" || t.LocalPort == 0 {
							allReady = false
							break
						}
					}
					if allReady {
						executed = true
						logEvent("Executing: %s", execCmd)
						parts := strings.Fields(execCmd)
						cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
						
						// Inject Env
						if len(fetchedEnv) > 0 {
							cmd.Env = append(os.Environ(), fetchedEnv...)
						} else {
							cmd.Env = os.Environ()
						}
						
						// We can't share stdout/stderr easily with TUI running.
						// Capture output and log it?
						out, err := cmd.CombinedOutput()
						if err != nil {
							logEvent("Exec failed: %v", err)
						} else {
							logEvent("Exec success (output hidden)")
						}
						// If output is short, maybe log it?
						if len(out) > 0 {
							lines := strings.Split(string(out), "\n")
							for _, line := range lines {
								if strings.TrimSpace(line) != "" {
									logEvent("> %s", line)
								}
							}
						}
					}
				}
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

func maintainTunnel(ctx context.Context, defaultClient *kube.Client, kubeconfig string, t *Tunnel) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Resolve Client
		kClient := defaultClient
		if t.KubeContext != "" {
			var err error
			kClient, err = getClient(ctx, kubeconfig, t.KubeContext)
			if err != nil {
				t.Status = "Client Error"
				t.Error = err
				logEvent("Failed to get client for context %s: %v", t.KubeContext, err)
				time.Sleep(5 * time.Second)
				continue
			}
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
		if !isPortAvailable(t.LocalPort, t.ListenAddr) {
			// Auto-fix: try next ports
			found := false
			originalPort := t.LocalPort
			for i := 1; i <= 10; i++ {
				next := originalPort + i
				if isPortAvailable(next, t.ListenAddr) {
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
	pf, err := portforward.NewOnAddresses(
		dialer,
		[]string{t.ListenAddr},
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
		
		// Start Health Check Loop
		go monitorHealth(ctx, t)
		
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return nil
	}

	return <-errChan
}

func monitorHealth(ctx context.Context, t *Tunnel) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	check := func() {
		addr := fmt.Sprintf("127.0.0.1:%d", t.LocalPort)
		if t.ListenAddr == "0.0.0.0" {
			addr = fmt.Sprintf("127.0.0.1:%d", t.LocalPort)
		}

		if strings.HasPrefix(t.Protocol, "http") {
			client := http.Client{Timeout: 2 * time.Second}
			resp, err := client.Head(fmt.Sprintf("%s://%s", t.Protocol, addr))
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode < 500 {
					t.Health = "OK"
				} else {
					t.Health = fmt.Sprintf("HTTP %d", resp.StatusCode)
				}
			} else {
				t.Health = "FAIL"
			}
		} else {
			// TCP Check
			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err == nil {
				conn.Close()
				t.Health = "OK"
			} else {
				t.Health = "FAIL"
			}
		}
	}

	check() // Initial check
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.StopChan:
			return
		case <-ticker.C:
			check()
		}
	}
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
	// formats: name, port:name, name:port, name@context
	// 1. Check for context
	var ctx string
	if strings.Contains(raw, "@") {
		parts := strings.Split(raw, "@")
		raw = parts[0]
		ctx = parts[1]
	}

	t := &Tunnel{
		Name:        raw,
		Target:      raw,
		Namespace:   ns,
		KubeContext: ctx,
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

func isPortAvailable(port int, addr string) bool {
	if addr == "" {
		addr = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", addr, port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func expandDependencies(ctx context.Context, targets []string, configPath string) ([]string, error) {
	// If no config path provided, look for stack.yaml in current dir
	if configPath == "" {
		if _, err := os.Stat("stack.yaml"); err == nil {
			configPath = "stack.yaml"
		} else if _, err := os.Stat("release.yaml"); err == nil {
			configPath = "release.yaml"
		} else {
			return targets, fmt.Errorf("stack.yaml not found (pass --config)")
		}
	}

	root, _ := filepath.Abs(filepath.Dir(configPath))
	u, err := stack.Discover(root)
	if err != nil {
		return targets, err
	}
	p, err := stack.Compile(u, stack.CompileOptions{})
	if err != nil {
		return targets, err
	}
	g, err := stack.BuildGraph(p)
	if err != nil {
		return targets, err
	}

	// Map simple names to IDs for lookup
	// We assume we are operating on the current context/cluster, so we filter by that if possible.
	// But for now, let's just match by Name.
	nameToID := make(map[string]string)
	idToName := make(map[string]string)
	for _, n := range p.Nodes {
		nameToID[n.Name] = n.ID
		idToName[n.ID] = n.Name
	}

	expanded := make(map[string]struct{})
	var result []string

	var visit func(name string)
	visit = func(name string) {
		if _, seen := expanded[name]; seen {
			return
		}
		expanded[name] = struct{}{}
		result = append(result, name)

		id, ok := nameToID[name]
		if !ok {
			return // Not in stack
		}
		
		deps := g.DepsOf(id)
		for _, depID := range deps {
			if depName, ok := idToName[depID]; ok {
				visit(depName)
			}
		}
	}

	for _, t := range targets {
		parts := strings.Split(t, ":")
		// Handle "port:name" or "name:port" or "name"
		var name string
		if len(parts) > 1 {
			// Heuristic: if first part is int, name is second
			if _, err := strconv.Atoi(parts[0]); err == nil {
				name = parts[1]
			} else {
				name = parts[0]
			}
		} else {
			name = parts[0]
		}
		visit(name)
	}
	
	return result, nil
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
		
		mapping := fmt.Sprintf("%s:%d -> :%d", t.ListenAddr, t.LocalPort, t.RemotePort)
		if t.LocalPort == 0 {
			mapping = "resolving..."
		}
		
		marker := "   "
		if i == selectedIndex {
			marker = " > "
		}

		targetDisplay := t.Name
		if t.KubeContext != "" {
			targetDisplay += "@" + t.KubeContext
		}

		statusStr := t.Status
		if t.Status == "Active" && t.Health != "" {
			if t.Health == "OK" {
				statusStr += " ✅"
			} else {
				statusStr += " ❌ " + t.Health
			}
		}

		fmt.Printf("%s %-20s %-20s %-10s %-15s %-10s %-10s\n", 
			marker,
			targetDisplay, 
			mapping,
			t.Protocol,
			statusColor.Sprint(statusStr),
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

func loadTunnelProfiles() (map[string][]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".ktl", "tunnels.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string][]string), nil
	}
	if err != nil {
		return nil, err
	}
	var profiles map[string][]string
	if err := yaml.Unmarshal(data, &profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}

func saveTunnelProfile(name string, targets []string) error {
	profiles, err := loadTunnelProfiles()
	if err != nil {
		return err
	}
	profiles[name] = targets
	
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".ktl")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	
	data, err := yaml.Marshal(profiles)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "tunnels.yaml"), data, 0644)
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

const hostsStartMarker = "# ktl-tunnel-start"
const hostsEndMarker = "# ktl-tunnel-end"

func updateHostsFile(tunnels []*Tunnel) error {
	path := "/etc/hosts"
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	
	lines := strings.Split(string(content), "\n")
	var newLines []string
	inBlock := false
	
	for _, line := range lines {
		if strings.TrimSpace(line) == hostsStartMarker {
			inBlock = true
			continue
		}
		if strings.TrimSpace(line) == hostsEndMarker {
			inBlock = false
			continue
		}
		if !inBlock {
			newLines = append(newLines, line)
		}
	}
	
	// Remove trailing empty lines to be clean
	for len(newLines) > 0 && newLines[len(newLines)-1] == "" {
		newLines = newLines[:len(newLines)-1]
	}
	
	newLines = append(newLines, hostsStartMarker)
	for _, t := range tunnels {
		// Use t.Name + .local
		// If t.Name has dots or slashes, sanitize?
		// Usually service names are simple.
		name := t.Name
		if strings.Contains(name, "/") {
			parts := strings.Split(name, "/")
			name = parts[len(parts)-1]
		}
		name = strings.Split(name, ":")[0] // handle port:name
		
		newLines = append(newLines, fmt.Sprintf("127.0.0.1 %s.local", name))
	}
	newLines = append(newLines, hostsEndMarker)
	newLines = append(newLines, "") // newline at end
	
	return os.WriteFile(path, []byte(strings.Join(newLines, "\n")), 0644)
}

func restoreHostsFile() {
	path := "/etc/hosts"
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}
	
	lines := strings.Split(string(content), "\n")
	var newLines []string
	inBlock := false
	
	for _, line := range lines {
		if strings.TrimSpace(line) == hostsStartMarker {
			inBlock = true
			continue
		}
		if strings.TrimSpace(line) == hostsEndMarker {
			inBlock = false
			continue
		}
		if !inBlock {
			newLines = append(newLines, line)
		}
	}
	
	_ = os.WriteFile(path, []byte(strings.Join(newLines, "\n")), 0644)
}
