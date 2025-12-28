# Stack Run Console Spec (TTY)

Applies to `ktl stack apply` and `ktl stack delete` when stderr is a TTY. This is a single-screen, in-place updating console: a stable header, an optional sticky failure rail, and a main table with one line per node.

## Layout

### Header (single line)

Ordered fields (left → right), separated by ` • `:

1. `stackName` (or `-` if unknown)
2. `command` (`apply` / `delete`)
3. `runId=<id>` (or `runId=…` while unknown)
4. `totals ok=<n> fail=<n> blocked=<n> running=<n>`
5. `concurrency <target>/<active>`
6. `elapsed <duration>`

Rules:

- Always one line (truncate rightmost fields as needed; never wrap).
- Totals are derived from current node status:
  - `ok`: `succeeded`
  - `fail`: `failed`
  - `blocked`: `blocked`
  - `running`: `running` + `retrying`
- `target` is the latest concurrency target announced by the runner; `active` is the current number of `running`/`retrying` nodes.

### Sticky Rail (failures only)

Rendered only when failures exist. One line per failed node attempt.

Line format (segments separated by ` • `):

`node • a<attempt> • <class> • <digestShort> • <message>`

Rules:

- No wrapping; message truncates first.
- `digestShort` is a shortened digest (display is stable and deterministic).
- Rail is capped to a small fixed number of most recent failures; overflow is summarized as `… +N more`.

### Main Table (one updating line per node)

Fixed columns, no wrapping, stable truncation with ellipsis.

Columns (left → right):

1. `Node` (release identifier)
2. `Status` (glyph + uppercase label)
3. `Att` (attempt integer)
4. `Phase` (current phase; hides “noisy” phases unless verbose)
5. `Note` (budget wait reason or last error class)

Ordering rules:

- Deterministic run order: critical path first, then remaining nodes sorted by execution group → parallelism group → id.

Collapse rules:

- Sticky rail hidden when empty.
- “Noisy” phases (`render`, `wait`, `pre-*`, `post-*`) collapse to `-` unless verbose or failed.

## Colors & Glyphs

Status glyph + color mapping:

- `· PLANNED` (dim)
- `⧗ QUEUED` (cyan)
- `▶ RUNNING` (blue, bold)
- `↻ RETRYING` (yellow, bold)
- `✓ SUCCEEDED` (green, bold)
- `✖ FAILED` (red, bold)
- `⏸ BLOCKED` (yellow)

Failure rail lines are rendered in red; the header is rendered as a single bold line.

### Helm Logs Section (optional)

Rendered only when `--helm-logs` is enabled (TTY-only). This is a separate section below the main table.

Rules:

- The main node table remains unchanged; helm logs never replace it.
- The header includes a compact `helmLogs nodes=<n> lines=<n> tail=<n>` segment when enabled.
- Default mode (`--helm-logs` / `--helm-logs=on`) shows logs only for active/problematic nodes: `failed|running|retrying|blocked`.
- `--helm-logs=all` shows logs for every node that has any captured lines.
- Each node block starts with a single header line prefixed by a separator `─`, including `cluster/ns/<namespace>/<release>` when available.
- Log lines are indented, prefixed by a gutter `│`, and include a dim timestamp (`HH:MM:SS.mmm`).
