// resourceutil.go provides helpers for calculating request/limit utilization ratios across workloads.
package resourceutil

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// QuantityInt returns the absolute value represented by the quantity (bytes for memory, count for pods, etc.).
func QuantityInt(q resource.Quantity) int64 {
	return q.Value()
}

// QuantityMilli returns the milli-value representation useful for CPU requests.
func QuantityMilli(q resource.Quantity) int64 {
	return q.MilliValue()
}

// SumRequests aggregates resource requests for the provided containers using the supplied conversion function.
func SumRequests(containers []corev1.Container, resourceName corev1.ResourceName, conv func(resource.Quantity) int64) int64 {
	var total int64
	for _, c := range containers {
		if quantity, ok := c.Resources.Requests[resourceName]; ok {
			total += conv(quantity)
		}
	}
	return total
}

// FromResourceList returns the converted quantity for the given resource name or zero.
func FromResourceList(list corev1.ResourceList, resourceName corev1.ResourceName, conv func(resource.Quantity) int64) int64 {
	if list == nil {
		return 0
	}
	if quantity, ok := list[resourceName]; ok {
		return conv(quantity)
	}
	return 0
}
