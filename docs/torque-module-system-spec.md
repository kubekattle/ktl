# Torque Module System Spec

Torque modules let typed stack resources live outside the core repository while
still using Torque's graph, transport, plan/apply lifecycle, evidence, audit,
and replay machinery.

## Goal

Core Torque must not accumulate every integration. The core owns the runtime
contract:

- stack parsing, dependency graph, selection, locks, and policy gates;
- the resource lifecycle: `observe -> diff -> plan -> apply/delete -> verify`;
- evidence capture, redaction, audit, replay, and drift-safe input hashing;
- transport boundaries such as local execution, SSH, and NATS-backed agents.

External module collections own domain behavior:

- `mysql.replication.verify`
- `gitlab.runner.ensure`
- `docker.container.ensure`
- `postgres.user.ensure`
- `kernel.sysctl.ensure`
- provider-specific cloud or SaaS resources

## Stack Shape

A module-backed node keeps a domain-specific `kind`. The implementation is
selected with `module.command`.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: module-demo
nodes:
  - name: counter
    kind: demo.counter.ensure
    module:
      source: oci://ghcr.io/torque-modules/demo
      version: 0.1.0
      command: ["./modules/demo-counter"]
      timeout: 2m
      input:
        path: /var/lib/demo/counter
        value: ready
```

`kind: module.resource` is also accepted as a generic escape hatch, but named
domain kinds are preferred because they make plans, audits, policy, and
dependencies self-describing.

## Module Request

Torque invokes the module command once per lifecycle phase. The command receives
a JSON request on stdin and returns one JSON result on stdout.

```json
{
  "apiVersion": "torque.dev/module-resource/v1",
  "phase": "plan",
  "command": "apply",
  "runId": "2026-05-26T15-00-00Z",
  "node": {
    "id": "local/default/counter",
    "name": "counter",
    "kind": "demo.counter.ensure",
    "effectiveInputHash": "sha256:..."
  },
  "module": {
    "source": "oci://ghcr.io/torque-modules/demo",
    "version": "0.1.0",
    "phases": []
  },
  "input": {
    "path": "/var/lib/demo/counter",
    "value": "ready"
  }
}
```

Torque also sets these environment variables:

- `TORQUE_STACK_RUN_ID`
- `TORQUE_STACK_NODE_ID`
- `TORQUE_STACK_PHASE`
- `TORQUE_STACK_COMMAND`
- `TORQUE_STACK_INTENT_DIGEST`
- `TORQUE_MODULE_KIND`
- `TORQUE_MODULE_PHASE`

## Module Result

Every phase returns a structured result.

```json
{
  "status": "planned",
  "changed": true,
  "safeToRun": true,
  "risk": "low",
  "message": "file will be created",
  "before": {"exists": false},
  "after": {"exists": true},
  "diff": {"create": true},
  "evidence": {"target": "host-a"},
  "receipt": {"desired": "ready"},
  "artifacts": {
    "provider-debug.json": {"requestId": "abc123"}
  }
}
```

Allowed statuses are:

- `succeeded`
- `noop`
- `planned`
- `changed`
- `skipped`
- `blocked`
- `failed`
- `error`

`status=blocked`, `status=failed`, `status=error`, or `safeToRun=false` during
`plan` stops the node before mutation.

## Lifecycle

For `stack apply`, the default phase sequence is:

```text
observe -> diff -> plan -> apply -> verify
```

For `stack delete`, the default phase sequence is:

```text
observe -> diff -> plan -> delete -> verify
```

For dry-run and diff modes, Torque runs only:

```text
observe -> diff -> plan
```

Modules may restrict phases explicitly, but a configured phase set must include
`observe`, `diff`, `plan`, `verify`, and at least one mutating phase
(`apply` or `delete`).

## Evidence

Torque records phase receipts in the normal stack audit bundle:

- `module-observe.json`
- `module-diff.json`
- `module-plan.json`
- `module-apply.json`
- `module-delete.json`
- `module-verify.json`
- `module-resource.json`
- `decision.json`
- module-provided artifacts

The module spec participates in the node effective input hash. Changing module
source, version, command, environment, timeout, phases, or input changes the
intent digest used for drift-safe resume and replay.

## Packaging Direction

The first implementation supports local commands. The registry form should use
signed OCI collections:

```yaml
imports:
  - source: oci://ghcr.io/torque-modules/mysql
    version: 1.4.0
    digest: sha256:...
```

A collection should contain:

```text
collection.yaml
schemas/
modules/
examples/
tests/
signatures/
```

Publishing requirements:

- schema for every exported kind;
- conformance tests for lifecycle behavior;
- secret redaction tests;
- deterministic receipt shape;
- signature and digest verification;
- compatibility metadata for local, SSH, and NATS execution.

This keeps Torque core small while allowing an ecosystem of typed,
proof-producing operations modules.
