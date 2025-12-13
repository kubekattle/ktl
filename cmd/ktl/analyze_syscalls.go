// analyze_syscalls.go wires up the 'ktl analyze syscalls' command so responders can spin up helper pods and collect syscall profiles from live workloads.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/sniff"
	"github.com/example/ktl/internal/syscalls"
	"github.com/example/ktl/internal/syscallscast"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"
)

const defaultSyscallsImage = "ghcr.io/example/ktl/syscalls-helper:latest"

func newAnalyzeSyscallsCommand(namespace *string, kubeconfig *string, kubeContext *string) *cobra.Command {
	var (
		targetArgs      []string
		matchFilters    []string
		format          string
		helperImage     string
		pullPolicyFlag  string
		privileged      bool
		profileDuration time.Duration
		startupTimeout  time.Duration
		topN            int
		targetPID       int
		uiAddr          string
		wsListenAddr    string
	)

	format = "table"
	helperImage = defaultSyscallsImage
	pullPolicyFlag = string(corev1.PullIfNotPresent)
	privileged = true
	profileDuration = 30 * time.Second
	startupTimeout = 45 * time.Second
	targetPID = 1
	topN = 15

	cmd := &cobra.Command{
		Use:   "syscalls",
		Short: "Profile pod syscalls through ephemeral helpers",
		Long: `ktl analyze syscalls injects a helper container that shares the target pod's PID namespace, attaches strace/bcc tooling, and reports the busiest syscalls.
Targets follow the same format as analyze traffic: --target [namespace/]<pod>[:container]. Examples:
  ktl analyze syscalls --target checkout-0 --profile-duration 15s --top 10
  ktl analyze syscalls --namespace payments --target checkout-0:proxy --match open,connect
  ktl analyze syscalls --target payments/checkout-0 --target payments/checkout-1 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(targetArgs) == 0 {
				return fmt.Errorf("at least one --target is required (format: [namespace/]<pod>[:container])")
			}
			if profileDuration <= 0 {
				return fmt.Errorf("--profile-duration must be > 0")
			}
			if topN < 0 {
				return fmt.Errorf("--top must be >= 0")
			}
			if targetPID <= 0 {
				return fmt.Errorf("--target-pid must be >= 1")
			}

			outputFormat := strings.ToLower(strings.TrimSpace(format))
			switch outputFormat {
			case "table", "json":
			default:
				return fmt.Errorf("unsupported --format %q (use table or json)", format)
			}

			traceFilter, err := normalizeSyscallMatches(matchFilters)
			if err != nil {
				return err
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

			pullPolicy := corev1.PullPolicy(pullPolicyFlag)
			if err := validatePullPolicy(pullPolicy); err != nil {
				return err
			}

			injector := sniff.NewInjector(kubeClient, sniff.InjectorOptions{
				Image:          helperImage,
				PullPolicy:     pullPolicy,
				Privileged:     privileged,
				StartupTimeout: startupTimeout,
			})
			runner := syscalls.NewRunner(kubeClient)

			type helperInstance struct {
				spec   sniffTarget
				helper string
			}

			var helpers []helperInstance
			defer func() {
				if len(helpers) == 0 {
					return
				}
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				for _, inst := range helpers {
					if err := injector.Shutdown(cleanupCtx, inst.spec.Namespace, inst.spec.Pod, inst.helper); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to stop syscall helper %s/%s:%s: %v\n", inst.spec.Namespace, inst.spec.Pod, inst.helper, err)
					}
				}
			}()

			for _, spec := range targetSpecs {
				helper, err := injector.Ensure(ctx, sniff.PodRef{
					Namespace:       spec.Namespace,
					Pod:             spec.Pod,
					TargetContainer: spec.Container,
				})
				if err != nil {
					return err
				}
				helpers = append(helpers, helperInstance{spec: spec, helper: helper})
			}

			contextName := ""
			if kubeContext != nil {
				contextName = *kubeContext
			}
			clusterInfo := describeClusterLabel(kubeClient, contextName)
			observer := buildSyscallsObservers(ctx, cmd, clusterInfo, uiAddr, wsListenAddr)

			var (
				results []syscalls.ProfileResult
				mu      sync.Mutex
			)
			eg, egCtx := errgroup.WithContext(ctx)
			for _, inst := range helpers {
				inst := inst
				eg.Go(func() error {
					res, err := runner.Profile(egCtx, syscalls.ProfileRequest{
						Namespace:       inst.spec.Namespace,
						Pod:             inst.spec.Pod,
						Container:       inst.helper,
						TargetPID:       targetPID,
						Duration:        profileDuration,
						TraceFilter:     traceFilter,
						Label:           fmt.Sprintf("%s/%s:%s", inst.spec.Namespace, inst.spec.Pod, inst.spec.Container),
						TargetContainer: inst.spec.Container,
					})
					if err != nil {
						return err
					}
					res.Rows = truncateRows(res.Rows, topN)
					mu.Lock()
					results = append(results, res)
					mu.Unlock()
					if observer != nil {
						observer.ObserveProfile(res)
					}
					return nil
				})
			}

			if err := eg.Wait(); err != nil {
				return err
			}

			sort.Slice(results, func(i, j int) bool {
				return results[i].Label < results[j].Label
			})

			switch outputFormat {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				payload := make([]syscallsReport, 0, len(results))
				for _, res := range results {
					payload = append(payload, newSyscallsReport(res))
				}
				return encoder.Encode(payload)
			default:
				out := cmd.OutOrStdout()
				for idx, res := range results {
					if idx > 0 {
						fmt.Fprintln(out)
					}
					renderSyscallTable(out, res)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringSliceVar(&targetArgs, "target", nil, "Pod target in the form [namespace/]<pod>[:container]; repeat to profile multiple pods")
	cmd.Flags().DurationVar(&profileDuration, "profile-duration", profileDuration, "How long to capture syscalls before summarizing")
	cmd.Flags().IntVar(&topN, "top", topN, "Only show the top N syscalls by time (0 = all)")
	cmd.Flags().StringSliceVar(&matchFilters, "match", nil, "Comma or repeatable list of syscall names/groups to trace (open,connect,execve,file,network)")
	cmd.Flags().StringVar(&format, "format", format, "Output format: table or json")
	cmd.Flags().IntVar(&targetPID, "target-pid", targetPID, "PID to attach to inside the target container (default 1)")
	cmd.Flags().StringVar(&helperImage, "image", helperImage, "Container image that provides strace/bcc tooling inside the helper container")
	cmd.Flags().StringVar(&pullPolicyFlag, "image-pull-policy", pullPolicyFlag, "Image pull policy for the helper container (Always, IfNotPresent, Never)")
	cmd.Flags().BoolVar(&privileged, "privileged", privileged, "Run the helper container in privileged mode (required for ptrace/bpf)")
	cmd.Flags().DurationVar(&startupTimeout, "startup-timeout", startupTimeout, "How long to wait for the helper container to become ready")
	cmd.Flags().StringVar(&uiAddr, "ui", "", "Serve a live HTML view of syscall summaries at this address (e.g. :8081)")
	if flag := cmd.Flags().Lookup("ui"); flag != nil {
		flag.NoOptDefVal = ":8081"
	}
	cmd.Flags().StringVar(&wsListenAddr, "ws-listen", "", "Serve a raw WebSocket feed of syscall summaries at this address (e.g. :9092)")

	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "analyze syscalls Flags")
	return cmd
}

func normalizeSyscallMatches(values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	var normalized []string
	seen := map[string]struct{}{}
	for _, raw := range values {
		for _, chunk := range strings.Split(raw, ",") {
			name := strings.TrimSpace(chunk)
			if name == "" {
				continue
			}
			for _, ch := range name {
				if !(ch == '-' || ch == '_' || ch == '+' || ch == '!' || (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')) {
					return "", fmt.Errorf("invalid --match value %q: only alphanumeric characters plus - _ + ! are supported", name)
				}
			}
			name = strings.ToLower(name)
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			normalized = append(normalized, name)
		}
	}
	return strings.Join(normalized, ","), nil
}

func truncateRows(rows []syscalls.Row, top int) []syscalls.Row {
	if top <= 0 || top >= len(rows) {
		return rows
	}
	trimmed := make([]syscalls.Row, top)
	copy(trimmed, rows[:top])
	return trimmed
}

type syscallsReport struct {
	Label           string         `json:"label"`
	Namespace       string         `json:"namespace"`
	Pod             string         `json:"pod"`
	TargetContainer string         `json:"targetContainer"`
	DurationSeconds float64        `json:"durationSeconds"`
	TargetPID       int            `json:"targetPid"`
	TraceFilter     string         `json:"traceFilter,omitempty"`
	TotalCalls      int            `json:"totalCalls"`
	TotalErrors     int            `json:"totalErrors"`
	TotalSeconds    float64        `json:"totalSeconds"`
	Rows            []syscalls.Row `json:"rows"`
	Notes           []string       `json:"notes,omitempty"`
}

func newSyscallsReport(res syscalls.ProfileResult) syscallsReport {
	return syscallsReport{
		Label:           res.Label,
		Namespace:       res.Namespace,
		Pod:             res.Pod,
		TargetContainer: res.TargetContainer,
		DurationSeconds: res.Duration.Seconds(),
		TargetPID:       res.TargetPID,
		TraceFilter:     res.TraceFilter,
		TotalCalls:      res.TotalCalls,
		TotalErrors:     res.TotalErrors,
		TotalSeconds:    res.TotalSeconds,
		Rows:            res.Rows,
		Notes:           res.Notes,
	}
}

func renderSyscallTable(out io.Writer, res syscalls.ProfileResult) {
	fmt.Fprintf(out, "[%s] traced %s/%s (container %s, pid %d) for %s\n", res.Label, res.Namespace, res.Pod, res.TargetContainer, res.TargetPID, res.Duration.Round(time.Millisecond))
	if res.TraceFilter != "" {
		fmt.Fprintf(out, "Filters: trace=%s\n", res.TraceFilter)
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SYSCALL\t% TIME\tSECONDS\tUSEC/CALL\tCALLS\tERRORS")
	if len(res.Rows) == 0 {
		fmt.Fprintln(tw, "(no data)\t\t\t\t\t")
	}
	for _, row := range res.Rows {
		fmt.Fprintf(tw, "%s\t%.2f\t%.6f\t%.0f\t%d\t%d\n",
			row.Syscall,
			row.Percent,
			row.Seconds,
			row.UsecPerCall,
			row.Calls,
			row.Errors,
		)
	}
	tw.Flush()
	if len(res.Notes) > 0 {
		for _, note := range res.Notes {
			fmt.Fprintf(out, "Note: %s\n", note)
		}
	}
}

type syscallsObserver interface {
	ObserveProfile(syscalls.ProfileResult)
}

type syscallsObserverList struct {
	items []syscallsObserver
}

func (o syscallsObserverList) ObserveProfile(res syscalls.ProfileResult) {
	for _, obs := range o.items {
		if obs == nil {
			continue
		}
		obs.ObserveProfile(res)
	}
}

func buildSyscallsObservers(ctx context.Context, cmd *cobra.Command, clusterInfo, uiAddr, wsAddr string) syscallsObserver {
	logger := ctrl.Log.WithName("syscallscast")
	var observers []syscallsObserver
	start := func(addr string, mode syscallscast.Mode, label string) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return
		}
		srv := syscallscast.New(addr, mode, clusterInfo, logger.WithName(label))
		go func() {
			if err := srv.Run(ctx); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: syscalls %s server stopped: %v\n", label, err)
			}
		}()
		fmt.Fprintf(cmd.ErrOrStderr(), "Serving ktl syscalls %s view on %s\n", label, addr)
		observers = append(observers, srv)
	}
	start(uiAddr, syscallscast.ModeWeb, "ui")
	start(wsAddr, syscallscast.ModeWS, "ws")
	if len(observers) == 0 {
		return nil
	}
	return syscallsObserverList{items: observers}
}
