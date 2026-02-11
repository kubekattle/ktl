# capture

UI-first viewer for `ktl --capture` SQLite databases.

## Build

- `make build-capture`

## Run

- Logs capture:
  - `./bin/ktl logs -A --kubeconfig ~/.kube/archimedes.yaml --capture`
- Apply capture (if enabled in your workflow):
  - `./bin/ktl apply ... --capture`
- Open UI:
  - `./bin/capture --ui :8081 <path-to-capture.sqlite>`
  - Optional: `./bin/capture --ui :8081 --session <session_id> <path-to-capture.sqlite>`

## Notes

- The UI is timeline-first: a single time axis drives filtering, navigation, and “follow” mode.
- `capture` opens the SQLite DB in read-only mode by default (`--ro=true`).

