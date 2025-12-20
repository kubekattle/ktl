# Frontend Style Book

Reference manual for every HTML-based `ktl` surface (current `ktl apply --ui`, `ktl delete --ui`, and `ktl plan --format=html --visualize`). Keep this doc open while designing or implementing UI changes, and update it first when the system evolves.

---

## How To Use This Document
- Start with **Quick Guardrails** before touching CSS or HTML.
- Each numbered section maps to a design review checklist; cite the subsections you touched inside your PR description.
- If you need a rule that is not here, add it under the matching heading rather than inventing a new pattern.

## Quick Guardrails
1. Reuse existing tokens (`--surface`, `--accent`, etc.) before adding new palette entries. Extend `:root` only with descriptive names (`--latency`, `--success`) and document them below.
2. Place new UI inside existing containers (`.chrome`, `.panel`, `.insight-panel`) so spacing, shadows, and print behavior stay consistent.
3. Connect interactive elements to the shared JS utilities (`data-filter`, `data-filter-chip`, `data-namespaces`, `applyFilters()`, clipboard helpers). No bespoke filtering widgets.
4. Honor accessibility + export rules: every control needs text/aria labels, focus rings must remain visible, and components must declare how they behave in print/PDF.
5. When you break a rule, document the exception in this file and link to it from your PR.

---

## 1. Visual Language

### 1.1 Color & Elevation Tokens

| Token | Value | Intent |
| --- | --- | --- |
| `--surface` | `rgba(255,255,255,0.9)` | Default panel background. |
| `--surface-soft` | `rgba(255,255,255,0.82)` | Score cards / stacked tiles. |
| `--border` | `rgba(15,23,42,0.12)` | Dividers, panel outlines, table rules. |
| `--text` | `#0f172a` | Primary body text + numerics. |
| `--muted` | `rgba(15,23,42,0.65)` | Secondary labels, subtitles. |
| `--accent` | `#2563eb` | Focus rings, links, chart highlights. |
| `--chip-bg` / `--chip-text` | `rgba(37,99,235,0.08)` / `#1d4ed8` | Filter chip background/text. |
| `--sparkline-color` | `#0ea5e9` | Sparkline stroke. |
| `--warn` | `#fbbf24` | Warning states (scores, timeline). |
| `--fail` | `#ef4444` | Failure states / blocking alerts. |

**Elevation:**
- Primary panels: `box-shadow: 0 40px 80px rgba(16,23,36,0.12)` + `backdrop-filter: blur(18px)`.
- Secondary widgets (budget donuts, runbook cards): `0 18px 40px rgba(15,23,42,0.12)`.
- Avoid heavier shadows; lighten instead of stacking multiple drops.

### 1.2 Typography
- Stack: `"SF Pro Display", "SF Pro Text", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`.
- Hierarchy:
  - `h1`: 2.8rem, weight 600, letter-spacing -0.04em.
  - Summary-card numerics: 2.2rem, weight ~570.
  - Score values: 2rem, tight tracking.
  - Section labels / table headers: uppercase 0.75–0.9rem with 0.18–0.2em tracking.
  - Body copy: 1rem in `--text`; helper captions in `--muted`.
- Align numbers for scanning; keep subtitles muted so primaries stay dominant.

### 1.3 Spacing, Radii, Motion
- Global padding: `48px 56px 72px` (desktop canvases).
- Panel padding: 32px on all sides.
- Grid gaps: 1.1rem (summary cards) / 1rem (score cards).
- Radii: 28px panels, 24px cards, 16px insight widgets. Chips remain fully rounded.
- Motion: prefer CSS transforms/opacity with 150–250ms easing (`cubic-bezier(0.22, 1, 0.36, 1)`). Layout shifts should be avoided during filters.

### 1.4 Responsive Behavior
- `.layout` defaults to two columns: main content + 320px `.insight-stack` (sticky). Collapse to a single column at widths <1100px and drop sticky positioning.
- Tables/accordions stretch to full width; avoid hard-coded widths so modules port to future dashboards.
- Keep sparklines and donuts vector-based (canvas/SVG/CSS) so they scale cleanly on high-DPI displays.

### 1.5 Print / Export Baseline
- Exports hide purely interactive chrome: `.insight-stack`, toolbars, chip rows, toast, CTA buttons.
- Shadows drop in favor of `border-color:#000` outlines; verify legibility on white paper.
- Add new selectors to the shared `@media print` block whenever components should hide or simplify.

---

## 2. Layout & Containers

### 2.1 Body & Chrome
- Background stays radial (white → `#dce3f1`) until theming exists.
- `.chrome` centers content, max-width 1200px. New top-level sections must live inside `.chrome` to inherit padding.
- Primary header uses `h1` + `.subtitle` (muted) for timestamp + cluster metadata; extend `.subtitle` with inline metadata rather than adding new chrome.

### 2.2 Panels
- Use `<div class="panel">` for any block needing depth (charts, rollups, insights). Avoid bespoke spacing or shadows.
- Namespace accordions wrap `<details>` with `class="panel namespace"` so they inherit spacing and frosted treatment.
- Keep internal layout as flex/auto grids; avoid nested panels unless there is a semantic split.

### 2.3 Insight Column
- `.insight-panel` houses timeline, budget widgets, runbook cards. Maintain its 320px width on desktop and stack it below main content when responsive.
- Widgets inside should respect the 16px radius and secondary shadow noted above.

---

## 3. Component Library

### 3.1 Toolbar
- Composition: pill search input + dynamic chip row.
- Search input: `border-radius:999px`, 0.75rem vertical padding, accent glow focus ring (3px). Placeholder text stays muted.
- Chips: uppercase weight-600 text; `.active` adds 2px accent outline + slight translateY. Name chips via `data-filter-chip` so JS auto-wires them.

### 3.2 Summary Cards (`.grid .card`)
- Anatomy: uppercase label (`span`), bold value (`strong`), optional inline delta badge.
- Grid: `grid-template-columns: repeat(auto-fit, minmax(180px, 1fr))`. Do not change card min widths; add new cards as additional children.

### 3.3 Score Cards (`.score-card`)
- Required content: title + delta block, primary percentage, optional sparkline, budget bar, supporting blurb.
- States: default/pass (green gradient), `.warn`, `.fail` toggle yellow/red progress bars + icon accents.
- Drilldown: single `<details class="score-drilldown">` per card. `<summary>` is chevron-only (`›`) with accompanying `.sr-only` text for screen readers.
- Sparklines share `--sparkline-color` and a fixed 140×36 canvas. Feed trends via `data-trend` attributes.

### 3.4 Namespace & Resource Accordions
- Structure: `<details class="panel namespace">` + summary row (name, counts, chevron).
- Tables live inside `.content` with uppercase headers, muted text, and `rgba(15,23,42,0.06)` row separators.
- Use `.hidden` to toggle filtered rows; never create new hide classes.
- Pod metadata uses `.labels` (muted) and `.badge` (stateful) to represent readiness.

### 3.5 Insight Stack Modules
1. **Log timeline panel (`.timeline-panel`)** – frosted glass card with a simple eyebrow label. The rail is monochrome (charcoal gradient) and beams are intentionally downsampled (≥36 px spacing) so high-volume logs stay readable. Only failure bursts render at full opacity; cache hits/misses stay muted charcoal. The scrubber is a metallic pill with brushed texture, while tooltips float as frosted capsules—no neon glows or rainbow ticks.
2. **Budget Widgets (`.budget-widget`)** – CSS `conic-gradient` donuts set via `style="--usage:<0-100>"` or `data-usage`. Clicking toggles `.active` and pipes `data-namespaces` into filters.
3. **Runbook Cards (`.runbook-card`)** – use for call-to-action or remediation notes. Optional `.cta` button or link sits at the bottom.

### 3.6 Toast (`#copyToast.toast`)
- Shared copy-to-clipboard feedback. Toggle `.visible` for <2s messages; reuse instead of inventing new toast patterns.

### 3.7 Log Stream Filters
- Filters live in their own panel above the log stream. The log panel itself should only contain the feed; labels such as “Live log stream” or running counts belong in supporting copy, not above every entry.
- The search input is additive: pressing `Enter` (or submitting while focused) turns the current text into a removable `.chip-button` rendered inside `#searchChipTray`. Each chip represents exactly one matcher (e.g., `ns:prod` or `!error`) and clicking the chip removes the filter.
- Namespace and pod filters reuse `.chip-stack` containers. Headings stay uppercase (“NAMESPACES”, “PODS”) with the right-hand meta reading `X ACTIVE` or `Y SELECTED`.
- Never auto-focus the search input when the page loads; mobile users need to scroll without triggering the keyboard.
- The log viewer auto-scrolls only while the reader is pinned to the bottom. As soon as they scroll upward, freeze the feed and reveal the `.follow-indicator` icon-only toggle (down-arrow glyph with an `.sr-only` label) at the bottom-right of the panel. Clicking the toggle (or manually returning to the tail) reenables auto-follow and hides the control.
- Keep the `.log-panel` layout as a flex column: feed first, follow indicator second. This guarantees keyboard/screen-reader users encounter the latest entry list before the optional button.
- Line counters and summaries were intentionally removed from the log chrome. Keep the wording minimal so responders can focus on the tail.

### 3.8 Deploy Viewer Panels
- The “Blocking resources” panel sits directly above “Events & logs” in the main column so responders see blockers before scrolling into the log stream. Do not move it back into the sidebar.
- Events/logs expose a single search box (15 % larger than the base input) without severity/source chips. Filtering is handled by the shared search helper; new chips must not be reintroduced.
- Blocking meta (`11 total · Updated … · ready/progressing/pending/failed`) stays inside the blockers panel, not the log feed.

---

## 4. Interaction Patterns

| Pattern | Description | Implementation Notes |
| --- | --- | --- |
| Filter search | Live filtering across pods/resources/log lines. | Dedicated filter panel hosts the search input. Pressing `Enter` adds a removable chip (`.chip-button`) to `#searchChipTray`; chips reuse `buildSearchToken()` so prefixes like `ns:`/`pod:` behave the same across viewers. |
| Chip filters | Multi-select toggles for readiness, namespaces, severities. | Chips carry `data-filter-chip`; active chips mutate shared `filterState`. |
| Budget widget filter | Single-select focus on namespace groups/app tiers. | Widgets populate `data-namespaces` strings; `applyFilters()` handles highlighting. |
| Score drilldown | Expandable `<details>` block for supporting bullets/history. | One chevron trigger per card, `.sr-only` summary text required. |
| Namespace accordion | Native `<details>` with CSS chevron rotation. | `.content` is a flex column (gap 24px); reuse for other resource types. |
| Copy-to-clipboard | `.cta.copy` buttons hook into clipboard helper + toast. | Provide `data-command` strings; no inline `onclick` logic. |
| Auto-follow indicator | Communicate when the log feed stops following the tail. | Track scroll position on `.log-feed`; when >120 px from the bottom add `.follow-indicator` inside `.log-panel`. Clicking it sets `autoScrollEnabled=true` and immediately jumps to the tail. |

Add new patterns to this table with a terse description + implementation hook before merging.

---

## 5. Accessibility & Export

### 5.1 Accessibility Checklist
- Every interactive element needs visible text or `aria-label`. Glyph-only triggers must include `.sr-only` descriptions.
- Focus states: keep accent glow on search input, border/color shifts on `.cta` buttons, solid black outline on `<details>` summaries. Extend these rules rather than overriding.
- Maintain 4.5:1 contrast on any new background/text pairing. Default `#0f172a` on `--surface` already passes.
- Respect reduced motion preferences; keep animations subtle and cancellable via `prefers-reduced-motion` queries if adding new ones.

### 5.2 Print & PDF Export
- Components must define whether they appear in exported surfaces. If hidden, add selectors under the existing `@media print` block.
- Replace gradients/shadows with solid borders for better toner usage.
- Keep typography weights the same; do not swap fonts in print mode.

---

## 6. Implementation Notes
- **CSS organization:** tokens live in `:root`; component styles are namespaced (`.panel`, `.score-card`, `.insight-panel`). Append modifiers via BEM-like suffixes (e.g., `.score-card.warn`).
- **Data attributes:** `data-filter`, `data-filter-chip`, `data-namespaces`, `data-trend`, and `data-command` are the sanctioned hooks. New behaviors should extend this vocabulary instead of introducing new attribute names.
- **JavaScript utilities:** reuse `applyFilters()`, chip handlers, and clipboard helpers. If they need new flags, extend the shared module rather than duplicating logic per component.
- **Assets:** keep inline SVGs lightweight; prefer CSS for decorative icons. Any raster assets belong under `testdata/charts` for fixtures, not inline.

---

## 7. Contribution Checklist
1. Does the change reuse existing tokens, spacing, and container classes? If not, document the exception above.
2. Are new components anchored inside `.panel`/`.insight-panel` and registered in Section 3?
3. Did you update Interaction Patterns or Accessibility rules if behavior changed?
4. Are print/export rules accurate for the new component?
5. Have you run the relevant visual tests (manual `ktl apply --ui` / `ktl delete --ui`, `ktl plan --format=html --visualize`, screenshot diffs) and noted them in your PR?

Only merge once every question above can be answered “yes” or the rationale is recorded here.
