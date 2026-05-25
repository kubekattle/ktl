package stack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

func (e *customNodeExecutor) runHostFileCopyNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Host
	phase := "host-file-copy"
	operation := "apply"
	if strings.EqualFold(command, "delete") {
		phase = "delete-host-file-copy"
		operation = "delete"
	}
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"transport": strings.TrimSpace(spec.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	var desired []byte
	var desiredDigest string
	var err error
	if operation != "delete" {
		desired, err = readHostFileCopySource(node)
		if err != nil {
			return wrapNodeErr(node.ResolvedRelease, err)
		}
		desiredDigest = digestBytes(desired)
	}
	pathDigest := digestString(spec.Path)
	targetID, guardMode, selected := e.hostCommandTargetContext(node)
	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		observe := e.hostFileObserveReceipt(node, phase, targetID, guardMode, selected, "", pathDigest, hostFileState{}, "skipped")
		plan := e.hostFilePlanReceipt(node, phase, targetID, guardMode, selected, desiredDigest, pathDigest, hostFileChangeSet{}, "skipped", reason)
		diff := e.hostFileDiffReceipt(node, phase, targetID, pathDigest, desiredDigest, hostFileState{}, hostFileChangeSet{}, "skipped")
		verify := e.hostFileVerifyReceipt(node, phase, targetID, desiredDigest, pathDigest, hostFileOperationResult{Status: "skipped", Reason: reason})
		e.recordHostFileCopyReceipts(node, phase, "skipped", reason, observe, plan, diff, nil, verify)
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "skipped: "+reason, map[string]any{
			"phase":  phase,
			"status": "skipped",
			"reason": reason,
			"cursor": cursor,
		}, nil)
		return nil
	}

	transportClient, err := hostCommandTransport(spec)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	targetDigest := transportClient.TargetDigest()
	observeResult, err := e.runHostFileCopyOperation(ctx, transportClient, hostFileCopyPayload(spec, "observe", nil))
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	observe := e.hostFileObserveReceipt(node, phase, targetID, guardMode, selected, targetDigest, pathDigest, observeResult.After, observeResult.Status)
	changes := hostFileChanges(observeResult.After, spec, desiredDigest)
	if operation == "delete" {
		changes = hostFileChangeSet{Content: observeResult.After.Exists}
	}
	plan := e.hostFilePlanReceipt(node, phase, targetID, guardMode, selected, desiredDigest, pathDigest, changes, "planned", "eligible")
	diff := e.hostFileDiffReceipt(node, phase, targetID, pathDigest, desiredDigest, observeResult.After, changes, "planned")
	if guardErr := e.validateHostAdapterOpsGuard(node, targetID, NodeKindHostFileCopy); guardErr != nil {
		plan.Status = "blocked"
		plan.Reason = guardErr.Error()
		verify := e.hostFileVerifyReceipt(node, phase, targetID, desiredDigest, pathDigest, hostFileOperationResult{Status: "blocked", Reason: guardErr.Error(), Before: observeResult.After, After: observeResult.After})
		e.recordHostFileCopyReceipts(node, phase, "blocked", guardErr.Error(), observe, plan, diff, nil, verify)
		runErr := &RunError{Class: "HOST_FILE_COPY_BLOCKED", Message: guardErr.Error(), Digest: computeRunErrorDigest("HOST_FILE_COPY_BLOCKED", guardErr.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, guardErr.Error(), map[string]any{
			"phase":    phase,
			"status":   "blocked",
			"targetId": targetID,
			"cursor":   cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host file copy phase %s: %w", phase, guardErr))
	}

	var result hostFileOperationResult
	if operation == "delete" {
		result, err = e.runHostFileCopyOperation(ctx, transportClient, hostFileCopyPayload(spec, "delete", nil))
	} else {
		result, err = e.runHostFileCopyOperation(ctx, transportClient, hostFileCopyPayload(spec, "apply", desired))
	}
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	result.TargetDigest = targetDigest
	result.PathDigest = pathDigest
	if result.DesiredDigest == "" {
		result.DesiredDigest = desiredDigest
	}
	if result.SourceDigest == "" {
		result.SourceDigest = desiredDigest
	}
	if result.Changes == (hostFileChangeSet{}) {
		result.Changes = changes
	}
	verify := e.hostFileVerifyReceipt(node, phase, targetID, desiredDigest, pathDigest, result)
	e.recordHostFileCopyReceipts(node, phase, result.Status, strings.TrimSpace(result.Error), observe, plan, diff, &result, verify)
	if !nodeStepSucceeded(result.Status) || verify.Status == "failed" {
		msg := firstNonEmptyString(result.Error, result.Validation.Error, result.Validation.Stderr, result.Reason, verify.Reason, "host file copy failed")
		runErr := &RunError{Class: "HOST_FILE_COPY_FAILED", Message: msg, Digest: computeRunErrorDigest("HOST_FILE_COPY_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
			"result": result,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host file copy phase %s: %s", phase, msg))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":  phase,
		"status": "success",
		"cursor": cursor,
		"result": result,
	}, nil)
	return nil
}

func readHostFileCopySource(node *runNode) ([]byte, error) {
	if node == nil {
		return nil, fmt.Errorf("nil host.file.copy node")
	}
	spec := node.Host
	if strings.TrimSpace(spec.SourcePath) != "" {
		path := spec.SourcePath
		if !filepath.IsAbs(path) {
			path = filepath.Join(node.Dir, path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read host.file.copy source: %w", err)
		}
		return raw, nil
	}
	if strings.TrimSpace(spec.Content) != "" {
		return []byte(spec.Content), nil
	}
	return nil, fmt.Errorf("host.file.copy requires sourcePath or content")
}

func (e *customNodeExecutor) runHostFileCopyOperation(ctx context.Context, runner hostCommandRunner, payload map[string]any) (hostFileOperationResult, error) {
	command, err := hostFileCopyPythonCommand(payload)
	if err != nil {
		return hostFileOperationResult{}, err
	}
	receipt := runner.Run(ctx, command)
	var result hostFileOperationResult
	if strings.TrimSpace(receipt.Stdout) != "" {
		if err := json.Unmarshal([]byte(receipt.Stdout), &result); err != nil {
			return hostFileOperationResult{}, fmt.Errorf("decode host.file.copy receipt: %w: %s", err, strings.TrimSpace(receipt.Stdout))
		}
	}
	if result.APIVersion == "" {
		result.APIVersion = "torque.dev/host-file-copy-node/v1"
	}
	if result.Kind == "" {
		result.Kind = "HostFileCopyOperationReceipt"
	}
	if result.Operation == "" {
		if op, ok := payload["operation"].(string); ok {
			result.Operation = op
		}
	}
	if result.Status == "" {
		result.Status = receipt.Status
	}
	if result.Status == "" {
		result.Status = "failed"
	}
	if !nodeStepSucceeded(receipt.Status) && nodeStepSucceeded(result.Status) {
		result.Status = "failed"
	}
	if result.Error == "" && strings.TrimSpace(receipt.Stderr) != "" {
		result.Error = strings.TrimSpace(receipt.Stderr)
	}
	if result.CompletedAt == "" {
		result.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return result, nil
}

func hostFileCopyPayload(spec HostCommandSpec, operation string, desired []byte) map[string]any {
	backupPath := firstNonEmptyString(strings.TrimSpace(spec.BackupPath), defaultHostFileBackupPath(spec.Path))
	return map[string]any{
		"operation":       strings.TrimSpace(operation),
		"path":            strings.TrimSpace(spec.Path),
		"contentB64":      base64.StdEncoding.EncodeToString(desired),
		"mode":            strings.TrimSpace(spec.Mode),
		"owner":           strings.TrimSpace(spec.Owner),
		"group":           strings.TrimSpace(spec.Group),
		"validate":        strings.TrimSpace(spec.Validate),
		"backup":          spec.Backup,
		"backupPath":      backupPath,
		"removeOnDelete":  spec.RemoveOnDelete,
		"restoreOnDelete": spec.RestoreOnDelete,
	}
}

func hostFileCopyPythonCommand(payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return "TORQUE_FILE_COPY_PAYLOAD_B64=" + transport.ShellQuote(encoded) + " python3 - <<'PY'\n" + hostFileCopyPythonScript + "\nPY", nil
}

func defaultHostFileBackupPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return path + ".torque-backup"
}

func (e *customNodeExecutor) recordHostFileCopyReceipts(node *runNode, phase string, status string, reason string, observe hostFileObserveReceipt, plan hostFilePlanReceipt, diff hostFileDiffReceipt, apply *hostFileOperationResult, verify hostFileVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/host-file-copy-node/v1",
		"kind":       "HostFileCopyNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      phase,
		"status":     strings.TrimSpace(status),
		"targetId":   strings.TrimSpace(plan.TargetID),
		"guardMode":  strings.TrimSpace(plan.GuardMode),
		"observe":    observe,
		"plan":       plan,
		"diff":       diff,
		"verify":     verify,
	}
	if strings.TrimSpace(reason) != "" {
		payload["reason"] = strings.TrimSpace(reason)
	}
	if apply != nil {
		payload["targetDigest"] = apply.TargetDigest
		payload["apply"] = *apply
	}
	e.run.RecordJSONArtifact(node.ID, "host-file-copy-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "host-file-copy-plan.json", plan)
	e.run.RecordJSONArtifact(node.ID, "host-file-copy-diff.json", diff)
	if apply != nil {
		e.run.RecordJSONArtifact(node.ID, "host-file-copy-apply.json", *apply)
	}
	e.run.RecordJSONArtifact(node.ID, "host-file-copy-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

const hostFileCopyPythonScript = `
import base64
import grp
import hashlib
import json
import os
import pwd
import shutil
import stat
import subprocess
import tempfile
import time

def now():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

def digest_bytes(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()

def normalize_mode(value):
    value = str(value or "").strip()
    if not value:
        return ""
    if value.startswith("0o"):
        value = value[2:]
    value = value[-4:]
    return format(int(value, 8), "04o")

def observe(path):
    path = str(path or "").strip()
    doc = {"exists": False, "path": path}
    if not path or not os.path.lexists(path):
        return doc
    st = os.lstat(path)
    doc.update({
        "exists": True,
        "mode": format(stat.S_IMODE(st.st_mode), "04o"),
        "uid": int(st.st_uid),
        "gid": int(st.st_gid),
    })
    try:
        doc["owner"] = pwd.getpwuid(st.st_uid).pw_name
    except KeyError:
        doc["owner"] = str(st.st_uid)
    try:
        doc["group"] = grp.getgrgid(st.st_gid).gr_name
    except KeyError:
        doc["group"] = str(st.st_gid)
    if stat.S_ISREG(st.st_mode):
        doc["type"] = "file"
        with open(path, "rb") as fh:
            data = fh.read()
        doc["sha256"] = digest_bytes(data)
        doc["size"] = len(data)
    elif stat.S_ISLNK(st.st_mode):
        doc["type"] = "symlink"
    elif stat.S_ISDIR(st.st_mode):
        doc["type"] = "directory"
    else:
        doc["type"] = "other"
    return doc

def maybe_chown_from_state(path, state):
    if not state or not state.get("exists"):
        return
    if hasattr(os, "geteuid") and os.geteuid() != 0:
        return
    uid = state.get("uid")
    gid = state.get("gid")
    if uid is not None and gid is not None:
        os.chown(path, int(uid), int(gid))

def run_validation(command, temp_path, path, mode, backup_path):
    command = str(command or "").strip()
    if not command:
        return {"status": "skipped"}
    env = os.environ.copy()
    env["TORQUE_FILE_COPY_PATH"] = path
    env["TORQUE_FILE_COPY_TEMP_PATH"] = temp_path
    env["TORQUE_FILE_COPY_MODE"] = mode
    env["TORQUE_FILE_COPY_BACKUP_PATH"] = backup_path
    proc = subprocess.run(command, shell=True, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return {
        "status": "succeeded" if proc.returncode == 0 else "failed",
        "command": command,
        "exitCode": int(proc.returncode),
        "stdout": proc.stdout.strip(),
        "stderr": proc.stderr.strip(),
    }

def finish(doc, code=0):
    doc.setdefault("apiVersion", "torque.dev/host-file-copy-node/v1")
    doc.setdefault("kind", "HostFileCopyOperationReceipt")
    doc.setdefault("completedAt", now())
    print(json.dumps(doc, sort_keys=True))
    raise SystemExit(code)

try:
    payload = json.loads(base64.b64decode(os.environ["TORQUE_FILE_COPY_PAYLOAD_B64"]).decode("utf-8"))
    operation = str(payload.get("operation") or "").strip()
    path = str(payload.get("path") or "").strip()
    desired = base64.b64decode(str(payload.get("contentB64") or ""))
    desired_digest = digest_bytes(desired) if desired else ""
    mode = normalize_mode(payload.get("mode"))
    owner = str(payload.get("owner") or "").strip()
    group = str(payload.get("group") or "").strip()
    validate = str(payload.get("validate") or "").strip()
    backup = bool(payload.get("backup"))
    backup_path = str(payload.get("backupPath") or "").strip() or (path + ".torque-backup" if path else "")
    remove_on_delete = bool(payload.get("removeOnDelete"))
    restore_on_delete = bool(payload.get("restoreOnDelete"))
    if not path:
        finish({"operation": operation, "status": "failed", "error": "path is required"}, 1)
    before = observe(path)
    backup_state = observe(backup_path)
    if operation == "observe":
        finish({
            "operation": operation,
            "status": "succeeded",
            "pathDigest": digest_bytes(path.encode("utf-8")),
            "backupPathDigest": digest_bytes(backup_path.encode("utf-8")) if backup_path else "",
            "desiredDigest": desired_digest,
            "sourceDigest": desired_digest,
            "changed": False,
            "changes": {"content": False, "mode": False, "owner": False, "group": False},
            "before": before,
            "after": before,
            "backup": backup_state,
            "validation": {"status": "skipped"},
        })
    if operation == "delete":
        if restore_on_delete and backup_state.get("exists"):
            if backup_state.get("type") != "file":
                finish({"operation": operation, "status": "failed", "before": before, "after": before, "backup": backup_state, "error": "backup path is not a regular file"}, 1)
            os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
            shutil.copy2(backup_path, path)
            maybe_chown_from_state(path, backup_state)
            os.unlink(backup_path)
            after = observe(path)
            finish({
                "operation": operation,
                "status": "succeeded",
                "before": before,
                "after": after,
                "backup": observe(backup_path),
                "backupPathDigest": digest_bytes(backup_path.encode("utf-8")) if backup_path else "",
                "changed": True,
                "restored": True,
                "changes": {"content": True, "mode": True, "owner": True, "group": True},
                "validation": {"status": "skipped"},
            })
        if not remove_on_delete:
            finish({
                "operation": operation,
                "status": "skipped",
                "reason": "removeOnDelete is false and no backup restore was available",
                "before": before,
                "after": before,
                "backup": backup_state,
                "changed": False,
                "changes": {"content": False, "mode": False, "owner": False, "group": False},
                "validation": {"status": "skipped"},
            })
        if before.get("exists"):
            if before.get("type") != "file":
                finish({"operation": operation, "status": "failed", "before": before, "after": before, "backup": backup_state, "error": "target path is not a regular file"}, 1)
            os.unlink(path)
        after = observe(path)
        finish({
            "operation": operation,
            "status": "succeeded",
            "before": before,
            "after": after,
            "backup": backup_state,
            "backupPathDigest": digest_bytes(backup_path.encode("utf-8")) if backup_path else "",
            "changed": bool(before.get("exists")),
            "changes": {"content": bool(before.get("exists")), "mode": False, "owner": False, "group": False},
            "validation": {"status": "skipped"},
        })
    if operation != "apply":
        finish({"operation": operation, "status": "failed", "error": "unsupported operation"}, 1)
    if before.get("exists") and before.get("type") != "file":
        finish({"operation": operation, "status": "failed", "before": before, "after": before, "backup": backup_state, "error": "target path is not a regular file"}, 1)
    changes = {
        "content": (not before.get("exists")) or before.get("sha256") != desired_digest,
        "mode": bool(mode) and before.get("mode") != mode,
        "owner": bool(owner) and before.get("owner") != owner and str(before.get("uid", "")) != owner,
        "group": bool(group) and before.get("group") != group and str(before.get("gid", "")) != group,
    }
    changed = bool(changes["content"] or changes["mode"] or changes["owner"] or changes["group"])
    parent = os.path.dirname(path) or "."
    os.makedirs(parent, exist_ok=True)
    fd, temp_path = tempfile.mkstemp(prefix=".torque-file-copy-", dir=parent)
    try:
        with os.fdopen(fd, "wb") as fh:
            fh.write(desired)
        if mode:
            os.chmod(temp_path, int(mode, 8))
        validation = run_validation(validate, temp_path, path, mode, backup_path)
        if validation.get("status") == "failed":
            finish({
                "operation": operation,
                "status": "failed",
                "before": before,
                "after": before,
                "backup": backup_state,
                "desiredDigest": desired_digest,
                "sourceDigest": desired_digest,
                "changed": False,
                "changes": changes,
                "validation": validation,
                "error": "validation command failed",
            }, 1)
        if changed:
            if backup and before.get("exists"):
                os.makedirs(os.path.dirname(backup_path) or ".", exist_ok=True)
                shutil.copy2(path, backup_path)
                maybe_chown_from_state(backup_path, before)
            os.replace(temp_path, path)
        else:
            os.unlink(temp_path)
            temp_path = ""
        if mode:
            os.chmod(path, int(mode, 8))
        if owner or group:
            shutil.chown(path, user=owner or None, group=group or None)
        after = observe(path)
        finish({
            "operation": operation,
            "status": "succeeded",
            "before": before,
            "after": after,
            "backup": observe(backup_path),
            "backupPathDigest": digest_bytes(backup_path.encode("utf-8")) if backup_path else "",
            "desiredDigest": desired_digest,
            "sourceDigest": desired_digest,
            "changed": changed,
            "changes": changes,
            "validation": validation,
        })
    finally:
        if temp_path:
            try:
                os.unlink(temp_path)
            except FileNotFoundError:
                pass
except Exception as exc:
    finish({"operation": locals().get("operation", ""), "status": "failed", "error": str(exc)}, 1)
`
