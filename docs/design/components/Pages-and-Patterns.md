# Pages and patterns

## Page width (R4.2)

- **Data pages are full-bleed:** Simulation review, Workloads, Inbound flows, Listening services, Applied policy, Flow map and Policy.
- **Reading and form pages** use one centered column, `max-width: var(--content-max)` (1120px including its 20px padding). Workload **Overview** is the first.
- **Inside the column,** cards are fluid: `grid-template-columns: repeat(auto-fit, minmax(300px, 1fr))`. The 3-card row collapses below about 1024px.

## Tables

- Rows are `row-pitch` **40px border-box, including the 1px `border-hairline`.** Header rows are `row-header` 32px, set in `meta-label`.
- Row states: hover `wash-hover`; selected `wash-selected`; focus an inset `accent-ring`.
- Workloads uses attention ordering (offline, then degraded, then pending, then synced). The collapsible **Removed** bucket header sits on `surface-card`. Columns:
  - Workload
  - Labels
  - Mode (chrome)
  - State (glyph + word)
  - Agent (mono)
  - Last seen

## Workload detail (A′)

- **Chrome:** the strip (identity), then underline tabs (Overview · Inbound flows · Listening services · Applied policy), with state chips on the right of the tab row.
- **Lands on** Overview.
- **Overview** sits in the `content-max` column:
  1. alert cards, when active;
  2. three cards on `surface-card` with `border-hairline` and `radius-panel` (Status and sync facts in a 2-column grid of `meta`-style labels with mono values; Labels plus provenance note; Host facts);
  3. the histogram;
  4. the history.
- **The main sidebar stays expanded.**

## Histogram and history (S5)

- **Bars:** 48 bars, each split into neutral volume (`border-stronger`) and a warn would-block slice (`status-warn-fg`). The legend shows a glyph per series.
- **Gap:** a hatched band, `repeating-linear-gradient(135deg, border-overlay 0 2px, transparent 2px 5px)`.
- **`table_removed`:** a 1px warn marker line, with a severity `alert` glyph and a mono label on the axis.
- **History rows:**
  - an error event (credential expired);
  - a hatched "No data a → b · duration" row;
  - a `table_removed` event row with an inline-code chip.

## Flow map (W2)

- **Edges:** every edge is **`edge-width` 2px.** Volume never sets width.
- **Dashes:**

| Decision | Dash |
| --- | --- |
| allowed | solid |
| would-block | `6 4` |
| blocked | `8 3 2 3` |
| observed | `1 4`, round caps |

- **Edge color:** `status-<tone>-fg`.
- **Arrowheads** sit at the destination.
- **Port chip** at the midpoint: glyph `glyph-sm` + port, mono 11, on `surface-card` with `border-overlay`.
- **Nodes:** 176×52 on `surface-card`, `border-overlay`, with `boxes` or `globe` + label (mono 12) + a sub-line.
- **Selection:** everything outside the selection dims to `opacity-dimmed` 0.32. The selected node edge is `border-control-hover`.
- **Inspector (300px):**
  - "Peer × service";
  - a status badge;
  - a subhead with the scope once;
  - a peer chip + port chip (no arrow);
  - Flows / First seen / Last seen;
  - workload pairs.
  - **Volume appears only here and in pair counts.**
- **Legend:** glyph + dash sample + word for each decision. There is no volume line.
- *Open proposal, not applied:* show the count in the port chip.

## Policy

- Rule groups sit on `surface-card`.
- **Subhead:** "Allow inbound — `scope`", with the scope stated once.
- **Rule rows** (40px): peer chip + port chip(s), with no arrow. Then Matched 24 h and Since (rev N), plus a "new in N" pill when it applies.

## Simulation review and dialogs

- **Verdict (S1):**
  - a `radius-panel` section in `status-critical-bg` / `-border`;
  - a severity `error` glyph (18px) with a 24/30 headline;
  - caveat lines, each with its own glyph;
  - a totals row of four mono 24/28 figures, each with its glyph.
- **Evidence table:** a segmented filter (Would-block · All) and a sunk search.
- **Dialogs:**
  - surface `surface-popover`, `border-overlay`, `radius-panel`, over `scrim`;
  - title `dialog-title`;
  - footer on a hairline, actions on the right.
- **Promote dialogs:**
  - the standard dialog carries a flow `allowed` "Safe to enforce" badge;
  - the **acknowledgment** variant carries a Not-safe verdict block and a sunk acknowledgment checkbox. Its plate is disabled until the box is checked; the hint reads "Acknowledge to enable".
- **Secret shown once:** a "shown once" warning badge plus a sentence, then a mono value in a sunk well (`border-stronger`) with Copy. Later rows show a truncated prefix.

## Empty and error pages

- **Empty state:** the chrome icon in a 40px hairline disc on `surface-overlay`, a `dialog-title`, a 14/20 body in text-secondary (max 360px), and one action.
- **Sign-in and 404:** `page-title` 24/30, with 34px controls on the form. The sign-in fields are a **placeholder** (S3); the repo owns the flow.
