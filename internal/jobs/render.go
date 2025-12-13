// render.go formats CronJob + Job data into tables.
package jobs

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RenderOptions controls output toggles.
type RenderOptions struct {
	ShowJobs bool
}

// Print renders CronJob and Job data.
func Print(summary *Summary, opts RenderOptions) {
	if summary == nil {
		fmt.Println("No data available.")
		return
	}
	printCronJobs(summary.CronJobs)
	if opts.ShowJobs {
		printJobs(summary.Jobs)
	}
}

func printCronJobs(cronJobs []CronJobSummary) {
	if len(cronJobs) == 0 {
		fmt.Println("No CronJobs matched the filter.")
		return
	}
	fmt.Println("CronJobs:")
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CRONJOB\tSCHEDULE\tACTIVE\tLAST SCHEDULE\tLAST SUCCESS\tSTATUS\tNOTE")
	for _, cron := range cronJobs {
		fmt.Fprintf(tw, "%s/%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
			cron.Namespace,
			cron.Name,
			cron.Schedule,
			cron.Active,
			formatTime(cron.LastScheduleTime),
			formatTime(cron.LastSuccessfulRun),
			formatStatus(cron.Status),
			cron.Note,
		)
	}
	_ = tw.Flush()
}

func printJobs(jobs []JobSummary) {
	if len(jobs) == 0 {
		fmt.Println("\nNo Jobs found.")
		return
	}
	fmt.Println("\nJobs (most recent first):")
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "JOB\tCRONJOB\tACTIVE/SUCCEEDED/FAILED\tSTART\tFINISH\tSTATUS")
	for _, job := range jobs {
		fmt.Fprintf(tw, "%s/%s\t%s\t%d/%d/%d\t%s\t%s\t%s %s\n",
			job.Namespace,
			job.Name,
			dashIfEmpty(job.CronJob),
			job.Active,
			job.Succeeded,
			job.Failed,
			formatTime(job.StartTime),
			formatTime(job.FinishTime),
			formatStatus(job.Status),
			job.Message,
		)
	}
	_ = tw.Flush()
}

func formatTime(ts *metav1.Time) string {
	if ts == nil {
		return "-"
	}
	return ts.Time.Format(time.RFC3339)
}

func formatStatus(status string) string {
	if color.NoColor {
		return status
	}
	switch status {
	case "FAILED", "FAILING":
		return color.New(color.FgHiRed).Sprint(status)
	case "STALE", "NO RUNS", "SUSPENDED":
		return color.New(color.FgYellow).Sprint(status)
	default:
		return status
	}
}

func dashIfEmpty(val string) string {
	if val == "" {
		return "-"
	}
	return val
}
