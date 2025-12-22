// File: internal/deploy/plan_server.go
// Brief: Optional server-side dry-run classification for plan replace detection.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/ktl/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

type ServerPlanOptions struct {
	FieldManager string
	Force        bool
}

// DetectServerSideReplaceKeys attempts a server-side apply dry-run for each object in the proposed
// manifest and returns keys that should be treated as "replace" due to immutable-field errors.
func DetectServerSideReplaceKeys(ctx context.Context, client *kube.Client, proposedManifest string, opts ServerPlanOptions) (map[string]bool, error) {
	if client == nil || client.Dynamic == nil || client.RESTMapper == nil {
		return nil, fmt.Errorf("kube client missing dynamic/mapper")
	}
	if strings.TrimSpace(proposedManifest) == "" {
		return map[string]bool{}, nil
	}
	if strings.TrimSpace(opts.FieldManager) == "" {
		opts.FieldManager = "ktl-plan"
	}

	objs, err := parseManifestObjects(proposedManifest)
	if err != nil {
		return nil, err
	}

	replace := make(map[string]bool)
	for _, obj := range objs {
		if obj.IsHook {
			continue
		}
		if obj.Normalized == nil {
			continue
		}
		gvk := schema.GroupVersionKind{Group: obj.Group, Version: obj.Version, Kind: obj.Kind}
		if gvk.Kind == "" || gvk.Version == "" {
			continue
		}
		mapping, mapErr := client.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if mapErr != nil || mapping == nil {
			continue
		}

		body, encErr := json.Marshal(obj.Normalized.Object)
		if encErr != nil {
			continue
		}
		ns := strings.TrimSpace(obj.Namespace)
		res := client.Dynamic.Resource(mapping.Resource)
		var r dynamic.ResourceInterface
		if mapping.Scope.Name() == "namespace" {
			r = res.Namespace(ns)
		} else {
			r = res
		}
		force := opts.Force
		patchOpts := metav1.PatchOptions{
			FieldManager: opts.FieldManager,
			DryRun:       []string{metav1.DryRunAll},
		}
		if force {
			patchOpts.Force = &force
		}
		_, applyErr := r.Patch(ctx, obj.Name, types.ApplyPatchType, body, patchOpts)
		if applyErr == nil {
			continue
		}
		if isImmutableFieldError(applyErr) {
			replace[planObjectKey(obj.Group, obj.Version, obj.Kind, obj.Namespace, obj.Name)] = true
			continue
		}
	}
	return replace, nil
}

func isImmutableFieldError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "field is immutable") {
		return true
	}
	if strings.Contains(msg, "immutable") && strings.Contains(msg, "spec") {
		return true
	}
	return false
}

func planObjectKey(group, version, kind, namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", strings.ToLower(strings.TrimSpace(group)), strings.ToLower(strings.TrimSpace(version)), strings.ToLower(strings.TrimSpace(kind)), strings.TrimSpace(namespace), strings.TrimSpace(name))
}
