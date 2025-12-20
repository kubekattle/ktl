// File: internal/deploy/status.go
// Brief: Internal deploy package implementation for 'status'.

// Package deploy provides deploy helpers.

package deploy

import (
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourceStatus captures the readiness state of a Kubernetes object managed by a release.
type ResourceStatus struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

func deploymentStatus(dep *appsv1.Deployment) ResourceStatus {
	rs := baseStatus("Deployment", dep.ObjectMeta)
	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	ready := dep.Status.ReadyReplicas
	rs.Message = fmt.Sprintf("%d/%d pods ready", ready, desired)
	rs.Status = statusForReplicas(desired, ready)
	for _, cond := range dep.Status.Conditions {
		if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
			rs.Status = "Failed"
			rs.Message = cond.Message
		}
		if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
			rs.Status = "Failed"
			rs.Message = cond.Message
		}
	}
	return rs
}

func statefulSetStatus(sts *appsv1.StatefulSet) ResourceStatus {
	rs := baseStatus("StatefulSet", sts.ObjectMeta)
	desired := int32(1)
	if sts.Spec.Replicas != nil {
		desired = *sts.Spec.Replicas
	}
	ready := sts.Status.ReadyReplicas
	rs.Message = fmt.Sprintf("%d/%d pods ready", ready, desired)
	rs.Status = statusForReplicas(desired, ready)
	return rs
}

func daemonSetStatus(ds *appsv1.DaemonSet) ResourceStatus {
	rs := baseStatus("DaemonSet", ds.ObjectMeta)
	desired := ds.Status.DesiredNumberScheduled
	ready := ds.Status.NumberReady
	rs.Message = fmt.Sprintf("%d/%d pods ready", ready, desired)
	rs.Status = statusForReplicas(desired, ready)
	return rs
}

func jobStatus(job *batchv1.Job) ResourceStatus {
	rs := baseStatus("Job", job.ObjectMeta)
	switch {
	case job.Status.Failed > 0:
		rs.Status = "Failed"
		rs.Message = renderConditions(job.Status.Conditions, "Job failed")
	case job.Status.Succeeded > 0 && (job.Spec.Completions == nil || job.Status.Succeeded >= *job.Spec.Completions):
		rs.Status = "Ready"
		if job.Status.CompletionTime != nil && job.Status.StartTime != nil {
			rs.Message = fmt.Sprintf("Completed (%s)", job.Status.CompletionTime.Time.Sub(job.Status.StartTime.Time).Truncate(time.Second))
		} else {
			rs.Message = "Completed"
		}
	default:
		rs.Status = "Progressing"
		rs.Message = fmt.Sprintf("%d/%d completions", job.Status.Succeeded, valueOrDefault(job.Spec.Completions, 1))
	}
	return rs
}

func baseStatus(kind string, meta metav1.ObjectMeta) ResourceStatus {
	return ResourceStatus{
		Kind:      kind,
		Namespace: meta.Namespace,
		Name:      meta.Name,
		Action:    "-",
		Status:    "Pending",
		Message:   "",
	}
}

func cronJobStatus(job *batchv1.CronJob) ResourceStatus {
	rs := baseStatus("CronJob", job.ObjectMeta)
	active := len(job.Status.Active)
	lastRun := "never"
	if job.Status.LastScheduleTime != nil {
		lastRun = job.Status.LastScheduleTime.Time.UTC().Format(time.RFC3339)
	}
	rs.Message = fmt.Sprintf("Active: %d, Last schedule: %s", active, lastRun)
	if job.Spec.Suspend != nil && *job.Spec.Suspend {
		rs.Status = "Suspended"
	} else {
		rs.Status = "Ready"
	}
	return rs
}

func podStatus(pod *corev1.Pod) ResourceStatus {
	rs := baseStatus("Pod", pod.ObjectMeta)
	switch pod.Status.Phase {
	case corev1.PodRunning:
		rs.Status = "Ready"
		rs.Message = renderContainerSummary(pod)
	case corev1.PodSucceeded:
		rs.Status = "Ready"
		rs.Message = "Completed"
	case corev1.PodFailed:
		rs.Status = "Failed"
		rs.Message = describePodFailure(pod)
	case corev1.PodPending:
		rs.Status = "Pending"
		rs.Message = renderContainerSummary(pod)
	default:
		rs.Status = string(pod.Status.Phase)
		rs.Message = renderContainerSummary(pod)
	}
	return rs
}

func pdbStatus(pdb *policyv1.PodDisruptionBudget) ResourceStatus {
	rs := baseStatus("PodDisruptionBudget", pdb.ObjectMeta)
	current := pdb.Status.CurrentHealthy
	desired := pdb.Status.DesiredHealthy
	allowed := pdb.Status.DisruptionsAllowed
	rs.Message = fmt.Sprintf("Healthy %d/%d, Disruptions allowed %d", current, desired, allowed)
	switch {
	case allowed == 0 && current < desired:
		rs.Status = "Pending"
	case current < desired:
		rs.Status = "Progressing"
	default:
		rs.Status = "Ready"
	}
	return rs
}

func hpaStatus(hpa *autoscalingv2.HorizontalPodAutoscaler) ResourceStatus {
	rs := baseStatus("HorizontalPodAutoscaler", hpa.ObjectMeta)
	current := hpa.Status.CurrentReplicas
	desired := hpa.Status.DesiredReplicas
	rs.Message = fmt.Sprintf("%d/%d pods (current/desired)", current, desired)
	switch {
	case desired == 0 && current == 0:
		rs.Status = "Ready"
	case desired == current:
		rs.Status = "Ready"
	default:
		rs.Status = "Progressing"
	}
	for _, cond := range hpa.Status.Conditions {
		if cond.Status == corev1.ConditionFalse && cond.Message != "" {
			rs.Status = "Failed"
			rs.Message = cond.Message
			break
		}
	}
	return rs
}

func genericStatus(kind string, meta metav1.ObjectMeta, message string) ResourceStatus {
	rs := baseStatus(kind, meta)
	if strings.TrimSpace(message) == "" {
		message = "Tracking resource readiness"
	}
	rs.Status = "Pending"
	rs.Message = message
	return rs
}

func renderConditions(conds []batchv1.JobCondition, fallback string) string {
	for _, cond := range conds {
		if cond.Status == corev1.ConditionTrue && cond.Message != "" {
			return cond.Message
		}
	}
	return fallback
}

func statusForReplicas(desired, ready int32) string {
	switch {
	case desired == 0:
		return "Ready"
	case ready >= desired:
		return "Ready"
	case ready > 0:
		return "Progressing"
	default:
		return "Pending"
	}
}

func valueOrDefault(v *int32, def int32) int32 {
	if v == nil {
		return def
	}
	return *v
}

func sortKey(rs ResourceStatus) string {
	return fmt.Sprintf("%s/%s/%s", rs.Kind, rs.Namespace, rs.Name)
}

func normalizeStatus(rs ResourceStatus) ResourceStatus {
	if rs.Message == "" {
		rs.Message = strings.ToLower(rs.Status)
	}
	return rs
}

func renderContainerSummary(pod *corev1.Pod) string {
	total := len(pod.Status.ContainerStatuses)
	if total == 0 {
		return strings.ToLower(string(pod.Status.Phase))
	}
	ready := 0
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
	}
	return fmt.Sprintf("%d/%d containers ready", ready, total)
}

func describePodFailure(pod *corev1.Pod) string {
	if pod.Status.Message != "" {
		return pod.Status.Message
	}
	if pod.Status.Reason != "" {
		return pod.Status.Reason
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Terminated != nil {
			state := cs.State.Terminated
			if state.Message != "" {
				return state.Message
			}
			if state.Reason != "" {
				return state.Reason
			}
			return fmt.Sprintf("ExitCode %d", state.ExitCode)
		}
		if cs.State.Waiting != nil && cs.State.Waiting.Message != "" {
			return cs.State.Waiting.Message
		}
	}
	return "Pod failed"
}
