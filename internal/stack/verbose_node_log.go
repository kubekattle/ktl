package stack

import (
	"bytes"
	"io"
	"strings"
	"sync"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

func (r *runState) verboseTaskLogsEnabled() bool {
	return r != nil && r.VerboseTaskLogs
}

func (e *customNodeExecutor) verboseTaskLogsEnabled() bool {
	return e != nil && e.run != nil && e.run.verboseTaskLogsEnabled()
}

func (e *customNodeExecutor) emitVerboseNodeLog(node *runNode, phase string, stream string, message string, extra map[string]any) {
	if !e.verboseTaskLogsEnabled() || node == nil || e.run == nil {
		return
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	fields := map[string]any{
		"kind":  "task-log",
		"phase": strings.TrimSpace(phase),
	}
	if stream = strings.TrimSpace(stream); stream != "" {
		fields["stream"] = stream
	}
	for key, value := range extra {
		fields[key] = value
	}
	e.run.EmitEphemeralEvent(node.ID, NodeLog, node.Attempt, message, fields)
}

func (e *customNodeExecutor) transportLineObserver(node *runNode, phase string) transport.LineObserver {
	if !e.verboseTaskLogsEnabled() || node == nil {
		return nil
	}
	return transport.LineObserverFunc(func(rec transport.LineRecord) {
		line := strings.TrimRight(rec.Line, "\r\t ")
		if strings.TrimSpace(line) == "" {
			return
		}
		e.emitVerboseNodeLog(node, phase, rec.Stream, line, nil)
	})
}

func (e *customNodeExecutor) nodeLogWriter(node *runNode, phase string, stream string) *nodeLogLineWriter {
	if !e.verboseTaskLogsEnabled() || node == nil {
		return nil
	}
	return newNodeLogLineWriter(func(line string) {
		e.emitVerboseNodeLog(node, phase, stream, line, nil)
	})
}

func (e *customNodeExecutor) emitVerboseReceiptLogs(node *runNode, phase string, receipt transport.OperationResult) {
	if !e.verboseTaskLogsEnabled() || node == nil {
		return
	}
	for _, entry := range []struct {
		stream string
		text   string
	}{
		{stream: "stdout", text: receipt.Stdout},
		{stream: "stderr", text: receipt.Stderr},
	} {
		for _, line := range strings.Split(entry.text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			e.emitVerboseNodeLog(node, phase, entry.stream, line, nil)
		}
	}
}

type nodeLogLineWriter struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	emit func(string)
}

func newNodeLogLineWriter(emit func(string)) *nodeLogLineWriter {
	if emit == nil {
		return nil
	}
	return &nodeLogLineWriter{emit: emit}
}

func (w *nodeLogLineWriter) Write(p []byte) (int, error) {
	if w == nil {
		return len(p), nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.buf.Write(p)
	for {
		data := w.buf.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(string(data[:idx]), "\r")
		if strings.TrimSpace(line) != "" {
			w.emit(line)
		}
		w.buf.Next(idx + 1)
	}
	return len(p), nil
}

func (w *nodeLogLineWriter) Flush() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.TrimSpace(w.buf.String()) != "" {
		w.emit(strings.TrimRight(w.buf.String(), "\r"))
	}
	w.buf.Reset()
	return nil
}

var _ io.Writer = (*nodeLogLineWriter)(nil)
