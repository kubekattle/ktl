// compose.go declares the 'ktl compose' command group, offering BuildKit-backed compose build/push helpers for multi-service projects.
package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/docker/cli/cli/config"
	"github.com/example/ktl/pkg/buildkit"
	appcompose "github.com/example/ktl/pkg/compose"
	"github.com/example/ktl/pkg/registry"
	"github.com/spf13/cobra"
)

type composeFilesFlag []string

func newComposeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Compose-aware build and push commands",
	}
	cmd.AddCommand(newComposeBuildCommand(), newComposePushCommand())
	decorateCommandHelp(cmd, "Compose Flags")
	return cmd
}

func newComposeBuildCommand() *cobra.Command {
	var (
		files       []string
		projectName string
		profiles    []string
		services    []string
		push        bool
		load        bool
		noCache     bool
		platforms   []string
		buildArgs   []string
		builder     string
		cacheDir    = buildkit.DefaultCacheDir()
	)

	cmd := &cobra.Command{
		Use:   "build [SERVICES...]",
		Short: "Build services defined in docker-compose files",
		RunE: func(cmd *cobra.Command, args []string) error {
			runServices := services
			if len(args) > 0 {
				runServices = args
			}
			runBuilder := builder
			if !cmd.Flags().Changed("builder") {
				runBuilder = ""
			}
			return runComposeBuild(cmd, files, projectName, profiles, runServices, buildArgs, platforms, runBuilder, cacheDir, push, load, noCache)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "Specify an additional compose file (repeatable)")
	cmd.Flags().StringArrayVar(&profiles, "profile", nil, "Enable an optional profile")
	cmd.Flags().StringArrayVar(&buildArgs, "build-arg", nil, "Inject a build argument (KEY=VALUE)")
	cmd.Flags().StringSliceVar(&platforms, "platform", nil, "Target platforms for builds")
	cmd.Flags().BoolVar(&push, "push", false, "Push images after building")
	cmd.Flags().BoolVar(&load, "load", false, "Load built images into the local container runtime (docker build --load)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Disable cache for compose builds")
	cmd.Flags().StringVar(&projectName, "project-name", "", "Override the compose project name")
	cmd.Flags().StringVar(&builder, "builder", "", "BuildKit address (override with KTL_BUILDKIT_HOST)")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", cacheDir, "Cache directory for compose builds")

	decorateCommandHelp(cmd, "Compose Build Flags")
	return cmd
}

func runComposeBuild(cmd *cobra.Command, files []string, projectName string, profiles []string, services []string, buildArgs []string, platforms []string, builder string, cacheDir string, push bool, load bool, noCache bool) error {
	ctx := cmd.Context()
	resolvedFiles, err := resolveComposeFiles(files)
	if err != nil {
		return err
	}

	argMap, err := parseKeyValueArgs(buildArgs)
	if err != nil {
		return err
	}

	dockerCfg := config.LoadDefaultConfigFile(cmd.ErrOrStderr())
	progressOut := resolveConsoleFile(cmd.ErrOrStderr())

	opts := appcompose.ComposeBuildOptions{
		Files:                resolvedFiles,
		ProjectName:          projectName,
		Services:             services,
		Profiles:             profiles,
		BuilderAddr:          builder,
		AllowBuilderFallback: builder == "",
		CacheDir:             cacheDir,
		Push:                 push,
		Load:                 load,
		NoCache:              noCache,
		Platforms:            buildkit.NormalizePlatforms(expandPlatforms(platforms)),
		BuildArgs:            argMap,
		ProgressOutput:       progressOut,
		DockerConfig:         dockerCfg,
	}

	results, err := composeRunner.BuildCompose(ctx, opts)
	if err != nil {
		return err
	}

	for _, svc := range results {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", svc.Service, strings.Join(svc.Tags, ", "))
	}
	return nil
}

func newComposePushCommand() *cobra.Command {
	var (
		files       []string
		projectName string
		profiles    []string
	)

	cmd := &cobra.Command{
		Use:   "push [SERVICES...]",
		Short: "Push compose service images recorded by ktl build",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runComposePush(cmd, files, projectName, profiles, args)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "Specify an additional compose file (repeatable)")
	cmd.Flags().StringArrayVar(&profiles, "profile", nil, "Enable an optional profile")
	cmd.Flags().StringVar(&projectName, "project-name", "", "Override the compose project name")

	decorateCommandHelp(cmd, "Compose Push Flags")
	return cmd
}

func runComposePush(cmd *cobra.Command, files []string, projectName string, profiles []string, services []string) error {
	resolvedFiles, err := resolveComposeFiles(files)
	if err != nil {
		return err
	}

	project, err := appcompose.LoadComposeProject(resolvedFiles, projectName, profiles)
	if err != nil {
		return err
	}

	tasks, skipped, err := appcompose.CollectBuildableServices(project, services)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		if len(skipped) > 0 {
			return fmt.Errorf("no buildable services found; skipped: %s", strings.Join(skipped, ", "))
		}
		return errors.New("no buildable services found")
	}

	regOpts := registry.PushOptions{Output: cmd.ErrOrStderr()}
	for name, svc := range tasks {
		tags := appcompose.ServiceTags(project, name, svc)
		for _, tag := range tags {
			fmt.Fprintf(cmd.ErrOrStderr(), "Pushing %s (%s)\n", name, tag)
			if err := registryClient.PushReference(cmd.Context(), tag, regOpts); err != nil {
				return err
			}
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Pushed %d service(s)\n", len(tasks))
	return nil
}
