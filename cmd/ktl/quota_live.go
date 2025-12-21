package main

import (
	"context"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type quotaSnapshot struct {
	Name string            `json:"name"`
	Hard map[string]string `json:"hard,omitempty"`
	Used map[string]string `json:"used,omitempty"`
}

type quotaHeadroom struct {
	Quota    string `json:"quota"`
	Resource string `json:"resource"`
	Hard     string `json:"hard,omitempty"`
	Used     string `json:"used,omitempty"`
	Desired  string `json:"desired,omitempty"`
	After    string `json:"after,omitempty"`
	Headroom string `json:"headroom,omitempty"`
	Status   string `json:"status"` // pass|warn|fail|unknown
}

func populateQuotaLive(ctx context.Context, client kubernetes.Interface, report *quotaReport) error {
	if client == nil || report == nil {
		return nil
	}
	ns := report.Namespace
	if ns == "" {
		ns = "default"
	}
	list, err := client.CoreV1().ResourceQuotas(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(list.Items) == 0 {
		return nil
	}

	desired := desiredQuotaAsMap(report.Desired)
	snapshots := make([]quotaSnapshot, 0, len(list.Items))
	var rows []quotaHeadroom

	for _, rq := range list.Items {
		hard := make(map[string]string)
		used := make(map[string]string)
		for k, v := range rq.Status.Hard {
			hard[string(k)] = v.String()
		}
		for k, v := range rq.Status.Used {
			used[string(k)] = v.String()
		}
		if len(hard) == 0 {
			for k, v := range rq.Spec.Hard {
				hard[string(k)] = v.String()
			}
		}
		snapshots = append(snapshots, quotaSnapshot{Name: rq.Name, Hard: hard, Used: used})

		for resKey, desiredQty := range desired {
			hardQty, hasHard := quantityFromMap(hard, resKey)
			usedQty, _ := quantityFromMap(used, resKey)
			row := quotaHeadroom{
				Quota:    rq.Name,
				Resource: resKey,
				Desired:  desiredQty.String(),
			}
			if !hasHard {
				row.Status = "unknown"
				rows = append(rows, row)
				continue
			}
			after := usedQty.DeepCopy()
			after.Add(desiredQty)
			headroom := hardQty.DeepCopy()
			headroom.Sub(after)
			row.Hard = hardQty.String()
			row.Used = usedQty.String()
			row.After = after.String()
			row.Headroom = headroom.String()
			row.Status = headroomStatus(hardQty, headroom)
			rows = append(rows, row)
		}
	}

	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Name < snapshots[j].Name })
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Quota != rows[j].Quota {
			return rows[i].Quota < rows[j].Quota
		}
		return rows[i].Resource < rows[j].Resource
	})

	report.Live = snapshots
	report.Headroom = rows
	return nil
}

func desiredQuotaAsMap(t quotaUsageTotals) map[string]resource.Quantity {
	out := map[string]resource.Quantity{}
	put := func(key string, val string) {
		val = strings.TrimSpace(val)
		if val == "" || val == "0" {
			return
		}
		q, err := resource.ParseQuantity(val)
		if err != nil {
			return
		}
		out[key] = q
	}
	put(string(corev1.ResourceRequestsCPU), t.CPURequests.Value)
	put(string(corev1.ResourceLimitsCPU), t.CPULimits.Value)
	put(string(corev1.ResourceRequestsMemory), t.MemoryRequests.Value)
	put(string(corev1.ResourceLimitsMemory), t.MemoryLimits.Value)
	put(string(corev1.ResourceRequestsStorage), t.Storage.Value)
	// Object counts are also represented as quantities in ResourceQuota.
	out[string(corev1.ResourcePods)] = *resource.NewQuantity(t.Pods, resource.DecimalSI)
	out[string(corev1.ResourceServices)] = *resource.NewQuantity(t.Services, resource.DecimalSI)
	out[string(corev1.ResourceConfigMaps)] = *resource.NewQuantity(t.ConfigMaps, resource.DecimalSI)
	out[string(corev1.ResourceSecrets)] = *resource.NewQuantity(t.Secrets, resource.DecimalSI)
	out[string(corev1.ResourcePersistentVolumeClaims)] = *resource.NewQuantity(t.PVCs, resource.DecimalSI)
	return out
}

func quantityFromMap(m map[string]string, key string) (resource.Quantity, bool) {
	val, ok := m[key]
	if !ok {
		return resource.Quantity{}, false
	}
	q, err := resource.ParseQuantity(val)
	if err != nil {
		return resource.Quantity{}, false
	}
	return q, true
}

func headroomStatus(hard, headroom resource.Quantity) string {
	if headroom.Sign() < 0 {
		return "fail"
	}
	if hard.Sign() == 0 {
		return "unknown"
	}
	// Warn when remaining <= 10% of hard.
	hardMilli := hard.MilliValue()
	if hardMilli <= 0 {
		return "unknown"
	}
	remainMilli := headroom.MilliValue()
	if remainMilli*10 <= hardMilli {
		return "warn"
	}
	return "pass"
}
