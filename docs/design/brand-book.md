# Console v3 — locked (2026-10-14 / 2026-10-15)

> **Status: LOCKED — 2026-10-15 (Avi).** This is the design of record for the console. It locked on the lab-estate live preview against real data (brief §8): the CC build deployed a branch with `lab-deploy.sh`, Avi ruled the lock there, and the PR (#118) merged and ships in v0.2.0. At lock:
> - the README's v2 console foundations are superseded for the shipped console;
> - the v3 tokens in **package v3** (`tokens.json` + generated `tokens.css`) replace this system's v2 `tokens.json`.
>
> *Status updated 2026-10-16: this block read "PROPOSED, locks only on the lab-estate live preview" until #118 merged after the lock ruling.*
>
> Nothing in the site extensions changes. The site keeps Geist until its own redesign, and that mismatch is accepted and recorded (R1).

## 0. Provenance

| Source | What it supplied |
| --- | --- |
| Brief `innerwall/exploration-6-brief-console-v3.md` (2026-10-14) and its **Erratum 1** | Objective, fixed constraints (§2), the register (§3) [origin withheld], rulings already made (§5), and the lock gate (§8). Erratum 1 sets the per-theme warning hue: dark hue 75, light hue 58. |
| [origin withheld] | Origin references are withheld in this repository (CLAUDE.md: no origin-story references anywhere in the repo; design package v3 erratum E1). The unredacted provenance is in the project records and the design-system artifact. |
| Exploration 6 canvas (Claude Design) | Boards 0–2, the screens round, the Workload detail IA round and the States board, plus the canvas notes recording every ruling. |
| `innerwall/exploration-6-amendment-f2a-light-warning.md` | The light warning set, the tint recipe, and the W1 waiver record. |
| `innerwall/exploration-6-ia-workload-detail.md` | The A′ IA-change entry, as amended in round 4. |
| Avi's rulings | Round 1 (R1–R3, F1–F6 and nits), F2-a, round 2 (W1–W3, S1–S8), round 3 (A′), round 4 (R4.1, R4.2), and close-out. All dated 2026-10-14 or 2026-10-15. |

**Naming rule:** [origin withheld]. In this repository the stricter rule holds: no origin reference anywhere (CLAUDE.md), and every token name stays Innerwall-semantic.

**OKLCH values** in this section are the clamped values of the shipped sRGB hex, not the values that were requested.

## 1. Principles (v3)

1. **Dense data that doesn't feel dense.** Generous row rhythm over hairlines, layered low-chroma surfaces instead of heavy borders, compact controls, calm chrome.
2. **Surface steps plus hairlines, near-shadowless.** This supersedes v2's "borders, not shadows". Elevation is lightness; separation is a surface step plus a translucent hairline.
3. **One accent, used structurally.** Lime marks only these: where you are (the sidebar pill), focus rings, and the primary plate.
4. **Status is color plus glyph, always.** The three glyph families are unchanged. A Lucide chrome icon never takes a status color.
5. **The system speaks in mono.** IDs, `key=value` labels, addresses, ports, rule IDs, timestamps and counts are set in Source Code Pro; human copy is in DM Sans.
6. **Chrome is one quiet register.** The header strip, tabs and state chips are calm. Content headings carry the hierarchy.

## 2. Type (R1, ruled 2026-10-14)

- **Faces:**
  - UI is **DM Sans** at 400 / 500 / 600.
  - Mono is **Source Code Pro** at 400 / 500.
  - Both are SIL OFL 1.1 and **self-hosted as woff2**, with their licenses shipped in the package and in-repo.
  - No third-party font requests.
- **Geist is retired for the console.** The site keeps it until its own redesign; the mismatch is accepted and recorded.
- **Tabular numerals** on every numeric cell, count, timestamp and port.

| Style | Size / line | Weight | Use |
| --- | --- | --- | --- |
| `page-title` | 24 / 30, −0.02em | 600 | Non-dense pages only: sign-in, 404 |
| `section` | 20 / 28, −0.01em | 600 | Section headings in reading content |
| `dialog-title` | 16 / 22, −0.01em | 600 | Dialog titles, empty-state titles |
| `card-title` | 14 / 20 | 600 | Card and panel titles |
| `body` | 16 / 24 | 400 | Longer reading copy |
| `dense`, `dense-strong` | 14 / 20 | 400, 500 | Table rows, nav, dialog and panel copy |
| `header-register` | 13 / 20 | **500** | Everything in the 48px header strip, the tabs and the identity line (W3) |
| `control` | 12 / 16 | 500 | Buttons at 26px, chips, badges, status words |
| `meta-label` | 12 / 16 **mono, uppercase, 0.06em** | 400 | Column headers, eyebrows, group labels, in text-tertiary |
| `mono-data` | 13 / 20 mono, tabular | 400 | Times, counts, addresses in rows |
| `mono-sm` | 12 / 16 mono | 400 | `key=value` chips, port chips, inline code, rule IDs |
| `mono-xs` | 11 / 16 mono | 400 | Edge labels, legend values, axis ticks |

v2's rule "column headers sentence case, no uppercase" is **superseded** by `meta-label`.

## 3. Grounds and neutrals (R2, F4)

**The model:** neutral hue **145** (Innerwall's green band), chroma **0.005** in dark and **0.002** in light. Elevation is a lightness step at level ratios **1 / 1.5 / 2 / 3** for card, popover and dialog, overlay, and top. Higher is lighter **in both themes**.

### Dark (default) — ground B (ruled 2026-10-14)

- **Step:** 0.025 L.
- **Rejected:** ground A at oklch(0.188 0.005 145.4) `#121412`, a warm ground [origin withheld]. It is kept on Board 2, labeled.

| Level | Token | Hex | OKLCH |
| --- | --- | --- | --- |
| 0 canvas (sidebar shares it) | `surface-canvas` | `#0a0c0a` | 0.151 0.005 145.3 |
| 1 card | `surface-card` | `#0f110f` | 0.175 0.005 145.3 |
| 1.5 popover / dialog | `surface-popover` | `#121412` | 0.188 0.005 145.4 |
| 2 overlay | `surface-overlay` | `#151715` | 0.202 0.005 145.4 |
| 3 top | `surface-top` | `#1a1c1a` | 0.224 0.005 145.4 |

### Light — 0.975 → 1.0 (F4, a deliberate deviation)

- **Step:** 0.0083 L, one-third of the dark step.
- **The deviation:** a light ground at L 0.995 [origin withheld] leaves no headroom for higher-is-lighter.

| Level | Token | Hex | OKLCH |
| --- | --- | --- | --- |
| 0 canvas | `surface-canvas` | `#f6f7f6` | 0.975 0.002 145.6 |
| 1 card | `surface-card` | `#f9faf9` | 0.984 0.002 145.6 |
| 1.5 popover / dialog | `surface-popover` | `#fafbfa` | 0.987 0.002 145.6 |
| 2 overlay | `surface-overlay` | `#fbfdfb` | 0.992 0.003 145.6 |
| 3 top | `surface-top` | `#fefffe` | 0.999 0.002 145.6 |

### Text (worst contrast across levels 0–3)

| Token | Dark | Light |
| --- | --- | --- |
| `text-primary` | `#f8fbf8`, 0.985 0.005 145.5 · 16.45 | `#171917`, 0.210 0.005 145.4 · 16.46 |
| `text-secondary` | `#bbbfbb`, 0.800 0.007 145.5 · 9.21 | `#4a4e4a`, 0.419 0.008 145.4 · 7.89 |
| `text-tertiary` | `#909390`, 0.660 0.006 145.5 · 5.52 | `#666a66`, 0.520 0.008 145.5 · 5.12 |
| `text-disabled` | `#5a5d5a` (contrast-exempt) | `#a3a6a3` (contrast-exempt) |

## 4. Borders, washes, fields

**Border tiers** are the foreground color at fixed alphas, so they composite on any plate:

| Token | Alpha | Use |
| --- | --- | --- |
| `border-hairline` | 7% | Rows, section hairlines, card edges |
| `border-overlay` | 13% | Popovers, dialogs, pills, the count badge |
| `border-strong` | 13% | Inputs |
| `border-stronger` | 17% | Unchecked checkboxes, the secret well |
| `border-control-hover` | 28% | Hover on bordered controls |

**Hover rule:** hover chrome is carried by **border strengthening** on bordered controls and by a **wash** on borderless items. There is no fill change on bordered controls.

**Washes** are the foreground color at an alpha:

| Token | Dark | Light |
| --- | --- | --- |
| `wash-hover` | 4% | 3.5% |
| `wash-selected` | 7% | 6% |

**Field and control surfaces:**
- Text fields are **sunk** (`field-sunk`: black at 0.12 in dark, 0.015 in light).
- Select-type choosers are **raised** (`control-raised`: white at 0.04 in dark, solid `#ffffff` in light).

**Scrim:** black at 0.66 in dark; `text-primary` at 0.32 in light.

## 5. Accent family and color reservation

| Role | Dark | Light |
| --- | --- | --- |
| `accent`: sidebar active pill edge and tint, focus ring | lime `#80ee64`, 0.854 0.204 139.7 | `#007f35`, 0.521 0.147 149.1 (4.78:1) |
| `accent-ring`: focus | lime at **55%** (4.57:1 vs canvas) | accent at **80%** (3.42:1). **F3** deviation: 55% measures 2.26:1 here and fails the 3:1 indicator bar. |
| `accent-edge` / `accent-tint`: active nav pill | 45% / 8% (edge 3.42:1 over its tint) | 80% / 6% (edge 3.21:1 over its tint; nit 3: 50% measured 2.01:1) |
| `status-ok-fg` (calmer mid step) | `#65d371`, 0.779 0.170 145.9 | `#137738`, 0.500 0.130 150.0 |
| `plate`: primary button | deep `#09af58`, 0.661 0.174 151.5 + ink `#071009` (6.70) | **plate B (S8)** `#007f35` + ink `#ffffff` (5.14) |
| `highlight` | `#bff9b4`, 0.925 0.109 140.6: **reserved, no role in v3.0** | same |

**The mark never takes the accent.** One plate per view.

**Color reservation (F6):**
- **Blue retires.** v2's only blue roles were link and selection.
- **Links** are underlined at a 4px offset, `text-secondary` changing to `text-primary` on hover.
- **Selection** is the `wash-selected` foreground wash.
- **Checked** checkboxes are inverted monochrome (`checkbox-on` = text-primary, ink = canvas).
- No color is added in blue's place.

## 6. Status palette

**Fixed-identity anchors**, independent of the neutral ramp:
- **Per-theme warning hue (brief Erratum 1):** dark 75, light 58, because amber darkened toward AA turns olive or brown [origin withheld].
- **Tints:**
  - dark: the tone at **8%**;
  - light: the tone's anchor at **10%** (F2-a).
- **Borders:**
  - the tone at **30%**;
  - except light warning, which uses a solid step.
- **Tints never sit on the selection wash.**

| Tone | Dark fg · plain / on tint | Light fg · plain / on tint |
| --- | --- | --- |
| ok | `#65d371` 0.779 0.170 145.9 · 9.07 / 7.77 | `#137738` 0.500 0.130 150.0 · 5.25 / 4.57 |
| warn | `#f2af48` 0.799 0.140 74.7 · 8.98 / 7.72 | **`#dc7b18` 0.677 0.155 58.1 · 2.84 / 2.49 — waiver W1** |
| critical (text and glyph) | **`#e2726b` 0.680 0.141 25.1 · 5.59 / 5.04 (F1)** | `#ab413e` 0.520 0.140 24.8 · 5.50 / 4.77 |
| neutral | `#909390` (= text-tertiary) · 5.52 / 4.97 | `#666a66` · 5.12 / 4.77 |

**F1 — two-step critical (dark):**
- `#e2726b` is `status-critical-fg`, used for text and glyphs.
- **`#b54a46` = oklch(0.550 0.140 24.9)** is the **destructive fill anchor**, used for plates and scrims (white ink 5.21; hover `#a3423e`, 6.17).
- Provenance: [origin withheld].
- Light: critical stays `#ab413e` (white ink 5.90; hover `#9b3734`, 7.02).

**F2-a — light warning set:**
- tint anchor **`#ac5800`** = oklch(0.551 0.136 55.4) at 10%;
- border **`#f3ba63`** = oklch(0.824 0.123 76.1), a decorative light step (1.63:1) [origin withheld];
- withdrawn: `#a06200` (oklch 0.552 0.121 67.7), `#935b00`, and the no-tint variant.

### Waiver W1 — light warning foreground (verbatim record; a waiver, not a rule change)

- **The rule still stands:** §2's AA requirement applies system-wide. This is a single, dated exception ruled by Avi on 2026-10-14.
- **Scope:** the light-theme warning foreground only, meaning the word and the glyph in warning contexts.
- **Measured:** 2.84:1 on the light canvas and 2.49:1 on its own tint.
- **Mitigation that makes it acceptable:**
  - Innerwall status is never shown by color alone: every warning carries its glyph (shape) and its word.
  - Chips also carry the border and the tint.
  - The dark theme, which is the default, is unaffected.
- **Scope guard:**
  - Does not extend to ok, critical or neutral; those all stay AA.
  - Does not extend to the dark theme.
  - Does not extend to body text: banner, card and popover sentences are always in `text-primary`.
  - Does not extend to any use of this color outside warning status contexts.
  - Any new sub-AA color needs a new waiver from Avi.
- **AA fallback, recorded beside it:** `#a94f00` = oklch(0.532 0.139 50.8), requested oklch(0.53 0.15 58). It measures 5.14 plain and 4.51 on its tint. This is the seam if the waiver is revisited, for example by an a11y-strict deployment toggle, which is named but not built.

**Contrast summary:**
- **Worst AA pair:** light ok `#137738` on its own tint, **4.57:1**.
- **Waived pair:** light warning, 2.84 / 2.49 (W1).

**Glyphs:**
- Shapes and family rules are unchanged from v2 and Amendment 1:
  - flow = outlined rings and triangle;
  - health = solid or boxed;
  - severity = outlined pointed polygons.
- Colors are re-derived per the table above.

## 7. Controls and states

**Sizes** follow this control model [origin withheld]:
- **Default** controls are 26px tall, with 12px text at 500 and 10px side padding.
- **Small** is 34px and **medium** is 38px; these are used on standalone forms such as sign-in.
- **Chips and badges** are 22px; label chips are 20px.
- **Icons** are 14px.

**Radii:**
- controls 6;
- panels, cards, dialogs and popovers 6;
- inner items (chips, checkboxes, inline code) 4;
- pills full.

**Plates (S6, S8):**

| | Rest | Hover | Pressed | Ink |
| --- | --- | --- | --- | --- |
| Dark plate | `#09af58` | `#2fbc5b` (0.701 0.180 149.0) · 7.79 | `#00a14f` (0.621 0.166 151.3) · 5.71 | `#071009` |
| Light plate B | `#007f35` | `#006e2b` (0.470 0.134 148.6) · 6.43 | `#006125` (0.430 0.123 148.5) · 7.67 | `#ffffff` |
| Destructive dark / light | `#b54a46` / `#ab413e` | `#a3423e` / `#9b3734` | = hover | `#ffffff` |

- **Light plate A** (`#09af58` + `#071009`) is **retired**, and kept on Board 1 labeled.
- **Why not a lighter destructive hover:** a lighter step, `#c7524e`, measured 4.43:1 and was rejected.

**States** are recorded on the States board:
- **Secondary buttons:** hover strengthens the border; pressed adds `wash-selected` plus the stronger border.
- **Ghost buttons:** use washes.
- **Focus:** a 2px `accent-ring` at a 1px offset.
- **Disabled:** text-disabled on a hairline.
- **The board also covers:** inputs (sunk), selects (raised; open adds the selected wash and a rotated chevron), checkbox rows, nav items, table rows (hover wash, selected wash, inset focus ring), the header-strip control, underline tabs, the state chip, and chips and badges.

## 8. Density and page layout

- **Row pitch (R3):** **40px border-box, including the 1px hairline.** Table header rows are 32px. This is a dated v3 decision that supersedes v2's 36px fleet rows; it is not an erratum.
- **Header strip (W3):**
  - 48px, one quiet line.
  - Every text item is **13/20 at 500**, with hierarchy carried by **color only**: title in text-primary, secondary items in text-secondary or tertiary, plus pills and hairlines.
  - The title stays an `h1` semantically.
  - IA is unchanged per E1: this is the page-header strip, with no global breadcrumb bar and no renames.
  - The sidebar wordmark also moves to 13/500.
- **Tabs (round 3, amended):**
  - **The pill marks primary navigation (the sidebar) only.** Tabs are underlined.
  - Active = text-primary label with a **2px underline in text-primary** (neutral, never accent).
  - Inactive = text-secondary; hover = text-primary.
  - 13/500, in a 40px row.
- **Width rule (R4.2):**
  - **Data pages are full-bleed.** Reading and form pages use a centered column of **`content-max: 1120px`**, which includes its padding.
  - The Workload Overview is the first such page.
  - Rejected alternatives: full-width fluid cards (stretched lines, orphaned cards) and fixed cards on a full-bleed canvas (a dead right half).

## 9. Navigation — Sidebar (E1 order)

- **Base:** shadcn Sidebar with `collapsible="icon"`, cookie persistence, ⌘B, and a mobile sheet below 768px (280px wide).
- **Behavior is untouched.** **Collapse is a user action only; nothing auto-collapses (S7).**
- **Items, in E1 order:**
  - Simulation review (`flask-conical`)
  - Flow map (`network`)
  - Workloads (`server`)
  - Policy (`scroll-text`)
- **Surface:** the sidebar is flat with the canvas, separated by a hairline.
- **Item states:**
  - active = rounded outlined pill with `accent-edge` and `accent-tint`;
  - other items: muted icon and label.
- **Wordmark:** 13/500 beside the native 22px mark.
- **Footer:** the avatar popover (240px), with the theme toggle and Sign out.
- **Widths:** 232 expanded, 56 collapsed.
- **v3 ships** a Sidebar spec and component in this order. The Fleet-first nav in the v2 README, the v2 Sidebar spec and the bundled v2 Sidebar is an existing v2 erratum under ruling E1, cited here and not re-filed (F5).

## 10. Patterns

- **Verdict (S1):** the Not-safe headline carries the severity **error** glyph; the Safe verdict carries flow **allowed**. Each caveat line carries its own glyph. This is the verdict-glyph rule.
- **Mode is chrome (S2):** Visibility / Simulation / Enforced use Lucide `eye` / `flask-conical` / `shield` at icon-default, with the word in text-secondary. It never takes a status color.
- **Tables:**
  - 40px rows on hairlines, with `meta-label` column headers.
  - Hover wash, selected wash.
  - Attention ordering on Workloads, with the `removed` bucket collapsible.
  - **Removed rows carry no glyph (F6).**
- **Dialogs:**
  - The dialog surface sits at the popover level (×1.5), with an overlay hairline, over the scrim.
  - A risky confirm requires an acknowledgment checkbox before the plate enables.
- **Secret shown once:**
  - The value sits in mono in a sunk well with a `border-stronger` edge, plus Copy.
  - A "shown once" warning **badge** precedes the sentence; it is not a banner.
  - Later rows show a truncated prefix.
- **Flow map (W2):**
  - **Every edge renders at one width, 2px.**
  - The dash encodings are unchanged: allowed solid, would-block `6 4`, blocked `8 3 2 3`, observed `1 4`.
  - Unselected items dim to 0.32.
  - Volume lives only in the selected-edge panel and the pair counts. The legend's volume line is gone.
  - An on-map count in the port chip remains an open proposal and is not applied.
- **Histogram (S5):**
  - Neutral volume bars (`border-stronger` tone), a warn would-block slice, and a glyph legend.
  - A hatched gap band and a hatched "No data" row.
  - `table_removed` is a severity **alert** marker and event row.
- **Exceptional states (R4.1):** state chips, popovers and Overview alert cards. See §11.
- **Badges vs banners:**
  - Tinted status badges inside content stay. For example, "would-block under revision 14" in the flow-map panel.
  - **Stacked banners are retired as a presentation.**

## 11. IA change — Workload detail A′ (dated entry, 2026-10-15)

**Type:** a **proposed product IA change**, the first in v3 that goes beyond restyle. The CC build implements it as a deliberate IA change, not as styling.

**Source:** `innerwall/exploration-6-ia-workload-detail.md`, as amended in round 4.

### Pages

- **Overview** (new; the workload page lands here).
- **Inbound flows**
- **Listening services**
- **Applied policy**

The last three names are repo functionality, unchanged per E1.

### Strip identity

- The header strip carries:
  - `Workloads /` in text-secondary;
  - the health glyph and the name in text-primary;
  - the mode pill;
  - `last seen <t>` in text-tertiary.
- There is no identity band.
- The back-path is the page's own path, not a global breadcrumb bar.
- Steady state: strip + tabs + content.

### Exceptional states — chips, popover, Overview inline (R4.1)

**The states:**
- offline (the former status-card content);
- credential expired;
- clock skew;
- evidence gap.

**State chips:**
- Compact chips sit right-aligned in the tab row, on every page including Overview, in severity order (critical first).
- Anatomy: the tinted-badge vocabulary, about 22px, glyph and word in the status tone, status tint, status hairline.

**Popovers:**
- Each chip is a real button. Click or Enter opens a popover at the popover surface level, 320px wide.
- The popover holds the one-sentence detail in text-primary and the action where there is one. **Resend snapshot** lives in the offline popover.
- Hover may preview; hover is never the only path.
- Only one popover is open at a time.

**Overview inline:**
- Active states render as **alert cards** above Status and sync: tint, status hairline, glyph and lead word in tone, body in text-primary, action on the right.
- The evidence-gap summary fact stays on the status card.

**Rationale (recorded):**
- These states change how the visible data must be read:
  - offline = stale last-seen;
  - skew = shifted timestamps;
  - gap = missing rows.
- So the signal stays persistently visible on data pages. Hover-only or hidden presentation was rejected: it leaves dead ends for keyboard and touch users.

**Overflow:**
- Chips show only a glyph and a word.
- When they can't all fit beside the tabs, the remainder collapses into one **"+N"** chip in the highest tone it hides, opening the same popover as a list.
- At 1280, all four fit (an estimated 470px of chips in about 590px of free row). The board carries a forced demo.

**Superseded:**
- Round 3's stacked banners, kept on the board labeled rejected.
- The v2 clock-skew banner precedent; the skew chip is its successor.

### Glyph mappings

| State | Glyph |
| --- | --- |
| Credential expired | Severity **error** |
| Evidence gap | Severity **alert** |

Neither is a flow decision or a health state.

### Overview page

- **Width:** a centered `content-max` column.
- **Contents:** alert cards when active; then three cards:
  - **Status and sync:** applied, rendered, last ack, snapshot, flow report, enrolled, dropped flows, overruns, an evidence-gap summary, agent version.
  - **Labels**, with the provenance note.
  - **Host facts.**
- **Then:** the histogram and the gap / `table_removed` history.
- The 3-card row collapses below about 1024px.
- Mode and last-seen live in the strip only.

### Filter panel

- **Where:** Inbound flows and Listening services only, left of the table, 280px wide.
- **Panel contents:**
  - in-panel search and the time range;
  - collapsible titled groups with checkbox rows and right-aligned mono counts.
- **Groups:**
  - Decision (replaces v2's all/allowed/would-block/blocked/observed chips).
  - Port.
  - Source, with the **"not managed"** annotation.
  - Matched rule, including the **"— no rule matched"** value. The live v2 table already carries rule IDs, for example `bastion to nginx 99ffd253/tcp`.
- **Data:** filtering is client-side over columns the console already serves.

### Layout

- The main sidebar stays expanded; collapse is user-only.
- **Inbound flows table area:** 926px at 1440, 766px at 1280. The table minimum is 724px.

### Rejected

- **Variant B (section-nav second sidebar):** three left columns, and a 566px table area at 1280.

## 12. Iconography

- **Lucide 1.47.0 (ISC)** stays the only chrome-icon source, at **14px**, stroke 2 on the 24px grid, `currentColor`, icon-default = text-secondary.
- **v3 bundle subset:**
  - `server`, `network`, `flask-conical`, `scroll-text`
  - `panel-left-close`, `panel-left-open`, `user`, `chevrons-up-down`, `sun`, `moon`, `log-out`
  - `eye`, `shield`, `clock`, `chevron-down`
  - `x`, `copy`, `plus`, `key-round`, `circle-minus`
  - `boxes`, `globe`
  - **`search`, `check` (new in v3)**
- **v2 erratum (F5, new, dated 2026-10-14):** the README states that the site-v2 Lucide names "join the bundle now", but `components/bundle.js` carries only the older subset (no `search`, `check`, `chevron-right`, …). Recorded, not silently fixed. Package v3's subset carries what the v3 screens use.
- **Status glyphs:** the custom set, shapes unchanged. They are never replaced by Lucide.
- **The mark:** the 22px native mark, monochrome only.

## 13. Placeholders and build notes (non-normative content)

- **Board copy is non-normative.** The repo owns every string, and the CC build diffs each rendered string against the repo.
- **Placeholders the repo owns:**
  - S3: the sign-in fields (username and password shown as a placeholder for the real auth flow).
  - S4: the token value, prefix and expiry options.
  - S7: placement guesses — "Mint token" in the Workloads header, and the 404 page recovering to Simulation review.
- **Sidebar collapse is user-only.** The collapsed sidebar on any board is layout illustration only.
- **Workloads Mode and Agent-version columns:** build them only where the data is already served; otherwise escalate.

## 14. Errata and supersession

| Item | Record |
| --- | --- |
| Brief Erratum 1 (2026-10-14) | §3.4 warning hue 75 holds for dark only; light is hue 58. Its F2-a text value `#a94f00` was later moved to fallback by W1. |
| v2 nav order (E1) | An existing v2 erratum, cited, not re-filed. |
| v2 Lucide claim (F5) | A new, dated v2 erratum (see §12). |
| v2 principles 2 and 4, Themes, Color roles, the Status hex columns, Typography, Spacing/radius/size (heights, icon size, sidebar colors), and the Navigation / Verdict banner / Tables / Dialogs patterns | **Superseded on lock** by §§1–11 here. Kept in the README as history. |

## 15. Tokens

- **Names and values:** package v3 `tokens.json` is the full list, in two themes (dark first, light), with aliases and a generated `tokens.css`.
- **Color families:** surface, text, border, wash, field and control, scrim, accent, plate, destructive, status, checkbox, and logo/icon.
- **Non-color tokens:** sizes (`control-*`, `chip`, `row-pitch`, `row-header`, `header-strip`, `tab-row`, `sidebar-*`, `filter-panel`, `popover-*`, `content-max`, `icon-size`, `logo-size`, `glyph-*`), radii, spacing (the v2 4px scale, unchanged), edge (dashes + `edge-width` 2), and opacity (`opacity-dimmed` 0.32).
