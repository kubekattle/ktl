package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/example/ktl/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func collectNamespacedObjects(ctx context.Context, client *kube.Client, namespace string) ([]map[string]any, error) {
	if client == nil || client.Clientset == nil {
		return nil, fmt.Errorf("kube client is required")
	}
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	var objs []map[string]any
	add := func(v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		objs = append(objs, out)
		return nil
	}
	addList := func(list any, err error) error {
		if err != nil {
			return err
		}
		raw, err := json.Marshal(list)
		if err != nil {
			return err
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		items, _ := m["items"].([]any)
		for _, it := range items {
			if obj, ok := it.(map[string]any); ok {
				objs = append(objs, obj)
			}
		}
		return nil
	}

	opts := metav1.ListOptions{}
	if err := addList(client.Clientset.AppsV1().Deployments(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.AppsV1().StatefulSets(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.AppsV1().DaemonSets(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.BatchV1().Jobs(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.BatchV1().CronJobs(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.CoreV1().Pods(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.CoreV1().Services(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.NetworkingV1().Ingresses(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.NetworkingV1().NetworkPolicies(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.CoreV1().ConfigMaps(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.CoreV1().Secrets(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.CoreV1().ServiceAccounts(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.RbacV1().Roles(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}
	if err := addList(client.Clientset.RbacV1().RoleBindings(namespace).List(ctx, opts)); err != nil {
		return nil, err
	}

	_ = add
	return objs, nil
}
