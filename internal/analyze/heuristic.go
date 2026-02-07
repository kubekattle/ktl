package analyze

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

type HeuristicAnalyzer struct{}

func NewHeuristicAnalyzer() *HeuristicAnalyzer {
	return &HeuristicAnalyzer{}
}

func (a *HeuristicAnalyzer) Analyze(ctx context.Context, evidence *Evidence) (*Diagnosis, error) {
	d := &Diagnosis{
		ConfidenceScore: 0.5, // Baseline
	}

	// 1. Check Pod Status
	phase := evidence.Pod.Status.Phase
	if phase == corev1.PodRunning {
		// Check for CrashLoopBackOff or individual container issues
		for _, cs := range evidence.Pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil && (cs.State.Waiting.Reason == "CrashLoopBackOff" || cs.State.Waiting.Reason == "ImagePullBackOff") {
				return a.diagnoseContainerState(cs.Name, cs.State, evidence), nil
			}
			if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
				return a.diagnoseExitCode(cs.Name, cs.State.Terminated.ExitCode, evidence), nil
			}
		}
		d.RootCause = "Pod appears running but might be unhealthy internally."
		d.Suggestion = "Check application logs for logic errors."
		return d, nil
	} else if phase == corev1.PodPending {
		// Check events for scheduling issues
		for _, e := range evidence.Events {
			if e.Reason == "FailedScheduling" {
				d.RootCause = fmt.Sprintf("Scheduling Failed: %s", e.Message)
				d.Suggestion = "Check node resources (CPU/Memory) or taints/tolerations."
				d.ConfidenceScore = 0.9
				return d, nil
			}
		}
	}

	// 2. Fallback: Scan logs for common keywords
	for cName, log := range evidence.Logs {
		if strings.Contains(log, "panic:") {
			d.RootCause = fmt.Sprintf("Go Panic detected in container '%s'", cName)
			d.Suggestion = "Fix the nil pointer dereference or logic error in your Go code."
			d.ConfidenceScore = 0.95
			return d, nil
		}
		if strings.Contains(log, "ClassCastException") || strings.Contains(log, "NullPointerException") {
			d.RootCause = fmt.Sprintf("Java Exception detected in container '%s'", cName)
			d.Suggestion = "Check the stack trace in the logs."
			d.ConfidenceScore = 0.9
			return d, nil
		}
	}

	d.RootCause = "Unknown issue."
	d.Suggestion = "Review the full logs with 'ktl logs'."
	return d, nil
}

func (a *HeuristicAnalyzer) diagnoseContainerState(name string, state corev1.ContainerState, evidence *Evidence) *Diagnosis {
	d := &Diagnosis{ConfidenceScore: 0.8}
	
	if state.Waiting != nil {
		reason := state.Waiting.Reason
		msg := state.Waiting.Message
		
		if reason == "ImagePullBackOff" || reason == "ErrImagePull" {
			d.RootCause = fmt.Sprintf("Container '%s' failed to pull image.", name)
			d.Suggestion = "Check if the image name/tag is correct and if you have the necessary registry credentials (imagePullSecrets)."
			d.Explanation = fmt.Sprintf("Kubelet message: %s", msg)
			return d
		}
		if reason == "CrashLoopBackOff" {
			// Check exit code of previous run if available
			// Or check logs
			logs := evidence.Logs[name]
			if strings.Contains(logs, "command not found") {
				d.RootCause = fmt.Sprintf("Container '%s' entrypoint command not found.", name)
				d.Suggestion = "Verify the ENTRYPOINT or CMD in your Dockerfile."
				d.ConfidenceScore = 0.95
				return d
			}
			d.RootCause = fmt.Sprintf("Container '%s' is crashing repeatedly.", name)
			d.Suggestion = "Check logs for application startup errors."
			return d
		}
	}
	return d
}

func (a *HeuristicAnalyzer) diagnoseExitCode(name string, exitCode int32, evidence *Evidence) *Diagnosis {
	d := &Diagnosis{ConfidenceScore: 0.85}
	d.RootCause = fmt.Sprintf("Container '%s' exited with code %d.", name, exitCode)
	
	switch exitCode {
	case 137: // OOMKilled usually
		d.RootCause = fmt.Sprintf("Container '%s' was OOMKilled (Exit Code 137).", name)
		d.Suggestion = "Increase memory limits in your pod spec."
	case 1:
		d.Suggestion = "Application error. Check logs."
	default:
		d.Suggestion = "Check exit code meaning for your specific application runtime."
	}
	return d
}
