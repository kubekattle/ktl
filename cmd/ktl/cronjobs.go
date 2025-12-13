// cronjobs.go registers 'ktl diag cronjobs', pulling CronJob/Job summaries to highlight stuck schedules and failed job history.
package main

import (
	"github.com/example/ktl/internal/jobs"
	"github.com/example/ktl/internal/kube"
	"github.com/spf13/cobra"
)

func newCronJobsCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespaces []string
	var allNamespaces bool
	var showJobs bool
	var jobLimit int

	cmd := &cobra.Command{
		Use:   "cronjobs",
		Short: "Inspect CronJob and Job health",
		Long: `Lists CronJobs with schedule, active counts, last runs, and flags silently dead schedules,
plus (optionally) the most recent Jobs so you can see which workloads failed or got preempted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			summary, err := jobs.Collect(ctx, kubeClient.Clientset, jobs.Options{
				Namespaces:       namespaces,
				AllNamespaces:    allNamespaces,
				DefaultNamespace: kubeClient.Namespace,
				JobLimit:         jobLimit,
			})
			if err != nil {
				return err
			}
			jobs.Print(summary, jobs.RenderOptions{ShowJobs: showJobs})
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&namespaces, "namespace", "n", nil, "Namespaces to inspect (defaults to active kubeconfig namespace)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Inspect CronJobs in every namespace")
	cmd.Flags().BoolVarP(&showJobs, "show-jobs", "j", false, "Include a Jobs table sorted by most recent start time")
	cmd.Flags().IntVar(&jobLimit, "job-limit", 20, "Maximum number of jobs to display when --show-jobs is set (default 20)")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "CronJob Flags")

	return cmd
}
