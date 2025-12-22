// File: internal/deploy/plan_server.go
// Brief: Optional server-side dry-run classification for plan replace detection.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/ktl/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		if isImmutableFieldError(applyErr, obj.Kind) {
			replace[planObjectKey(obj.Group, obj.Version, obj.Kind, obj.Namespace, obj.Name)] = true
			continue
		}
	}
	return replace, nil
}

func isImmutableFieldError(err error, kind string) bool {
	if err == nil {
		return false
	}
	kind = strings.TrimSpace(kind)
	// Prefer structured status details when available.
	if statusErr, ok := err.(*apierrors.StatusError); ok && statusErr != nil {
		status := statusErr.ErrStatus
		if status.Details != nil {
			for _, cause := range status.Details.Causes {
				field := strings.TrimSpace(cause.Field)
				message := strings.TrimSpace(cause.Message)
				if looksLikeImmutableCause(kind, field, "", message) {
					return true
				}
			}
		}
	}

	// Fall back to string matching, but keep it conservative.
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "immutable") {
		return false
	}
	return looksLikeImmutableCause(kind, "", "", msg)
}

func planObjectKey(group, version, kind, namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", strings.ToLower(strings.TrimSpace(group)), strings.ToLower(strings.TrimSpace(version)), strings.ToLower(strings.TrimSpace(kind)), strings.TrimSpace(namespace), strings.TrimSpace(name))
}

func looksLikeImmutableCause(kind, field, reason, message string) bool {
	kind = strings.TrimSpace(kind)
	field = strings.TrimSpace(field)
	reason = strings.ToLower(strings.TrimSpace(reason))
	messageLower := strings.ToLower(strings.TrimSpace(message))

	// If the apiserver provides a field, only treat known-immutable fields as replace.
	if field != "" {
		switch kind {
		case "Service":
			return field == "spec.clusterIP"
		case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
			return strings.HasPrefix(field, "spec.selector")
		case "PersistentVolumeClaim":
			return field == "spec.storageClassName" || field == "spec.volumeName"
		case "Ingress":
			return field == "spec.ingressClassName"
		}
		return false
	}

	// Otherwise, accept immutable hints only when the message looks like a spec immutability error.
	if reason == "fieldvalueinvalid" || reason == "fieldvalueforbidden" || reason == "invalid" {
		// fall through to message checks
	}
	if !strings.Contains(messageLower, "immutable") {
		return false
	}
	if strings.Contains(messageLower, "field is immutable") {
		return true
	}
	// Heuristic: immutable + spec.<known>
	switch kind {
	case "Service":
		return strings.Contains(messageLower, "spec.clusterip")
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
		return strings.Contains(messageLower, "spec.selector")
	case "PersistentVolumeClaim":
		return strings.Contains(messageLower, "spec.storageclassname") || strings.Contains(messageLower, "spec.volumename")
	case "Ingress":
		return strings.Contains(messageLower, "spec.ingressclassname")
	}
	return false
}
