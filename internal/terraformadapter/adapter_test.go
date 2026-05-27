package terraformadapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderTerraformConfig(t *testing.T) {
	hcl, err := renderTerraformConfig(adapterInput{
		Provider: providerInput{
			Source:    "hashicorp/aws",
			Version:   "~> 5.0",
			LocalName: "aws",
			Config: map[string]any{
				"region": "us-east-1",
			},
		},
		Resource: resourceInput{
			Type: "aws_s3_bucket",
			Name: "this",
			Values: map[string]any{
				"bucket":        "torque-example",
				"force_destroy": true,
				"tags": map[string]any{
					"owner": "platform",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("renderTerraformConfig: %v", err)
	}
	for _, want := range []string{
		`source = "hashicorp/aws"`,
		`provider "aws"`,
		`resource "aws_s3_bucket" "this"`,
		`force_destroy = true`,
		`"owner" = "platform"`,
	} {
		if !strings.Contains(hcl, want) {
			t.Fatalf("rendered HCL missing %q:\n%s", want, hcl)
		}
	}
}

func TestExecuteLifecycleWithFakeTerraform(t *testing.T) {
	root := t.TempDir()
	fake := writeFakeTerraform(t, root)
	req := terraformRequest(root, "plan", "apply", false)
	opts := Options{
		TerraformBin: fake,
		Now: func() time.Time {
			return time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
		},
	}

	plan := Execute(context.Background(), req, opts)
	if plan.Status != "planned" || !plan.Changed || plan.SafeToRun == nil || !*plan.SafeToRun {
		t.Fatalf("plan result = %#v", plan)
	}

	req.Phase = "apply"
	apply := Execute(context.Background(), req, opts)
	if apply.Status != "succeeded" || !apply.Changed {
		t.Fatalf("apply result = %#v", apply)
	}

	req.Phase = "verify"
	verify := Execute(context.Background(), req, opts)
	if verify.Status != "succeeded" || verify.Changed {
		t.Fatalf("verify result = %#v", verify)
	}

	metaPath := filepath.Join(root, ".torque", "terraform", "local-default-bucket", planMetaFileName)
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read plan metadata: %v", err)
	}
	var meta planMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta.Command != "apply" || meta.Provider.Source != "example.test/example/fake" || meta.Resource.Type != "fake_bucket" {
		t.Fatalf("metadata mismatch: %#v", meta)
	}
}

func TestPlanBlocksDestructiveApplyWithoutAllowDestroy(t *testing.T) {
	summary, err := summarizePlan([]byte(`{
		"terraform_version": "1.6.0",
		"resource_changes": [{
			"address": "fake_bucket.this",
			"mode": "managed",
			"type": "fake_bucket",
			"name": "this",
			"change": {"actions": ["delete", "create"]}
		}]
	}`), false)
	if err != nil {
		t.Fatalf("summarizePlan: %v", err)
	}
	if !summary.Changed || !summary.UnsafeDestroy || summary.Risk != "high" {
		t.Fatalf("summary = %#v", summary)
	}
}

func terraformRequest(root string, phase string, command string, allowDestroy bool) moduleRequest {
	return moduleRequest{
		APIVersion: moduleAPIVersion,
		Phase:      phase,
		Command:    command,
		RunID:      "run-1",
		Stack: stackContext{
			Root: root,
			Name: "terraform-test",
		},
		Node: nodeContext{
			ID:                 "local/default/bucket",
			Name:               "bucket",
			Kind:               "fake.bucket.ensure",
			EffectiveInputHash: "sha256:intent",
		},
		Input: map[string]any{
			"allowDestroy": allowDestroy,
			"provider": map[string]any{
				"source":    "example.test/example/fake",
				"version":   "1.0.0",
				"localName": "fake",
			},
			"resource": map[string]any{
				"type": "fake_bucket",
				"name": "this",
				"values": map[string]any{
					"name": "torque-test",
				},
			},
		},
	}
}

func writeFakeTerraform(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "terraform-fake.sh")
	script := `#!/bin/sh
set -eu
cmd="${1:-}"
if [ "$#" -gt 0 ]; then
  shift
fi
case "$cmd" in
  init)
    printf '# fake lock\n' > .terraform.lock.hcl
    exit 0
    ;;
  plan)
    out=""
    destroy=0
    for arg in "$@"; do
      case "$arg" in
        -out=*) out="${arg#-out=}" ;;
        -destroy) destroy=1 ;;
      esac
    done
    if [ -n "$out" ]; then
      printf 'fake-plan' > "$out"
    fi
    if [ -f applied.marker ] && [ "$destroy" = "0" ]; then
      exit 0
    fi
    exit 2
    ;;
  show)
    target="${2:-}"
    case "$target" in
      torque.tfplan|torque-diff.tfplan|torque-verify.tfplan)
        cat <<'JSON'
{"terraform_version":"1.6.0","resource_changes":[{"address":"fake_bucket.this","mode":"managed","type":"fake_bucket","name":"this","provider_name":"example.test/example/fake","change":{"actions":["create"]}}]}
JSON
        ;;
      *)
        cat <<'JSON'
{"values":{"root_module":{"resources":[{"address":"fake_bucket.this","mode":"managed","type":"fake_bucket","name":"this"}]}}}
JSON
        ;;
    esac
    ;;
  apply)
    touch applied.marker
    printf '{"version":4}' > terraform.tfstate
    exit 0
    ;;
  *)
    echo "unexpected fake terraform command: $cmd" >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake terraform: %v", err)
	}
	return path
}
