#!/usr/bin/env python3
"""Validate Torque ops E2E evidence contract."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys
import tarfile
from pathlib import Path
from typing import Any


REQUIRED_FILES = [
    "metadata.json",
    "target-snapshot.json",
    "decision.json",
    "verification/receipt.json",
    "cleanup/receipt.json",
    "result.json",
    "redaction-report.json",
]


def load_json(path: Path, errors: list[str]) -> dict[str, Any]:
    try:
        with path.open("r", encoding="utf-8") as f:
            value = json.load(f)
    except FileNotFoundError:
        errors.append(f"missing file: {path}")
        return {}
    except json.JSONDecodeError as exc:
        errors.append(f"invalid json: {path}: {exc}")
        return {}
    if not isinstance(value, dict):
        errors.append(f"json root must be object: {path}")
        return {}
    return value


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def relative_files(root: Path) -> list[str]:
    out: list[str] = []
    for base, _, files in os.walk(root):
        for name in files:
            path = Path(base) / name
            rel = path.relative_to(root).as_posix()
            if rel == "manifest.json":
                continue
            out.append(rel)
    return sorted(out)


def validate_common(run_dir: Path, expected_task_id: str | None, expected_run_id: str | None, errors: list[str]) -> tuple[str, str]:
    for rel in REQUIRED_FILES:
        path = run_dir / rel
        if not path.is_file():
            errors.append(f"missing required artifact: {rel}")

    metadata = load_json(run_dir / "metadata.json", errors)
    task_id = str(metadata.get("taskId") or "")
    run_id = str(metadata.get("runId") or "")
    if not task_id:
        errors.append("metadata.json missing taskId")
    if not run_id:
        errors.append("metadata.json missing runId")
    if not metadata.get("startedAt"):
        errors.append("metadata.json missing startedAt")
    if expected_task_id and task_id != expected_task_id:
        errors.append(f"metadata.json taskId mismatch: got {task_id!r}, want {expected_task_id!r}")
    if expected_run_id and run_id != expected_run_id:
        errors.append(f"metadata.json runId mismatch: got {run_id!r}, want {expected_run_id!r}")

    target_snapshot = load_json(run_dir / "target-snapshot.json", errors)
    targets = target_snapshot.get("targets")
    if not isinstance(targets, list) or not targets:
        errors.append("target-snapshot.json must contain a non-empty targets array")

    decision = load_json(run_dir / "decision.json", errors)
    if not decision.get("decision"):
        errors.append("decision.json missing decision")
    if decision.get("status") not in {"succeeded", "passed", "allowed", "blocked", "noop"}:
        errors.append("decision.json status must be succeeded, passed, allowed, blocked, or noop")

    verification = load_json(run_dir / "verification" / "receipt.json", errors)
    if verification.get("status") != "succeeded":
        errors.append("verification/receipt.json status must be succeeded")

    cleanup = load_json(run_dir / "cleanup" / "receipt.json", errors)
    if cleanup.get("status") != "succeeded":
        errors.append("cleanup/receipt.json status must be succeeded")

    result = load_json(run_dir / "result.json", errors)
    if result.get("status") != "succeeded":
        errors.append("result.json status must be succeeded")
    if task_id and result.get("taskId") != task_id:
        errors.append("result.json taskId must match metadata.json")
    if run_id and result.get("runId") != run_id:
        errors.append("result.json runId must match metadata.json")

    redaction = load_json(run_dir / "redaction-report.json", errors)
    if redaction.get("status") != "passed":
        errors.append("redaction-report.json status must be passed")
    if redaction.get("secretMaterialFound") is not False:
        errors.append("redaction-report.json secretMaterialFound must be false")

    return task_id, run_id


def validate_manifest(run_dir: Path, task_id: str, run_id: str, errors: list[str]) -> None:
    manifest_path = run_dir / "manifest.json"
    manifest = load_json(manifest_path, errors)
    if not manifest:
        return
    if manifest.get("apiVersion") != "torque.dev/e2e/v1":
        errors.append("manifest.json apiVersion must be torque.dev/e2e/v1")
    if manifest.get("kind") != "OpsLabEvidenceManifest":
        errors.append("manifest.json kind must be OpsLabEvidenceManifest")
    if task_id and manifest.get("taskId") != task_id:
        errors.append("manifest.json taskId must match metadata.json")
    if run_id and manifest.get("runId") != run_id:
        errors.append("manifest.json runId must match metadata.json")

    artifacts = manifest.get("artifacts")
    if not isinstance(artifacts, list):
        errors.append("manifest.json artifacts must be an array")
        return
    if manifest.get("artifactCount") != len(artifacts):
        errors.append("manifest.json artifactCount must equal artifacts length")

    by_path: dict[str, dict[str, Any]] = {}
    for artifact in artifacts:
        if not isinstance(artifact, dict):
            errors.append("manifest.json artifacts entries must be objects")
            continue
        rel = artifact.get("path")
        if not isinstance(rel, str) or not rel:
            errors.append("manifest artifact missing path")
            continue
        if rel in by_path:
            errors.append(f"duplicate manifest artifact path: {rel}")
        by_path[rel] = artifact

    for rel in REQUIRED_FILES:
        if rel not in by_path:
            errors.append(f"manifest missing required artifact: {rel}")

    actual = relative_files(run_dir)
    manifest_paths = sorted(by_path)
    if actual != manifest_paths:
        missing = sorted(set(actual) - set(manifest_paths))
        stale = sorted(set(manifest_paths) - set(actual))
        if missing:
            errors.append("manifest missing actual files: " + ", ".join(missing[:20]))
        if stale:
            errors.append("manifest references missing files: " + ", ".join(stale[:20]))

    for rel, artifact in by_path.items():
        path = run_dir / rel
        if not path.is_file():
            continue
        size = path.stat().st_size
        digest = sha256_file(path)
        if artifact.get("size") != size:
            errors.append(f"manifest size mismatch for {rel}: got {artifact.get('size')}, want {size}")
        if artifact.get("sha256") != digest:
            errors.append(f"manifest sha256 mismatch for {rel}")


def validate_bundle(run_dir: Path, bundle: Path, errors: list[str]) -> None:
    if not bundle.is_file():
        errors.append(f"missing evidence bundle: {bundle}")
        return
    if bundle.stat().st_size <= 0:
        errors.append(f"empty evidence bundle: {bundle}")
        return
    root_name = run_dir.name.rstrip("/")
    required = {f"{root_name}/{rel}" for rel in REQUIRED_FILES + ["manifest.json"]}
    try:
        with tarfile.open(bundle, "r:gz") as tar:
            names = {member.name.rstrip("/") for member in tar.getmembers() if member.isfile()}
    except (tarfile.TarError, OSError) as exc:
        errors.append(f"invalid evidence bundle: {bundle}: {exc}")
        return
    missing = sorted(required - names)
    if missing:
        errors.append("bundle missing required entries: " + ", ".join(missing))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run-dir", required=True, type=Path)
    parser.add_argument("--bundle", type=Path)
    parser.add_argument("--task-id")
    parser.add_argument("--run-id")
    parser.add_argument("--skip-manifest", action="store_true")
    parser.add_argument("--skip-bundle", action="store_true")
    args = parser.parse_args()

    errors: list[str] = []
    run_dir = args.run_dir
    if not run_dir.is_dir():
        errors.append(f"missing run directory: {run_dir}")
        task_id = ""
        run_id = ""
    else:
        task_id, run_id = validate_common(run_dir, args.task_id, args.run_id, errors)
        if not args.skip_manifest:
            validate_manifest(run_dir, task_id, run_id, errors)
        if not args.skip_bundle:
            if args.bundle is None:
                errors.append("missing --bundle")
            else:
                validate_bundle(run_dir, args.bundle, errors)

    report = {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsLabEvidenceValidation",
        "status": "failed" if errors else "passed",
        "runDir": str(run_dir),
        "bundle": str(args.bundle) if args.bundle else "",
        "errors": errors,
    }
    json.dump(report, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write("\n")
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
