//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/example/ktl/internal/workflows/buildsvc"
	"github.com/spf13/cobra"
)

type sandboxDoctorResult struct {
	NsjailPath     string
	NsjailVersion  string
	UserNS         string
	Cgroup         string
	PolicyPath     string
	PolicySource   string
	Policy         sandboxPolicySummary
	ProbeMount     string
	ProbeBind      string
	ProbeDNS       string
	ProbeConnect   string
	ProbeNotes     []string
	ProbeLogTail   string
	EffectiveCwd   string
	EffectiveCache string
}

func runBuildSandboxDoctor(cmd *cobra.Command, parent *buildCLIOptions, contextDir string) error {
	if cmd == nil {
		return fmt.Errorf("sandbox doctor: command is nil")
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("sandbox doctor is only supported on Linux (nsjail)")
	}
	if parent == nil {
		return fmt.Errorf("sandbox doctor: missing build options")
	}

	res := sandboxDoctorResult{}

	bin := strings.TrimSpace(parent.sandboxBin)
	if bin == "" {
		bin = "nsjail"
	}
	nsjailPath, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("nsjail not found (%s): %w", bin, err)
	}
	res.NsjailPath = nsjailPath
	if version, err := runCommandOutput(nsjailPath, "--version"); err == nil {
		res.NsjailVersion = strings.TrimSpace(version)
	} else {
		res.NsjailVersion = "unknown"
		res.ProbeNotes = append(res.ProbeNotes, fmt.Sprintf("nsjail --version unavailable: %v", err))
	}

	res.UserNS = detectUserNS()
	res.Cgroup = detectCgroup()

	effectiveContext, err := effectiveDoctorContext(contextDir)
	if err != nil {
		return err
	}
	res.EffectiveCwd = effectiveContext

	cacheDir := strings.TrimSpace(parent.cacheDir)
	if cacheDir == "" {
		cacheDir = os.TempDir()
	}
	cacheDir, _ = filepath.Abs(cacheDir)
	res.EffectiveCache = cacheDir

	policyPath, err := buildsvc.ResolveSandboxConfigPath(parent.sandboxConfig, parent.hermetic, parent.allowNetwork)
	if err != nil {
		return err
	}
	res.PolicyPath = policyPath
	res.PolicySource = sandboxPolicySource(parent.sandboxConfig, parent.hermetic, parent.allowNetwork)
	policySummary, err := parseSandboxPolicy(policyPath)
	if err != nil {
		return fmt.Errorf("parse sandbox policy: %w", err)
	}
	res.Policy = policySummary

	// Probes: best-effort. We bind a minimal set of host paths needed to run /bin/sh and basic tooling.
	proofDir := filepath.Join(cacheDir, "sandbox-doctor")
	if err := os.MkdirAll(proofDir, 0o755); err != nil {
		return fmt.Errorf("sandbox doctor: create probe dir: %w", err)
	}
	hostProof := filepath.Join(proofDir, "bind-proof.txt")
	if err := os.WriteFile(hostProof, []byte("ok\n"), 0o600); err != nil {
		return fmt.Errorf("sandbox doctor: write proof: %w", err)
	}

	logPath := filepath.Join(proofDir, fmt.Sprintf("nsjail-%d.log", time.Now().UnixNano()))
	baseArgs := []string{"--config", policyPath, "--log", logPath, "--cwd", "/", "--quiet"}
	for _, bind := range doctorBaseBinds() {
		if pathExists(bind) {
			baseArgs = append(baseArgs, "--bindmount_ro", fmt.Sprintf("%s:%s", bind, bind))
		}
	}
	// Ensure the proof directory is visible for bind testing.
	baseArgs = append(baseArgs, "--bindmount_ro", fmt.Sprintf("%s:%s", proofDir, proofDir))

	res.ProbeMount = probeSandbox(nsjailPath, append(baseArgs, "--", "/bin/sh", "-c", "touch /tmp/ktl-sandbox-doctor && echo mount-ok"), res.Policy.NetworkMode == "isolated", false)
	res.ProbeBind = probeSandbox(nsjailPath, append(baseArgs, "--", "/bin/sh", "-c", fmt.Sprintf("cat %s >/dev/null && echo bind-ok", shellEscape(hostProof))), res.Policy.NetworkMode == "isolated", false)

	res.ProbeDNS = "skipped"
	if _, err := exec.LookPath("getent"); err == nil {
		res.ProbeDNS = probeSandbox(nsjailPath, append(baseArgs, "--", "/bin/sh", "-c", "getent hosts example.com >/dev/null && echo dns-ok"), false, false)
	} else {
		res.ProbeNotes = append(res.ProbeNotes, "dns probe skipped: getent not found on host")
	}

	res.ProbeConnect = "skipped"
	if _, err := exec.LookPath("curl"); err == nil {
		wantFail := res.Policy.NetworkMode == "isolated"
		res.ProbeConnect = probeSandbox(nsjailPath, append(baseArgs, "--", "/bin/sh", "-c", "curl -fsSL --max-time 2 https://example.com >/dev/null && echo connect-ok"), wantFail, false)
	} else {
		res.ProbeNotes = append(res.ProbeNotes, "connect probe skipped: curl not found on host")
	}

	if tail, err := readTail(logPath, 40); err == nil {
		res.ProbeLogTail = tail
	}
	_ = os.Remove(logPath)

	writeSandboxDoctorReport(cmd.ErrOrStderr(), res)
	if strings.HasPrefix(res.ProbeMount, "fail") || strings.HasPrefix(res.ProbeBind, "fail") || strings.HasPrefix(res.ProbeConnect, "fail") {
		return errors.New("sandbox doctor: required probes failed")
	}
	return nil
}

func effectiveDoctorContext(contextDir string) (string, error) {
	contextDir = strings.TrimSpace(contextDir)
	if contextDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("sandbox doctor: cwd: %w", err)
		}
		contextDir = wd
	}
	abs, err := filepath.Abs(contextDir)
	if err != nil {
		return "", fmt.Errorf("sandbox doctor: resolve context: %w", err)
	}
	return abs, nil
}

func runCommandOutput(bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(buf.String()))
	}
	return buf.String(), nil
}

func probeSandbox(nsjailPath string, args []string, wantNetworkFailure bool, tolerateUnexpectedFailure bool) string {
	cmd := exec.Command(nsjailPath, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err == nil {
		if wantNetworkFailure && strings.Contains(out, "connect-ok") {
			return "fail (unexpected network access)"
		}
		return "ok"
	}
	if wantNetworkFailure {
		return "ok (network isolated)"
	}
	if tolerateUnexpectedFailure {
		return fmt.Sprintf("warn (%s)", summarizeExecError(err, out))
	}
	return fmt.Sprintf("fail (%s)", summarizeExecError(err, out))
}

func summarizeExecError(err error, out string) string {
	msg := err.Error()
	if out != "" {
		msg = msg + ": " + out
	}
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.TrimSpace(msg)
	if len(msg) > 180 {
		msg = msg[:180] + "..."
	}
	return msg
}

func readTail(path string, maxLines int) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n"), nil
}

func writeSandboxDoctorReport(w io.Writer, res sandboxDoctorResult) {
	fmt.Fprintf(w, "Sandbox doctor (nsjail)\n")
	fmt.Fprintf(w, "  nsjail: %s\n", res.NsjailPath)
	if res.NsjailVersion != "" {
		fmt.Fprintf(w, "  nsjail version: %s\n", res.NsjailVersion)
	}
	fmt.Fprintf(w, "  userns: %s\n", res.UserNS)
	fmt.Fprintf(w, "  cgroup: %s\n", res.Cgroup)
	fmt.Fprintf(w, "  policy: %s (%s)\n", res.PolicyPath, res.PolicySource)
	fmt.Fprintf(w, "  network mode: %s\n", res.Policy.NetworkMode)
	fmt.Fprintf(w, "  tmpfs mounts: %s\n", strings.Join(res.Policy.TmpfsMounts, ", "))
	fmt.Fprintf(w, "  bind mounts: %d\n", res.Policy.BindMountCount)
	fmt.Fprintf(w, "  limits: as=%s cpu=%ss fsize=%s nofile=%s nproc=%s\n", res.Policy.RlimitAS, res.Policy.RlimitCPU, res.Policy.RlimitFsize, res.Policy.RlimitNofile, res.Policy.RlimitNproc)
	fmt.Fprintf(w, "  probes: mount=%s bind=%s dns=%s connect=%s\n", res.ProbeMount, res.ProbeBind, res.ProbeDNS, res.ProbeConnect)
	for _, note := range res.ProbeNotes {
		fmt.Fprintf(w, "  note: %s\n", note)
	}
	if strings.TrimSpace(res.ProbeLogTail) != "" {
		fmt.Fprintf(w, "  nsjail log (tail):\n")
		for _, line := range strings.Split(res.ProbeLogTail, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			fmt.Fprintf(w, "    %s\n", line)
		}
	}
}

func sandboxPolicySource(explicit string, hermetic bool, allowNetwork bool) string {
	if strings.TrimSpace(explicit) != "" {
		return "explicit"
	}
	if hermetic && !allowNetwork {
		return "embedded hermetic"
	}
	return "embedded default"
}

func doctorBaseBinds() []string {
	return []string{
		"/bin",
		"/sbin",
		"/usr/bin",
		"/usr/sbin",
		"/usr/lib",
		"/usr/lib64",
		"/lib",
		"/lib64",
	}
}

func shellEscape(path string) string {
	// Minimal shell escaping for paths used in our probes.
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
