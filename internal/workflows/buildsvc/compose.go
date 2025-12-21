// File: internal/workflows/buildsvc/compose.go
// Brief: Internal buildsvc package implementation for 'compose'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/ktl/internal/dockerconfig"
	"github.com/example/ktl/pkg/buildkit"
	appcompose "github.com/example/ktl/pkg/compose"
)

func (s *service) runComposeBuild(ctx context.Context, composeFiles []string, opts Options, observers []buildkit.ProgressObserver, diagnosticObservers []buildkit.BuildDiagnosticObserver, quietProgress bool, stream *buildProgressBroadcaster, streams Streams) error {
	argMap, err := parseKeyValueArgs(opts.BuildArgs)
	if err != nil {
		return err
	}

	dockerCfg, err := dockerconfig.LoadConfigFile(opts.AuthFile, streams.ErrWriter())
	if err != nil {
		return err
	}
	progressOut := resolveConsoleFile(streams.ErrWriter())

	composeOpts := appcompose.ComposeBuildOptions{
		Files:                composeFiles,
		ProjectName:          opts.ComposeProject,
		Services:             opts.ComposeServices,
		Profiles:             opts.ComposeProfiles,
		BuilderAddr:          opts.Builder,
		AllowBuilderFallback: opts.Builder == "",
		CacheDir:             opts.CacheDir,
		Hermetic:             opts.Hermetic,
		AllowUnpinnedBases:   opts.AllowUnpinnedBases,
		Push:                 opts.Push,
		Load:                 opts.Load,
		NoCache:              opts.NoCache,
		AttestProvenance:     opts.AttestProvenance,
		AttestSBOM:           opts.AttestSBOM,
		Platforms:            buildkit.NormalizePlatforms(expandPlatforms(opts.Platforms)),
		BuildArgs:            argMap,
		ProgressOutput:       progressOut,
		DockerConfig:         dockerCfg,
		Parallelism:          opts.ComposeParallelism,
		ProgressObservers:    observers,
		DiagnosticObservers:  diagnosticObservers,
	}
	if quietProgress {
		composeOpts.ProgressMode = "quiet"
	}
	if stream != nil {
		composeOpts.HeatmapListener = &heatmapStreamBridge{stream: stream}
	}

	results, err := s.composeRunner.BuildCompose(ctx, composeOpts)
	if err != nil {
		return err
	}

	if dir := strings.TrimSpace(opts.AttestationDir); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create --attest-dir: %w", err)
		}
		for _, svc := range results {
			if svc.Result == nil || strings.TrimSpace(svc.Result.OCIOutputPath) == "" {
				continue
			}
			dest := filepath.Join(dir, svc.Service)
			_, attErr := buildkit.WriteAttestationsFromOCI(svc.Result.OCIOutputPath, dest)
			if attErr != nil {
				return attErr
			}
		}
		if stream != nil {
			stream.emitInfo(fmt.Sprintf("Wrote compose attestations to %s (one subdir per service)", dir))
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].Service) < strings.ToLower(results[j].Service)
	})
	for _, svc := range results {
		fmt.Fprintf(streams.OutWriter(), "%s: %s\n", svc.Service, strings.Join(svc.Tags, ", "))
	}
	return nil
}
