// collect.go fetches CronJobs/Jobs and builds the health summary powering 'ktl diag cronjobs'.
package jobs

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Options controls which namespaces are inspected.
type Options struct {
	Namespaces       []string
	AllNamespaces    bool
	DefaultNamespace string
	JobLimit         int
}

// Summary groups CronJob and Job insights.
type Summary struct {
	CronJobs []CronJobSummary
	Jobs     []JobSummary
}

// CronJobSummary highlights health indicators for one CronJob.
type CronJobSummary struct {
	Namespace         string
	Name              string
	Schedule          string
	Suspend           bool
	Active            int32
	LastScheduleTime  *metav1.Time
	LastSuccessfulRun *metav1.Time
	SuccessfulJobs    int
	FailedJobs        int
	Status            string
	Note              string
}

// JobSummary captures the important status bits for a Job.
type JobSummary struct {
	Namespace  string
	Name       string
	CronJob    string
	Active     int32
	Succeeded  int32
	Failed     int32
	StartTime  *metav1.Time
	FinishTime *metav1.Time
	Status     string
	Message    string
}

// Collect builds a Summary for the selected namespaces.
func Collect(ctx context.Context, client kubernetes.Interface, opts Options) (*Summary, error) {
	namespaces, err := resolveNamespaces(ctx, client, opts)
	if err != nil {
		return nil, err
	}

	var cronSummaries []CronJobSummary
	var jobSummaries []JobSummary

	for _, ns := range namespaces {
		cronJobs, err := client.BatchV1().CronJobs(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list CronJobs in %s: %w", ns, err)
		}
		jobs, err := client.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list Jobs in %s: %w", ns, err)
		}

		cronJobJobs := map[string][]batchv1.Job{}
		for _, job := range jobs.Items {
			parent := owningCronJob(job)
			if parent != "" {
				cronJobJobs[parent] = append(cronJobJobs[parent], job)
			}
			jobSummaries = append(jobSummaries, summarizeJob(job))
		}

		for _, cron := range cronJobs.Items {
			cronSummaries = append(cronSummaries, summarizeCronJob(cron, cronJobJobs[cron.Name]))
		}
	}

	sort.Slice(cronSummaries, func(i, j int) bool {
		if cronSummaries[i].Namespace == cronSummaries[j].Namespace {
			return cronSummaries[i].Name < cronSummaries[j].Name
		}
		return cronSummaries[i].Namespace < cronSummaries[j].Namespace
	})
	sort.Slice(jobSummaries, func(i, j int) bool {
		ti := jobSummaries[i].StartTime
		tj := jobSummaries[j].StartTime
		var iTime, jTime time.Time
		if ti != nil {
			iTime = ti.Time
		}
		if tj != nil {
			jTime = tj.Time
		}
		if iTime.Equal(jTime) {
			if jobSummaries[i].Namespace == jobSummaries[j].Namespace {
				return jobSummaries[i].Name < jobSummaries[j].Name
			}
			return jobSummaries[i].Namespace < jobSummaries[j].Namespace
		}
		return iTime.After(jTime)
	})
	if opts.JobLimit > 0 && len(jobSummaries) > opts.JobLimit {
		jobSummaries = jobSummaries[:opts.JobLimit]
	}

	return &Summary{
		CronJobs: cronSummaries,
		Jobs:     jobSummaries,
	}, nil
}

func resolveNamespaces(ctx context.Context, client kubernetes.Interface, opts Options) ([]string, error) {
	if opts.AllNamespaces {
		list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces: %w", err)
		}
		names := make([]string, 0, len(list.Items))
		for _, ns := range list.Items {
			names = append(names, ns.Name)
		}
		return names, nil
	}

	var names []string
	for _, ns := range opts.Namespaces {
		ns = strings.TrimSpace(ns)
		if ns != "" {
			names = append(names, ns)
		}
	}
	if len(names) > 0 {
		return names, nil
	}
	if opts.DefaultNamespace != "" {
		return []string{opts.DefaultNamespace}, nil
	}
	return []string{"default"}, nil
}

func summarizeCronJob(cron batchv1.CronJob, jobs []batchv1.Job) CronJobSummary {
	var successful, failed int
	var lastFailure *batchv1.Job
	for _, job := range jobs {
		if job.Status.Succeeded > 0 {
			successful++
		}
		if job.Status.Failed > 0 {
			failed++
			if lastFailure == nil || jobStart(job).After(jobStart(*lastFailure)) {
				lastFailure = &job
			}
		}
	}

	status := "OK"
	note := ""
	if cron.Spec.Suspend != nil && *cron.Spec.Suspend {
		status = "SUSPENDED"
	} else if cron.Status.LastScheduleTime == nil && successful == 0 && failed == 0 {
		status = "NO RUNS"
		note = "CronJob has never scheduled"
	} else if failed > 0 && (successful == 0 || (cron.Status.LastSuccessfulTime != nil && cron.Status.LastScheduleTime != nil && cron.Status.LastSuccessfulTime.Before(cron.Status.LastScheduleTime))) {
		status = "FAILING"
		if lastFailure != nil {
			note = fmt.Sprintf("Last failure: %s at %s", lastFailure.Name, jobStart(*lastFailure).Format(time.RFC3339))
		}
	} else if cron.Status.LastScheduleTime != nil {
		elapsed := time.Since(cron.Status.LastScheduleTime.Time)
		if elapsed > 24*time.Hour {
			status = "STALE"
			note = fmt.Sprintf("Last schedule %s ago", humanizeDuration(elapsed))
		}
	}

	return CronJobSummary{
		Namespace:         cron.Namespace,
		Name:              cron.Name,
		Schedule:          cron.Spec.Schedule,
		Suspend:           cron.Spec.Suspend != nil && *cron.Spec.Suspend,
		Active:            int32(len(cron.Status.Active)),
		LastScheduleTime:  cron.Status.LastScheduleTime,
		LastSuccessfulRun: cron.Status.LastSuccessfulTime,
		SuccessfulJobs:    successful,
		FailedJobs:        failed,
		Status:            status,
		Note:              note,
	}
}

func summarizeJob(job batchv1.Job) JobSummary {
	status := "RUNNING"
	if job.Status.Failed > 0 {
		status = "FAILED"
	} else if job.Status.Succeeded > 0 {
		status = "SUCCEEDED"
	}
	message := ""
	if cond := latestJobCondition(job.Status.Conditions); cond != nil {
		if cond.Reason != "" {
			message = cond.Reason
		}
		if cond.Message != "" {
			if message != "" {
				message += ": "
			}
			message += cond.Message
		}
	}
	return JobSummary{
		Namespace:  job.Namespace,
		Name:       job.Name,
		CronJob:    owningCronJob(job),
		Active:     job.Status.Active,
		Succeeded:  job.Status.Succeeded,
		Failed:     job.Status.Failed,
		StartTime:  job.Status.StartTime,
		FinishTime: job.Status.CompletionTime,
		Status:     status,
		Message:    message,
	}
}

func latestJobCondition(conditions []batchv1.JobCondition) *batchv1.JobCondition {
	var latest *batchv1.JobCondition
	for i := range conditions {
		cond := conditions[i]
		if latest == nil || cond.LastTransitionTime.After(latest.LastTransitionTime.Time) {
			latest = &cond
		}
	}
	return latest
}

func owningCronJob(job batchv1.Job) string {
	for _, owner := range job.OwnerReferences {
		if owner.Kind == "CronJob" && owner.Controller != nil && *owner.Controller {
			return owner.Name
		}
	}
	if name, ok := job.Labels["cronjob.kubernetes.io/instance"]; ok {
		return name
	}
	return ""
}

func jobStart(job batchv1.Job) time.Time {
	if job.Status.StartTime != nil {
		return job.Status.StartTime.Time
	}
	return job.CreationTimestamp.Time
}

func humanizeDuration(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
