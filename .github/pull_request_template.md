## What

## Why

## How to test

Paste exact commands, e.g.:

```bash
make preflight
go test ./cmd/...
```

## Risk / rollout

Describe user-facing impact, compatibility concerns, and failure modes.

## Checklist

- [ ] Tests: I ran the relevant commands (prefer `make preflight`, plus any scoped `go test ...` as needed).
- [ ] Docs: I updated `README.md`/`docs/*`/`AGENTS.md` if this changes workflows or CLI surface.
- [ ] UX: For new CLI surface (commands/flags/env vars/output), help text and examples are updated.
- [ ] Artifacts: I did not commit generated binaries under `bin/` or `dist/`.

