package main

import (
	"reflect"
	"testing"
)

func TestEnforceStrictShortFlagsRejectsAllNamespacesSuffix(t *testing.T) {
	if err := enforceStrictShortFlags([]string{"-Asdfs"}); err == nil {
		t.Fatalf("expected error for -A with attached text")
	}
}

func TestEnforceStrictShortFlagsRejectsNamespaceWithoutSpace(t *testing.T) {
	if err := enforceStrictShortFlags([]string{"-nexample"}); err == nil {
		t.Fatalf("expected error for -nexample usage")
	}
}

func TestEnforceStrictShortFlagsAllowsValidForms(t *testing.T) {
	args := []string{"logs", "-A", "-n", "kube-system", "pod.*"}
	if err := enforceStrictShortFlags(args); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnforceStrictShortFlagsSkipsAfterDoubleDash(t *testing.T) {
	args := []string{"logs", "--", "-nfoo"}
	if err := enforceStrictShortFlags(args); err != nil {
		t.Fatalf("arguments after -- should be ignored, got: %v", err)
	}
}

func TestNormalizeOptionalValueArgsJoinsUiValue(t *testing.T) {
	args := []string{"ktl", "--ui", ":8080"}
	got := normalizeOptionalValueArgs(args)
	want := []string{"ktl", "--ui=:8080"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestNormalizeOptionalValueArgsHandlesNegativeTail(t *testing.T) {
	args := []string{"ktl", "logs", "--tail", "-1"}
	got := normalizeOptionalValueArgs(args)
	want := []string{"ktl", "logs", "--tail=-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestNormalizeOptionalValueArgsSkipsFlags(t *testing.T) {
	args := []string{"ktl", "--ui", "--help"}
	got := normalizeOptionalValueArgs(args)
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("expected %v, got %v", args, got)
	}
}
