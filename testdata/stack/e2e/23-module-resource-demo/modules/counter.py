#!/usr/bin/env python3
import json
import os
import pathlib
import sys


def emit(doc):
    sys.stdout.write(json.dumps(doc, separators=(",", ":")))


req = json.load(sys.stdin)
phase = req.get("phase", "")
command = req.get("command", "apply")
inputs = req.get("input") or {}
path = pathlib.Path(inputs.get("path") or "/tmp/torque-module-resource-demo-counter.txt")
desired = str(inputs.get("value") or "ready")
exists = path.exists()
current = path.read_text().strip() if exists else ""

if phase == "observe":
    emit({
        "status": "succeeded",
        "message": "observed",
        "before": {"exists": exists, "value": current},
        "evidence": {"path": str(path)},
    })
elif phase == "diff":
    target_exists = command != "delete"
    changed = exists != target_exists or (target_exists and current != desired)
    emit({
        "status": "planned",
        "changed": changed,
        "diff": {"exists": exists, "targetExists": target_exists, "valueChanged": current != desired},
    })
elif phase == "plan":
    emit({
        "status": "planned",
        "safeToRun": True,
        "risk": "low",
        "message": "counter resource is safe to reconcile",
    })
elif phase == "apply":
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(desired + "\n")
    emit({
        "status": "succeeded",
        "changed": current != desired,
        "after": {"exists": True, "value": desired},
        "receipt": {"path": str(path), "value": desired},
    })
elif phase == "delete":
    if exists:
        path.unlink()
    emit({
        "status": "succeeded",
        "changed": exists,
        "after": {"exists": False},
        "receipt": {"path": str(path), "deleted": True},
    })
elif phase == "verify":
    if command == "delete":
        ok = not path.exists()
        emit({
            "status": "succeeded" if ok else "failed",
            "message": "counter absent" if ok else "counter still exists",
            "receipt": {"path": str(path), "absent": ok},
        })
    else:
        ok = path.exists() and path.read_text().strip() == desired
        emit({
            "status": "succeeded" if ok else "failed",
            "message": "counter verified" if ok else "counter mismatch",
            "receipt": {"path": str(path), "value": desired, "verified": ok},
        })
else:
    emit({"status": "failed", "message": "unsupported phase: " + phase})
    sys.exit(1)
