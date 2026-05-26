#!/usr/bin/env python3
import base64
import hashlib
import json
import os
import re
import shlex
import socket
import ssl
import subprocess
import sys
import time
import urllib.parse
import uuid


ASSIGNMENT_API_VERSION = "torque.dev/nats-assignment/v1"
ASSIGNMENT_KIND = "CommandAssignment"
DEFAULT_NATS_URL = "nats://127.0.0.1:4222"


def emit(doc):
    sys.stdout.write(json.dumps(doc, separators=(",", ":"), sort_keys=True))


def digest(value):
    raw = str(value or "").strip().encode("utf-8")
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def normalize_target(target):
    target = str(target or "").strip()
    for prefix in ("nats-mesh://", "nats://"):
        if target.startswith(prefix):
            return target[len(prefix):]
    return target


def normalize_mode(value):
    value = str(value or "0644").strip()
    if not re.fullmatch(r"0?[0-7]{3,4}", value):
        raise ValueError("mode must be an octal string")
    return f"{int(value, 8) & 0o7777:04o}"


def bool_input(value, default=False):
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    return str(value).strip().lower() in ("1", "true", "yes", "y", "on")


def desired_content(inputs):
    if "contentBase64" in inputs:
        return base64.b64decode(str(inputs.get("contentBase64") or ""))
    return str(inputs.get("content") or "").encode("utf-8")


def operation_summary(result):
    return {
        "status": result.get("status", ""),
        "exitCode": result.get("exitCode", 0),
        "timedOut": bool(result.get("timedOut", False)),
        "durationMillis": result.get("durationMillis", 0),
        "targetDigest": result.get("targetDigest", ""),
        "operation": result.get("operation", ""),
    }


def redact_args(args, values):
    out = []
    for arg in args:
        item = str(arg)
        for value in values:
            if value:
                item = item.replace(value, "[REDACTED]")
        out.append(item)
    return out


def read_line(sock, deadline):
    buf = bytearray()
    while True:
        if time.time() > deadline:
            raise TimeoutError("timed out reading NATS protocol line")
        chunk = sock.recv(1)
        if not chunk:
            raise ConnectionError("NATS connection closed")
        buf.extend(chunk)
        if buf.endswith(b"\r\n"):
            return bytes(buf[:-2]).decode("utf-8", "replace")


def read_exact(sock, size, deadline):
    buf = bytearray()
    while len(buf) < size:
        if time.time() > deadline:
            raise TimeoutError("timed out reading NATS payload")
        chunk = sock.recv(size - len(buf))
        if not chunk:
            raise ConnectionError("NATS connection closed")
        buf.extend(chunk)
    return bytes(buf)


def nats_connect(server_url, timeout):
    if "://" not in server_url:
        server_url = "nats://" + server_url
    parsed = urllib.parse.urlparse(server_url)
    scheme = parsed.scheme or "nats"
    host = parsed.hostname or "127.0.0.1"
    port = parsed.port or 4222
    raw = socket.create_connection((host, port), timeout=timeout)
    raw.settimeout(timeout)
    if scheme in ("tls", "tls+nats", "nats+tls"):
        raw = ssl.create_default_context().wrap_socket(raw, server_hostname=host)
        raw.settimeout(timeout)
    connect = {
        "verbose": False,
        "pedantic": False,
        "lang": "python",
        "version": "torque-module",
        "name": "torque-host-file-ensure-module",
    }
    if parsed.username:
        username = urllib.parse.unquote(parsed.username)
        if parsed.password:
            connect["user"] = username
            connect["pass"] = urllib.parse.unquote(parsed.password)
        else:
            connect["auth_token"] = username
    raw.sendall(b"CONNECT " + json.dumps(connect, separators=(",", ":")).encode("utf-8") + b"\r\nPING\r\n")
    deadline = time.time() + timeout
    while True:
        line = read_line(raw, deadline)
        if line.startswith("INFO ") or line == "+OK":
            continue
        if line == "PING":
            raw.sendall(b"PONG\r\n")
            continue
        if line == "PONG":
            return raw
        if line.startswith("-ERR"):
            raise RuntimeError(line)


def nats_request(server_url, subject, payload, timeout):
    subject = normalize_target(subject)
    inbox = "_INBOX.torque." + uuid.uuid4().hex
    sid = "1"
    raw = nats_connect(server_url, timeout)
    try:
        raw.sendall(f"SUB {inbox} {sid}\r\nPING\r\n".encode("utf-8"))
        deadline = time.time() + timeout
        while True:
            line = read_line(raw, deadline)
            if line == "PING":
                raw.sendall(b"PONG\r\n")
                continue
            if line == "+OK" or line.startswith("INFO "):
                continue
            if line == "PONG":
                break
            if line.startswith("-ERR"):
                raise RuntimeError(line)
        body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
        raw.sendall(f"PUB {subject} {inbox} {len(body)}\r\n".encode("utf-8") + body + b"\r\n")
        deadline = time.time() + timeout
        while True:
            line = read_line(raw, deadline)
            if line == "PING":
                raw.sendall(b"PONG\r\n")
                continue
            if line == "+OK" or line.startswith("INFO "):
                continue
            if line.startswith("-ERR"):
                raise RuntimeError(line)
            if not line.startswith("MSG "):
                continue
            parts = line.split()
            if len(parts) not in (4, 5):
                raise RuntimeError("unexpected NATS MSG header: " + line)
            size = int(parts[-1])
            data = read_exact(raw, size, deadline)
            _ = read_exact(raw, 2, deadline)
            return json.loads(data.decode("utf-8"))
    finally:
        try:
            raw.close()
        except OSError:
            pass


def remote_run(command, cfg):
    if cfg["transport"] == "ssh":
        return ssh_run(command, cfg)
    assignment = {
        "apiVersion": ASSIGNMENT_API_VERSION,
        "kind": ASSIGNMENT_KIND,
        "operation": "run",
        "target": cfg["target"],
        "command": command,
        "sentAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    result = nats_request(cfg["natsUrl"], cfg["target"], assignment, cfg["timeoutSeconds"])
    if not isinstance(result, dict):
        raise RuntimeError("worker returned non-object operation result")
    return result


def ssh_run(command, cfg):
    started = time.monotonic()
    target = cfg["target"]
    args = ["ssh"]
    if cfg.get("identityFile"):
        args.extend(["-i", cfg["identityFile"]])
    args.extend(cfg.get("sshOptions") or [])
    args.extend([target, command])
    try:
        proc = subprocess.run(
            args,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=cfg["timeoutSeconds"],
            check=False,
        )
        status = "succeeded" if proc.returncode == 0 else "failed"
        error = ""
        exit_code = proc.returncode
        timed_out = False
    except subprocess.TimeoutExpired as err:
        proc = err
        status = "timeout"
        error = "ssh command timed out"
        exit_code = 124
        timed_out = True
    duration_ms = int((time.monotonic() - started) * 1000)
    stdout = proc.stdout if isinstance(proc.stdout, str) else (proc.stdout or b"").decode("utf-8", "replace")
    stderr = proc.stderr if isinstance(proc.stderr, str) else (proc.stderr or b"").decode("utf-8", "replace")
    redact_values = [target, cfg.get("identityFile", "")]
    result = {
        "operation": "run",
        "status": status,
        "targetDigest": digest(target),
        "command": redact_args(args, redact_values),
        "stdout": stdout,
        "stderr": stderr,
        "exitCode": exit_code,
        "timedOut": timed_out,
        "durationMillis": duration_ms,
    }
    if error:
        result["error"] = error
    return result


def remote_json(command, cfg):
    result = remote_run(command, cfg)
    summary = operation_summary(result)
    if result.get("status") != "succeeded" or int(result.get("exitCode", 1)) != 0:
        message = result.get("error") or result.get("stderr") or "remote command failed"
        raise RuntimeError(message)
    stdout = str(result.get("stdout") or "").strip()
    if stdout == "":
        raise RuntimeError("remote command returned empty stdout")
    return json.loads(stdout), summary


def shell_quote(value):
    return shlex.quote(str(value))


def shell_helpers():
    return r"""
json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}
mode4() {
  value="$1"
  while [ "${#value}" -lt 4 ]; do
    value="0${value}"
  done
  printf '%s' "$value"
}
base64_decode() {
  if base64 -d >/dev/null 2>&1 </dev/null; then
    base64 -d
  else
    base64 --decode
  fi
}
"""


def observe_command(path):
    return f"""set -eu
{shell_helpers()}
path={shell_quote(path)}
path_json="$(json_escape "$path")"
if [ ! -e "$path" ] && [ ! -L "$path" ]; then
  printf '{{"exists":false,"path":"%s","type":"absent"}}\\n' "$path_json"
  exit 0
fi
mode="$(mode4 "$(stat -c '%a' -- "$path")")"
uid="$(stat -c '%u' -- "$path")"
gid="$(stat -c '%g' -- "$path")"
owner="$(json_escape "$(stat -c '%U' -- "$path")")"
group="$(json_escape "$(stat -c '%G' -- "$path")")"
if [ -L "$path" ]; then
  target="$(json_escape "$(readlink -- "$path")")"
  printf '{{"exists":true,"gid":%s,"group":"%s","mode":"%s","owner":"%s","path":"%s","target":"%s","type":"symlink","uid":%s}}\\n' "$gid" "$group" "$mode" "$owner" "$path_json" "$target" "$uid"
elif [ -f "$path" ]; then
  sha="$(sha256sum -- "$path" | awk '{{print $1}}')"
  size="$(wc -c <"$path" | tr -d ' ')"
  printf '{{"exists":true,"gid":%s,"group":"%s","mode":"%s","owner":"%s","path":"%s","sha256":"sha256:%s","size":%s,"type":"file","uid":%s}}\\n' "$gid" "$group" "$mode" "$owner" "$path_json" "$sha" "$size" "$uid"
elif [ -d "$path" ]; then
  printf '{{"exists":true,"gid":%s,"group":"%s","mode":"%s","owner":"%s","path":"%s","type":"directory","uid":%s}}\\n' "$gid" "$group" "$mode" "$owner" "$path_json" "$uid"
else
  printf '{{"exists":true,"gid":%s,"group":"%s","mode":"%s","owner":"%s","path":"%s","type":"other","uid":%s}}\\n' "$gid" "$group" "$mode" "$owner" "$path_json" "$uid"
fi"""


def apply_command(path, content, mode, owner, group, create_parents):
    content_b64 = base64.b64encode(content).decode("ascii")
    create = "1" if create_parents else "0"
    return f"""set -eu
{shell_helpers()}
path={shell_quote(path)}
content_b64={shell_quote(content_b64)}
mode={shell_quote(mode)}
owner={shell_quote(owner or "")}
group={shell_quote(group or "")}
create_parents={create}
if [ -e "$path" ] || [ -L "$path" ]; then
  if [ ! -f "$path" ]; then
    printf '{{"error":"path exists and is not a regular file","ok":false}}\\n'
    exit 0
  fi
fi
parent="${{path%/*}}"
if [ "$parent" = "$path" ]; then
  parent="."
elif [ -z "$parent" ]; then
  parent="/"
fi
if [ "$create_parents" = "1" ]; then
  mkdir -p -- "$parent"
elif [ ! -d "$parent" ]; then
  printf '{{"error":"parent directory does not exist","ok":false}}\\n'
  exit 0
fi
tmp="${{path}}.torque-$$.tmp"
trap 'rm -f -- "$tmp"' EXIT HUP INT TERM
if ! printf '%s' "$content_b64" | base64_decode >"$tmp"; then
  printf '{{"error":"content base64 decode failed","ok":false}}\\n'
  exit 0
fi
chmod "$mode" "$tmp"
if [ -n "$owner" ] || [ -n "$group" ]; then
  if [ -n "$owner" ] && [ -n "$group" ]; then
    chown_spec="${{owner}}:${{group}}"
  elif [ -n "$owner" ]; then
    chown_spec="$owner"
  else
    chown_spec=":${{group}}"
  fi
  chown "$chown_spec" "$tmp"
fi
mv -f -- "$tmp" "$path"
trap - EXIT HUP INT TERM
chmod "$mode" "$path"
if [ -n "$owner" ] || [ -n "$group" ]; then
  chown "$chown_spec" "$path"
fi
printf '{{"ok":true}}\\n'"""


def delete_command(path):
    return f"""set -eu
path={shell_quote(path)}
if [ ! -e "$path" ] && [ ! -L "$path" ]; then
  printf '{{"changed":false,"ok":true}}\\n'
  exit 0
fi
if [ -d "$path" ] && [ ! -L "$path" ]; then
  printf '{{"error":"refusing to delete directory","ok":false}}\\n'
  exit 0
fi
rm -f -- "$path"
printf '{{"changed":true,"ok":true}}\\n'"""


def observe(path, cfg):
    state, op = remote_json(observe_command(path), cfg)
    return state, op


def compare_state(state, desired, command):
    want_present = command != "delete"
    changes = {
        "create": want_present and not state.get("exists", False),
        "delete": (not want_present) and state.get("exists", False),
        "type": want_present and state.get("exists", False) and state.get("type") != "file",
        "content": False,
        "mode": False,
        "owner": False,
        "group": False,
    }
    if want_present and state.get("type") == "file":
        changes["content"] = state.get("sha256") != desired["sha256"]
        changes["mode"] = state.get("mode") != desired["mode"]
        if desired.get("owner"):
            changes["owner"] = str(state.get("owner", "")) != str(desired["owner"]) and str(state.get("uid", "")) != str(desired["owner"])
        if desired.get("group"):
            changes["group"] = str(state.get("group", "")) != str(desired["group"]) and str(state.get("gid", "")) != str(desired["group"])
    return changes, any(changes.values())


def desired_from_inputs(inputs):
    path = str(inputs.get("path") or "").strip()
    if path == "":
        raise ValueError("input.path is required")
    content = desired_content(inputs)
    return {
        "path": path,
        "content": content,
        "sha256": "sha256:" + hashlib.sha256(content).hexdigest(),
        "mode": normalize_mode(inputs.get("mode")),
        "owner": str(inputs.get("owner") or "").strip(),
        "group": str(inputs.get("group") or "").strip(),
        "createParents": bool_input(inputs.get("createParents"), True),
    }


def config_from_inputs(inputs):
    transport = str(inputs.get("transport") or "nats").strip().lower()
    if transport not in ("ssh", "nats"):
        raise ValueError("host.file.ensure supports transport: ssh or transport: nats")
    target = str(inputs.get("target") or "").strip()
    target_env = str(inputs.get("targetEnv") or "").strip()
    if target_env:
        target = os.environ.get(target_env, target)
    if transport == "nats":
        target = normalize_target(target)
    elif target.startswith("ssh://"):
        target = target[len("ssh://"):]
    if target == "":
        raise ValueError("input.target or input.targetEnv is required")
    timeout = float(inputs.get("timeoutSeconds") or 30)
    if timeout <= 0:
        raise ValueError("timeoutSeconds must be greater than zero")
    if transport == "ssh":
        ssh_options = inputs.get("sshOptions") or inputs.get("ssh_options") or []
        if isinstance(ssh_options, str):
            ssh_options = shlex.split(ssh_options)
        if not isinstance(ssh_options, list):
            raise ValueError("sshOptions must be a string or list")
        ssh_options = [str(item) for item in ssh_options]
        identity_file = str(inputs.get("identityFile") or inputs.get("identity_file") or "").strip()
        return {
            "transport": transport,
            "target": target,
            "identityFile": identity_file,
            "sshOptions": ssh_options,
            "timeoutSeconds": timeout,
        }
    nats_url = str(inputs.get("natsUrl") or "").strip()
    nats_url_env = str(inputs.get("natsUrlEnv") or "").strip()
    if nats_url_env:
        nats_url = os.environ.get(nats_url_env, nats_url)
    if nats_url == "":
        nats_url = os.environ.get("TORQUE_NATS_URL") or os.environ.get("TORQUE_NATS_SERVER") or DEFAULT_NATS_URL
    return {"transport": transport, "target": target, "natsUrl": nats_url, "timeoutSeconds": timeout}


def main():
    req = json.load(sys.stdin)
    phase = str(req.get("phase") or "").strip()
    command = str(req.get("command") or "apply").strip() or "apply"
    inputs = req.get("input") or {}
    try:
        desired = desired_from_inputs(inputs)
        cfg = config_from_inputs(inputs)
        path_digest = digest(desired["path"])
        base_evidence = {
            "transport": cfg["transport"],
            "targetDigest": digest(cfg["target"]),
            "pathDigest": path_digest,
            "desiredDigest": desired["sha256"],
        }
        if phase == "observe":
            state, op = observe(desired["path"], cfg)
            emit({
                "status": "succeeded",
                "message": "host file observed",
                "before": state,
                "evidence": dict(base_evidence, operation=op),
            })
            return
        if phase == "diff":
            state, op = observe(desired["path"], cfg)
            changes, changed = compare_state(state, desired, command)
            emit({
                "status": "planned",
                "changed": changed,
                "before": state,
                "diff": {"changes": changes, "targetExists": command != "delete"},
                "evidence": dict(base_evidence, operation=op),
            })
            return
        if phase == "plan":
            state, op = observe(desired["path"], cfg)
            changes, changed = compare_state(state, desired, command)
            blocked = False
            message = "host file change is safe to reconcile"
            if command != "delete" and state.get("exists") and state.get("type") not in ("file",):
                blocked = True
                message = "path exists and is not a regular file"
            if command == "delete" and state.get("type") == "directory":
                blocked = True
                message = "refusing to delete directory"
            emit({
                "status": "blocked" if blocked else "planned",
                "changed": changed,
                "safeToRun": not blocked,
                "risk": "low" if not blocked else "blocked",
                "message": message,
                "before": state,
                "diff": {"changes": changes, "targetExists": command != "delete"},
                "evidence": dict(base_evidence, operation=op),
            })
            return
        if phase == "apply":
            before, before_op = observe(desired["path"], cfg)
            changes, changed = compare_state(before, desired, "apply")
            if not changed:
                emit({
                    "status": "noop",
                    "changed": False,
                    "message": "host file already in desired state",
                    "before": before,
                    "after": before,
                    "diff": {"changes": changes},
                    "evidence": dict(base_evidence, observe=before_op),
                    "receipt": {"pathDigest": path_digest, "desiredDigest": desired["sha256"], "mode": desired["mode"], "transport": cfg["transport"]},
                })
                return
            doc, apply_op = remote_json(apply_command(
                desired["path"],
                desired["content"],
                desired["mode"],
                desired["owner"],
                desired["group"],
                desired["createParents"],
            ), cfg)
            if not doc.get("ok"):
                emit({"status": "failed", "message": str(doc.get("error") or "apply failed"), "before": before, "evidence": dict(base_evidence, operation=apply_op)})
                return
            after, after_op = observe(desired["path"], cfg)
            _, still_changed = compare_state(after, desired, "apply")
            emit({
                "status": "succeeded" if not still_changed else "failed",
                "changed": changed,
                "message": "host file reconciled" if not still_changed else "host file verify after apply failed",
                "before": before,
                "after": after,
                "diff": {"changes": changes},
                "evidence": dict(base_evidence, observe=before_op, apply=apply_op, verify=after_op),
                "receipt": {"pathDigest": path_digest, "desiredDigest": desired["sha256"], "mode": desired["mode"], "transport": cfg["transport"]},
            })
            return
        if phase == "delete":
            before, before_op = observe(desired["path"], cfg)
            doc, delete_op = remote_json(delete_command(desired["path"]), cfg)
            if not doc.get("ok"):
                emit({"status": "failed", "message": str(doc.get("error") or "delete failed"), "before": before, "evidence": dict(base_evidence, operation=delete_op)})
                return
            after, after_op = observe(desired["path"], cfg)
            emit({
                "status": "succeeded",
                "changed": bool(doc.get("changed", False)),
                "message": "host file removed" if doc.get("changed") else "host file already absent",
                "before": before,
                "after": after,
                "evidence": dict(base_evidence, observe=before_op, delete=delete_op, verify=after_op),
                "receipt": {"pathDigest": path_digest, "deleted": True, "transport": cfg["transport"]},
            })
            return
        if phase == "verify":
            state, op = observe(desired["path"], cfg)
            if command == "delete":
                ok = not state.get("exists", False)
                message = "host file absent" if ok else "host file still exists"
            else:
                _, changed = compare_state(state, desired, command)
                ok = not changed
                message = "host file verified" if ok else "host file drift detected"
            emit({
                "status": "succeeded" if ok else "failed",
                "message": message,
                "after": state,
                "evidence": dict(base_evidence, operation=op),
                "receipt": {"pathDigest": path_digest, "verified": ok, "transport": cfg["transport"]},
            })
            return
        emit({"status": "failed", "message": "unsupported phase: " + phase})
    except Exception as err:
        emit({"status": "failed", "message": str(err)})


if __name__ == "__main__":
    main()
