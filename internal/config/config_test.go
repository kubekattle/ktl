// File: internal/config/config_test.go
// Brief: Internal config package implementation for 'config'.

// config_test.go verifies Options parsing, validation, and template helpers for ktl logs flags.
package config

import (
	"testing"
)

func TestNewOptionsDefaults(t *testing.T) {
	opts := NewOptions()
	if !opts.Follow {
		t.Fatalf("follow should default to true")
	}
	if opts.TailLines != 10 {
		t.Fatalf("tail default mismatch, got %d", opts.TailLines)
	}
	if !opts.ShowTimestamp {
		t.Fatalf("timestamps should be enabled by default")
	}
	if opts.Template == "" {
		t.Fatalf("expected default template to be set")
	}
}

func TestValidatePodQueryNormalization(t *testing.T) {
	opts := NewOptions()
	opts.PodQuery = "cronjob/batch-\\d+"
	if err := opts.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if opts.PodQuery != "batch-\\d+" {
		t.Fatalf("expected pod query suffix, got %s", opts.PodQuery)
	}
}

func TestValidatePodQuerySetsNamespaceHint(t *testing.T) {
	opts := NewOptions()
	opts.PodQuery = "kube-system/nginx-.*"
	if err := opts.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if opts.PodQuery != "nginx-.*" {
		t.Fatalf("expected pod query suffix, got %s", opts.PodQuery)
	}
	if len(opts.Namespaces) != 1 || opts.Namespaces[0] != "kube-system" {
		t.Fatalf("expected namespace hint to set kube-system, got %v", opts.Namespaces)
	}
}

func TestValidatePodQueryNamespaceHintRespectsExplicitNamespace(t *testing.T) {
	opts := NewOptions()
	opts.PodQuery = "payments/api-.*"
	opts.Namespaces = []string{"staging"}
	if err := opts.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if len(opts.Namespaces) != 1 || opts.Namespaces[0] != "staging" {
		t.Fatalf("expected explicit namespace to win, got %v", opts.Namespaces)
	}
}

func TestValidateRejectsNamespaceConflict(t *testing.T) {
	opts := NewOptions()
	opts.PodQuery = ".*"
	opts.AllNamespaces = true
	opts.Namespaces = []string{"default"}
	if err := opts.Validate(); err == nil {
		t.Fatalf("expected validation error for namespace conflict")
	}
}

func TestValidateContainerRegex(t *testing.T) {
	opts := NewOptions()
	opts.PodQuery = ".*"
	opts.ContainerFilters = []string{"app.*"}
	if err := opts.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if len(opts.ContainerRegex) != 1 {
		t.Fatalf("expected compiled container regex")
	}
}

func TestValidateNoPrefixOverridesTemplate(t *testing.T) {
	opts := NewOptions()
	opts.PodQuery = ".*"
	opts.NoPrefix = true
	if err := opts.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if opts.Template != "{{.Message}}" {
		t.Fatalf("expected message-only template, got %s", opts.Template)
	}
}

func TestValidateJSONOutputAdjustments(t *testing.T) {
	opts := NewOptions()
	opts.PodQuery = ".*"
	opts.JSONOutput = true
	if err := opts.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if opts.NoPrefix != true || opts.ShowTimestamp {
		t.Fatalf("json mode should disable prefix and timestamps")
	}
}

func TestValidateNodeLogDefaults(t *testing.T) {
	opts := NewOptions()
	opts.PodQuery = ".*"
	opts.NodeLogs = true
	if err := opts.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if len(opts.NodeLogFiles) != 1 || opts.NodeLogFiles[0] != defaultNodeLogFile {
		t.Fatalf("expected default node log file, got %v", opts.NodeLogFiles)
	}
}

func TestValidateNodeLogRejectsTraversal(t *testing.T) {
	opts := NewOptions()
	opts.PodQuery = ".*"
	opts.NodeLogFiles = []string{"../../etc/passwd"}
	if err := opts.Validate(); err == nil {
		t.Fatalf("expected error for traversal path")
	}
}

func TestValidateNodeLogOnlyEnablesDefaults(t *testing.T) {
	opts := NewOptions()
	opts.PodQuery = ".*"
	opts.NodeLogsOnly = true
	if err := opts.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if !opts.NodeLogs {
		t.Fatalf("expected node logs to be enabled")
	}
	if len(opts.NodeLogFiles) == 0 {
		t.Fatalf("expected node log files to be configured")
	}
}
