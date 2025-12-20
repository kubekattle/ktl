// File: cmd/ktl/completion_helpers.go
// Brief: CLI command wiring and implementation for 'completion helpers'.

// completion_helpers.go centralizes Cobra flag completion helpers (namespaces today) so every command can reuse them consistently.
package main

import (
	"context"
	"strings"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func registerNamespaceCompletion(cmd *cobra.Command, flagName string, kubeconfig *string, kubeContext *string) {
	flag := cmd.Flags().Lookup(flagName)
	if flag == nil {
		flag = cmd.InheritedFlags().Lookup(flagName)
	}
	if flag == nil {
		return
	}
	cmd.RegisterFlagCompletionFunc(flagName, func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		client, err := kube.New(ctx, *kubeconfig, *kubeContext)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		list, err := client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var completions []string
		for _, ns := range list.Items {
			if toComplete == "" || strings.HasPrefix(ns.Name, toComplete) {
				completions = append(completions, ns.Name)
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	})
}
