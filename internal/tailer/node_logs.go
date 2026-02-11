// File: internal/tailer/node_logs.go
// Brief: Internal tailer package implementation for 'node logs'.

// node_logs.go extends the tailer with kubelet-proxy log streaming so ktl can
// enrich 'ktl logs' sessions with node/system files alongside pod output.
package tailer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type nodeLogKey struct {
	node string
	file string
}

// nodeLogManager streams node/system logs over the kubelet proxy.
type nodeLogManager struct {
	tailer      *Tailer
	logger      logr.Logger
	mu          sync.Mutex
	active      map[nodeLogKey]context.CancelFunc
	rbacWarning sync.Once
}

type permanentStreamError struct {
	err     error
	userMsg string
}

func (e permanentStreamError) Error() string { return e.err.Error() }

func newNodeLogManager(t *Tailer) *nodeLogManager {
	return &nodeLogManager{
		tailer: t,
		logger: t.log.WithName("nodeLogs"),
		active: make(map[nodeLogKey]context.CancelFunc),
	}
}

func (m *nodeLogManager) ensureForPod(pod *corev1.Pod) {
	if m == nil || pod == nil {
		return
	}
	m.ensureNode(strings.TrimSpace(pod.Spec.NodeName))
}

func (m *nodeLogManager) ensureForPods(pods []*corev1.Pod) {
	if m == nil {
		return
	}
	for _, pod := range pods {
		m.ensureForPod(pod)
	}
}

func (m *nodeLogManager) ensureAllNodes(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	list, err := m.tailer.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list nodes for node logs: %w", err)
	}
	for _, node := range list.Items {
		m.ensureNode(node.Name)
	}
	return nil
}

func (m *nodeLogManager) ensureNode(name string) {
	if m == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if len(m.tailer.opts.NodeLogFiles) == 0 {
		return
	}
	for _, file := range m.tailer.opts.NodeLogFiles {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		key := nodeLogKey{node: name, file: file}
		m.mu.Lock()
		if _, exists := m.active[key]; exists {
			m.mu.Unlock()
			continue
		}
		parentCtx := m.tailer.ctx
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithCancel(parentCtx)
		m.active[key] = cancel
		m.mu.Unlock()
		go m.runStream(ctx, key)
	}
}

func (m *nodeLogManager) runStream(ctx context.Context, key nodeLogKey) {
	logger := m.logger.WithValues("node", key.node, "file", key.file)
	defer func() {
		m.mu.Lock()
		delete(m.active, key)
		m.mu.Unlock()
	}()
	backoff := time.Second
	for {
		err := m.streamOnce(ctx, key)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			var perr permanentStreamError
			if errors.As(err, &perr) {
				if perr.userMsg != "" {
					logger.Info(perr.userMsg)
				} else {
					logger.Error(perr.err, "node log stream halted permanently")
				}
				return
			}
			if err != context.Canceled && err != context.DeadlineExceeded {
				logger.Error(err, "node log stream interrupted", "backoff", backoff.String())
			}
		}
		if !m.tailer.opts.Follow {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (m *nodeLogManager) warnRBAC(err error) {
	if m == nil {
		return
	}
	m.rbacWarning.Do(func() {
		m.logger.Info("node log streaming requires 'nodes/proxy' RBAC; cluster denied access", "error", err)
	})
}

func (m *nodeLogManager) streamOnce(ctx context.Context, key nodeLogKey) error {
	req := m.tailer.client.CoreV1().RESTClient().
		Get().
		Resource("nodes").
		Name(key.node).
		SubResource("proxy").
		Suffix("logs", key.file)
	if m.tailer.opts.Follow {
		req.Param("follow", "1")
	}
	if m.tailer.opts.TailLines >= 0 {
		req.Param("tailLines", fmt.Sprintf("%d", m.tailer.opts.TailLines))
	}
	if m.tailer.opts.Since > 0 {
		req.Param("sinceSeconds", fmt.Sprintf("%d", int64(m.tailer.opts.Since.Seconds())))
	}
	stream, err := req.Stream(ctx)
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsNotFound(err) {
			m.warnRBAC(err)
			return permanentStreamError{err: err, userMsg: "node log streaming disabled: cluster denied nodes/proxy access"}
		}
		if serr, ok := err.(*apierrors.StatusError); ok {
			code := int(serr.ErrStatus.Code)
			if code == 0 {
				code = int(serr.Status().Code)
			}
			if code >= 400 && code < 500 && code != http.StatusTooManyRequests {
				m.warnRBAC(err)
				return permanentStreamError{err: err, userMsg: "node log streaming disabled: cluster denied nodes/proxy access"}
			}
		}
		return err
	}
	defer stream.Close()
	scanner := bufio.NewScanner(stream)
	buf := m.tailer.getScannerBuffer()
	defer m.tailer.putScannerBuffer(buf)
	scanner.Buffer(buf, logScannerMax)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if m.tailer.opts.ExcludeLineRegex != nil && m.tailer.opts.ExcludeLineRegex.MatchString(line) {
			continue
		}
		m.tailer.outputLine(sourceNode, "node", key.node, key.file, line)
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}
