package stack

import (
	"fmt"
	"maps"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	RunnerModeLocal = "local"
	RunnerModeFleet = "fleet"

	RunnerReadinessSourceStore = "store"
	RunnerReadinessStoreFile   = "file"
	RunnerReadinessStoreEtcd   = "etcd"

	RunnerReadinessOnBlock = "block"
	RunnerReadinessOnWarn  = "warn"

	RunnerFanoutOnBlock    = "block"
	RunnerFanoutOnContinue = "continue"

	RunnerFanoutDeliveryRequestReply = "requestReply"
	RunnerFanoutDeliveryJetStream    = "jetstream"

	RunnerFanoutRetryOnBlock    = "block"
	RunnerFanoutRetryOnContinue = "continue"
)

func ResolveRunnerConfig(u *Universe, profile string) (RunnerResolved, error) {
	// Defaults keep behavior stable when runner is omitted.
	base := RunnerResolved{
		Mode:                   RunnerModeLocal,
		Concurrency:            1,
		ProgressiveConcurrency: false,
		Readiness: RunnerReadinessResolved{
			Source:              RunnerReadinessSourceStore,
			Store:               RunnerReadinessStoreFile,
			Tenant:              "default",
			MinReadyPercent:     100,
			FailureBudget:       0,
			StaleAfter:          45 * time.Second,
			OnInsufficientReady: RunnerReadinessOnBlock,
		},
		Fanout: RunnerFanoutResolved{
			MaxParallel:         64,
			MaxFailed:           0,
			MinSucceededPercent: 100,
			OnPartialFailure:    RunnerFanoutOnBlock,
			Delivery:            RunnerFanoutDeliveryRequestReply,
			TargetConcurrency: RunnerFanoutTargetConcurrencyResolved{
				RequireAvailable: true,
				MaxPerTarget:     1,
				LeaseTTL:         30 * time.Second,
			},
			Retry: RunnerFanoutRetryResolved{
				MaxDeliver:  3,
				AckWait:     30 * time.Second,
				OnExhausted: RunnerFanoutRetryOnBlock,
			},
		},
		Limits: RunnerLimitsResolved{
			ParallelismGroupLimit: 1,
		},
		Adaptive: RunnerAdaptiveResolved{
			Mode:               "balanced",
			Min:                1,
			Window:             20,
			RampAfterSuccesses: 2,
			RampMaxFailureRate: 0.30,
			CooldownSevere:     4,
		},
	}
	if u == nil {
		return base, nil
	}
	root := u.RootDir
	if strings.TrimSpace(root) != "" {
		base.Readiness.StorePath = filepath.Join(root, ".torque", "agent-registry.json")
	}
	sf, ok := u.Stacks[root]
	if !ok {
		return base, nil
	}
	merged := RunnerConfig{}
	mergeRunner(&merged, sf.Runner)
	if strings.TrimSpace(profile) != "" {
		if sp, ok := sf.Profiles[strings.TrimSpace(profile)]; ok {
			mergeRunner(&merged, sp.Runner)
		}
	}

	// Apply mode presets first (so explicit fields can override).
	mode := strings.ToLower(strings.TrimSpace(merged.Adaptive.Mode))
	if mode == "" {
		mode = base.Adaptive.Mode
	}
	switch mode {
	case "conservative":
		base.Adaptive.Mode = "conservative"
		base.Adaptive.Min = 1
		base.Adaptive.Window = 30
		base.Adaptive.RampAfterSuccesses = 3
		base.Adaptive.RampMaxFailureRate = 0.10
		base.Adaptive.CooldownSevere = 6
	case "aggressive":
		base.Adaptive.Mode = "aggressive"
		base.Adaptive.Min = 1
		base.Adaptive.Window = 12
		base.Adaptive.RampAfterSuccesses = 1
		base.Adaptive.RampMaxFailureRate = 0.40
		base.Adaptive.CooldownSevere = 2
	default:
		base.Adaptive.Mode = "balanced"
	}

	applyRunnerResolved(&base, merged)
	if err := ValidateRunnerResolved(base); err != nil {
		return RunnerResolved{}, err
	}
	return base, nil
}

func mergeRunner(dst *RunnerConfig, src RunnerConfig) {
	if dst == nil {
		return
	}
	if strings.TrimSpace(src.Mode) != "" {
		dst.Mode = src.Mode
	}
	if src.Concurrency != nil {
		dst.Concurrency = src.Concurrency
	}
	if src.ProgressiveConcurrency != nil {
		dst.ProgressiveConcurrency = src.ProgressiveConcurrency
	}
	if src.KubeQPS != nil {
		dst.KubeQPS = src.KubeQPS
	}
	if src.KubeBurst != nil {
		dst.KubeBurst = src.KubeBurst
	}
	mergeRunnerReadiness(&dst.Readiness, src.Readiness)
	mergeRunnerFanout(&dst.Fanout, src.Fanout)
	if src.Limits.MaxParallelPerNamespace != nil {
		dst.Limits.MaxParallelPerNamespace = src.Limits.MaxParallelPerNamespace
	}
	if src.Limits.ParallelismGroupLimit != nil {
		dst.Limits.ParallelismGroupLimit = src.Limits.ParallelismGroupLimit
	}
	if src.Limits.MaxParallelKind != nil {
		if dst.Limits.MaxParallelKind == nil {
			dst.Limits.MaxParallelKind = map[string]int{}
		}
		maps.Copy(dst.Limits.MaxParallelKind, src.Limits.MaxParallelKind)
	}
	if strings.TrimSpace(src.Adaptive.Mode) != "" {
		dst.Adaptive.Mode = src.Adaptive.Mode
	}
	if src.Adaptive.Min != nil {
		dst.Adaptive.Min = src.Adaptive.Min
	}
	if src.Adaptive.Window != nil {
		dst.Adaptive.Window = src.Adaptive.Window
	}
	if src.Adaptive.RampAfterSuccesses != nil {
		dst.Adaptive.RampAfterSuccesses = src.Adaptive.RampAfterSuccesses
	}
	if src.Adaptive.RampMaxFailureRate != nil {
		dst.Adaptive.RampMaxFailureRate = src.Adaptive.RampMaxFailureRate
	}
	if src.Adaptive.CooldownSevere != nil {
		dst.Adaptive.CooldownSevere = src.Adaptive.CooldownSevere
	}
}

func mergeRunnerReadiness(dst *RunnerReadiness, src RunnerReadiness) {
	if dst == nil {
		return
	}
	if strings.TrimSpace(src.Source) != "" {
		dst.Source = src.Source
	}
	if strings.TrimSpace(src.Store) != "" {
		dst.Store = src.Store
	}
	if strings.TrimSpace(src.StorePath) != "" {
		dst.StorePath = src.StorePath
	}
	if src.EtcdEndpoints != nil {
		dst.EtcdEndpoints = append([]string(nil), src.EtcdEndpoints...)
	}
	if strings.TrimSpace(src.EtcdPrefix) != "" {
		dst.EtcdPrefix = src.EtcdPrefix
	}
	if strings.TrimSpace(src.Tenant) != "" {
		dst.Tenant = src.Tenant
	}
	if src.Selector != nil {
		dst.Selector = cloneStringMap(src.Selector)
	}
	if src.RequireAgents != nil {
		dst.RequireAgents = src.RequireAgents
	}
	if src.MinReadyPercent != nil {
		dst.MinReadyPercent = src.MinReadyPercent
	}
	if src.FailureBudget != nil {
		dst.FailureBudget = src.FailureBudget
	}
	if src.StaleAfter != nil {
		dst.StaleAfter = src.StaleAfter
	}
	if strings.TrimSpace(src.OnInsufficientReady) != "" {
		dst.OnInsufficientReady = src.OnInsufficientReady
	}
}

func mergeRunnerFanout(dst *RunnerFanout, src RunnerFanout) {
	if dst == nil {
		return
	}
	if src.MaxParallel != nil {
		dst.MaxParallel = src.MaxParallel
	}
	if src.MaxFailed != nil {
		dst.MaxFailed = src.MaxFailed
	}
	if src.MinSucceededPercent != nil {
		dst.MinSucceededPercent = src.MinSucceededPercent
	}
	if strings.TrimSpace(src.OnPartialFailure) != "" {
		dst.OnPartialFailure = src.OnPartialFailure
	}
	if strings.TrimSpace(src.Delivery) != "" {
		dst.Delivery = src.Delivery
	}
	mergeRunnerFanoutTargetConcurrency(&dst.TargetConcurrency, src.TargetConcurrency)
	mergeRunnerFanoutRetry(&dst.Retry, src.Retry)
}

func mergeRunnerFanoutTargetConcurrency(dst *RunnerFanoutTargetConcurrency, src RunnerFanoutTargetConcurrency) {
	if dst == nil {
		return
	}
	if src.Enabled != nil {
		dst.Enabled = src.Enabled
	}
	if src.RequireAvailable != nil {
		dst.RequireAvailable = src.RequireAvailable
	}
	if src.MaxPerTarget != nil {
		dst.MaxPerTarget = src.MaxPerTarget
	}
	if src.LeaseTTL != nil {
		dst.LeaseTTL = src.LeaseTTL
	}
}

func mergeRunnerFanoutRetry(dst *RunnerFanoutRetry, src RunnerFanoutRetry) {
	if dst == nil {
		return
	}
	if src.MaxDeliver != nil {
		dst.MaxDeliver = src.MaxDeliver
	}
	if src.AckWait != nil {
		dst.AckWait = src.AckWait
	}
	if src.Backoff != nil {
		dst.Backoff = append([]time.Duration(nil), src.Backoff...)
	}
	if strings.TrimSpace(src.OnExhausted) != "" {
		dst.OnExhausted = src.OnExhausted
	}
}

func applyRunnerResolved(dst *RunnerResolved, cfg RunnerConfig) {
	if dst == nil {
		return
	}
	if strings.TrimSpace(cfg.Mode) != "" {
		dst.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	}
	if cfg.Concurrency != nil {
		dst.Concurrency = *cfg.Concurrency
	}
	if cfg.ProgressiveConcurrency != nil {
		dst.ProgressiveConcurrency = *cfg.ProgressiveConcurrency
	}
	if cfg.KubeQPS != nil {
		dst.KubeQPS = *cfg.KubeQPS
	}
	if cfg.KubeBurst != nil {
		dst.KubeBurst = *cfg.KubeBurst
	}
	applyRunnerReadinessResolved(dst, cfg.Readiness)
	applyRunnerFanoutResolved(dst, cfg.Fanout)
	if cfg.Limits.MaxParallelPerNamespace != nil {
		dst.Limits.MaxParallelPerNamespace = *cfg.Limits.MaxParallelPerNamespace
	}
	if cfg.Limits.ParallelismGroupLimit != nil {
		dst.Limits.ParallelismGroupLimit = *cfg.Limits.ParallelismGroupLimit
	}
	if cfg.Limits.MaxParallelKind != nil {
		dst.Limits.MaxParallelKind = maps.Clone(cfg.Limits.MaxParallelKind)
	}
	if cfg.Adaptive.Min != nil {
		dst.Adaptive.Min = *cfg.Adaptive.Min
	}
	if cfg.Adaptive.Window != nil {
		dst.Adaptive.Window = *cfg.Adaptive.Window
	}
	if cfg.Adaptive.RampAfterSuccesses != nil {
		dst.Adaptive.RampAfterSuccesses = *cfg.Adaptive.RampAfterSuccesses
	}
	if cfg.Adaptive.RampMaxFailureRate != nil {
		dst.Adaptive.RampMaxFailureRate = *cfg.Adaptive.RampMaxFailureRate
	}
	if cfg.Adaptive.CooldownSevere != nil {
		dst.Adaptive.CooldownSevere = *cfg.Adaptive.CooldownSevere
	}
	if strings.TrimSpace(cfg.Adaptive.Mode) != "" {
		dst.Adaptive.Mode = strings.ToLower(strings.TrimSpace(cfg.Adaptive.Mode))
	}
}

func applyRunnerFanoutResolved(dst *RunnerResolved, cfg RunnerFanout) {
	if dst == nil {
		return
	}
	if cfg.MaxParallel != nil {
		dst.Fanout.MaxParallel = *cfg.MaxParallel
	}
	if cfg.MaxFailed != nil {
		dst.Fanout.MaxFailed = *cfg.MaxFailed
	}
	if cfg.MinSucceededPercent != nil {
		dst.Fanout.MinSucceededPercent = *cfg.MinSucceededPercent
	}
	if strings.TrimSpace(cfg.OnPartialFailure) != "" {
		dst.Fanout.OnPartialFailure = strings.ToLower(strings.TrimSpace(cfg.OnPartialFailure))
	}
	if strings.TrimSpace(cfg.Delivery) != "" {
		dst.Fanout.Delivery = normalizeRunnerFanoutDelivery(cfg.Delivery)
	}
	applyRunnerFanoutTargetConcurrencyResolved(&dst.Fanout.TargetConcurrency, cfg.TargetConcurrency)
	applyRunnerFanoutRetryResolved(&dst.Fanout.Retry, cfg.Retry)
}

func applyRunnerFanoutTargetConcurrencyResolved(dst *RunnerFanoutTargetConcurrencyResolved, cfg RunnerFanoutTargetConcurrency) {
	if dst == nil {
		return
	}
	if cfg.Enabled != nil {
		dst.Enabled = *cfg.Enabled
	}
	if cfg.RequireAvailable != nil {
		dst.RequireAvailable = *cfg.RequireAvailable
	}
	if cfg.MaxPerTarget != nil {
		dst.MaxPerTarget = *cfg.MaxPerTarget
	}
	if cfg.LeaseTTL != nil {
		dst.LeaseTTL = *cfg.LeaseTTL
	}
}

func applyRunnerFanoutRetryResolved(dst *RunnerFanoutRetryResolved, cfg RunnerFanoutRetry) {
	if dst == nil {
		return
	}
	if cfg.MaxDeliver != nil {
		dst.MaxDeliver = *cfg.MaxDeliver
	}
	if cfg.AckWait != nil {
		dst.AckWait = *cfg.AckWait
	}
	if cfg.Backoff != nil {
		dst.Backoff = append([]time.Duration(nil), cfg.Backoff...)
	}
	if strings.TrimSpace(cfg.OnExhausted) != "" {
		dst.OnExhausted = normalizeRunnerFanoutRetryOnExhausted(cfg.OnExhausted)
	}
}

func applyRunnerReadinessResolved(dst *RunnerResolved, cfg RunnerReadiness) {
	if dst == nil {
		return
	}
	r := &dst.Readiness
	if strings.TrimSpace(cfg.Source) != "" {
		r.Source = strings.ToLower(strings.TrimSpace(cfg.Source))
	}
	if strings.TrimSpace(cfg.Store) != "" {
		r.Store = strings.ToLower(strings.TrimSpace(cfg.Store))
	}
	if strings.TrimSpace(cfg.StorePath) != "" {
		r.StorePath = strings.TrimSpace(cfg.StorePath)
	}
	if cfg.EtcdEndpoints != nil {
		r.EtcdEndpoints = normalizeTrimmedStringSlice(cfg.EtcdEndpoints)
	}
	if strings.TrimSpace(cfg.EtcdPrefix) != "" {
		r.EtcdPrefix = strings.TrimSpace(cfg.EtcdPrefix)
	}
	if strings.TrimSpace(cfg.Tenant) != "" {
		r.Tenant = strings.TrimSpace(cfg.Tenant)
	}
	if cfg.Selector != nil {
		r.Selector = trimRunnerSelector(cfg.Selector)
	}
	if cfg.RequireAgents != nil {
		r.RequireAgents = *cfg.RequireAgents
	}
	if cfg.MinReadyPercent != nil {
		r.MinReadyPercent = *cfg.MinReadyPercent
	}
	if cfg.FailureBudget != nil {
		r.FailureBudget = *cfg.FailureBudget
	}
	if cfg.StaleAfter != nil {
		r.StaleAfter = *cfg.StaleAfter
	}
	if strings.TrimSpace(cfg.OnInsufficientReady) != "" {
		r.OnInsufficientReady = strings.ToLower(strings.TrimSpace(cfg.OnInsufficientReady))
	}
	if dst.Mode == RunnerModeFleet {
		r.Enabled = true
		r.RequireAgents = true
	}
}

func ValidateRunnerResolved(r RunnerResolved) error {
	mode := strings.ToLower(strings.TrimSpace(r.Mode))
	if mode == "" {
		mode = RunnerModeLocal
	}
	if mode != RunnerModeLocal && mode != RunnerModeFleet {
		return fmt.Errorf("runner.mode must be local or fleet (got %q)", r.Mode)
	}
	if r.Concurrency < 1 {
		return fmt.Errorf("runner.concurrency must be >= 1 (got %d)", r.Concurrency)
	}
	if r.Limits.ParallelismGroupLimit < 1 {
		return fmt.Errorf("runner.limits.parallelismGroupLimit must be >= 1 (got %d)", r.Limits.ParallelismGroupLimit)
	}
	if r.Fanout.MaxParallel < 1 {
		return fmt.Errorf("runner.fanout.maxParallel must be >= 1 (got %d)", r.Fanout.MaxParallel)
	}
	if r.Fanout.MaxFailed < 0 {
		return fmt.Errorf("runner.fanout.maxFailed must be >= 0 (got %d)", r.Fanout.MaxFailed)
	}
	if r.Fanout.MinSucceededPercent < 0 || r.Fanout.MinSucceededPercent > 100 {
		return fmt.Errorf("runner.fanout.minSucceededPercent must be in [0,100] (got %d)", r.Fanout.MinSucceededPercent)
	}
	onPartial := strings.ToLower(strings.TrimSpace(r.Fanout.OnPartialFailure))
	if onPartial == "" {
		onPartial = RunnerFanoutOnBlock
	}
	if onPartial != RunnerFanoutOnBlock && onPartial != RunnerFanoutOnContinue {
		return fmt.Errorf("runner.fanout.onPartialFailure must be block or continue (got %q)", r.Fanout.OnPartialFailure)
	}
	delivery := normalizeRunnerFanoutDelivery(r.Fanout.Delivery)
	if delivery == "" {
		delivery = RunnerFanoutDeliveryRequestReply
	}
	if delivery != RunnerFanoutDeliveryRequestReply && delivery != RunnerFanoutDeliveryJetStream {
		return fmt.Errorf("runner.fanout.delivery must be requestReply or jetstream (got %q)", r.Fanout.Delivery)
	}
	if r.Fanout.TargetConcurrency.MaxPerTarget < 1 {
		return fmt.Errorf("runner.fanout.targetConcurrency.maxPerTarget must be >= 1 (got %d)", r.Fanout.TargetConcurrency.MaxPerTarget)
	}
	if r.Fanout.TargetConcurrency.LeaseTTL <= 0 {
		return fmt.Errorf("runner.fanout.targetConcurrency.leaseTTL must be > 0")
	}
	if r.Fanout.Retry.MaxDeliver < 1 {
		return fmt.Errorf("runner.fanout.retry.maxDeliver must be >= 1 (got %d)", r.Fanout.Retry.MaxDeliver)
	}
	if r.Fanout.Retry.AckWait <= 0 {
		return fmt.Errorf("runner.fanout.retry.ackWait must be > 0")
	}
	for idx, backoff := range r.Fanout.Retry.Backoff {
		if backoff <= 0 {
			return fmt.Errorf("runner.fanout.retry.backoff[%d] must be > 0", idx)
		}
	}
	onExhausted := normalizeRunnerFanoutRetryOnExhausted(r.Fanout.Retry.OnExhausted)
	if onExhausted == "" {
		onExhausted = RunnerFanoutRetryOnBlock
	}
	if onExhausted != RunnerFanoutRetryOnBlock && onExhausted != RunnerFanoutRetryOnContinue {
		return fmt.Errorf("runner.fanout.retry.onExhausted must be block or continue (got %q)", r.Fanout.Retry.OnExhausted)
	}
	if r.Limits.MaxParallelPerNamespace < 0 {
		return fmt.Errorf("runner.limits.maxParallelPerNamespace must be >= 0 (got %d)", r.Limits.MaxParallelPerNamespace)
	}
	if r.Adaptive.Window < 4 {
		return fmt.Errorf("runner.adaptive.window must be >= 4 (got %d)", r.Adaptive.Window)
	}
	if r.Adaptive.Min < 1 {
		return fmt.Errorf("runner.adaptive.min must be >= 1 (got %d)", r.Adaptive.Min)
	}
	if r.Adaptive.Min > r.Concurrency {
		return fmt.Errorf("runner.adaptive.min must be <= runner.concurrency (%d > %d)", r.Adaptive.Min, r.Concurrency)
	}
	if r.Adaptive.RampAfterSuccesses < 1 {
		return fmt.Errorf("runner.adaptive.rampAfterSuccesses must be >= 1 (got %d)", r.Adaptive.RampAfterSuccesses)
	}
	if r.Adaptive.RampMaxFailureRate < 0 || r.Adaptive.RampMaxFailureRate > 1 {
		return fmt.Errorf("runner.adaptive.rampMaxFailureRate must be in [0,1] (got %.3f)", r.Adaptive.RampMaxFailureRate)
	}
	if r.Adaptive.CooldownSevere < 0 {
		return fmt.Errorf("runner.adaptive.cooldownSevere must be >= 0 (got %d)", r.Adaptive.CooldownSevere)
	}
	for kind, v := range r.Limits.MaxParallelKind {
		if strings.TrimSpace(kind) == "" {
			return fmt.Errorf("runner.limits.maxParallelKind has empty kind")
		}
		if v < 1 {
			return fmt.Errorf("runner.limits.maxParallelKind[%s] must be >= 1 (got %d)", kind, v)
		}
	}
	if mode == RunnerModeFleet || r.Readiness.Enabled || r.Readiness.RequireAgents {
		if err := validateRunnerReadinessResolved(mode, r.Readiness); err != nil {
			return err
		}
	}
	return nil
}

func normalizeRunnerFanoutDelivery(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "requestreply", "request-reply", "request_reply", "request.reply":
		return RunnerFanoutDeliveryRequestReply
	case "jetstream", "jet-stream", "jet_stream", "jet.stream":
		return RunnerFanoutDeliveryJetStream
	default:
		return strings.TrimSpace(raw)
	}
}

func normalizeRunnerFanoutRetryOnExhausted(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "block":
		return RunnerFanoutRetryOnBlock
	case "continue":
		return RunnerFanoutRetryOnContinue
	default:
		return strings.TrimSpace(raw)
	}
}

func validateRunnerReadinessResolved(mode string, readiness RunnerReadinessResolved) error {
	source := strings.ToLower(strings.TrimSpace(readiness.Source))
	if source == "" {
		source = RunnerReadinessSourceStore
	}
	if source != RunnerReadinessSourceStore {
		return fmt.Errorf("runner.readiness.source must be store (got %q)", readiness.Source)
	}
	store := strings.ToLower(strings.TrimSpace(readiness.Store))
	if store == "" {
		store = RunnerReadinessStoreFile
	}
	if store != RunnerReadinessStoreFile && store != RunnerReadinessStoreEtcd {
		return fmt.Errorf("runner.readiness.store must be file or etcd (got %q)", readiness.Store)
	}
	if store == RunnerReadinessStoreFile && strings.TrimSpace(readiness.StorePath) == "" {
		return fmt.Errorf("runner.readiness.storePath is required for file store")
	}
	if readiness.MinReadyPercent < 0 || readiness.MinReadyPercent > 100 {
		return fmt.Errorf("runner.readiness.minReadyPercent must be in [0,100] (got %d)", readiness.MinReadyPercent)
	}
	if readiness.FailureBudget < 0 {
		return fmt.Errorf("runner.readiness.failureBudget must be >= 0 (got %d)", readiness.FailureBudget)
	}
	if readiness.StaleAfter <= 0 {
		return fmt.Errorf("runner.readiness.staleAfter must be > 0")
	}
	onInsufficient := strings.ToLower(strings.TrimSpace(readiness.OnInsufficientReady))
	if onInsufficient == "" {
		onInsufficient = RunnerReadinessOnBlock
	}
	if onInsufficient != RunnerReadinessOnBlock && onInsufficient != RunnerReadinessOnWarn {
		return fmt.Errorf("runner.readiness.onInsufficientReady must be block or warn (got %q)", readiness.OnInsufficientReady)
	}
	if mode == RunnerModeFleet && !readiness.RequireAgents {
		return fmt.Errorf("runner.readiness.requireAgents must be true in fleet mode")
	}
	return nil
}

func trimRunnerSelector(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func runnerMaxParallelKindFlagString(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, ",")
}
