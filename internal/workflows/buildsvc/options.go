// File: internal/workflows/buildsvc/options.go
// Brief: Internal buildsvc package implementation for 'options'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"io"
	"os"

	"github.com/example/ktl/internal/tailer"
	"golang.org/x/term"
)

// Streams defines the IO handles a workflow should use.
type Streams struct {
	In         io.Reader
	Out        io.Writer
	Err        io.Writer
	Terminals  []any
	LogOnClose io.Closer
}

type BuildMode string

const (
	ModeAuto       BuildMode = "auto"
	ModeDockerfile BuildMode = "dockerfile"
	ModeCompose    BuildMode = "compose"
)

func (s *Streams) validate() error {
	if s == nil {
		return nil
	}
	return nil
}

func (s Streams) InReader() io.Reader {
	if s.In != nil {
		return s.In
	}
	return os.Stdin
}

func (s Streams) OutWriter() io.Writer {
	if s.Out != nil {
		return s.Out
	}
	return os.Stdout
}

func (s Streams) ErrWriter() io.Writer {
	if s.Err != nil {
		return s.Err
	}
	if s.Out != nil {
		return s.Out
	}
	return os.Stderr
}

func (s *Streams) SetOutErr(out, err io.Writer) {
	if s == nil {
		return
	}
	s.Out = out
	s.Err = err
}

func (s Streams) terminalCandidates() []any {
	if len(s.Terminals) > 0 {
		return s.Terminals
	}
	return []any{s.In, s.Out, s.Err}
}

func (s Streams) IsTerminal(w io.Writer) bool {
	type fdProvider interface {
		Fd() uintptr
	}
	if v, ok := w.(fdProvider); ok {
		return term.IsTerminal(int(v.Fd()))
	}
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

// Options contains everything needed to execute a ktl build workflow.
type Options struct {
	ContextDir         string
	Dockerfile         string
	Tags               []string
	Platforms          []string
	BuildArgs          []string
	Secrets            []string
	CacheFrom          []string
	CacheTo            []string
	Push               bool
	Load               bool
	NoCache            bool
	Builder            string
	CacheDir           string
	Interactive        bool
	InteractiveShell   string
	BuildMode          string
	ComposeFiles       []string
	ComposeProfiles    []string
	ComposeServices    []string
	ComposeProject     string
	AuthFile           string
	SandboxConfig      string
	SandboxBin         string
	SandboxBinds       []string
	SandboxWorkdir     string
	SandboxLogs        bool
	LogFile            string
	RemoveIntermediate bool
	Quiet              bool
	UIAddr             string
	WSListenAddr       string
	Streams            Streams
	Observers          []tailer.LogObserver
}
