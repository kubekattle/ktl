# Release Proof Graph

`torque proof` turns existing Torque evidence into one reviewable graph:
commit, image digest, BuildKit capture, verifier findings, Helm render,
server-side dry-run, runtime drift, rollout capture, SLO outcome, rollback, and
repair artifacts.

```bash
torque proof graph ./apply-proof.json \
  --attach drift-proof.json \
  --attach repair-pr.md \
  --out proof.graph.json \
  --html proof.html
```

Sign the graph with the same ed25519 key format used by `torque stack keygen`:

```bash
torque stack keygen --out .torque/keys/proof-ed25519.json
torque proof graph ./apply-proof.json \
  --out proof.graph.json \
  --html proof.html \
  --key .torque/keys/proof-ed25519.json
```

## Verify

`proof verify` checks graph signatures and re-hashes every referenced file that
was present when the graph was built:

```bash
torque proof verify proof.graph.json --require-signature
torque proof verify proof.graph.json \
  --pub .torque/keys/proof-ed25519.json \
  --require-signature \
  --format json
```

You can also point verification directly at a proof source. Torque builds the
graph in memory and verifies the reachable file hashes:

```bash
torque proof verify ./torque-sim-proof
torque proof verify ./apply-proof.json
```

## Diff

Compare two graphs or proof sources before a release is promoted:

```bash
torque proof diff previous-proof.graph.json current-proof.graph.json
torque proof diff previous-proof.graph.json current-proof.graph.json --format json
```

The diff reports added, removed, and changed evidence artifacts, plus release
status changes such as `succeeded` to `failed`.

## Gate

`proof gate` evaluates a release policy against a signed graph. The built-in
release gate blocks promotion when the graph is unsigned, referenced files no
longer match their recorded hashes, required release evidence is absent, images
are unpinned, verifier evidence blocks, an SLO failure lacks rollback proof, or
repair PR evidence is missing:

```bash
torque proof gate proof.graph.json --out proof.gate.json
torque proof gate proof.graph.json \
  --policy release-policy.yaml \
  --format json
```

Policy files can tune the defaults:

```yaml
requireSignature: true
strictFiles: true
failOnUnpinnedImages: true
failOnVerifierBlocked: true
requireRollbackOnBlocked: true
requireRollbackOnSLO: true
requireRepairPR: true
requiredArtifacts:
  - image-digest
  - helm-render
  - verifier-report
  - build-capture
  - sbom
  - supply-chain-provenance
  - server-dry-run
  - runtime-drift
  - rollout-events
  - logs-capture
  - slo-outcome
  - repair-pr
```

## Attest

`proof attest` verifies a signed graph and signs a compact release verdict for
PR descriptions, release notes, or change-management records:

```bash
torque proof attest proof.graph.json \
  --release v1.0.9 \
  --key .torque/keys/proof-ed25519.json \
  --out release.attestation.json
```

Text output is intentionally pasteable:

```text
release=v1.0.9 commit=<commit> graph=<graph-sha256> verified=true artifacts=22 checked=14 signed=true
```

## Agent-Safe Operations

`torque agent` lets AI and automation callers prove they are allowed to perform
mutating Torque operations before they call the underlying workflow:

```bash
torque agent policy check agent-request.json \
  --proof proof.graph.json \
  --allow apply \
  --require-gate \
  --out agent-policy.json

torque agent run agent-request.json \
  --proof proof.graph.json \
  --allow apply \
  --require-gate \
  --out agent-run.json
```

Request files can include `actor`, `operation`, `command`, `release`,
`namespace`, `proof`, and `reason`. Mutating operations such as `apply`,
`delete`, `ship`, `repair`, and stack writes require an explicit `--allow`
entry and a passing proof gate. `agent run` is intentionally non-mutating; it
returns a signed-off authorization record for the caller to attach to the
change path.

## Release Score

`torque release score` converts a signed proof graph and gate result into a
compact readiness score for CI, PRs, and release notes:

```bash
torque release score proof.graph.json \
  --out release-score.json \
  --fail-below 90
```

Scores start at 100 and subtract penalties for failed graph verification,
missing signatures, missing required artifacts, unpinned images, blocked
verifier/SLO evidence, missing rollback proof, or missing repair PR evidence.

## Release Autopilot

`torque release autopilot` composes the release proof path into one command. In
the default evidence mode it is non-mutating and works from an existing proof
source or proof graph:

```bash
torque release autopilot proof.graph.json \
  --key .torque/keys/proof-ed25519.json \
  --policy release-policy.yaml \
  --fail-below 90 \
  --out-dir release-autopilot
```

The output directory contains `proof.graph.json`, `proof.html`,
`proof.gate.json`, `release-score.json`, `release.flight.torque`,
`flight-replay.json`, `flight-explain.json`, `agent-request.json`,
`agent-policy.json`, `agent-run.json`, and `release.attestation.json` when a
signing key is supplied.

To let Autopilot run `torque apply` first, use explicit execution confirmation:

```bash
torque release autopilot \
  --execute --yes \
  --chart ./chart \
  --release api \
  -n prod \
  --auto-rollback \
  --slo slo.yaml \
  --key .torque/keys/proof-ed25519.json
```

If the apply command fails but writes proof, Autopilot still tries to produce
the graph, gate, score, flight, agent, and attestation artifacts so the failure
is reviewable.

## Progressive Promotion

`torque release promote` turns canary and blue/green traffic decisions into
proof-backed promotion artifacts. It evaluates the proof gate and release
score, records agent authorization, writes a promotion decision log, attaches
that decision to a promoted proof graph, and signs the graph/attestation when a
key is supplied.

Canary promotion:

```bash
torque release promote proof.graph.json \
  --strategy canary \
  --steps 5,25,50,100 \
  --analysis-window 5m \
  --slo slo.yaml \
  --rollback-on-fail \
  --key .torque/keys/proof-ed25519.json \
  --out-dir release-promote-canary
```

Blue/green promotion:

```bash
torque release promote proof.graph.json \
  --strategy blue-green \
  --preview \
  --smoke smoke.json \
  --switch-traffic \
  --key .torque/keys/proof-ed25519.json \
  --out-dir release-promote-blue-green
```

The default provider is `evidence`, which is non-mutating. For deterministic
E2E tests and CI rehearsals, the `file` provider writes the final traffic state
only after proof checks pass and `--execute --yes` is present:

```bash
torque release promote proof.graph.json \
  --strategy blue-green \
  --preview --smoke smoke.json --switch-traffic \
  --provider file --state-out traffic-state.json \
  --execute --yes
```

Real cluster providers use the same gate-first path. `--provider kubernetes`
supports native canary scaling across stable/canary Deployments and blue/green
Service selector switching. `--provider argo-rollouts` updates the Rollout
strategy for canary or blue/green. Both providers write provider state, actions,
and touched objects into the promoted proof graph only after proof checks pass:

```bash
torque release promote proof.graph.json \
  --strategy canary \
  --steps 5,25,50,100 \
  --provider argo-rollouts \
  --execute --yes \
  --key .torque/keys/proof-ed25519.json
torque release promote proof.graph.json \
  --strategy blue-green \
  --preview --smoke smoke.json --switch-traffic \
  --provider kubernetes \
  --active-service api \
  --preview-service api-preview \
  --blue-deployment api-blue \
  --green-deployment api-green \
  --execute --yes
```

The focused provider E2E can be rerun with:

```bash
./scripts/e2e-release-promote-providers.sh
```

## Release Flight Recorder

`torque flight` records the proof graph as a portable release timeline that can
be replayed or explained without a cluster:

```bash
torque flight record proof.graph.json --out release.flight.torque
torque flight replay release.flight.torque
torque flight explain release.flight.torque
```

The flight file stores the graph digest, score, grade, release metadata, and an
ordered timeline across source, build, render, verify, dry-run, runtime,
rollout, SLO, rollback, repair, and extra evidence phases.

## Boole E2E

The repository includes a manual remote E2E for release proof graph hardening:

```bash
scripts/e2e-proof-graph-boole.sh
```

The script builds or reuses a Linux amd64 `torque` binary, copies it to Boole,
recreates a full proof graph fixture, verifies signature and file hashes, checks
HTML output, diffs previous/current graphs, runs `proof gate`, signs a release
attestation, checks `agent policy` and `agent run`, scores the release, records
and replays the release flight, runs `release autopilot`, runs canary and
blue/green `release promote`, proves tamper detection by modifying verifier
evidence, then repeats
graph/verify/diff/gate/attest/agent/score/flight/autopilot/promote for 100
iterations by default. It prints a single compact JSON line suitable for CI and
release notes.

GitHub Actions exposes the same run as the manual **Proof Graph E2E** workflow.

## Supported Inputs

- `torque apply --proof-bundle` JSON;
- `torque apply simulate --out` proof directories;
- Guardian drift and runtime proof JSON;
- Incident replay proof JSON;
- Runtime Contract proof JSON;
- extra files or directories supplied through repeated `--attach` flags.

The graph JSON is intentionally file-first. It stores paths, SHA256 hashes,
artifact types, status, image digests, and an optional ed25519 signature. It
does not copy raw logs, manifests, secret values, or SQLite capture contents
into the graph.
