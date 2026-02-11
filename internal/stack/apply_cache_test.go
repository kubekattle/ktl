package stack

import (
	"context"
	"testing"
	"time"
)

func TestCheckApplyCache_CacheMissStoresAndSkipsOnDigestMatch(t *testing.T) {
	root := t.TempDir()
	store, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	key := ApplyCacheKey{
		ClusterKey:         "c1\nkubeconfig\nctx",
		Namespace:          "default",
		ReleaseName:        "rel",
		Command:            "apply",
		EffectiveInputHash: "sha256:abc",
	}

	desiredCalls := 0
	dec, err := CheckApplyCache(context.Background(), store, key, "run1",
		func(context.Context) (string, bool, error) {
			desiredCalls++
			return "sha256:deadbeef", false, nil
		},
		func(context.Context) (string, bool, error) {
			return "sha256:deadbeef", true, nil
		},
	)
	if err != nil {
		t.Fatalf("CheckApplyCache: %v", err)
	}
	if desiredCalls != 1 {
		t.Fatalf("expected desiredCalls=1 got %d", desiredCalls)
	}
	if !dec.Skip || dec.Reason != "digest-match" || dec.CacheHit {
		t.Fatalf("unexpected decision: %+v", dec)
	}

	got, ok, err := store.GetApplyCache(context.Background(), key)
	if err != nil {
		t.Fatalf("GetApplyCache: %v", err)
	}
	if !ok {
		t.Fatalf("expected cache entry to exist")
	}
	if got.DesiredDigest != "sha256:deadbeef" || got.HasHooks {
		t.Fatalf("unexpected cache entry: %+v", got)
	}
}

func TestCheckApplyCache_CacheHitDoesNotComputeDesired(t *testing.T) {
	root := t.TempDir()
	store, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	key := ApplyCacheKey{
		ClusterKey:         "c1\nkubeconfig\nctx",
		Namespace:          "default",
		ReleaseName:        "rel",
		Command:            "apply",
		EffectiveInputHash: "sha256:abc",
	}
	if err := store.UpsertApplyCache(context.Background(), key, "sha256:cafebabe", false, "run0", time.Now().UTC().UnixNano()); err != nil {
		t.Fatalf("UpsertApplyCache: %v", err)
	}

	dec, err := CheckApplyCache(context.Background(), store, key, "run1",
		func(context.Context) (string, bool, error) {
			t.Fatalf("ComputeDesired should not be called on cache hit")
			return "", false, nil
		},
		func(context.Context) (string, bool, error) {
			return "sha256:cafebabe", true, nil
		},
	)
	if err != nil {
		t.Fatalf("CheckApplyCache: %v", err)
	}
	if !dec.Skip || dec.Reason != "digest-match" || !dec.CacheHit {
		t.Fatalf("unexpected decision: %+v", dec)
	}
}

func TestCheckApplyCache_HasHooksNeverSkips(t *testing.T) {
	root := t.TempDir()
	store, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	key := ApplyCacheKey{
		ClusterKey:         "c1\nkubeconfig\nctx",
		Namespace:          "default",
		ReleaseName:        "rel",
		Command:            "apply",
		EffectiveInputHash: "sha256:abc",
	}
	if err := store.UpsertApplyCache(context.Background(), key, "sha256:cafebabe", true, "run0", time.Now().UTC().UnixNano()); err != nil {
		t.Fatalf("UpsertApplyCache: %v", err)
	}

	dec, err := CheckApplyCache(context.Background(), store, key, "run1",
		func(context.Context) (string, bool, error) {
			t.Fatalf("ComputeDesired should not be called on cache hit")
			return "", false, nil
		},
		func(context.Context) (string, bool, error) {
			t.Fatalf("ComputeObserved should not be called when has hooks")
			return "", false, nil
		},
	)
	if err != nil {
		t.Fatalf("CheckApplyCache: %v", err)
	}
	if dec.Skip || dec.Reason != "has-hooks" || !dec.CacheHit {
		t.Fatalf("unexpected decision: %+v", dec)
	}
}
