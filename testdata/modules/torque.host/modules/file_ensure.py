#!/usr/bin/env python3
import base64
import hashlib
import json
import os
import re
import socket
import ssl
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


def observe_command(path):
    code = f"""
import hashlib,json,os,pwd,grp,stat,sys
path = {json.dumps(path)}
state = {{"path": path, "exists": os.path.lexists(path)}}
if not state["exists"]:
    state["type"] = "absent"
    print(json.dumps(state, sort_keys=True))
    sys.exit(0)
st = os.lstat(path)
mode = stat.S_IMODE(st.st_mode)
state.update({{"mode": format(mode, "04o"), "uid": st.st_uid, "gid": st.st_gid}})
try:
    state["owner"] = pwd.getpwuid(st.st_uid).pw_name
except KeyError:
    state["owner"] = str(st.st_uid)
try:
    state["group"] = grp.getgrgid(st.st_gid).gr_name
except KeyError:
    state["group"] = str(st.st_gid)
if stat.S_ISREG(st.st_mode):
    h = hashlib.sha256()
    with open(path, "rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            h.update(chunk)
    state.update({{"type": "file", "sha256": "sha256:" + h.hexdigest(), "size": st.st_size}})
elif stat.S_ISDIR(st.st_mode):
    state["type"] = "directory"
elif stat.S_ISLNK(st.st_mode):
    state.update({{"type": "symlink", "target": os.readlink(path)}})
else:
    state["type"] = "other"
print(json.dumps(state, sort_keys=True))
"""
    return "python3 - <<'PY'\n" + code.strip() + "\nPY"


def apply_command(path, content, mode, owner, group, create_parents):
    code = f"""
import base64,json,os,pwd,grp,stat,sys
path = {json.dumps(path)}
content = base64.b64decode({json.dumps(base64.b64encode(content).decode("ascii"))})
mode = int({json.dumps(mode)}, 8)
owner = {json.dumps(owner or "")}
group = {json.dumps(group or "")}
create_parents = {repr(bool(create_parents))}
def resolve_user(value):
    if value == "":
        return -1
    try:
        return int(value)
    except ValueError:
        return pwd.getpwnam(value).pw_uid
def resolve_group(value):
    if value == "":
        return -1
    try:
        return int(value)
    except ValueError:
        return grp.getgrnam(value).gr_gid
if os.path.lexists(path) and not os.path.isfile(path):
    print(json.dumps({{"ok": False, "error": "path exists and is not a regular file"}}))
    sys.exit(0)
parent = os.path.dirname(path) or "."
if create_parents:
    os.makedirs(parent, exist_ok=True)
elif not os.path.isdir(parent):
    print(json.dumps({{"ok": False, "error": "parent directory does not exist"}}))
    sys.exit(0)
uid = resolve_user(owner)
gid = resolve_group(group)
tmp = path + ".torque-" + str(os.getpid()) + ".tmp"
with open(tmp, "wb") as fh:
    fh.write(content)
os.chmod(tmp, mode)
if uid != -1 or gid != -1:
    os.chown(tmp, uid, gid)
os.replace(tmp, path)
os.chmod(path, mode)
if uid != -1 or gid != -1:
    os.chown(path, uid, gid)
print(json.dumps({{"ok": True}}))
"""
    return "python3 - <<'PY'\n" + code.strip() + "\nPY"


def delete_command(path):
    code = f"""
import json,os,stat,sys
path = {json.dumps(path)}
if not os.path.lexists(path):
    print(json.dumps({{"ok": True, "changed": False}}))
    sys.exit(0)
st = os.lstat(path)
if stat.S_ISDIR(st.st_mode):
    print(json.dumps({{"ok": False, "error": "refusing to delete directory"}}))
    sys.exit(0)
os.unlink(path)
print(json.dumps({{"ok": True, "changed": True}}))
"""
    return "python3 - <<'PY'\n" + code.strip() + "\nPY"


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
    if transport != "nats":
        raise ValueError("host.file.ensure currently supports transport: nats")
    target = str(inputs.get("target") or "").strip()
    target_env = str(inputs.get("targetEnv") or "").strip()
    if target_env:
        target = os.environ.get(target_env, target)
    target = normalize_target(target)
    if target == "":
        raise ValueError("input.target or input.targetEnv is required for transport: nats")
    nats_url = str(inputs.get("natsUrl") or "").strip()
    nats_url_env = str(inputs.get("natsUrlEnv") or "").strip()
    if nats_url_env:
        nats_url = os.environ.get(nats_url_env, nats_url)
    if nats_url == "":
        nats_url = os.environ.get("TORQUE_NATS_URL") or os.environ.get("TORQUE_NATS_SERVER") or DEFAULT_NATS_URL
    timeout = float(inputs.get("timeoutSeconds") or 30)
    if timeout <= 0:
        raise ValueError("timeoutSeconds must be greater than zero")
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
            "transport": "nats",
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
                    "receipt": {"pathDigest": path_digest, "desiredDigest": desired["sha256"], "mode": desired["mode"], "transport": "nats"},
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
                "receipt": {"pathDigest": path_digest, "desiredDigest": desired["sha256"], "mode": desired["mode"], "transport": "nats"},
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
                "receipt": {"pathDigest": path_digest, "deleted": True, "transport": "nats"},
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
                "receipt": {"pathDigest": path_digest, "verified": ok, "transport": "nats"},
            })
            return
        emit({"status": "failed", "message": "unsupported phase: " + phase})
    except Exception as err:
        emit({"status": "failed", "message": str(err)})


if __name__ == "__main__":
    main()
