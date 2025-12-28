package stack

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type applyCacheDesiredFunc func(context.Context) (desiredDigest string, hasHooks bool, err error)
type applyCacheObservedFunc func(context.Context) (observedDigest string, found bool, err error)

type ApplyCacheDecision struct {
	Skip           bool
	CacheHit       bool
	Reason         string
	DesiredDigest  string
	ObservedDigest string
	HasHooks       bool
}

func CheckApplyCache(ctx context.Context, store *stackStateStore, key ApplyCacheKey, runID string, computeDesired applyCacheDesiredFunc, computeObserved applyCacheObservedFunc) (ApplyCacheDecision, error) {
	if store == nil {
		return ApplyCacheDecision{Skip: false, Reason: "no-store"}, nil
	}

	entry, ok, err := store.GetApplyCache(ctx, key)
	if err != nil {
		return ApplyCacheDecision{}, err
	}
	if ok {
		dec := ApplyCacheDecision{
			CacheHit:      true,
			DesiredDigest: entry.DesiredDigest,
			HasHooks:      entry.HasHooks,
		}
		if dec.HasHooks {
			dec.Skip = false
			dec.Reason = "has-hooks"
			return dec, nil
		}
		observed, found, err := computeObserved(ctx)
		if err != nil {
			return ApplyCacheDecision{}, err
		}
		dec.ObservedDigest = observed
		if found && strings.TrimSpace(observed) != "" && strings.TrimSpace(observed) == strings.TrimSpace(dec.DesiredDigest) {
			dec.Skip = true
			dec.Reason = "digest-match"
			_ = store.UpsertApplyCache(ctx, key, dec.DesiredDigest, false, runID, time.Now().UTC().UnixNano())
			return dec, nil
		}
		dec.Skip = false
		dec.Reason = "digest-miss"
		return dec, nil
	}

	// Cache miss: compute desired digest and store it so subsequent runs can reuse it without
	// re-rendering (even if we can't skip this time).
	desired, hasHooks, err := computeDesired(ctx)
	if err != nil {
		return ApplyCacheDecision{}, err
	}
	desired = strings.TrimSpace(desired)
	_ = store.UpsertApplyCache(ctx, key, desired, hasHooks, runID, time.Now().UTC().UnixNano())

	dec := ApplyCacheDecision{
		CacheHit:      false,
		DesiredDigest: desired,
		HasHooks:      hasHooks,
	}
	if dec.HasHooks {
		dec.Skip = false
		dec.Reason = "has-hooks"
		return dec, nil
	}
	observed, found, err := computeObserved(ctx)
	if err != nil {
		return ApplyCacheDecision{}, err
	}
	dec.ObservedDigest = observed
	if found && desired != "" && strings.TrimSpace(observed) == desired {
		dec.Skip = true
		dec.Reason = "digest-match"
		return dec, nil
	}
	dec.Skip = false
	dec.Reason = "digest-miss"
	return dec, nil
}

func defaultNamespace(ns string) string {
	ns = strings.TrimSpace(ns)
	if ns == "" {
		return "default"
	}
	return ns
}

func applyCacheKeyForNode(clusterKey string, node *runNode, command string) (ApplyCacheKey, error) {
	if node == nil {
		return ApplyCacheKey{}, fmt.Errorf("nil node")
	}
	return ApplyCacheKey{
		ClusterKey:         strings.TrimSpace(clusterKey),
		Namespace:          defaultNamespace(node.Namespace),
		ReleaseName:        strings.TrimSpace(node.Name),
		Command:            strings.TrimSpace(command),
		EffectiveInputHash: strings.TrimSpace(node.EffectiveInputHash),
	}, nil
}
