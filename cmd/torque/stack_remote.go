package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/grpcutil"
	"github.com/ingresslabs/torque/internal/stack"
	apiv1 "github.com/ingresslabs/torque/pkg/api/torque/api/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

func runRemoteStackPlanCommand(cmd *cobra.Command, common stackCommandCommon, bundlePath string, bundleDiffSummary bool) error {
	if strings.TrimSpace(bundlePath) != "" || bundleDiffSummary {
		return fmt.Errorf("torque stack plan --remote-agent does not support --bundle/--bundle-diff-summary; run the bundle operation on the agent host")
	}
	conn, err := dialRemoteStackAgent(cmd, derefString(common.remoteAgent))
	if err != nil {
		return err
	}
	defer conn.Close()
	client := apiv1.NewStackServiceClient(conn)
	sessionID := newSessionID("remote-stack")
	req := &apiv1.StackPlanRequest{
		Selector:  stackSelectorProtoFromCommon(cmd, common),
		SessionId: sessionID,
		Requester: defaultRequester(),
	}
	resp, err := client.Plan(cmd.Context(), req)
	if err != nil {
		return err
	}
	return renderRemoteStackPlan(cmd, resp.GetJson(), strings.ToLower(strings.TrimSpace(derefString(common.output))))
}

func runRemoteStackRunCommand(cmd *cobra.Command, kind stackRunKind, common stackCommandCommon, opts stackRunCLIOptions) error {
	if opts.Resume || opts.Replan || strings.TrimSpace(opts.SealedDir) != "" || strings.TrimSpace(opts.FromBundle) != "" {
		return fmt.Errorf("torque stack %s --remote-agent currently supports config-based runs only; resume and sealed bundles must run on the agent host", kind)
	}
	if opts.PolicyOverride {
		return fmt.Errorf("torque stack %s --remote-agent does not support --policy-override yet; run the approved override on the agent host", kind)
	}
	conn, err := dialRemoteStackAgent(cmd, derefString(common.remoteAgent))
	if err != nil {
		return err
	}
	defer conn.Close()
	client := apiv1.NewStackServiceClient(conn)
	selector := stackSelectorProtoFromCommon(cmd, common)
	planOnly := common.planOnly != nil && *common.planOnly
	if planOnly || kind == stackRunDelete && !opts.Yes {
		plan, err := client.Plan(cmd.Context(), &apiv1.StackPlanRequest{
			Selector:  selector,
			SessionId: newSessionID("remote-stack"),
			Requester: defaultRequester(),
		})
		if err != nil {
			return err
		}
		if planOnly {
			return renderRemoteStackPlan(cmd, plan.GetJson(), strings.ToLower(strings.TrimSpace(derefString(common.output))))
		}
		threshold := opts.DeleteConfirmThreshold
		if threshold <= 0 {
			threshold = 20
		}
		if threshold > 0 && plan.GetNodeCount() >= int32(threshold) {
			dec, err := approvalMode(cmd, false, false)
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("About to delete %d stack nodes on remote agent %s. Only 'yes' will be accepted:", plan.GetNodeCount(), strings.TrimSpace(derefString(common.remoteAgent)))
			if err := confirmAction(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), dec, prompt, confirmModeYes, ""); err != nil {
				return err
			}
		}
	}
	sessionID := newSessionID("remote-stack")
	trySetRemoteMirrorSessionMeta(cmd.Context(), conn, sessionID, &apiv1.MirrorSessionMeta{
		Command:   cmd.CommandPath(),
		Args:      append([]string(nil), os.Args[1:]...),
		Requester: defaultRequester(),
	}, map[string]string{"stack.command": string(kind)})
	req := &apiv1.StackRunRequest{
		Command:   string(kind),
		Selector:  selector,
		SessionId: sessionID,
		Requester: defaultRequester(),
	}
	req.Options, err = stackRunOptionsProtoFromCLI(cmd, opts)
	if err != nil {
		return err
	}
	var stream interface {
		Recv() (*apiv1.StackEvent, error)
	}
	if kind == stackRunDelete {
		stream, err = client.Delete(cmd.Context(), req)
	} else {
		stream, err = client.Apply(cmd.Context(), req)
	}
	if err != nil {
		return err
	}
	return consumeRemoteStackStream(cmd, stream, strings.ToLower(strings.TrimSpace(derefString(common.output))))
}

func runRemoteStackStatusCommand(cmd *cobra.Command, rootDir *string, remoteAgent *string, runID string, follow bool, limit int, format string) error {
	if follow {
		return fmt.Errorf("torque stack status --remote-agent does not support --follow; use torque session tail against the remote mirror session")
	}
	conn, err := dialRemoteStackAgent(cmd, derefString(remoteAgent))
	if err != nil {
		return err
	}
	defer conn.Close()
	statusFormat := strings.ToLower(strings.TrimSpace(format))
	if statusFormat == "" || statusFormat == "table" || statusFormat == "tty" {
		statusFormat = "json"
	}
	resp, err := apiv1.NewStackServiceClient(conn).Status(cmd.Context(), &apiv1.StackStatusRequest{
		Root:      derefString(rootDir),
		RunId:     runID,
		Limit:     int32(limit),
		Format:    statusFormat,
		Requester: defaultRequester(),
	})
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table", "tty":
		var summary stack.RunSummary
		if err := json.Unmarshal([]byte(resp.GetJson()), &summary); err != nil {
			return err
		}
		return stack.PrintRunStatusTable(cmd.OutOrStdout(), resp.GetRunId(), &summary)
	case "json", "raw":
		_, err := fmt.Fprint(cmd.OutOrStdout(), resp.GetJson())
		return err
	default:
		return fmt.Errorf("unknown --format %q (expected raw|table|json|tty)", format)
	}
}

func dialRemoteStackAgent(cmd *cobra.Command, addr string) (*grpc.ClientConn, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("remote agent address is required")
	}
	creds, err := remoteTransportCredentials(cmd, addr)
	if err != nil {
		return nil, err
	}
	return grpcutil.Dial(cmd.Context(), addr,
		grpc.WithTransportCredentials(creds),
		grpcutil.WithBearerToken(remoteToken(cmd)),
	)
}

func stackSelectorProtoFromCommon(cmd *cobra.Command, common stackCommandCommon) *apiv1.StackSelector {
	out := &apiv1.StackSelector{
		Root:           derefString(common.rootDir),
		Profile:        derefString(common.profile),
		KubeContext:    derefString(common.kubeContext),
		KubeconfigPath: derefString(common.kubeconfig),
		SecretProvider: derefString(common.secretProvider),
		SecretConfig:   derefString(common.secretConfig),
	}
	if common.clusters != nil && len(*common.clusters) > 0 {
		out.Clusters = splitCSV(*common.clusters)
	}
	if common.tags != nil && len(*common.tags) > 0 {
		out.Tags = splitCSV(*common.tags)
	}
	if common.fromPaths != nil && len(*common.fromPaths) > 0 {
		out.FromPaths = splitCSV(*common.fromPaths)
	}
	if common.releases != nil && len(*common.releases) > 0 {
		out.Releases = splitCSV(*common.releases)
	}
	if common.gitRange != nil {
		out.GitRange = strings.TrimSpace(*common.gitRange)
	}
	setBoolIfChanged := func(flag string, value bool) *bool {
		if cmd != nil && flagChanged(cmd, flag) {
			v := value
			return &v
		}
		return nil
	}
	if common.gitIncludeDeps != nil {
		out.GitIncludeDeps = setBoolIfChanged("git-include-deps", *common.gitIncludeDeps)
	}
	if common.gitIncludeDependents != nil {
		out.GitIncludeDependents = setBoolIfChanged("git-include-dependents", *common.gitIncludeDependents)
	}
	if common.includeDeps != nil {
		out.IncludeDeps = setBoolIfChanged("include-deps", *common.includeDeps)
	}
	if common.includeDependents != nil {
		out.IncludeDependents = setBoolIfChanged("include-dependents", *common.includeDependents)
	}
	if common.allowMissingDeps != nil {
		out.AllowMissingDeps = setBoolIfChanged("allow-missing-deps", *common.allowMissingDeps)
	}
	if common.inferDeps != nil {
		out.InferDeps = setBoolIfChanged("infer-deps", *common.inferDeps)
	}
	if common.inferConfigRefs != nil {
		out.InferConfigRefs = setBoolIfChanged("infer-config-refs", *common.inferConfigRefs)
	}
	return out
}

func stackRunOptionsProtoFromCLI(cmd *cobra.Command, opts stackRunCLIOptions) (*apiv1.StackRunOptions, error) {
	out := &apiv1.StackRunOptions{
		RunId:     strings.TrimSpace(opts.RunID),
		LockOwner: strings.TrimSpace(opts.LockOwner),
	}
	setBool := func(flag string, value bool) *bool {
		if cmd != nil && flagChanged(cmd, flag) {
			v := value
			return &v
		}
		return nil
	}
	out.FailFast = setBool("fail-fast", opts.FailFast)
	out.ContinueOnError = setBool("continue-on-error", opts.ContinueOnError)
	if opts.Yes {
		v := true
		out.Yes = &v
	}
	out.DryRun = setBool("dry-run", opts.DryRun)
	out.Diff = setBool("diff", opts.Diff)
	out.CacheApply = setBool("cache-apply", opts.CacheApply)
	out.ProgressiveConcurrency = setBool(stackFlagProgressiveConcurrency, opts.ProgressiveConcurrency)
	out.Lock = setBool("lock", opts.Lock)
	out.TakeoverLock = setBool("takeover", opts.Takeover)
	if cmd != nil && flagChanged(cmd, "helm-logs") {
		v := stackHelmLogsEnabled(opts.HelmLogs)
		out.HelmLogs = &v
	}
	if cmd != nil && flagChanged(cmd, stackFlagConcurrency) {
		out.Concurrency = int32(opts.Concurrency)
	}
	if cmd != nil && flagChanged(cmd, "retry") {
		out.Retry = int32(opts.Retry)
	}
	if cmd != nil && flagChanged(cmd, stackFlagKubeQPS) {
		out.KubeQps = opts.RunnerKubeQPS
	}
	if cmd != nil && flagChanged(cmd, stackFlagKubeBurst) {
		out.KubeBurst = int32(opts.RunnerKubeBurst)
	}
	if cmd != nil && flagChanged(cmd, stackFlagMaxParallelPerNamespace) {
		out.MaxParallelPerNamespace = int32(opts.RunnerMaxParallelPerNamespace)
	}
	if cmd != nil && flagChanged(cmd, stackFlagMaxParallelKind) {
		parsed, err := parseMaxParallelKind(opts.RunnerMaxParallelKind)
		if err != nil {
			return nil, err
		}
		if len(parsed) > 0 {
			out.MaxParallelKind = map[string]int32{}
			for k, v := range parsed {
				out.MaxParallelKind[k] = int32(v)
			}
		}
	}
	if cmd != nil && flagChanged(cmd, stackFlagParallelismGroupLimit) {
		out.ParallelismGroupLimit = int32(opts.RunnerParallelismGroupLimit)
	}
	if cmd != nil && flagChanged(cmd, stackFlagAdaptiveMin) {
		out.AdaptiveMin = int32(opts.RunnerAdaptiveMin)
	}
	if cmd != nil && flagChanged(cmd, stackFlagAdaptiveWindow) {
		out.AdaptiveWindow = int32(opts.RunnerAdaptiveWindow)
	}
	if cmd != nil && flagChanged(cmd, stackFlagAdaptiveRampSuccesses) {
		out.AdaptiveRampSuccesses = int32(opts.RunnerAdaptiveRampSuccesses)
	}
	if cmd != nil && flagChanged(cmd, stackFlagAdaptiveRampFailureRate) {
		out.AdaptiveRampMaxFailureRate = opts.RunnerAdaptiveRampFailureRate
	}
	if cmd != nil && flagChanged(cmd, stackFlagAdaptiveCooldownSevere) {
		out.AdaptiveCooldownSevere = int32(opts.RunnerAdaptiveCooldownSevere)
	}
	if cmd != nil && flagChanged(cmd, "lock-ttl") {
		out.LockTtlSeconds = int64(opts.LockTTL / time.Second)
	}
	return out, nil
}

func renderRemoteStackPlan(cmd *cobra.Command, raw string, output string) error {
	if output == "" {
		output = "table"
	}
	if output == "json" {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), strings.TrimRight(raw, "\n"))
		return err
	}
	var p stack.Plan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return err
	}
	return stack.PrintPlanTable(cmd.OutOrStdout(), &p)
}

func consumeRemoteStackStream(cmd *cobra.Command, stream interface {
	Recv() (*apiv1.StackEvent, error)
}, output string) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	for {
		ev, err := stream.Recv()
		if err != nil {
			if err == io.EOF || errorsIsContextDone(cmd.Context(), err) {
				return nil
			}
			return err
		}
		raw := strings.TrimSpace(ev.GetJson())
		if output == "json" {
			fmt.Fprintln(out, raw)
			continue
		}
		var runEv stack.RunEvent
		if err := json.Unmarshal([]byte(raw), &runEv); err != nil {
			fmt.Fprintln(errOut, raw)
			continue
		}
		node := strings.TrimSpace(runEv.NodeID)
		if node == "" {
			node = "-"
		}
		msg := strings.TrimSpace(runEv.Message)
		if msg != "" {
			fmt.Fprintf(errOut, "%s\t%s\t%s\t%d\t%s\n", runEv.TS, runEv.Type, node, runEv.Attempt, msg)
		} else {
			fmt.Fprintf(errOut, "%s\t%s\t%s\t%d\n", runEv.TS, runEv.Type, node, runEv.Attempt)
		}
	}
}

func stackHelmLogsEnabled(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "false", "0", "off":
		return false
	default:
		return true
	}
}

func errorsIsContextDone(ctx context.Context, err error) bool {
	if ctx == nil || err == nil {
		return false
	}
	return ctx.Err() != nil && strings.Contains(strings.ToLower(err.Error()), strings.ToLower(ctx.Err().Error()))
}
