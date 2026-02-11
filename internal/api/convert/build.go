// File: internal/api/convert/build.go
// Brief: Internal convert package implementation for 'build'.

// Package convert provides convert helpers.

package convert

import (
	"io"
	"strings"

	"github.com/example/ktl/internal/workflows/buildsvc"
	apiv1 "github.com/example/ktl/pkg/api/ktl/api/v1"
)

// BuildOptionsToProto converts build options into a protobuf payload.
func BuildOptionsToProto(opts buildsvc.Options) *apiv1.BuildOptions {
	return &apiv1.BuildOptions{
		ContextDir:         opts.ContextDir,
		Dockerfile:         opts.Dockerfile,
		Tags:               append([]string(nil), opts.Tags...),
		Platforms:          append([]string(nil), opts.Platforms...),
		BuildArgs:          append([]string(nil), opts.BuildArgs...),
		Secrets:            append([]string(nil), opts.Secrets...),
		CacheFrom:          append([]string(nil), opts.CacheFrom...),
		CacheTo:            append([]string(nil), opts.CacheTo...),
		Push:               opts.Push,
		Load:               opts.Load,
		NoCache:            opts.NoCache,
		Builder:            opts.Builder,
		CacheDir:           opts.CacheDir,
		Interactive:        opts.Interactive,
		InteractiveShell:   opts.InteractiveShell,
		BuildMode:          opts.BuildMode,
		ComposeFiles:       append([]string(nil), opts.ComposeFiles...),
		ComposeProfiles:    append([]string(nil), opts.ComposeProfiles...),
		ComposeServices:    append([]string(nil), opts.ComposeServices...),
		ComposeProject:     opts.ComposeProject,
		AuthFile:           opts.AuthFile,
		SandboxConfig:      opts.SandboxConfig,
		SandboxBin:         opts.SandboxBin,
		SandboxBinds:       append([]string(nil), opts.SandboxBinds...),
		SandboxWorkdir:     opts.SandboxWorkdir,
		SandboxLogs:        opts.SandboxLogs,
		LogFile:            opts.LogFile,
		RemoveIntermediate: opts.RemoveIntermediate,
		Quiet:              opts.Quiet,
		DockerContext:      opts.DockerContext,
	}
}

// BuildOptionsFromProto hydrates buildsvc.Options from protobuf.
func BuildOptionsFromProto(pb *apiv1.BuildOptions) buildsvc.Options {
	if pb == nil {
		pb = &apiv1.BuildOptions{}
	}
	return buildsvc.Options{
		ContextDir:         pb.GetContextDir(),
		Dockerfile:         pb.GetDockerfile(),
		Tags:               append([]string(nil), pb.GetTags()...),
		Platforms:          append([]string(nil), pb.GetPlatforms()...),
		BuildArgs:          append([]string(nil), pb.GetBuildArgs()...),
		Secrets:            append([]string(nil), pb.GetSecrets()...),
		CacheFrom:          append([]string(nil), pb.GetCacheFrom()...),
		CacheTo:            append([]string(nil), pb.GetCacheTo()...),
		Push:               pb.GetPush(),
		Load:               pb.GetLoad(),
		NoCache:            pb.GetNoCache(),
		Builder:            pb.GetBuilder(),
		CacheDir:           pb.GetCacheDir(),
		Interactive:        pb.GetInteractive(),
		InteractiveShell:   pb.GetInteractiveShell(),
		BuildMode:          pb.GetBuildMode(),
		ComposeFiles:       append([]string(nil), pb.GetComposeFiles()...),
		ComposeProfiles:    append([]string(nil), pb.GetComposeProfiles()...),
		ComposeServices:    append([]string(nil), pb.GetComposeServices()...),
		ComposeProject:     pb.GetComposeProject(),
		AuthFile:           pb.GetAuthFile(),
		SandboxConfig:      pb.GetSandboxConfig(),
		SandboxBin:         pb.GetSandboxBin(),
		SandboxBinds:       append([]string(nil), pb.GetSandboxBinds()...),
		SandboxWorkdir:     pb.GetSandboxWorkdir(),
		SandboxLogs:        pb.GetSandboxLogs(),
		LogFile:            pb.GetLogFile(),
		RemoveIntermediate: pb.GetRemoveIntermediate(),
		Quiet:              pb.GetQuiet(),
		DockerContext:      pb.GetDockerContext(),
		Streams: buildsvc.Streams{
			In:  strings.NewReader(""),
			Out: io.Discard,
			Err: io.Discard,
		},
	}
}

// BuildResultToProto converts a build result to protobuf.
func BuildResultToProto(res *buildsvc.Result, err error) *apiv1.BuildResult {
	if res == nil && err == nil {
		return nil
	}
	br := &apiv1.BuildResult{}
	if res != nil {
		br.Tags = append([]string(nil), res.Tags...)
		br.Digest = res.Digest
		br.OciOutputDir = res.OCIOutputDir
	}
	if err != nil {
		br.Error = err.Error()
	}
	return br
}
