#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/e2e/terraform-provider-100.sh [options]

Generates a stack with 100 Terraform/OpenTofu-backed module nodes, runs apply
and delete through torque terraform-adapter, validates audit artifacts, and
proves no fake provider state remains.

Options:
  --count N       Number of module nodes. Default: 100.
  --concurrency N Stack runner concurrency. Default: 20.
  --workdir DIR  Working directory. Defaults to a temp directory.
  -h, --help     Show this help.

Environment:
  TORQUE_BIN     Path to torque. Defaults to ./bin/torque after make build.
EOF
}

count="${TORQUE_TERRAFORM_PROVIDER_E2E_COUNT:-100}"
concurrency="${TORQUE_TERRAFORM_PROVIDER_E2E_CONCURRENCY:-20}"
workdir="${WORKDIR:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --count)
      [[ $# -ge 2 ]] || { echo "--count requires a value" >&2; exit 1; }
      count="$2"
      shift 2
      ;;
    --concurrency)
      [[ $# -ge 2 ]] || { echo "--concurrency requires a value" >&2; exit 1; }
      concurrency="$2"
      shift 2
      ;;
    --workdir)
      [[ $# -ge 2 ]] || { echo "--workdir requires a value" >&2; exit 1; }
      workdir="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

case "${count}" in
  ''|*[!0-9]*) echo "--count must be a positive integer" >&2; exit 1 ;;
esac
case "${concurrency}" in
  ''|*[!0-9]*) echo "--concurrency must be a positive integer" >&2; exit 1 ;;
esac
if [[ "${count}" -le 0 || "${concurrency}" -le 0 ]]; then
  echo "--count and --concurrency must be positive" >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo_root="$(cd "${repo_root}/.." && pwd)"
cd "${repo_root}"

command -v python3 >/dev/null 2>&1 || { echo "missing python3" >&2; exit 1; }
command -v make >/dev/null 2>&1 || { echo "missing make" >&2; exit 1; }

make build >/dev/null
torque_bin="${TORQUE_BIN:-${repo_root}/bin/torque}"
if [[ -z "${workdir}" ]]; then
  workdir="$(mktemp -d "${TMPDIR:-/tmp}/torque-terraform-provider-100.XXXXXX")"
fi
mkdir -p "${workdir}"

fake_tf="${workdir}/terraform-fake.py"
stack_dir="${workdir}/stack"
mkdir -p "${stack_dir}"

cat > "${fake_tf}" <<'PY'
#!/usr/bin/env python3
import json
import os
import re
import sys
from pathlib import Path


def resource_id():
    raw = Path("main.tf").read_text(encoding="utf-8") if Path("main.tf").exists() else ""
    match = re.search(r'name\s*=\s*"([^"]+)"', raw)
    return match.group(1) if match else Path.cwd().name


def state_path():
    return Path("terraform.tfstate")


def marker_path():
    return Path("applied.marker")


def plan_mode(path):
    if not path:
        return "create"
    body = Path(path).read_text(encoding="utf-8") if Path(path).exists() else ""
    if "destroy" in body:
        return "destroy"
    if "noop" in body:
        return "noop"
    return "create"


def write_state(resources):
    body = {"version": 4, "serial": 1, "resources": resources}
    state_path().write_text(json.dumps(body, sort_keys=True), encoding="utf-8")


def show_state():
    rid = resource_id()
    resources = []
    if marker_path().exists():
        resources.append({
            "address": "fake_bucket.this",
            "mode": "managed",
            "type": "fake_bucket",
            "name": "this",
            "values": {"name": rid},
        })
    print(json.dumps({"values": {"root_module": {"resources": resources}}}, sort_keys=True))


def show_plan(path):
    rid = resource_id()
    mode = plan_mode(path)
    actions = []
    if mode == "create":
        actions = ["create"]
    elif mode == "destroy":
        actions = ["delete"]
    else:
        actions = ["no-op"]
    print(json.dumps({
        "terraform_version": "fake-1.0.0",
        "resource_changes": [{
            "address": "fake_bucket.this",
            "mode": "managed",
            "type": "fake_bucket",
            "name": "this",
            "provider_name": "example.test/torque/fake",
            "change": {"actions": actions, "after": {"name": rid}},
        }],
    }, sort_keys=True))


def main():
    args = sys.argv[1:]
    cmd = args[0] if args else ""
    rest = args[1:]

    if cmd == "init":
        Path(".terraform").mkdir(exist_ok=True)
        Path(".terraform.lock.hcl").write_text("# fake lock\n", encoding="utf-8")
        return 0

    if cmd == "plan":
        destroy = "-destroy" in rest
        out = None
        for arg in rest:
            if arg.startswith("-out="):
                out = arg.split("=", 1)[1]
        if destroy:
            mode = "destroy" if marker_path().exists() else "noop"
        else:
            mode = "noop" if marker_path().exists() else "create"
        if out:
            Path(out).write_text(mode, encoding="utf-8")
        return 0 if mode == "noop" else 2

    if cmd == "show":
        target = rest[-1] if rest and not rest[-1].startswith("-") else ""
        if target.endswith(".tfplan"):
            show_plan(target)
        else:
            show_state()
        return 0

    if cmd == "apply":
        target = rest[-1] if rest else ""
        mode = plan_mode(target)
        rid = resource_id()
        if mode == "destroy":
            marker_path().unlink(missing_ok=True)
            write_state([])
            print("fake_bucket.this: Destruction complete")
            print("Apply complete! Resources: 0 added, 0 changed, 1 destroyed.")
            return 0
        if mode == "noop":
            print("Apply complete! Resources: 0 added, 0 changed, 0 destroyed.")
            return 0
        marker_path().write_text(rid, encoding="utf-8")
        write_state([{"address": "fake_bucket.this", "type": "fake_bucket", "name": "this"}])
        print("fake_bucket.this: Creation complete")
        print("Apply complete! Resources: 1 added, 0 changed, 0 destroyed.")
        return 0

    print(f"unexpected fake terraform command: {cmd}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
PY
chmod +x "${fake_tf}"

python3 - "${stack_dir}/stack.yaml" "${torque_bin}" "${fake_tf}" "${count}" <<'PY'
import sys
path, torque, fake, raw_count = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
with open(path, "w", encoding="utf-8") as f:
    f.write("""apiVersion: torque.dev/v1
kind: Stack
name: terraform-provider-100
defaults:
  cluster:
    name: local
nodes:
""")
    for idx in range(1, raw_count + 1):
        f.write(f"""  - name: fake-bucket-{idx:03d}
    kind: fake.bucket.ensure
    module:
      source: oci://example.test/torque-modules/terraform-fake
      version: 0.1.0
      command: [{torque!r}, "terraform-adapter", "--terraform-bin", {fake!r}]
      timeout: 2m
      input:
        provider:
          source: example.test/torque/fake
          version: "1.0.0"
          localName: fake
        resource:
          type: fake_bucket
          name: this
          values:
            name: fake-bucket-{idx:03d}
            label: torque-provider-100
""")
PY

"${torque_bin}" stack apply --config "${stack_dir}" --yes --concurrency "${concurrency}" >"${workdir}/stack-apply.log" 2>&1
"${torque_bin}" stack audit --config "${stack_dir}" --output json --include-artifacts >"${workdir}/stack-apply-audit.json"

python3 - "${workdir}/stack-apply-audit.json" "${count}" <<'PY'
import json
import sys
doc = json.load(open(sys.argv[1], encoding="utf-8"))
expected = int(sys.argv[2])
if doc.get("status") != "succeeded":
    raise SystemExit(f"apply audit status={doc.get('status')!r}")
artifacts = doc.get("artifacts", [])
by_name = {}
for artifact in artifacts:
    by_name.setdefault(artifact.get("name"), set()).add(artifact.get("nodeId"))
for name in ["module-plan.json", "module-apply.json", "module-verify.json", "terraform-plan-summary.json", "terraform-plan-metadata.json", "terraform-state-summary.json"]:
    got = len(by_name.get(name, set()))
    if got != expected:
        raise SystemExit(f"apply audit artifact {name} count={got}, want {expected}")
state_bodies = [json.loads(a.get("body") or "{}") for a in artifacts if a.get("name") == "terraform-state-summary.json"]
if sum((body.get("value") or {}).get("resources", 0) for body in state_bodies) != expected:
    raise SystemExit("apply audit did not prove one fake resource per node")
text = json.dumps(artifacts)
for forbidden in ["stdoutTail", "stderrTail", "secret_key", "session_token"]:
    if forbidden in text:
        raise SystemExit(f"forbidden audit text leaked: {forbidden}")
PY

"${torque_bin}" stack delete --config "${stack_dir}" --yes --concurrency "${concurrency}" --delete-confirm-threshold 0 >"${workdir}/stack-delete.log" 2>&1
"${torque_bin}" stack audit --config "${stack_dir}" --output json --include-artifacts >"${workdir}/stack-delete-audit.json"

python3 - "${workdir}/stack-delete-audit.json" "${count}" <<'PY'
import json
import sys
doc = json.load(open(sys.argv[1], encoding="utf-8"))
expected = int(sys.argv[2])
if doc.get("status") != "succeeded":
    raise SystemExit(f"delete audit status={doc.get('status')!r}")
artifacts = doc.get("artifacts", [])
by_name = {}
for artifact in artifacts:
    by_name.setdefault(artifact.get("name"), set()).add(artifact.get("nodeId"))
for name in ["module-plan.json", "module-delete.json", "module-verify.json", "terraform-plan-summary.json", "terraform-plan-metadata.json", "terraform-state-summary.json"]:
    got = len(by_name.get(name, set()))
    if got != expected:
        raise SystemExit(f"delete audit artifact {name} count={got}, want {expected}")
for artifact in artifacts:
    if artifact.get("name") != "terraform-state-summary.json":
        continue
    body = json.loads(artifact.get("body") or "{}")
    if (body.get("value") or {}).get("resources") != 0:
        raise SystemExit(f"delete audit has non-empty fake state for {artifact.get('nodeId')}")
text = json.dumps(artifacts)
for forbidden in ["stdoutTail", "stderrTail", "secret_key", "session_token"]:
    if forbidden in text:
        raise SystemExit(f"forbidden audit text leaked: {forbidden}")
PY

echo "terraform_provider_100_e2e_ok count=${count} concurrency=${concurrency} workdir=${workdir}"
