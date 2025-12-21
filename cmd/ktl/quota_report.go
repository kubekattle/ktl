package main

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type quotaQuantity struct {
	Value string `json:"value"`
}

type quotaUsageTotals struct {
	CPURequests    quotaQuantity `json:"cpuRequests"`
	CPULimits      quotaQuantity `json:"cpuLimits"`
	MemoryRequests quotaQuantity `json:"memoryRequests"`
	MemoryLimits   quotaQuantity `json:"memoryLimits"`
	Storage        quotaQuantity `json:"storage"`
	Pods           int64         `json:"pods"`
	Services       int64         `json:"services"`
	ConfigMaps     int64         `json:"configmaps"`
	Secrets        int64         `json:"secrets"`
	PVCs           int64         `json:"pvcs"`
}

type quotaReport struct {
	Namespace string           `json:"namespace"`
	Desired   quotaUsageTotals `json:"desired"`
	Warnings  []string         `json:"warnings,omitempty"`
}

func buildDesiredQuotaReport(desired map[resourceKey]manifestDoc, targetNamespace string) *quotaReport {
	totals, warnings := computeDesiredQuotaTotals(desired, targetNamespace)
	if totals == nil {
		return nil
	}
	return &quotaReport{
		Namespace: targetNamespace,
		Desired:   *totals,
		Warnings:  warnings,
	}
}

func computeDesiredQuotaTotals(desired map[resourceKey]manifestDoc, targetNamespace string) (*quotaUsageTotals, []string) {
	if len(desired) == 0 {
		return nil, nil
	}
	if targetNamespace == "" {
		targetNamespace = "default"
	}

	var warnings []string
	var cpuReq, cpuLim, memReq, memLim, storage resource.Quantity
	var pods, services, configmaps, secrets, pvcs int64

	addQty := func(dst *resource.Quantity, v string, what string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		q, err := resource.ParseQuantity(v)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Skipping %s quantity %q: %v", what, v, err))
			return
		}
		dst.Add(q)
	}

	addPodTemplate := func(obj *unstructured.Unstructured, replicaGuess int64, source string) {
		template, ok, err := unstructured.NestedMap(obj.Object, "spec", "template")
		if err != nil || !ok {
			return
		}
		spec, ok := template["spec"].(map[string]interface{})
		if !ok {
			return
		}
		containers, _ := spec["containers"].([]interface{})
		initContainers, _ := spec["initContainers"].([]interface{})

		var perPodCPUReq, perPodCPULim, perPodMemReq, perPodMemLim resource.Quantity
		seenAny := false
		for _, c := range append(containers, initContainers...) {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := cm["name"].(string)
			resources, _ := cm["resources"].(map[string]interface{})
			requests, _ := resources["requests"].(map[string]interface{})
			limits, _ := resources["limits"].(map[string]interface{})

			cpu, _ := requests["cpu"].(string)
			mem, _ := requests["memory"].(string)
			cpuL, _ := limits["cpu"].(string)
			memL, _ := limits["memory"].(string)

			if cpu == "" && mem == "" && cpuL == "" && memL == "" {
				continue
			}
			seenAny = true
			addQty(&perPodCPUReq, cpu, "cpu request ("+source+"/"+name+")")
			addQty(&perPodMemReq, mem, "memory request ("+source+"/"+name+")")
			addQty(&perPodCPULim, cpuL, "cpu limit ("+source+"/"+name+")")
			addQty(&perPodMemLim, memL, "memory limit ("+source+"/"+name+")")
		}
		if !seenAny {
			warnings = append(warnings, fmt.Sprintf("Workload %s has no cpu/memory requests or limits; quota estimates may be incomplete.", source))
		}

		if replicaGuess < 0 {
			replicaGuess = 0
		}
		mul := replicaGuess
		pods += mul

		scale := func(q resource.Quantity, by int64) resource.Quantity {
			if by <= 1 {
				return q
			}
			// Quantity lacks a Mul(int64), so scale via milli-value for CPU and via value for others.
			// This is an approximation for very large values but keeps JSON stable.
			if q.Format == resource.DecimalSI || q.Format == resource.BinarySI {
				// Fall back to milli-based scaling.
				m := q.MilliValue()
				return *resource.NewMilliQuantity(m*by, q.Format)
			}
			m := q.MilliValue()
			return *resource.NewMilliQuantity(m*by, q.Format)
		}

		cpuReq.Add(scale(perPodCPUReq, mul))
		cpuLim.Add(scale(perPodCPULim, mul))
		memReq.Add(scale(perPodMemReq, mul))
		memLim.Add(scale(perPodMemLim, mul))
	}

	for key, doc := range desired {
		ns := key.Namespace
		if ns == "" && strings.EqualFold(key.Kind, "Namespace") {
			continue
		}
		if ns == "" {
			ns = targetNamespace
		}
		if ns != targetNamespace {
			continue
		}
		kind := strings.ToLower(key.Kind)
		switch kind {
		case "service":
			services++
		case "configmap":
			configmaps++
		case "secret":
			secrets++
		case "persistentvolumeclaim":
			pvcs++
			req, ok, _ := unstructured.NestedString(doc.Obj.Object, "spec", "resources", "requests", "storage")
			if ok {
				addQty(&storage, req, "pvc storage ("+key.Name+")")
			}
		case "pod":
			// Standalone pods count as 1.
			pods++
			addPodResourcesFromPodSpec(doc.Obj, &cpuReq, &cpuLim, &memReq, &memLim, &warnings, "pod/"+key.Name)
		case "deployment", "statefulset", "replicaset":
			replicas := int64(1)
			if r, ok := nestedInt64(doc.Obj.Object, "spec", "replicas"); ok {
				replicas = max64(0, r)
			}
			addPodTemplate(doc.Obj, replicas, kind+"/"+key.Name)
		case "daemonset":
			// Node count is unknown at plan time; estimate 1 and warn.
			warnings = append(warnings, fmt.Sprintf("DaemonSet %s replica count is cluster-dependent; estimating 1 pod for quota calculations.", key.Name))
			addPodTemplate(doc.Obj, 1, kind+"/"+key.Name)
		case "job":
			guess := jobReplicaGuess(doc.Obj, &warnings, "job/"+key.Name)
			addPodTemplate(doc.Obj, guess, kind+"/"+key.Name)
		case "cronjob":
			warnings = append(warnings, fmt.Sprintf("CronJob %s runs on a schedule; estimating 1 active job pod for quota calculations.", key.Name))
			addPodTemplate(doc.Obj, 1, kind+"/"+key.Name)
		}
	}

	return &quotaUsageTotals{
		CPURequests:    quotaQuantity{Value: cpuReq.String()},
		CPULimits:      quotaQuantity{Value: cpuLim.String()},
		MemoryRequests: quotaQuantity{Value: memReq.String()},
		MemoryLimits:   quotaQuantity{Value: memLim.String()},
		Storage:        quotaQuantity{Value: storage.String()},
		Pods:           pods,
		Services:       services,
		ConfigMaps:     configmaps,
		Secrets:        secrets,
		PVCs:           pvcs,
	}, warnings
}

func addPodResourcesFromPodSpec(obj *unstructured.Unstructured, cpuReq, cpuLim, memReq, memLim *resource.Quantity, warnings *[]string, source string) {
	spec, ok, _ := unstructured.NestedMap(obj.Object, "spec")
	if !ok {
		return
	}
	containers, _ := spec["containers"].([]interface{})
	initContainers, _ := spec["initContainers"].([]interface{})
	seenAny := false
	for _, c := range append(containers, initContainers...) {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := cm["name"].(string)
		resources, _ := cm["resources"].(map[string]interface{})
		requests, _ := resources["requests"].(map[string]interface{})
		limits, _ := resources["limits"].(map[string]interface{})
		cpu, _ := requests["cpu"].(string)
		mem, _ := requests["memory"].(string)
		cpuL, _ := limits["cpu"].(string)
		memL, _ := limits["memory"].(string)
		if cpu == "" && mem == "" && cpuL == "" && memL == "" {
			continue
		}
		seenAny = true
		if cpu != "" {
			if q, err := resource.ParseQuantity(cpu); err == nil {
				cpuReq.Add(q)
			} else {
				*warnings = append(*warnings, fmt.Sprintf("Skipping cpu request %q (%s/%s): %v", cpu, source, name, err))
			}
		}
		if mem != "" {
			if q, err := resource.ParseQuantity(mem); err == nil {
				memReq.Add(q)
			} else {
				*warnings = append(*warnings, fmt.Sprintf("Skipping memory request %q (%s/%s): %v", mem, source, name, err))
			}
		}
		if cpuL != "" {
			if q, err := resource.ParseQuantity(cpuL); err == nil {
				cpuLim.Add(q)
			} else {
				*warnings = append(*warnings, fmt.Sprintf("Skipping cpu limit %q (%s/%s): %v", cpuL, source, name, err))
			}
		}
		if memL != "" {
			if q, err := resource.ParseQuantity(memL); err == nil {
				memLim.Add(q)
			} else {
				*warnings = append(*warnings, fmt.Sprintf("Skipping memory limit %q (%s/%s): %v", memL, source, name, err))
			}
		}
	}
	if !seenAny {
		*warnings = append(*warnings, fmt.Sprintf("Pod %s has no cpu/memory requests or limits; quota estimates may be incomplete.", source))
	}
}

func jobReplicaGuess(obj *unstructured.Unstructured, warnings *[]string, source string) int64 {
	parallelism, hasPar := nestedInt64(obj.Object, "spec", "parallelism")
	completions, hasComp := nestedInt64(obj.Object, "spec", "completions")
	guess := int64(1)
	if hasPar {
		guess = max64(1, parallelism)
	}
	if hasComp {
		guess = max64(guess, completions)
	}
	if hasPar || hasComp {
		return guess
	}
	*warnings = append(*warnings, fmt.Sprintf("Job %s has no parallelism/completions; estimating 1 pod for quota calculations.", source))
	return 1
}

func nestedInt64(obj map[string]interface{}, fields ...string) (int64, bool) {
	raw, ok, _ := unstructured.NestedFieldNoCopy(obj, fields...)
	if !ok || raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	default:
		return 0, false
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
