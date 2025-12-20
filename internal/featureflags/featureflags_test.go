// File: internal/featureflags/featureflags_test.go
// Brief: Internal featureflags package implementation for 'featureflags'.

// Package featureflags provides featureflags helpers.

package featureflags

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestResolve(t *testing.T) {
	flags, err := Resolve([]string{"deploy-plan-html-v3"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !flags.Enabled(FeatureDeployPlanHTMLV3) {
		t.Fatalf("expected feature %s to be enabled", FeatureDeployPlanHTMLV3)
	}
}

func TestResolveUnknown(t *testing.T) {
	_, err := Resolve([]string{"not-a-real-flag"})
	if !errors.Is(err, ErrUnknownFeature) {
		t.Fatalf("expected ErrUnknownFeature, got %v", err)
	}
}

func TestEnabledFromEnv(t *testing.T) {
	env := []string{
		"KTL_FEATURE_DEPLOY_PLAN_HTML_V3=1",
		"SOME_OTHER=value",
		"KTL_FEATURE_BOGUS=0",
	}
	list := EnabledFromEnv(env)
	flags, err := Resolve(list)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !flags.Enabled(FeatureDeployPlanHTMLV3) {
		t.Fatalf("expected env to enable %s", FeatureDeployPlanHTMLV3)
	}
}

func TestContextHelpers(t *testing.T) {
	flags, err := Resolve([]string{"deploy-plan-html-v3"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := ContextWithFlags(context.Background(), flags)
	actual := FromContext(ctx)
	if !actual.Enabled(FeatureDeployPlanHTMLV3) {
		t.Fatalf("expected flag to survive context round-trip")
	}
	if FromContext(context.Background()).Enabled(FeatureDeployPlanHTMLV3) {
		t.Fatalf("zero context should not report feature enabled")
	}
}

func TestEnabledFromEnvUsesProcessEnv(t *testing.T) {
	t.Setenv("KTL_FEATURE_DEPLOY_PLAN_HTML_V3", "true")
	list := EnabledFromEnv(nil)
	if len(list) != 1 {
		t.Fatalf("expected 1 env flag, got %d", len(list))
	}
	flags, err := Resolve(list)
	if err != nil {
		t.Fatal(err)
	}
	if !flags.Enabled(FeatureDeployPlanHTMLV3) {
		t.Fatalf("expected process env to enable flag")
	}
	os.Unsetenv("KTL_FEATURE_DEPLOY_PLAN_HTML_V3")
}
