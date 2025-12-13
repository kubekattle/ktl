// runner.go powers 'ktl analyze traffic' by orchestrating tcpdump helpers and streaming pcap data.
package sniff

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/kube"
	"golang.org/x/sync/errgroup"
)

// Target describes a pod/container where tcpdump should run.
type Target struct {
	Namespace string
	Pod       string
	Container string
	Label     string
	Filter    string
}

// StreamOptions control how tcpdump runs inside the target pods.
type StreamOptions struct {
	Interface      string
	SnapLen        int
	PacketCount    int
	AbsoluteTime   bool
	GlobalFilter   string
	Targets        []Target
	Stdout         io.Writer
	Stderr         io.Writer
	SuppressErrors bool
	Observer       Observer
}

// Record captures a single tcpdump line emitted by analyze traffic.
type Record struct {
	Timestamp time.Time
	Target    Target
	Stream    string
	Line      string
}

// Observer receives callbacks whenever a tcpdump line is emitted.
type Observer interface {
	ObserveTraffic(Record)
}

// Stream executes tcpdump in the provided targets and streams output back to the caller.
func Stream(ctx context.Context, client *kube.Client, opts StreamOptions) error {
	if len(opts.Targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}
	if opts.Stdout == nil {
		return fmt.Errorf("stdout writer must be provided")
	}
	if opts.Stderr == nil {
		opts.Stderr = opts.Stdout
	}

	combinedOut := &safeWriter{w: opts.Stdout}
	combinedErr := &safeWriter{w: opts.Stderr}

	eg, ctx := errgroup.WithContext(ctx)
	for _, target := range opts.Targets {
		target := target
		eg.Go(func() error {
			return streamTarget(ctx, client, target, opts, combinedOut, combinedErr)
		})
	}
	return eg.Wait()
}

func streamTarget(ctx context.Context, client *kube.Client, target Target, opts StreamOptions, out *safeWriter, errOut *safeWriter) error {
	cmd := buildCommand(opts.Interface, opts.SnapLen, opts.PacketCount, opts.AbsoluteTime, combineFilters(opts.GlobalFilter, target.Filter))

	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	prefix := fmt.Sprintf("[%s]", target.Label)

	pg, ctx := errgroup.WithContext(ctx)
	pg.Go(func() error {
		return copyStream(ctx, stdoutReader, prefix+" ", out, func(line string) {
			notifyObserver(opts.Observer, target, "stdout", line)
		})
	})
	pg.Go(func() error {
		return copyStream(ctx, stderrReader, prefix+" [stderr] ", errOut, func(line string) {
			notifyObserver(opts.Observer, target, "stderr", line)
		})
	})

	execErr := client.Exec(ctx, target.Namespace, target.Pod, target.Container, cmd, nil, stdoutWriter, stderrWriter)
	stdoutWriter.Close()
	stderrWriter.Close()

	if err := pg.Wait(); err != nil && !opts.SuppressErrors {
		return err
	}
	if execErr != nil {
		return fmt.Errorf("sniff on %s: %w", target.Label, execErr)
	}
	return nil
}

func combineFilters(global, local string) string {
	global = strings.TrimSpace(global)
	local = strings.TrimSpace(local)
	switch {
	case global == "" && local == "":
		return ""
	case global == "":
		return local
	case local == "":
		return global
	default:
		return fmt.Sprintf("(%s) and (%s)", global, local)
	}
}

func buildCommand(iface string, snapLen int, packetCount int, absoluteTime bool, filter string) []string {
	args := []string{"tcpdump", "-nn"}
	if absoluteTime {
		args = append(args, "-tttt")
	}
	if iface != "" {
		args = append(args, "-i", iface)
	}
	if snapLen <= 0 {
		snapLen = 0
	}
	args = append(args, "-s", fmt.Sprintf("%d", snapLen))
	if packetCount > 0 {
		args = append(args, "-c", fmt.Sprintf("%d", packetCount))
	}
	if filter != "" {
		args = append(args, filter)
	}
	return args
}

type safeWriter struct {
	w io.Writer
	m sync.Mutex
}

func (s *safeWriter) WriteLine(line string) {
	s.m.Lock()
	defer s.m.Unlock()
	fmt.Fprintln(s.w, line)
}

func copyStream(ctx context.Context, r io.Reader, prefix string, writer *safeWriter, hook func(string)) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if shouldSuppressLine(line) {
			continue
		}
		if hook != nil {
			hook(line)
		}
		writer.WriteLine(prefix + line)
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func shouldSuppressLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}
	switch {
	case strings.HasPrefix(line, "tcpdump: verbose output suppressed"):
		return true
	case strings.Contains(line, "pcap_loop:") && strings.Contains(line, "No such device"):
		return true
	default:
		return false
	}
}

func notifyObserver(observer Observer, target Target, stream string, line string) {
	if observer == nil {
		return
	}
	record := Record{
		Timestamp: time.Now(),
		Target:    target,
		Stream:    stream,
		Line:      line,
	}
	observer.ObserveTraffic(record)
}
