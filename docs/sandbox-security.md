# Sandbox Security: Real-World Threat Scenario

This document explains *why* `ktl build` defaults to re-executing inside a sandbox on Linux, and shows a safe, real-world-style scenario where:

- Running the build **directly on the host** (Docker/BuildKit running with permissive settings) can let an attacker pivot into **host/root-impacting actions**.
- Running the build **inside the `ktl` sandbox** prevents that pivot by constraining what the build process can see and touch.

## Threat Model (What’s being protected)

You run `ktl build` against a repository you don’t fully trust (for example: a PR from a fork, a vendor “example” repo, or a copied Dockerfile from the internet).

In this situation, the attacker’s goal is not “break the container”; it’s to use your local build environment to reach *host resources*:

- Read sensitive host files (credentials, kubeconfigs, SSH keys).
- Write to host locations (persistence, tampering).
- Use privileged local endpoints (e.g. a rootful build daemon) as an escalation primitive.

`ktl`’s sandbox is designed to make the build behave as if it’s running in a minimal, controlled environment:

- The build can access the build context.
- The build can access `ktl`’s cache directory.
- Everything else is “not there” unless explicitly bound in.

## The “Docker/BuildKit is permissive” precondition (common in practice)

This class of issue typically requires a permissive host builder configuration. Examples include:

- A **rootful** BuildKit daemon used for local builds.
- A builder configured to allow host bind-mount style entitlements or other “insecure” capabilities.

Not every system is vulnerable by default; the point is that developers often change builder settings for convenience (performance, debugging, legacy builds), and those settings can quietly turn a build into a host-privilege boundary crossing.

## Scenario Overview

An attacker publishes a repo with a Dockerfile that *looks* normal but contains a build step that attempts to access the host filesystem via a host-mount capability provided by a permissive builder.

If the build runs directly on the host, the build process may be able to read host files and, in worse cases, perform host modifications that have “root-equivalent” impact.

If the build runs inside the `ktl` sandbox, the same attempt only sees the sandbox’s restricted filesystem, so the host is not exposed.

## Safe Step-by-Step Demonstration (no secrets, no persistence)

This demonstration is intentionally **non-destructive** and avoids sensitive paths. It’s meant to validate “host access” vs “sandbox access”, not to teach escalation.

### Preconditions (so results are meaningful)

Run this on a **Linux host** where `ktl` can use the sandbox runtime (for example, the Archimedes box).

1) Ensure the sandbox runtime is available:

```bash
command -v nsjail
```

2) Pick a sandbox policy (recommended defaults):

```bash
export KTL_SANDBOX_CONFIG="$(pwd)/testdata/sandbox/linux-ci.cfg"
```

3) Confirm you’re using a builder that can build Dockerfiles (BuildKit). If your builder is configured to *deny* host bind mounts/entitlements, step (2) below may “fail closed” even without the sandbox. That’s still a safe outcome; it just means you won’t see a difference between the two runs on that host.

### 1) Create a minimal demo context

```bash
mkdir -p /tmp/ktl-sandbox-demo
cat > /tmp/ktl-sandbox-demo/Dockerfile <<'EOF'
# syntax=docker/dockerfile:1.6
FROM alpine:3.20

# This step is intentionally benign: it *tries* to read a host file via a host bind-mount.
# On a permissive/rootful builder, it may succeed and print host data.
# Inside the ktl sandbox, it should either fail or show sandbox-local data.
RUN --mount=type=bind,src=/etc,readwrite=false,target=/host-etc \
  ls -la /host-etc >/tmp/host_etc_listing.txt || true

RUN cat /tmp/host_etc_listing.txt
EOF
```

### 2) Run without sandbox (host-exposed execution)

**Warning:** only do this in a throwaway environment, and only if you understand your builder configuration. This is the “what if I disable ktl’s guardrails?” comparison run.

```bash
KTL_SANDBOX_DISABLE=1 ./bin/ktl build /tmp/ktl-sandbox-demo
```

What to look for:

- If your builder is permissive, the listing may reflect the **host’s** `/etc` layout.
- If your builder is not permissive, the mount will fail and the build will continue (because the Dockerfile uses `|| true`). That means your builder already blocks this class of host access.

### 3) Run with sandbox (default on Linux when available)

```bash
./bin/ktl build /tmp/ktl-sandbox-demo
```

What to look for:

- You should see a banner like: `Running ktl build inside the sandbox ...` before build output.
- The build should **not** be able to traverse arbitrary host paths outside what `ktl` binds into the sandbox.
- Even if the underlying builder supports host-mount style features, the “host” from the build’s perspective is now the **sandbox root**, not the machine root.

### 4) If the sandbox run produces no output, capture sandbox runtime logs

If the sandbox runtime fails before `ktl` starts inside the jail, the normal build stream won’t start. Run with sandbox logs enabled to see the sandbox runtime’s error output:

```bash
./bin/ktl build /tmp/ktl-sandbox-demo --sandbox-logs
```

Expected output includes `[sandbox] ...` lines describing what the sandbox runtime is doing and why it failed (missing mounts, denied syscalls, missing namespaces, etc.).

## Automated demo (recommended for talks)

On a Linux host with `nsjail` installed (for example `root@188.124.37.233`), run:

```bash
export KTL_SANDBOX_CONFIG="$(pwd)/testdata/sandbox/linux-ci.cfg"
./scripts/sandbox-demo.sh
```

Notes:

- The script includes multiple checks and reports `PASS`/`FAIL`/`SKIP`.
- The “host marker” check is only meaningful if your builder is permissive enough to allow host bind mounts; if not, that check is reported as `SKIP` (safe default).

## Why this prevents “root on the host”

The key security property is **constraining the build’s view of the filesystem and local endpoints**.

When a build runs directly on the host:

- Any “host access” the builder enables is host access to *your real machine*.
- If the builder is rootful or exposes privileged functionality, a malicious Dockerfile can sometimes turn that into host-impacting changes.

When the same build runs inside the `ktl` sandbox:

- The build process is jailed to a restricted filesystem.
- The build can’t “see” host paths unless `ktl` explicitly binds them in.
- Attempted host mounts resolve against the sandbox environment, so “read host file” becomes “read sandbox file”, which removes the host privilege boundary crossing.

## Operational Guidance

- Treat `KTL_SANDBOX_DISABLE=1` as a **high-risk debug switch**.
- Don’t run `ktl build` on untrusted repos with sandbox disabled.
- If you need additional binds for legitimate builds, prefer adding them via `--sandbox-bind ...` and codifying them in a policy under `testdata/sandbox/*.cfg` for reviewability.
