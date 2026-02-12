package verify

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kubekattle/ktl/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func CollectNamespacedObjects(ctx context.Context, client *kube.Client, namespace string) ([]map[string]any, error) {
	if client == nil || client.Clientset == nil {
		return nil, fmt.Errorf("kube client is required")
	}
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	var objs []map[string]any

	addList := func(kind string, list any, err error) error {
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
			obj, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if obj["kind"] == nil && kind != "" {
				obj["kind"] = kind
			}
			objs = append(objs, obj)
		}
		return nil
	}

	opts := metav1.ListOptions{}
	if list, err := client.Clientset.AppsV1().Deployments(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("Deployment", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.AppsV1().StatefulSets(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("StatefulSet", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.AppsV1().DaemonSets(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("DaemonSet", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.BatchV1().Jobs(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("Job", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.BatchV1().CronJobs(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("CronJob", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.CoreV1().Pods(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("Pod", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.CoreV1().Services(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("Service", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.NetworkingV1().Ingresses(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("Ingress", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.NetworkingV1().NetworkPolicies(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("NetworkPolicy", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.CoreV1().ConfigMaps(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("ConfigMap", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.CoreV1().Secrets(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("Secret", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.CoreV1().ServiceAccounts(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("ServiceAccount", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.RbacV1().Roles(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("Role", list, nil); err != nil {
		return nil, err
	}
	if list, err := client.Clientset.RbacV1().RoleBindings(namespace).List(ctx, opts); err != nil {
		return nil, err
	} else if err := addList("RoleBinding", list, nil); err != nil {
		return nil, err
	}

	return objs, nil
}
