package stack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestValidateHooksConfig_DisallowsRunOnceOutsideRoot(t *testing.T) {
	cfg := StackHooksConfig{
		PreApply: []HookSpec{
			{
				Name:    "x",
				Type:    "script",
				RunOnce: true,
				Script:  &ScriptHookConfig{Command: []string{"bash", "-lc", "echo hi"}},
			},
		},
	}
	if err := ValidateHooksConfig(cfg, false, "subdir/stack.yaml hooks"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHTTPHook_SucceedsOn200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok\n"))
	}))
	defer srv.Close()

	p := &Plan{StackRoot: t.TempDir(), Profile: "p"}
	r := &runState{RunID: "run-1", Plan: p}
	var sawBody bool
	r.observers = append(r.observers, RunEventObserverFunc(func(ev RunEvent) {
		if ev.Type != string(NodeLog) {
			return
		}
		if strings.Contains(ev.Message, "ok") {
			sawBody = true
		}
	}))
	hc := hookRunContext{
		run:    r,
		opts:   RunOptions{Plan: p, Command: "apply"},
		phase:  "post-apply",
		status: "success",
	}
	hook := HookSpec{
		Type: "http",
		HTTP: &HTTPHookConfig{
			Method: "GET",
			URL:    srv.URL,
		},
	}
	if err := runOneHookAttempt(context.Background(), hc, hook, "post-apply http"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !sawBody {
		t.Fatalf("expected hook output to be observed via NODE_LOG events")
	}
}

func TestScriptHook_RunsCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash not available on windows")
	}
	p := &Plan{StackRoot: t.TempDir(), Profile: "p"}
	r := &runState{RunID: "run-1", Plan: p}
	var sawLine bool
	r.observers = append(r.observers, RunEventObserverFunc(func(ev RunEvent) {
		if ev.Type != string(NodeLog) {
			return
		}
		if strings.Contains(ev.Message, "hook-ok") {
			sawLine = true
		}
	}))
	hc := hookRunContext{
		run:     r,
		opts:    RunOptions{Plan: p, Command: "apply"},
		phase:   "pre-apply",
		status:  "success",
		baseDir: p.StackRoot,
	}
	hook := HookSpec{
		Type: "script",
		Script: &ScriptHookConfig{
			Command: []string{"bash", "-lc", "echo hook-ok"},
		},
	}
	if err := runOneHookAttempt(context.Background(), hc, hook, "pre-apply script"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !sawLine {
		t.Fatalf("expected hook output to be observed via NODE_LOG events")
	}
}
