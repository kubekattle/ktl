# Torque Ops E2E Tasks

This directory contains task-level E2E scripts for
[Torque Ops Automation](../../../docs/torque-ops-automation-spec.md).

Each script is named after its spec task ID and must write:

- a run directory;
- a manifest with artifact hashes;
- `target-snapshot.json`;
- `decision.json`;
- `verification/receipt.json`;
- a redaction report;
- a cleanup receipt;
- an exported `.tgz` evidence bundle.

Validate any run with:

```bash
scripts/e2e/ops/validate-evidence.py \
  --run-dir /tmp/torque-ops-e2e/OPS-LAB-001-... \
  --bundle /tmp/torque-ops-e2e/OPS-LAB-001-....tgz
```

## STACK-OPS-001

`STACK-OPS-001.sh` proves stack unification basics: generic `nodes:` with a
`host.command.run` SSH node, a dependent `release.helm` node, and a legacy
`releases:` Helm alias in the same change graph. It applies to a real
Kubernetes namespace and real `lab.ssh-linux` host, then audits and exports the
stack run.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
KUBECONFIG_PATH="$HOME/.kube/config" \
scripts/e2e/ops/STACK-OPS-001.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## STACK-FC-K8S-001

`STACK-FC-K8S-001.sh` proves a full stack-driven Firecracker Kubernetes lab on
the real SSH host. The generated stack bootstraps 8-12 Firecracker VMs, forms a
k3s cluster, opens a local API tunnel, installs a Helm HTTP DaemonSet app,
verifies all node-local HTTP endpoints, reapplies the same stack for
idempotence, then audits, exports, and deletes the stack.

The direct stackfile version lives at
`testdata/stack/e2e/14-firecracker-k8s-stackfile/stack.yaml` and can be run
with `torque stack plan/apply/delete` without the E2E wrapper.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/STACK-FC-K8S-001.sh \
  --nodes 8 \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-HOST-001

`OPS-HOST-001.sh` proves the first guarded host adapter on the real
Firecracker lab host. It boots one microVM on `root@141.105.65.227`, collects
facts over SSH through the lab host, seals TargetGraph/facts/lock/policy inputs
into stack plan bundles, runs an approved `host.command.run` inside the VM,
proves a policy-blocked command does not execute, proves a timeout receipt,
then audits, exports, and cleans up.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/OPS-HOST-001.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-HOST-002

`OPS-HOST-002.sh` proves `host.file.render` on the real SSH lab host. It
renders a templated file to a temporary path, verifies owner/mode and content
digest evidence, repeats apply as a no-op, audits and exports the stack run,
then deletes the rendered file.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/OPS-HOST-002.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-HOST-003

`OPS-HOST-003.sh` proves `host.file.copy` on the real SSH lab host. It copies a
local source file to a temporary remote path, verifies checksum, owner/mode, and
backup evidence, repeats apply as a no-op, audits and exports the stack run,
then deletes through the stack and proves the original file was restored.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/OPS-HOST-003.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-HOST-004

`OPS-HOST-004.sh` proves `host.package.install` on the real SSH lab host. It
selects an absent harmless package from the host package cache, installs it,
verifies package-manager before/after evidence, repeats apply as a no-op,
audits and exports the stack run, then deletes through the stack and proves the
package was removed.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/OPS-HOST-004.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-HOST-005

`OPS-HOST-005.sh` proves `host.service.manage` on the real SSH lab host. It
creates an isolated systemd test unit, starts and enables it through the stack,
verifies service before/after evidence, repeats apply as a no-op, proves restart
evidence, then deletes through the stack and proves the unit was stopped and
disabled.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/OPS-HOST-005.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-CLI-004b

`OPS-CLI-004b.sh` reuses the same real Firecracker VM harness to prove approved
stack apply replay. It runs an eligible `--from-bundle --yes` host command, then
proves replay blocks before mutation when approval is missing, TargetGraph
changes, fact evidence changes, policy changes, or the planned lock holder
changes.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/OPS-CLI-004b.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-CLI-005

`OPS-CLI-005.sh` reuses the approved replay Firecracker evidence, audits the
exported stack run bundle with `torque stack audit --from-bundle`, and proves a
tampered bundle fails ops verification when host command receipts or redaction
proof are inconsistent.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/OPS-CLI-005.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-CLI-006

`OPS-CLI-006.sh` proves `torque stack export` as a portable redacted evidence
archive. It exports an explicit run and the default latest run, verifies
`manifest.json` hashes and run digests, audits the exported bundle
read-only, proves raw secret-like command material is absent from exported
SQLite state, rejects a tampered `state.sqlite`, and cleans up the local
fixture.

```bash
scripts/e2e/ops/OPS-CLI-006.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-CLI-007

`OPS-CLI-007.sh` proves `torque ops adapter capabilities`. It verifies the
local adapter contract catalog in table and JSON formats, proves the
implemented `host.command.run` adapter can run a local redaction probe, and
runs the same read-only probe over the lab SSH target without leaking raw probe
secret material.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/OPS-CLI-007.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## STACK-LIFE-008

`STACK-LIFE-008.sh` proves the GitLab Firecracker hybrid Kubernetes lifecycle
hardening loop. It destructively deletes any existing lab, recreates and applies
the stack, proves inspect -> derived targets -> cert renew -> cluster verify,
reruns the same stack for idempotence, deletes the lab again, recreates it, and
exports the final lifecycle summary evidence.

By default the recreated lab is left running so the final app probe remains
inspectable. Pass `--final-cleanup` to delete it after proof collection.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/STACK-LIFE-008.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --no-final-cleanup
```

## STACK-LIFE-012

`STACK-LIFE-012.sh` proves Kubernetes lifecycle parity on the real Firecracker
lab host. It generates the same lifecycle DAG for k3s and kubeadm/upstream
Kubernetes: `k8s.cluster.inspect`, dynamic `targetsFrom`,
`k8s.cert.inspect`, policy-gated `k8s.cert.renew`, `k8s.cluster.verify`, and
`k8s-lifecycle-summary.json`. Each cluster is created over SSH, exported after
the first apply, rerun to prove idempotence, exported again, and summarized in
`verification/parity-report.json`.

By default the VMs are deleted after proof collection. Pass `--no-cleanup` to
leave them running for inspection.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/STACK-LIFE-012.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --no-cleanup
```

## OPS-LAB-001

`OPS-LAB-001.sh` proves the lab harness itself. By default it runs
`lab.local`, `lab.ssh-linux`, and `lab.k3s`.

The SSH profile also records KVM/QEMU/Firecracker availability from the lab
host so later task scripts can choose isolated VM or microVM fixtures for
destructive host tests.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@lab-host" \
TORQUE_LAB_K3S_SSH="ssh://root@lab-k3s" \
scripts/e2e/ops/OPS-LAB-001.sh --cleanup
```

Useful options:

```bash
scripts/e2e/ops/OPS-LAB-001.sh --lab-profile lab.local
scripts/e2e/ops/OPS-LAB-001.sh --evidence-root /tmp/torque-ops-e2e --cleanup
```

## OPS-LAB-002

`OPS-LAB-002.sh` proves the evidence contract validator. It creates a valid
local fixture and intentionally broken fixtures for missing target snapshot,
decision, verification receipt, cleanup receipt, and export bundle.

```bash
scripts/e2e/ops/OPS-LAB-002.sh --evidence-root /tmp/torque-ops-e2e --cleanup
```

## OPS-TG-001

`OPS-TG-001.sh` proves the `TargetGraph` schema and loader. It creates a lab
fixture with targets, groups, transports, variables, facts, and privilege
profiles; runs the Go loader; and optionally proves a real `lab.ssh-linux`
target is reachable.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@lab-host" \
scripts/e2e/ops/OPS-TG-001.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

Debug options:

```bash
scripts/e2e/ops/OPS-TG-001.sh --evidence-root /tmp/torque-ops-e2e --skip-ssh-canary --cleanup
```

## OPS-TG-002

`OPS-TG-002.sh` proves TargetGraph selectors and groups. It expands selector
groups, narrows by labels, applies a deterministic limit with omitted-target
evidence, reports a mixed-group conflict, and optionally mirrors the selected
execution set on a real `lab.ssh-linux` host.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@lab-host" \
scripts/e2e/ops/OPS-TG-002.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

Debug options:

```bash
scripts/e2e/ops/OPS-TG-002.sh --evidence-root /tmp/torque-ops-e2e --skip-ssh-canary --cleanup
```

## OPS-TG-003

`OPS-TG-003.sh` proves TargetGraph variable layering with provenance and
redaction. It resolves graph, group, target, environment, and CLI layers; proves
the final precedence order; records redacted provenance; and optionally mirrors
the final non-secret values on a real `lab.ssh-linux` host.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@lab-host" \
scripts/e2e/ops/OPS-TG-003.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

Debug options:

```bash
scripts/e2e/ops/OPS-TG-003.sh --evidence-root /tmp/torque-ops-e2e --skip-ssh-canary --cleanup
```

## OPS-TR-001

`OPS-TR-001.sh` proves the OpenSSH-backed transport primitive against a real
`lab.ssh-linux` host. It connects, runs a remote command, uploads and downloads
a temp file, records a bounded timeout as evidence, redacts command/output
receipts, and verifies cleanup.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@lab-host" \
scripts/e2e/ops/OPS-TR-001.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-TR-002

`OPS-TR-002.sh` proves the localhost transport primitive against `lab.local`.
It shares the operation receipt contract with the SSH transport, runs local
commands, uploads and downloads a temp file through local copy primitives,
records a bounded timeout, redacts command/output receipts, and verifies
cleanup.

```bash
scripts/e2e/ops/OPS-TR-002.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-FACT-001

`OPS-FACT-001.sh` proves `host.fact.collect` against a real `lab.ssh-linux`
host. It collects OS, kernel, package, service, user, disk, and network facts;
records `observedAt`, TTL, `expiresAt`, command receipts, and a stable snapshot
digest; checks redaction; and verifies the standard evidence contract.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@lab-host" \
scripts/e2e/ops/OPS-FACT-001.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-FACT-003

`OPS-FACT-003.sh` proves fact cache and staleness handling against a real
`lab.ssh-linux` host. It refreshes a missing cache, proves a fresh cache hit,
intentionally expires the cached snapshot to block a plan, then refreshes the
stale cache and exports the decision evidence.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@lab-host" \
scripts/e2e/ops/OPS-FACT-003.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-INV-001

`OPS-INV-001.sh` proves `torque ops inventory show`. It loads a TargetGraph
fixture, renders JSON and table inventory views, proves selector output matches
resolved targets, checks redaction, and records a real `lab.ssh-linux`
reachability canary for the same inventory profile.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@lab-host" \
scripts/e2e/ops/OPS-INV-001.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-INV-002

`OPS-INV-002.sh` proves `torque ops inventory graph`. It exports JSON and HTML
target graph views, proves selector-highlighted targets and graph edges, checks
secret redaction, and records a real `lab.ssh-linux` reachability canary for
the GitLab-style lab inventory.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@lab-host" \
scripts/e2e/ops/OPS-INV-002.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

## OPS-SCALE-001

`OPS-SCALE-001.sh` proves the 10,000-target synthetic scale harness. It stores
shard manifests and aggregate digests rather than per-host logs, then validates
the standard evidence contract.

```bash
scripts/e2e/ops/OPS-SCALE-001.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

Debug options:

```bash
scripts/e2e/ops/OPS-SCALE-001.sh --targets 1000 --shard-size 100
scripts/e2e/ops/OPS-SCALE-001.sh --max-bundle-bytes 1048576
```

## OPS-SCALE-002

`OPS-SCALE-002.sh` proves deterministic shard planning. It repeats a baseline
10,000-target plan three times, then adds, removes, and mutates target metadata.
Targets that keep the same stable ID must remain in the same shard.

```bash
scripts/e2e/ops/OPS-SCALE-002.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

Debug options:

```bash
scripts/e2e/ops/OPS-SCALE-002.sh --targets 1000 --shard-size 100 --change-count 25
```

## OPS-SCALE-003

`OPS-SCALE-003.sh` proves worker lease behavior. It heartbeats shard leases,
expires one worker mid-shard, steals the lease from another worker, resumes from
the checkpoint, and verifies every synthetic target mutates exactly once.

```bash
scripts/e2e/ops/OPS-SCALE-003.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

Debug options:

```bash
scripts/e2e/ops/OPS-SCALE-003.sh --targets 1000 --shard-size 100 --failure-shard 3
```

## OPS-SCALE-004

`OPS-SCALE-004.sh` proves distributed target/object locks. It simulates 10,000
object-scoped writes with conflicting workers and runs a real `lab.ssh-linux`
canary where a second writer is blocked by an atomic remote lock.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@lab-host" \
scripts/e2e/ops/OPS-SCALE-004.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

Debug options:

```bash
scripts/e2e/ops/OPS-SCALE-004.sh --targets 1000 --shard-size 100 --conflicts 50 --skip-ssh-canary
```

## OPS-SCALE-005

`OPS-SCALE-005.sh` proves fact digest caching. It simulates a baseline
10,000-target fact collection, then a second collection where unchanged facts
are referenced by digest and only changed facts are stored as new payloads.

```bash
scripts/e2e/ops/OPS-SCALE-005.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

Debug options:

```bash
scripts/e2e/ops/OPS-SCALE-005.sh --targets 1000 --shard-size 100 --changed-facts 25 --ttl-refresh 5
```

## OPS-SCALE-006

`OPS-SCALE-006.sh` proves evidence chunking, compression, dedupe, and
summarization. It simulates a 10,000-target run with repetitive per-host
payloads, writes unique payloads into content-addressed gzip chunks, and stores
shard manifests with digest references instead of copied host evidence.

```bash
scripts/e2e/ops/OPS-SCALE-006.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

Debug options:

```bash
scripts/e2e/ops/OPS-SCALE-006.sh --targets 1000 --shard-size 100 --payload-variants 64 --chunk-records 16
```

## OPS-SCALE-007

`OPS-SCALE-007.sh` proves backpressure and blast-radius control. It simulates
per-shard, per-adapter, per-provider, and global rate limits, injects a retry
storm, pauses safely, and records decision evidence proving no new mutations
were admitted after pause.

```bash
scripts/e2e/ops/OPS-SCALE-007.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

Debug options:

```bash
scripts/e2e/ops/OPS-SCALE-007.sh --targets 1000 --shard-size 100 --retry-storm-targets 120 --pause-threshold 40
```

## OPS-SCALE-008

`OPS-SCALE-008.sh` proves the pull-agent protocol. It simulates signed
assignment pull, checkpoint/resume after disconnect, reconnect resume, and
bounded evidence upload for 10,000 synthetic agents, then runs an optional real
`lab.ssh-linux` canary unless skipped for local debugging.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@lab-host" \
scripts/e2e/ops/OPS-SCALE-008.sh \
  --evidence-root /tmp/torque-ops-e2e \
  --cleanup
```

Debug options:

```bash
scripts/e2e/ops/OPS-SCALE-008.sh --agents 1000 --shard-size 100 --disconnect-agents 120 --skip-ssh-canary
```

Do not commit evidence bundles. Lab targets, kubeconfigs, SSH keys, and real
hostnames belong in environment variables or local operator config.
