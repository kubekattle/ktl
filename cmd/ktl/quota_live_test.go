package main

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPopulateQuotaLive(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rq",
			Namespace: "demo",
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("1"),
				corev1.ResourcePods:        resource.MustParse("10"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("250m"),
				corev1.ResourcePods:        resource.MustParse("3"),
			},
		},
	})

	report := &quotaReport{
		Namespace: "demo",
		Desired: quotaUsageTotals{
			CPURequests: quotaQuantity{Value: "200m"},
			Pods:        2,
		},
	}

	if err := populateQuotaLive(context.Background(), client, report); err != nil {
		t.Fatalf("populate: %v", err)
	}
	if len(report.Live) != 1 {
		t.Fatalf("expected 1 quota snapshot, got %d", len(report.Live))
	}
	if len(report.Headroom) == 0 {
		t.Fatalf("expected headroom rows")
	}

	var cpuRow *quotaHeadroom
	for i := range report.Headroom {
		if report.Headroom[i].Resource == string(corev1.ResourceRequestsCPU) {
			cpuRow = &report.Headroom[i]
			break
		}
	}
	if cpuRow == nil {
		t.Fatalf("missing cpu headroom row")
	}
	if cpuRow.After != "450m" {
		t.Fatalf("expected after=450m, got %q", cpuRow.After)
	}
	if cpuRow.Headroom == "" || cpuRow.Status == "" {
		t.Fatalf("expected headroom and status, got %+v", *cpuRow)
	}
}
