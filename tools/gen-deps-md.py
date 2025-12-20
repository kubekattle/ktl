#!/usr/bin/env python3

import json
import subprocess
from collections import defaultdict

MODULE = "github.com/example/ktl"


def run_go_list():
    # Package list for this module.
    cmd = ["go", "list", "-deps", "-json", "./..."]
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    stdout, stderr = proc.communicate()
    if proc.returncode != 0:
        raise SystemExit(stderr.strip() or f"go list failed: {proc.returncode}")
    # go list -json outputs a stream of JSON objects.
    decoder = json.JSONDecoder()
    i = 0
    packages = []
    while i < len(stdout):
        # skip whitespace
        while i < len(stdout) and stdout[i].isspace():
            i += 1
        if i >= len(stdout):
            break
        obj, j = decoder.raw_decode(stdout, i)
        packages.append(obj)
        i = j
    return packages


def main():
    packages = run_go_list()

    internal_deps_by_pkg = defaultdict(set)
    stdlib_deps_by_pkg = defaultdict(set)
    third_party_deps_by_pkg = defaultdict(set)

    for pkg in packages:
        imp_path = pkg.get("ImportPath", "")
        if not imp_path.startswith(MODULE):
            continue

        for dep in pkg.get("Deps", []) or []:
            if dep.startswith(MODULE):
                if dep != imp_path:
                    internal_deps_by_pkg[imp_path].add(dep)
            else:
                # Heuristic: third-party deps have a dot in the first path segment.
                first = dep.split("/", 1)[0]
                if "." in first:
                    third_party_deps_by_pkg[imp_path].add(dep)
                else:
                    stdlib_deps_by_pkg[imp_path].add(dep)

    pkgs = sorted(set(list(internal_deps_by_pkg.keys()) + list(stdlib_deps_by_pkg.keys()) + list(third_party_deps_by_pkg.keys())))

    out = []
    out.append("# Dependency Map (Generated)\n")
    out.append("\n")
    out.append("This file is generated. Do not edit by hand.\n")
    out.append("\n")
    out.append("Regenerate with:\n")
    out.append("\n")
    out.append("```bash\n")
    out.append("make deps\n")
    out.append("```\n")
    out.append("\n")

    for p in pkgs:
        out.append(f"## `{p}`\n\n")

        internal = sorted(internal_deps_by_pkg.get(p, set()))
        stdlib = sorted(stdlib_deps_by_pkg.get(p, set()))
        third_party = sorted(third_party_deps_by_pkg.get(p, set()))

        out.append("**Internal deps**\n\n")
        if internal:
            for d in internal:
                out.append(f"- `{d}`\n")
        else:
            out.append("- (none)\n")
        out.append("\n")

        out.append("**Third-party deps**\n\n")
        if third_party:
            cap = 80
            for d in third_party[:cap]:
                out.append(f"- `{d}`\n")
            if len(third_party) > cap:
                out.append(f"- ... ({len(third_party) - cap} more)\n")
        else:
            out.append("- (none)\n")
        out.append("\n")

        out.append("**Stdlib deps**\n\n")
        if stdlib:
            out.append(f"- {len(stdlib)} packages\n")
        else:
            out.append("- 0 packages\n")
        out.append("\n")

    print("".join(out), end="")


if __name__ == "__main__":
    main()
