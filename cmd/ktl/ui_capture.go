package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/capture"
	"github.com/example/ktl/internal/caststream"
	"github.com/example/ktl/internal/config"
	"github.com/example/ktl/internal/kube"
	"github.com/go-logr/logr"
)

type uiCaptureController struct {
	baseCtx    context.Context
	kubeClient *kube.Client
	logOpts    *config.Options
	capOpts    *capture.Options
	logger     logr.Logger

	mu       sync.Mutex
	running  bool
	id       string
	artifact string
	started  time.Time
	cancel   context.CancelFunc
	done     chan struct{}
	lastErr  error
}

func newUICaptureController(ctx context.Context, kubeClient *kube.Client, logOpts *config.Options, capOpts *capture.Options, logger logr.Logger) *uiCaptureController {
	if ctx == nil {
		ctx = context.Background()
	}
	return &uiCaptureController{baseCtx: ctx, kubeClient: kubeClient, logOpts: logOpts, capOpts: capOpts, logger: logger}
}

func (c *uiCaptureController) Status(ctx context.Context) (caststream.CaptureStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return caststream.CaptureStatus{
		Running:   c.running,
		ID:        c.id,
		StartedAt: fmtTimeRFC3339(c.started),
		Artifact:  c.artifact,
		LastError: fmtErr(c.lastErr),
	}, nil
}

func (c *uiCaptureController) Start(ctx context.Context) (caststream.CaptureStatus, error) {
	c.mu.Lock()
	if c.running {
		st := caststream.CaptureStatus{
			Running:   true,
			ID:        c.id,
			StartedAt: fmtTimeRFC3339(c.started),
			Artifact:  c.artifact,
			LastError: fmtErr(c.lastErr),
		}
		c.mu.Unlock()
		return st, nil
	}
	if c.kubeClient == nil || c.kubeClient.Clientset == nil {
		c.mu.Unlock()
		return caststream.CaptureStatus{}, fmt.Errorf("kube client unavailable")
	}
	if c.logOpts == nil {
		c.mu.Unlock()
		return caststream.CaptureStatus{}, fmt.Errorf("log options unavailable")
	}
	if c.capOpts == nil {
		c.mu.Unlock()
		return caststream.CaptureStatus{}, fmt.Errorf("capture options unavailable")
	}
	started := time.Now().UTC()
	id := fmt.Sprintf("ui-%s", started.Format("20060102-150405"))

	optsCopy := *c.logOpts
	capOptsCopy := *c.capOpts
	out := strings.TrimSpace(capOptsCopy.OutputPath)
	if out == "" {
		out = filepath.Join("dist", fmt.Sprintf("ktl-ui-capture-%s.tar.gz", started.Format("20060102-150405")))
	}
	capOptsCopy.OutputPath = out
	capOptsCopy.SessionName = fmt.Sprintf("UI capture (%s)", strings.TrimSpace(optsCopy.PodQuery))

	_ = os.MkdirAll(filepath.Dir(out), 0o755)

	runCtx, cancel := context.WithCancel(c.baseCtx)
	done := make(chan struct{})

	c.running = true
	c.started = started
	c.id = id
	c.artifact = out
	c.cancel = cancel
	c.done = done
	c.lastErr = nil
	c.mu.Unlock()

	go func() {
		defer close(done)
		session, err := capture.NewSession(c.kubeClient, &optsCopy, &capOptsCopy, c.logger.WithName("capture"))
		if err != nil {
			c.mu.Lock()
			c.lastErr = err
			c.running = false
			c.mu.Unlock()
			return
		}
		_, runErr := session.Run(runCtx)
		c.mu.Lock()
		c.lastErr = runErr
		c.running = false
		c.mu.Unlock()
	}()

	return c.Status(ctx)
}

func (c *uiCaptureController) Stop(ctx context.Context) (caststream.CaptureStatus, caststream.CaptureView, error) {
	c.mu.Lock()
	if !c.running {
		status := caststream.CaptureStatus{
			Running:   false,
			ID:        c.id,
			StartedAt: fmtTimeRFC3339(c.started),
			Artifact:  c.artifact,
			LastError: fmtErr(c.lastErr),
		}
		c.mu.Unlock()
		return status, caststream.CaptureView{}, nil
	}
	cancel := c.cancel
	done := c.done
	id := c.id
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return caststream.CaptureStatus{}, caststream.CaptureView{}, ctx.Err()
		}
	}

	status, _ := c.Status(ctx)
	return status, caststream.CaptureView{ID: id}, nil
}

func fmtTimeRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func fmtErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
