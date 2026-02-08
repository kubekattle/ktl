package kube

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FetchWorkloadEnv retrieves environment variables from a pod or deployment.
// target format: "pod/name" or "deployment/name" or just "name" (assumed deployment).
func FetchWorkloadEnv(ctx context.Context, client *Client, namespace, target string) ([]string, error) {
	kind := "deployment"
	name := target
	
	if strings.Contains(target, "/") {
		parts := strings.SplitN(target, "/", 2)
		kind = strings.ToLower(parts[0])
		name = parts[1]
	}

	var podSpec corev1.PodSpec

	switch kind {
	case "pod":
		pod, err := client.Clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		podSpec = pod.Spec
	case "deployment", "deploy":
		deploy, err := client.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		podSpec = deploy.Spec.Template.Spec
	case "statefulset", "sts":
		sts, err := client.Clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		podSpec = sts.Spec.Template.Spec
	default:
		return nil, fmt.Errorf("unsupported kind for env fetch: %s", kind)
	}

	if len(podSpec.Containers) == 0 {
		return nil, fmt.Errorf("no containers found in %s/%s", kind, name)
	}

	// Use the first container for now
	// TODO: Allow selecting container?
	container := podSpec.Containers[0]
	
	return resolveEnv(ctx, client, namespace, container)
}

func resolveEnv(ctx context.Context, client *Client, namespace string, container corev1.Container) ([]string, error) {
	var envs []string
	envMap := make(map[string]string)

	// 1. Handle envFrom (ConfigMaps and Secrets)
	for _, from := range container.EnvFrom {
		if from.ConfigMapRef != nil {
			cm, err := client.Clientset.CoreV1().ConfigMaps(namespace).Get(ctx, from.ConfigMapRef.Name, metav1.GetOptions{})
			if err != nil {
				if from.ConfigMapRef.Optional != nil && *from.ConfigMapRef.Optional {
					continue
				}
				return nil, fmt.Errorf("configmap %s not found: %w", from.ConfigMapRef.Name, err)
			}
			prefix := from.Prefix
			for k, v := range cm.Data {
				envMap[prefix+k] = v
			}
		}
		if from.SecretRef != nil {
			sec, err := client.Clientset.CoreV1().Secrets(namespace).Get(ctx, from.SecretRef.Name, metav1.GetOptions{})
			if err != nil {
				if from.SecretRef.Optional != nil && *from.SecretRef.Optional {
					continue
				}
				return nil, fmt.Errorf("secret %s not found: %w", from.SecretRef.Name, err)
			}
			prefix := from.Prefix
			for k, v := range sec.Data {
				envMap[prefix+k] = string(v)
			}
		}
	}

	// 2. Handle env (explicit values override envFrom)
	for _, e := range container.Env {
		if e.Value != "" {
			envMap[e.Name] = e.Value
		} else if e.ValueFrom != nil {
			// Handle ValueFrom
			if e.ValueFrom.ConfigMapKeyRef != nil {
				cm, err := client.Clientset.CoreV1().ConfigMaps(namespace).Get(ctx, e.ValueFrom.ConfigMapKeyRef.Name, metav1.GetOptions{})
				if err != nil {
					// skip if optional?
					continue
				}
				if val, ok := cm.Data[e.ValueFrom.ConfigMapKeyRef.Key]; ok {
					envMap[e.Name] = val
				}
			} else if e.ValueFrom.SecretKeyRef != nil {
				sec, err := client.Clientset.CoreV1().Secrets(namespace).Get(ctx, e.ValueFrom.SecretKeyRef.Name, metav1.GetOptions{})
				if err != nil {
					continue
				}
				if val, ok := sec.Data[e.ValueFrom.SecretKeyRef.Key]; ok {
					envMap[e.Name] = string(val)
				}
			}
			// FieldRef and ResourceFieldRef are skipped for now
		}
	}

	for k, v := range envMap {
		envs = append(envs, fmt.Sprintf("%s=%s", k, v))
	}
	return envs, nil
}
