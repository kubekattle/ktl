// session.go coordinates live capture sessions by wiring Kubernetes informers to log writers.
package capture

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/example/ktl/internal/config"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/tailer"
)

// Session captures log lines plus workload state into a replayable artifact.
type Session struct {
	client     *kube.Client
	log        logr.Logger
	cfg        *config.Options
	options    *Options
	graph      *graph
	sqlitePath string

	tempDir    string
	logsPath   string
	logsFile   *os.File
	logsWriter *bufio.Writer

	start time.Time
	end   time.Time

	mu           sync.Mutex
	observedPods map[string]struct{}
}

// NewSession wires the capture session using the provided kubernetes client and CLI options.
func NewSession(client *kube.Client, cfg *config.Options, opts *Options, logger logr.Logger) (*Session, error) {
	if client == nil {
		return nil, errors.New("kube client is required")
	}
	if cfg == nil {
		return nil, errors.New("tailer config is required")
	}
	if opts == nil {
		return nil, errors.New("capture options are required")
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	graph, err := newGraph(client.Clientset, cfg, logger)
	if err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "ktl-capture-")
	if err != nil {
		return nil, fmt.Errorf("create capture temp dir: %w", err)
	}
	logsPath := filepath.Join(tempDir, "logs.jsonl")
	logsFile, err := os.Create(logsPath)
	if err != nil {
		return nil, fmt.Errorf("create capture log file: %w", err)
	}
	session := &Session{
		client:       client,
		log:          logger.WithName("capture"),
		cfg:          cfg,
		options:      opts,
		graph:        graph,
		tempDir:      tempDir,
		logsPath:     logsPath,
		logsFile:     logsFile,
		logsWriter:   bufio.NewWriterSize(logsFile, 64*1024),
		observedPods: make(map[string]struct{}),
	}
	if opts.SQLite {
		session.sqlitePath = filepath.Join(tempDir, "logs.sqlite")
	}
	return session, nil
}

type captureStatus struct {
	GeneratedAt time.Time `json:"generatedAt"`
	Warnings    []string  `json:"warnings,omitempty"`
	Errors      []string  `json:"errors,omitempty"`
}

// Run executes the capture session and returns the path to the archived artifact.
func (s *Session) Run(ctx context.Context) (string, error) {
	rootCtx := ctx
	captureCtx, cancel := context.WithTimeout(ctx, s.options.Duration)
	defer cancel()

	status := captureStatus{GeneratedAt: time.Now().UTC()}
	s.start = time.Now()

	const graphSyncTimeout = 15 * time.Second
	if s.graph != nil && !s.graph.start(captureCtx, graphSyncTimeout) {
		status.Warnings = append(status.Warnings, fmt.Sprintf("capture graph informers did not sync within %s; log enrichment may be partial", graphSyncTimeout))
	}

	newTailerOpts := []tailer.Option{tailer.WithLogObserver(s), tailer.WithOutput(io.Discard)}
	if s.sqlitePath != "" {
		newTailerOpts = append(newTailerOpts, tailer.WithSQLiteSink(s.sqlitePath))
	}
	t, err := tailer.New(s.client.Clientset, s.cfg, s.log.WithName("tailer"), newTailerOpts...)
	if err != nil {
		status.Errors = append(status.Errors, fmt.Sprintf("create tailer: %v", err))
		s.end = time.Now()
		artifact, archiveErr := s.finalize(rootCtx, &status)
		if archiveErr != nil {
			return "", archiveErr
		}
		return artifact, err
	}
	runErr := t.Run(captureCtx)
	captureDone := captureCtx.Err()
	if runErr != nil && captureDone != nil && (errors.Is(captureDone, context.Canceled) || errors.Is(captureDone, context.DeadlineExceeded)) {
		status.Warnings = append(status.Warnings, fmt.Sprintf("tailer stopped early: %v", runErr))
		runErr = nil
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
		status.Errors = append(status.Errors, fmt.Sprintf("run tailer: %v", runErr))
	}
	s.end = time.Now()
	artifact, archiveErr := s.finalize(rootCtx, &status)
	if archiveErr != nil {
		return "", archiveErr
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
		return artifact, runErr
	}
	return artifact, nil
}

func (s *Session) finalize(ctx context.Context, status *captureStatus) (string, error) {
	if err := s.flushLogs(); err != nil {
		status.Errors = append(status.Errors, fmt.Sprintf("flush logs: %v", err))
	}
	if err := s.writeMetadata(); err != nil {
		status.Errors = append(status.Errors, fmt.Sprintf("write metadata: %v", err))
	}
	finalizeCtx := ctx
	if finalizeCtx == nil || finalizeCtx.Err() != nil {
		// Capture finalization should be best-effort even when the user interrupts
		// the live session (Ctrl+C). Use a fresh context so artifact enrichment
		// (events/manifests) can still complete.
		finalizeCtx = context.Background()
	}
	var cancel context.CancelFunc
	finalizeCtx, cancel = context.WithTimeout(finalizeCtx, 30*time.Second)
	defer cancel()
	s.enrichArtifacts(finalizeCtx)
	if status != nil && (len(status.Warnings) > 0 || len(status.Errors) > 0) {
		if err := s.writeStatus(status); err != nil {
			s.log.V(1).Info("failed to write capture status", "error", err)
		}
	}
	artifact, err := s.archive()
	if err != nil {
		return "", err
	}
	if err := os.RemoveAll(s.tempDir); err != nil {
		s.log.V(1).Info("failed to remove capture temp dir", "path", s.tempDir, "error", err)
	}
	return artifact, nil
}

func (s *Session) writeStatus(status *captureStatus) error {
	if status == nil {
		return nil
	}
	path := filepath.Join(s.tempDir, "capture-status.json")
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	return enc.Encode(status)
}

func (s *Session) flushLogs() error {
	if s.logsWriter != nil {
		if err := s.logsWriter.Flush(); err != nil {
			return err
		}
	}
	if s.logsFile != nil {
		if err := s.logsFile.Close(); err != nil {
			return err
		}
	}
	return nil
}

// ObserveLog implements tailer.LogObserver.
func (s *Session) ObserveLog(record tailer.LogRecord) {
	entry := s.buildEntry(record)
	data, err := json.Marshal(entry)
	if err != nil {
		s.log.Error(err, "marshal capture entry", "namespace", record.Namespace, "pod", record.Pod)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.logsWriter.Write(data); err != nil {
		s.log.Error(err, "write capture entry")
		return
	}
	if err := s.logsWriter.WriteByte('\n'); err != nil {
		s.log.Error(err, "write capture newline")
	}
	s.observedPods[fmt.Sprintf("%s/%s", record.Namespace, record.Pod)] = struct{}{}
}

func (s *Session) buildEntry(record tailer.LogRecord) Entry {
	entry := Entry{
		Timestamp:          record.Timestamp.UTC(),
		FormattedTimestamp: record.FormattedTimestamp,
		Namespace:          record.Namespace,
		Pod:                record.Pod,
		Container:          record.Container,
		Raw:                record.Raw,
		Rendered:           record.Rendered,
	}
	if pod, err := s.graph.getPod(record.Namespace, record.Pod); err == nil {
		entry.PodState = summarizePod(pod)
		if pod.Spec.NodeName != "" {
			if node, err := s.graph.getNode(pod.Spec.NodeName); err == nil {
				entry.NodeState = summarizeNode(node)
			}
		}
		entry.Owners = s.graph.buildOwnerChain(pod)
	}
	return entry
}

func (s *Session) observedPodsByNamespace() map[string][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]string)
	for key := range s.observedPods {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		ns, pod := parts[0], parts[1]
		out[ns] = append(out[ns], pod)
	}
	for ns := range out {
		sort.Strings(out[ns])
	}
	return out
}

func (s *Session) writeMetadata() error {
	meta := Metadata{
		SessionName:      strings.TrimSpace(s.options.SessionName),
		StartedAt:        s.start.UTC(),
		EndedAt:          s.end.UTC(),
		DurationSeconds:  s.end.Sub(s.start).Seconds(),
		Namespaces:       s.resolvedNamespaces(),
		AllNamespaces:    s.cfg.AllNamespaces,
		PodQuery:         s.cfg.PodQuery,
		TailLines:        s.cfg.TailLines,
		Since:            s.cfg.Since.String(),
		Context:          s.cfg.Context,
		Kubeconfig:       s.cfg.KubeConfigPath,
		PodCount:         len(s.observedPods),
		EventsEnabled:    s.cfg.Events,
		Follow:           s.cfg.Follow,
		SQLitePath:       s.sqliteArchivePath(),
		ManifestsEnabled: s.options.AttachManifests,
	}
	metaPath := filepath.Join(s.tempDir, "metadata.json")
	file, err := os.Create(metaPath)
	if err != nil {
		return fmt.Errorf("create metadata file: %w", err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(meta)
}

func (s *Session) archive() (string, error) {
	output := s.options.ResolveOutputPath(s.start)
	if !filepath.IsAbs(output) {
		abs, err := filepath.Abs(output)
		if err == nil {
			output = abs
		}
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return "", fmt.Errorf("ensure capture output dir: %w", err)
	}
	archiveFile, err := os.Create(output)
	if err != nil {
		return "", fmt.Errorf("create capture archive: %w", err)
	}
	defer archiveFile.Close()
	gz := gzip.NewWriter(archiveFile)
	defer gz.Close()
	tarWriter := tar.NewWriter(gz)
	defer tarWriter.Close()
	walkErr := filepath.WalkDir(s.tempDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.tempDir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header := &tar.Header{
			Name:    rel,
			Mode:    int64(info.Mode().Perm()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tarWriter, file); err != nil {
			file.Close()
			return err
		}
		return file.Close()
	})
	if walkErr != nil {
		return "", fmt.Errorf("archive capture contents: %w", walkErr)
	}
	return output, nil
}

func (s *Session) resolvedNamespaces() []string {
	if s.cfg.AllNamespaces {
		return []string{metav1.NamespaceAll}
	}
	if len(s.cfg.Namespaces) == 0 {
		return []string{"default"}
	}
	out := make([]string, len(s.cfg.Namespaces))
	copy(out, s.cfg.Namespaces)
	sort.Strings(out)
	return out
}

func (s *Session) sqliteArchivePath() string {
	if s.sqlitePath == "" {
		return ""
	}
	return "logs.sqlite"
}

func summarizePod(pod *corev1.Pod) *PodState {
	if pod == nil {
		return nil
	}
	state := &PodState{
		Phase:    pod.Status.Phase,
		NodeName: pod.Spec.NodeName,
		HostIP:   pod.Status.HostIP,
		PodIP:    pod.Status.PodIP,
	}
	if len(pod.Status.Conditions) > 0 {
		state.Conditions = make(map[string]corev1.ConditionStatus, len(pod.Status.Conditions))
		for _, cond := range pod.Status.Conditions {
			state.Conditions[string(cond.Type)] = cond.Status
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		state.Containers = append(state.Containers, ContainerState{
			Name:         cs.Name,
			Ready:        cs.Ready,
			RestartCount: cs.RestartCount,
			State:        describeContainerState(cs.State),
			LastState:    describeContainerState(cs.LastTerminationState),
		})
	}
	return state
}

func summarizeNode(node *corev1.Node) *NodeState {
	if node == nil {
		return nil
	}
	state := &NodeState{
		Name: node.Name,
	}
	if len(node.Status.Conditions) > 0 {
		state.Conditions = make(map[string]corev1.ConditionStatus, len(node.Status.Conditions))
		for _, cond := range node.Status.Conditions {
			state.Conditions[string(cond.Type)] = cond.Status
		}
	}
	if len(node.Status.Allocatable) > 0 {
		state.Allocatable = make(map[corev1.ResourceName]string, len(node.Status.Allocatable))
		for name, qty := range node.Status.Allocatable {
			state.Allocatable[name] = qty.String()
		}
	}
	if len(node.Status.Capacity) > 0 {
		state.Capacity = make(map[corev1.ResourceName]string, len(node.Status.Capacity))
		for name, qty := range node.Status.Capacity {
			state.Capacity[name] = qty.String()
		}
	}
	return state
}

func describeContainerState(state corev1.ContainerState) string {
	switch {
	case state.Waiting != nil:
		return "waiting:" + state.Waiting.Reason
	case state.Running != nil:
		return "running"
	case state.Terminated != nil:
		return "terminated:" + state.Terminated.Reason
	default:
		return ""
	}
}
