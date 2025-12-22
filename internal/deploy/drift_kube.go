package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/example/ktl/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func DriftLiveGetterFromKube(client *kube.Client) DriftLiveGetter {
	return func(ctx context.Context, target resourceTarget) (*unstructured.Unstructured, error) {
		if client == nil || client.Dynamic == nil || client.RESTMapper == nil {
			return nil, fmt.Errorf("kubernetes client unavailable")
		}
		name := strings.TrimSpace(target.Name)
		kind := strings.TrimSpace(target.Kind)
		if name == "" || kind == "" {
			return nil, nil
		}
		ns := strings.TrimSpace(target.Namespace)
		if ns == "" {
			ns = strings.TrimSpace(client.Namespace)
		}
		gvk := schema.GroupVersionKind{Group: target.Group, Version: target.Version, Kind: kind}
		mapping, err := client.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return nil, fmt.Errorf("resolve REST mapping for %s: %w", gvk.String(), err)
		}
		res := client.Dynamic.Resource(mapping.Resource)
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace && ns != "" {
			obj, err := res.Namespace(ns).Get(ctx, name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return nil, nil
			}
			return obj, err
		}
		obj, err := res.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return obj, err
	}
}
