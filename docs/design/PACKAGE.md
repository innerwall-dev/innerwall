# Innerwall Console — design package v4

**Dated:** 2026-11-27.

**Status:** the **design of record: v4, locked 2026-11-27** (Avi), after three review rounds on the build's captures (INN-174).

**Supersedes:** package v3, locked 2026-10-15, which stays below and in history (`git log -- docs/design`). Everything in v3 that v4 does not change carries over: the status glyph shapes and their three families, the 40px row pitch, the label grammar's quoted drawing, the Workload detail IA (A′), the state chips and their popovers' copy, and every v3 erratum and amendment that is about content rather than style.

**Provenance:** the maintainer approved the v4 designs: 23 screens, each in a dark and a light theme. The designs are reference material kept with the project's records, not in this repository; their sample data is not product copy. Outbound enforcement is drawn on one screen of them and is out of scope: it is not in the product (ADR-0010), and nothing is built for it.

**The rule that decides every conflict:** the repository owns functionality; the design owns style. Routes, navigation order, strings, columns, states, API calls and behaviour are as they were under v3. Where a design shows less, more or different content than a screen has, the screen's content wins and takes the design's styling. Every such place is in the errata below.

## Where each piece lives

| Package piece | Where it lives |
| --- | --- |
| `tokens.json` | `docs/design/tokens.json`: v4 colors in two themes, type, spacing, radii, sizes, the edge tokens. |
| `tokens.css` | **Generated** from `tokens.json`; do not hand-edit. `ui/src/tokens.css` is the same file with its font URLs pointing at `ui/src/fonts/`. |
| `brand-book.md` | The v4 record leads it; the v3 record follows, superseded where the v4 record says so. |
| `components/` | Each spec opens with what v4 changes; the v3 text after it stands where v4 says nothing. |
| Fonts | `ui/src/fonts/`: Inter 4.001 and Manrope 4.504 (variable woff2, latin and latin-ext, from the `@fontsource-variable` packages 5.3.0) beside `OFL-Inter.txt` and `OFL-Manrope.txt`; Source Code Pro 1.026 beside `OFL-SourceCodePro.txt`. All self-hosted; the console fetches nothing from another origin. DM Sans is retired. |

## What v4 is

From first principles: an operator reads this console for long stretches and acts on it rarely, so the page should be calm and the few things that need a decision should be the only things that carry color.

- **Neutral grounds.** One ground (the page, the header and the sidebar), one surface (cards, sheets, dialogs), one selected / hover fill, one control fill. Elevation is a step of the surface plus a border; dialogs and floating sheets add a shadow because they float over live content.
- **Two border roles.** The border, for anything that is an object (cards, dialogs, pills, inputs, buttons); and the subtle rule, for divisions inside an object (rows, sections) and for the header's and the sidebar's edges.
- **Selection is neutral.** The active sidebar entry, the selected row and the active tab underline use the selected fill or text 1. Green is for primary buttons, focus rings and the allowed status only, so it never competes with a verdict.
- **Status takes no fill.** A status is its glyph in the status's mark color and its word in the status's text color, on the neutral surface; chips and badges are pills on the neutral border. A notice with a severity sits on the selected fill behind the status mark's border.
- **Type.** Manrope for headings and the wordmark; Inter for body and UI text; Source Code Pro for mono (data: addresses, versions, services, identifiers). Nothing is heavier than 500 except the wordmark at 600. Column labels and section eyebrows are mono, uppercase, 11px, 0.06em, in text 3. The base size is 13/20.
- **Shape.** Controls 6px (the map's nodes 7px), cards and sheets 8px, dialogs 10px; tags and chips are pills; status markers are circles; switches are rounded.
- **Density and air.** Tables sit in bordered, rounded cards with only the subtle rule between rows; pages have a 32px gutter and 20px above the first line; buttons are 28px (toolbar controls and filter chips 26px).

## Tokens (normative values)

| Role | Token(s) | Dark | Light |
| --- | --- | --- | --- |
| ground | `surface-canvas` | `#0f100f` | `#fcfcfc` |
| surface | `surface-card`, `surface-popover` | `#151716` | `#ffffff` |
| selected / hover fill | `wash-hover`, `wash-selected`, `surface-overlay`, `surface-top` | `#1c1e1d` | `#f5f6f5` |
| control fill | `field-sunk`, `control-raised` | `#191b1a` | `#ffffff` |
| border | `border-overlay`, `border-strong` | `#282a29` | `#e3e5e4` |
| border, subtle | `border-hairline` | `#1f2120` | `#eef0ef` |
| text 1 / 2 / 3 / 4 | `text-primary` / `text-secondary` / `text-tertiary` / `text-disabled` | `#edefee` / `#bcbdbc` / `#989a99` / `#6b6d6c` | `#1a1c1b` / `#4d504f` / `#686b6a` / `#a4a7a6` |
| primary button | `plate` / `plate-border` / `plate-ink` | `#236b1c` / `#3f9a33` / white | `#007f35` / `#006b2c` / white |
| allowed (mark / text) | `status-ok-mark` / `status-ok-fg` | `#80ee64` | `#00993f` / `#007f35` |
| warning (mark / text) | `status-warn-mark` / `status-warn-fg` | `#f5d547` | `#edb900` / `#dba400` |
| error (mark / text) | `status-critical-mark` / `status-critical-fg` | `#ff5b4f` | `#e5484d` / `#ce2c31` |
| destructive button | `destructive` | `#c93a30` | `#ce2c31` |
| accent (focus, allowed) | `accent`, `accent-ring` | `#80ee64`, 55% | `#007f35`, 80% |

Glyph aliases (`flow-*`, `health-*`, `severity-*`) now point at the `status-*-mark` tokens, so the glyphs take the mark color and the words beside them take the text color.

### Values the designs do not give

These are derived, not drawn, and are recorded so they are not mistaken for design values:

- `border-stronger` (unchecked boxes, the secret well) is text 4 as a border, as the designs draw the empty checkbox.
- `border-control-hover` (`#363837` / `#d0d3d1`): one step past the border.
- `plate-hover` / `plate-pressed` (`#2a7a22` / `#1e5d18`; light `#00722f` / `#006b2c`) and `destructive-hover` (`#b5342b` / `#b8252a`): one step toward the border or darker.
- `status-*-bg` are `transparent` and `status-*-border` are the border: status takes no fill.
- `scrim` (60% black / 28% black) and the two shadows are read from the dialog and sheet designs.

### Token names added, kept and retired

- **Added:** `plate-border`; `status-ok-mark`, `status-warn-mark`, `status-critical-mark`; `shadow-overlay` (dialogs, the account popover); `shadow-sheet` (floating sheets, tooltips); `radius-dialog` (10px); `--font-display` (Manrope).
- **Kept with a new value or role:** every v3 color name survives. `surface-top` and `highlight` have no role in v4 and are kept so nothing that names them breaks; `border-strong` and `border-overlay` are one value.
- **Retired:** none.
- **Sizes changed:** `sidebar-width` 208px (ruled in review); `control` 28px; `row-header` 34px; `tab-row` 34px; `tab-underline` 1.5px; `sidebar-width-collapsed` 48px; `sidebar-item` 30px; `radius-panel` 8px; `edge-width` 1.5px; `avatar` 26px; `icon-stroke` 1.75.

## The three structural changes

Ruled with the designs; they are the only places v4 changes structure rather than style.

1. **Shell.** A full-width 48px header with the subtle rule below it: logo and wordmark at the left, the existing page-header content (the path, or a workload's identity) after them, the existing search trigger (⌘K) as a pill, and the account control at the far right: a 26px avatar (initials on the selected fill behind the border, the operator's name as its title) whose existing popover opens below it, anchored to its right edge. Under the header, the sidebar at the left and the content at the right. The sidebar stays the shadcn `Sidebar` with `collapsible="icon"`, its cookie persistence, ⌘B and the mobile sheet; its footer holds only the fleet sync block (on a fresh install, as before) and the collapse toggle at its foot. On a phone the avatar stays in the header; the mobile sheet, which covers the header, repeats the logo. *Ruled in review, 2026-11-27: the account control moves to the header as the designs draw it, which supersedes the first build's reading that it stays in the sidebar.*
2. **Flow map.** The graph runs edge to edge in the content area under one 44px toolbar strip holding the existing controls on one row (`white-space: nowrap`). The details that sat in a fixed side panel open in a sheet floating over the graph's right side: 12px inset, rounded, bordered, shadowed and closable; the graph stays interactive beside it. Edges stay bezier at one uniform width. The legend is a strip along the canvas's foot. (There was no minimap on main to remove.)
3. **Promote button.** **Promote to enforced…** takes the neutral button style while the verdict is unsafe and the primary style otherwise. Its enabled / disabled logic does not change. This supersedes v3's D1 for this one button; the promote dialog's confirm keeps the primary style when enabled.

## Accessibility

AA holds on the text pairs of the normative table except the one waiver below (W2).

### W2: light warning text (waiver, scoped)

| | |
| --- | --- |
| **Waived** | The light-theme warning text `status-warn-fg` = `#dba400`: **2.20:1** on the ground, **2.25:1** on the surface. The warning mark `#edb900` is 1.82:1 on the surface (a glyph, not text). Ruled by the maintainer with the v4 designs: warnings must read yellow, not brown, and no yellow fill is put behind text. |
| **Scope** | The warning word and glyph in warning contexts only (status words, chip words, the attention count, warning captions). **Not** body text: sentences in banners, alert cards, popovers and notices are text 1 or text 2 beside the toned glyph. **Not** the dark theme. **Not** any non-warning use. Any new sub-AA color needs a new waiver. |
| **Mitigation** | Status is never shown by color alone: the glyph's shape and the word always. |
| **AA fallback** | `#8f6400` (5.26:1 on white) is the seam for a possible strict-contrast toggle, which is not built. |
| **Build instruction** | Do not "fix" it, and do not extend it. |

W1 (v3's light warning, `#dc7b18`) is superseded by W2.

### Resolved findings (ruled 2026-11-27, fixes #271)

The first build recorded two light-theme pairs below AA for a ruling; the maintainer ruled new values, now in the token table:

- **Light text 3** is `#686b6a` (was `#707372`): **5.25:1** on the ground, **5.38:1** on the surface, **4.97:1** on the selected fill (was 4.42:1).
- **Light error text and the light destructive fill** are `#ce2c31` (was `#e5484d`): error text **5.21:1** on white, **5.08:1** on the ground, **4.81:1** on the selected fill; white ink on the destructive fill **5.21:1** (was 3.91:1). The light error mark (dot and glyph) stays `#e5484d`, so `status-critical-mark` and `status-critical-fg` now differ in the light theme. `destructive-hover` follows at `#b8252a` (derived, 6.29:1 with white ink).

Dark: text 3 on the selected fill is 5.92:1 and on the ground 6.73:1; error on the surface 5.88:1; white on the dark plate 6.58:1 and on the dark destructive fill 5.08:1.

## Errata (where the screens' real content departs from the designs)

| # | Design | Built |
| --- | --- | --- |
| V1 | *Retired 2026-11-27.* The account avatar at the header's right. | Built as drawn (structural change 1, as ruled in review). The avatar shows initials; the site label that the sidebar entry showed beside the name is in the popover's identity line. |
| V2 | Inbound / Outbound tabs (review, workload detail, policy), Outbound columns (workloads), per-direction mode rows, and outbound wording in the promote and mode-change dialogs. | Not built: policy is inbound-only (ADR-0010) and outbound is out of scope. The repo's tabs, columns and copy stand. |
| V3 | Controls the screens do not have: "Find workload…", "Fit view", "Auto layout", "Export", "Sort rows" (map), "Group: …" and "Filter" in tab rows, a "Rulesets in simulation" sidebar section, header buttons ("Change mode…", "Retire…", "Mint token") placed at the page's top right, "+ Add label" inline in the scope. | Not built. The repo's controls stay where they are and take the design's styling. |
| V4 | An in-content `h1` with a description line on Workloads, Workload detail, Simulation review and Policy. | Workloads keeps its path and tabs (no heading is added: no new strings). Simulation review keeps its ruleset row and scope line, and its verdict headline stays the `h1`. A workload's name stays in the header as the `h1` (v3 E12). The policy editor's ruleset name takes the page title style. |
| V5 | Review figures drawn inline, with design wording ("of 40 workloads affected"). | *Ruled departure, 2026-11-27.* The four figures (would-block pairs, connections, workloads affected, allowed) are four equal cards in one row (two by two below 1280px, where four would crush their labels): the surface behind the border at an 8px radius, the 20px tabular figure on top (a decision's figure in its status text tone), its label below in the mono uppercase 11px label style, with the repo's labels, glyphs and "/ N simulating". The rest of the screen is as drawn. |
| V6 | Review table: Peer · Service · Rule · Reached · Connections · Seen, under decision group rows. | The repo's columns (Decision · Peer · Service · Rule · Workloads · Connections · Last seen), flat rows and decision chips stand; the selected row takes the design's mark. |
| V7 | Workloads table: Workload · Labels · Inbound · Outbound · Policy · Credential · Agent · Seen, grouped under "Needs attention". | The repo's columns (v3 E8) and order stand; labels stay chips, which wrap. The Sync cell lets its note wrap so the table fits 1440px. |
| V8 | The Overview: notices over a row of Policy, Evidence and Credential cards in the main column, and one 320px side card of Mode, Labels and Host sections. | *Rewritten 2026-11-27, as ruled in review.* Built to the design's grid with the repo's fields: the retired card and alert cards, then the repo's Status card split into the three drawn cards by its own field groups (Policy: the status line, applied, rendered, last ack, rendered at, last snapshot, the apply error and Resend snapshot; Evidence: flow report, dropped flows, overruns, evidence gaps; Credential: credential, issued, in use / presented), then the repo's evidence-gap history; beside them one 320px card holding Labels and Host facts. Card titles are mono eyebrows; labels sit in a 96px column (72px in Credential) at a 10px row gap. **What still differs:** the side card has no Mode section (the mode is in the header beside the workload's name, and a per-direction mode does not exist: V2); "enrolled" moves from the status fields into Host facts, since the design has no row for it in the cards; the apply error and Resend stay in the Policy card rather than in the degraded notice, because the repo shows both in every state, not only while degraded; the host's OS stays a Host facts row (the design puts it under the heading, which the repo does not draw: V4); the evidence-gap history, which the design does not draw, stays under the cards. Field labels are the repo's lowercase words set with a capital first letter ("OS" and "ID" in capitals). Copy added: the card titles "Policy", "Evidence" and "Credential", which replace the "Status" title, and the side card's accessible name "Workload facts". |
| V9 | Map nodes 148×50 with two lines; totals at the legend strip's left; the range as plain text. | Nodes stay 176×64 with the repo's third line (v3 E13). The totals and the evidence warnings stay in the toolbar (in the Matrix view there is no legend strip to hold them); the range control moves into the toolbar. The Graph / Matrix switch takes the design's segmented form. |
| V10 | The flow map matrix as a flex grid with column rules. | The repo's table is kept and runs edge to edge on the subtle rule; it leaves room for the sheet when one is open. |
| V11 | The mint dialog's label / field grid; the mode-change dialog's workload pills. | The repo's stacked fields and its selection list stand, restyled. |
| V12 | Empty-state steps with design copy. | The repo's copy stands; the steps take the design's bordered card. The v3 icon disc above the heading is not drawn. |
| V13 | The account popover without the control plane's version line. | The version line stays (v3 E9). |
| V14 | The search pill at 208px reading "Search…". | The repo's placeholder ("Search workloads and rulesets…") stands, in a 240px pill. |
| V15 | Peer, host and ruleset names in the sans; some versions and addresses in the sans. | Names (hostnames, peers, rulesets, token names) move to the sans as drawn; data values (addresses, versions, services, identifiers, rule ids) stay in mono, as v3's "the system speaks in mono" rule had it for data, except on the Overview, whose field values follow the design: the sans with tabular figures, with addresses, the identity and credential serials in mono. |
| V16 | The review's pair detail at 340px. | 360px, so the pair table's four columns fit. The map's sheet is 344px as drawn. |
| V17 | Promote dialog title "Enforce inbound on this scope". | The repo's title ("Switch scope to enforced") and copy stand. |

### Changes ruled in review (2026-11-27)

These depart from the designs by the maintainer's ruling on the first build's captures.

- **Simulation review figures** are four equal cards (erratum V5).
- **Filter panels are a secondary rail.** On a workload's Inbound flows and Listening services (the only screens with the filter panel), the panel is a full-height 232px rail directly right of the sidebar, in the pattern of the policy editor's ruleset rail: the ground, the subtle rule on its right, no card border or radius, its own vertical scroll, its section headers as before. The table area takes the remaining width at the 32px gutter. Filters and behaviour are unchanged. The designs draw the panel as a card beside the table.
- **The account control is in the header** and the **Overview takes the design's grid** (structural change 1, errata V1 and V8).
- **Light-theme contrast**: see Accessibility.
- **Dark neutrals one step darker.** The whole dark neutral ramp moves one step down, keeping its spacing: ground `#0f100f`, surface `#151716`, selected / hover fill `#1c1e1d`, control fill `#191b1a`, border `#282a29`, subtle rule `#1f2120` (each was 4 levels lighter: `#131413`, `#181a19`, `#202221`, `#1d1f1e`, `#2c2e2d`, `#232524`). The derived control-hover border follows (`#363837`) and the checkbox ink takes the new ground. Text, status and primary colours are unchanged; the designs draw the earlier ramp.
- **The sidebar is 208px wide** when expanded (`sidebar-width`; the designs draw 232px). The collapsed rail stays 48px; the secondary rails keep their widths (the policy editor's ruleset rail 216px, the filter rail 232px).
- **The collapse toggle is icon-only** in the expanded state too: the same icon at the same place, its accessible name and a tooltip saying the action ("Collapse sidebar" / "Expand sidebar").

### Build decisions (style, not content)

- **Sidebar ground.** The token table lists the sidebar with the surface; every design draws it on the ground behind the subtle rule, continuous with the header. Built as the designs draw it.
- **Control height.** Designs draw 26px buttons; the package's density note says 28–32px. `control` is 28px; the map's toolbar controls and the filter chips are 26px as drawn.
- **Edge width.** Designs draw 1.25px edges; `edge-width` is 1.5px, one width for every edge, for legibility in the light theme at 1×.
- **Synced** reads in text 2 in lists (as the designs draw it); only states that need attention take a status color.
- **Status badges** in drawers and dialogs are pills on the neutral border with the toned glyph and the text-toned word.

---

# Design package v3 (superseded by v4, locked 2026-11-27)

**Dated:** 2026-10-14 / 2026-10-15.

**Status:** the **design of record: v3, locked 2026-10-15** (Avi), after three lab-preview polish rounds. W5's wide-screen containment was withdrawn in the third and is recorded in amendment 6.

**Lock:** v3 locked on the lab-estate live preview against real data (brief §8), as the gate required.
- The CC build was a preview vehicle on a branch (#118).
- Avi deployed it with `./lab-deploy.sh` and ruled three polish rounds there: round 1 (P1–P14), round 2 (D1 and the first-frame theme), and round 3 (W5 and W5-a, withdrawn as W5-b; amendment 6). The lock rulings followed on 2026-10-15 (E15, P13-a).
- #118 then merged, and v3 ships in v0.2.0.

**Supersedes:** package v2, which stays in history (`git log -- docs/design`).

*Status updated 2026-10-16: this header read "proposed, locks only on the lab-estate live preview" until #118 merged after the lock ruling.*

## Contents

| Path | What |
| --- | --- |
| `tokens.json` | All v3 tokens, with amendment 1 (`caption`) in two themes (dark first = default, then light), with usage notes. Glyph colors are aliases (`flow-*`, `health-*`, `severity-*` → `status-*-fg`). |
| `tokens.css` | **Generated** from `tokens.json` (amendment 1's `.type-caption` added; font URLs point at `ui/src/fonts/`): `@font-face` rules, `:root` / `[data-theme="dark"]`, `[data-theme="light"]`, the non-color tokens, and the `.type-*` style classes. Do not hand-edit. |
| `brand-book.md` | The **Console v3** record: principles, type, grounds, borders, accent, status palette, controls, density and layout, navigation, patterns, the dated IA-change entry, iconography, placeholders, errata. It is identical to the design-system artifact's `console-v3.md` section. |
| `components/` | Component specs: `Sidebar.md` (E1 order), `HeaderStrip-and-Tabs.md`, `StateChip-Popover-AlertCard.md`, `FilterPanel.md`, `Controls-Badges-Chips.md` (includes light plate B), and `Pages-and-Patterns.md`. |
| `fonts/` (in this repository: `ui/src/fonts/`) | DM Sans (variable wght 100–1000) and Source Code Pro (variable wght 200–900), latin and latin-ext woff2, plus `OFL-DMSans.txt` and `OFL-SourceCodePro.txt` (SIL OFL 1.1). Self-host them; make no third-party font requests. |
| `icons/` | The Lucide **1.47.0** (ISC) chrome-icon subset: `lucide-subset.json` (icon nodes), `svg/`, and `LUCIDE-LICENSE.txt`. There are 24 names; **`search` and `check` are new in v3.** |
| `records/README.md` | Where the exploration records live. |

## Provenance

| Source | Detail |
| --- | --- |
| Brief | `innerwall/exploration-6-brief-console-v3.md` (Innerwall project), dated 2026-10-14. Includes **Erratum 1**: warning hue is per theme, dark 75 and light 58. |
| [origin withheld] | Origin references are withheld in this repository (erratum E1). The unredacted provenance is in the project records and the design-system artifact. |
| Exploration 6 canvas (Claude Design) | Boards 0–2, the screens round, the Workload detail IA rounds 3–4, and the States board. Rulings are recorded on its notes. |
| Amendment record | `innerwall/exploration-6-amendment-f2a-light-warning.md`: light-warning set and W1 waiver. |
| IA-change record | `innerwall/exploration-6-ia-workload-detail.md`: A′, as amended in round 4. |
| Rulings by Avi | R1–R3, F1–F6 and nits, F2-a, W1–W3, S1–S8, round 3 (A′), round 4 (R4.1, R4.2), and close-out. All dated 2026-10-14 or 2026-10-15. |

**Naming rule:** [origin withheld]. In this repository the stricter rule holds: no origin reference anywhere (CLAUDE.md, erratum E1).

## Accessibility — one recorded waiver (W1)

AA contrast holds on every shipped pair **except one dated, scoped waiver**.

| | |
| --- | --- |
| **Waived** | The light-theme warning foreground `status-warn-fg` = `#dc7b18`, oklch(0.677 0.155 58.1) [origin withheld]. **2.84:1** on the light canvas and **2.49:1** on its own tint. Ruled by Avi on 2026-10-14. |
| **Scope** | The warning word and glyph in warning contexts only. **Not** ok, critical or neutral; **not** the dark theme (the default); **not** body text, since banner, card and popover sentences are always text-primary; **not** any non-warning use. Any new sub-AA color needs a new waiver. |
| **Mitigation** | Status is never shown by color alone: glyph shape plus word always. Warning chips also carry the `#f3ba63` border and the `#ac5800` @10% tint. |
| **AA fallback** | **`#a94f00`** = oklch(0.532 0.139 50.8), at 5.14 plain and 4.51 on its tint. This is the seam for a possible a11y-strict deployment toggle, which is not built. |
| **Build instruction** | Do not "fix" it, and do not extend it. |

**Worst AA pair:** light ok `#137738` on its own tint, **4.57:1**.

## Key values (full list in `tokens.json`)

- **Type (R1):** DM Sans at 400 / 500 / 600 and Source Code Pro, with tabular numerals on numeric cells.
- **Header register:** 13/20 at 500.
- **Dark ground B (R2):** `#0a0c0a` → `#0f110f` → `#121412` → `#151715` → `#1a1c1a`. Elevation ratios are 1 / 1.5 / 2 / 3 at a 0.025 L step.
- **Light ground (F4):** `#f6f7f6` → `#fefffe`, i.e. 0.975 → 1.0.
- **Borders:** foreground at 7 / 13 / 13 / 17 / 28%. Hover is shown by strengthening the border.
- **Accent:**
  - dark lime `#80ee64`, ring at 55%;
  - light `#007f35`, ring at **80%** (F3).
- **Critical (F1):** dark text and glyph `#e2726b`; destructive fill `#b54a46`.
- **Plates:**
  - dark `#09af58` + `#071009`;
  - **light plate B** `#007f35` + white;
  - hover and pressed per S6.
- **Row pitch (R3):** **40px border-box.**
- **Layout sizes:**
  - header strip 48;
  - tab row 40;
  - chip 22;
  - filter panel 280;
  - `content-max` **1120**.
- **Flow map (W2):** `edge-width` **2px**, one width for every edge.
- **Tabs:** underlined in **text-primary**. The pill marks primary navigation only.

## Build notes (for the CC session)

- **Repo controls functionality; the package controls style.** Board copy is non-normative. Diff every rendered string against the repo, and record deviations as design-package errata in-repo.
- **Placeholders the repo owns:**
  - sign-in fields (S3);
  - token value, prefix and expiry options (S4);
  - "Mint token" placement and 404 → Simulation review (S7).
- **The sidebar never auto-collapses.** Collapse is user-only.
- **Workloads Mode and Agent-version columns:** build only where the data is already served; otherwise escalate.
- **Workload detail Overview** is the one deliberate product IA change, per the dated entry in `brand-book.md` §11.
- **Lucide:** add `search` and `check` to the bundle. This also corrects the v2 README's bundle-claim erratum (F5).

---

# In this repository

Recorded by the console v3 build (INN-73), 2026-10-15. The package above is the attached package v3 with its origin references withheld (E1). It supersedes design package v2, which stays in history (`git log -- docs/design`). Rulings are Avi's, of 2026-10-15, on the build's Task 0 escalations 1–19. Each item below is mirrored into the design-system artifact at lock: recorded, never silently fixed.

## Where each piece lives

| Package piece | Where it lives |
| --- | --- |
| `tokens.css` | `ui/src/tokens.css`, names and values verbatim (amendment 1 included); font URLs resolve to `ui/src/fonts/`. `docs/design/tokens.css` is the same file as recorded here. |
| `tokens.json` | `docs/design/tokens.json`, with amendment 1 and the E1 redaction |
| Utility names | `ui/src/index.css` maps the tokens onto utility names; the tokens are imported into the base layer, so their `.type-*` classes sit under the utilities |
| Fonts | `ui/src/fonts/`: DM Sans 4.004 and Source Code Pro 1.026 variable woff2, latin and latin-ext, each beside its license (`OFL-DMSans.txt`, `OFL-SourceCodePro.txt`). The build writes each license beside the fonts in `dist/assets/`. Geist is retired from the console. |
| Chrome icons | `lucide-react` pinned at 1.47.0, behind `ui/src/components/Icon.tsx`: the v3 subset plus `info` (E10). The bundle carries every name the screens use, which corrects the v2 bundle-claim erratum (F5). |
| Status and severity glyphs | `ui/src/components/StatusGlyph.tsx` (custom set, never Lucide) |
| Component specs | `docs/design/components/`; the console's components follow them as amended below |

## Amendments

- **Amendment 1 (ruling 4): the `caption` type style.** 12/16 at 400 in DM Sans, for help text under a field or a section: notes and explanations, about 95 uses in the console. The v3 scale missed this role. It is in `tokens.json` (`type.groups[UI].styles`) and `tokens.css` (`.type-caption`).
- **Amendment 2 (ruling 9): two more state chips.**
  - Enforcement removed: critical, severity `error` glyph, word "Enforcement removed".
  - Degraded (apply failed): warn, health `degraded` glyph.
  - These sit beside the four in `StateChip-Popover-AlertCard.md`. Order: critical first, the removed table first of all; then the warnings. Popover and alert-card sentences reuse the repo's existing copy verbatim. Reconnecting rides the offline chip under the repo's word "Reconnecting".
- **Amendment 3 (ruling 11): Resend in the offline popover.**
  - The control plane refuses a resend to an agent it records as offline (`agent-offline`; a reconnecting agent is offline too). So the offline popover's and alert card's **Resend snapshot** is held disabled (`aria-disabled`).
  - The refusal reason is its caption and its title: "Refused while the agent is offline; it is sent a fresh snapshot whenever it next connects."
  - The Status card keeps Resend in every state, as before.
- **Amendment 4 (ruling 18): the search field in the W3 strip.** The header strip keeps the console's search field (INN-39) at its right, as a 26px sunk field.
- **Amendment 5 (ruling 12): the popover sentence limit.** The one-sentence limit is waived for the clock-skew state, whose existing text is three sentences and is kept whole.
- **Amendment 6 (W5, W5-a): wide-screen page containment. Withdrawn 2026-10-15 (W5-b).** Both variants were judged on a 2560px preview and reverted. Every page is full-bleed again, as R4.2 has it, and the `page-max` token is removed. `content-max` (1120) on the Overview column is unaffected.
  - *W5, a whole-page container* (`page-max` 1600, centered): rejected. Structural chrome and the filter rail went into a floating island, and hairlines stopped mid-screen.
  - *W5-a, docked rails with centered content*: rejected. It left a dead gutter between the rail and the table (about 220px on Inbound flows) that reads worse than a full-width table.
  - Full-bleed record tables at wide sizes are also the measured behavior of the reference the package was drawn against [origin withheld].
  - **The recorded seam, unexplored:** if wide-screen density comes back, the next attempt should be column-width caps inside full-bleed tables, not page containers.

## Accessibility

AA holds on every shipped pair except the one recorded waiver, **W1**: the light-theme warning foreground `#dc7b18`, for the warning word and glyph only. The AA fallback is `#a94f00`. It is not "fixed" and not extended.

The console scopes it in code:
- `SeverityNote` sets a sentence (`prose`) in text-primary beside the toned glyph.
- Alert cards and popovers set their bodies in text-primary.
- Only status words and glyphs take the warning tone.

## Errata

| # | Record | Ruling |
| --- | --- | --- |
| E1 | **Provenance redaction.** The package names a third-party brand as an accent source and cites a reference model throughout `PACKAGE.md`, `brand-book.md` and one `tokens.json` usage note. CLAUDE.md forbids origin-story references anywhere in the repository, which is stricter than the package's own naming rule. Those passages are marked `[origin withheld]`; the unredacted provenance lives in the project records and the design-system artifact. | 1: keep |
| E2 | **Source Code Pro license.** The package's `OFL-SourceCodePro.txt` carried the wrong copyright line ("Google Inc."), while the font files embed "© 2023 Adobe (http://www.adobe.com/), with Reserved Font Name 'Source'". `ui/src/fonts/OFL-SourceCodePro.txt` is Adobe's upstream `LICENSE.md` (adobe-fonts/source-code-pro, `release` @ 803b7e2, sha256 `7c940e28a5388e9bba866cf0e408edda45fe0899ba98665b8f6ab31dc5e4b8ff`), and THIRD-PARTY-NOTICES.md follows. For a package revision. | 2: fixed here |
| E3 | **Tabular numerals.** The package claims tabular figures, but its DM Sans files have proportional digits and no `tnum` feature. Numeric cells are set in Source Code Pro, which is fixed-width (existing practice), and the dead `tabular-nums` declarations are dropped. | 3 |
| E4 | **Inbound flows columns.** The package specifies Time · Decision · Source · Port · Matched rule · Flows. The repo's columns win: Decision · Source · Port · Process · Matched rule · Conns · Last seen. Source draws the peer's labels as label chips. | 7 |
| E5 | **Filtering over a paginated read.** "Client-side over served columns" sees only the loaded pages of the cursor-paged `/flows`. The panel's counts and result line are of the loaded rows and say so (as amended in polish round 1, P8), and the tab count stays the range total. Server-side filtering is INN-74. | 5 |
| E6 | **Listening services groups.** Decision and Source are not columns of that table. Its panel has Port, Process, and Covered by rule ("— no rule covers it"). | 8 |
| E7 | **Histogram (S5).** No time-bucketed flow read is served, so the Overview has no histogram and no reserved slot. The gap and `table_removed` history comes from `/flows/gaps`, and there is no credential event row (no event log is served). Follow-up INN-75 (ADR). | 14 |
| E8 | **Workloads table.** The repo's columns and headers stay (Hostname, Labels, Mode, Sync, Applied, Credential, Agent, Last seen); Agent is new, from the served `agent.version`. The package's collapsible "Removed" bucket with no glyph misreads "removed": in the console it is *enforcement removed on host*, critical with the error glyph (issues #64, #96). | 15 |
| E9 | **Account popover.** The control plane's version line (INN-42) stays; the package's spec omits it (INN-52). | 16 |
| E10 | **Lucide subset.** The v3 subset must include `info` (the policy editor's notes). It is registered beside the 24 names. | 17 |
| E11 | **Sidebar.** The last-opened workload entry under the four E1 sections stays. It is existing behavior, not an IA change. | 19 |
| E12 | **Strip title semantics.** Screens other than a workload's detail keep the breadcrumb in the strip and their own in-content `h1`. Making every strip title an `h1` would duplicate a ruleset's or a verdict's heading. On a workload's detail the name in the strip is the `h1`. | build |
| E13 | **Flow map nodes and chips.** Nodes are 176 × 64: the console draws a third line (mode, or the unlabeled warning). The on-map chip keeps the repo's count; the package's count-in-chip proposal stays unapplied, and the chip is restyled only. | build |
| E14 | **Overview cards.** Card titles are the repo's ("Status", "Labels", "Host facts"; the package's board said "Status and sync"). The agent version stays a host fact, not a status row. The Status card keeps the credential row and the apply error. The removed and degraded explanations move to their alert cards. | build |
| E15 | **"Shown once" badge.** The package specifies a "shown once" warning badge before the mint dialog's sentence. The repo has no such string, and the package implies the copy rather than specifying the repo's. It is **not built**: the repo's sentence stands, shown as prose with its glyph, and the secret sits in the sunk well. For a package revision. | lock: keep the repo sentence |
| E16 | **Totals figures.** "Mono 24/28" has no package type style; the console keeps `type-mono-figure` locally in `index.css`. | build |
| E17 | **Sign-in title.** The repo's sign-in has no page title (S3 placeholder; the repo owns the flow). `page-title` is used on the 404. | build |

## Polish round 1 (live lab walkthrough, ruled 2026-10-15)

Display and style only, within the package's rules. Wordings are as ruled.

| # | Change |
| --- | --- |
| P1 | **Simulation review stat tiles.** The verdict banner holds only the headline, its sentence and the caveat lines. The four figures are a tile row under it: `surface-card` tiles, each a mono-uppercase label (`meta-label`) over a mono 24/28 figure. The labels: **WOULD-BLOCK · PAIRS** (would-block glyph), **CONNECTIONS**, **WORKLOADS AFFECTED** (with "/ N simulating" in text-tertiary), **ALLOWED** (allowed glyph). |
| P2 | The scope line sits at the right of the ruleset row in text-tertiary, truncating, with a tooltip; one band fewer before the table. |
| P3 | The UTC windows readout is the time-range control's tooltip, on Simulation review and Flow map. The text stays available to assistive technology. |
| P4 | View switchers are underline tabs: views are places. This covers the review's Grouped by peer / Peer × service matrix / By recency and, by the same rule, the map's Graph / Matrix. The decision chips are the only boxed elements in the review toolbar. |
| P5 | Review rows are one line: the peer, its kind tag, and its note, which truncates. The note "not managed, in no address group", the same on every row of the kind, is the `unknown` tag's tooltip. |
| P6 | The review's connection mini-bars are **neutral and kept**: the `border-stronger` tone on the selected wash. They compare volume, and no status tone is used as decoration. |
| P7 | When an alert card says a workload's state, the Status card keeps the facts but drops its health line (removed, offline or reconnecting, degraded). Beside the credential-expired card, its credential row is plain text. Each alarm is said once. |
| P8 | **Inbound flows result line:** "*N* of *M* loaded", where *M* is the range's total (the tab's count, from the rollup). With a filter or search on, it leads with "*K* match ·". It ends with **"filter counts cover loaded rows"**. Replaces "N of M loaded · more not loaded". |
| P9 | **Empty optional columns are hidden.** A column no loaded row has a value for (Inbound flows' Process; Listening services' Process and Path) is left out until one does. This is display logic, not a schema change. |
| P10 | **Flow-map edge labels** keep clear of each other and of the nodes. A chip that would collide slides along its own edge to the nearest free place; only when its curve has none is it moved off it. |
| P11 | The unmanaged-peer caption ("unmanaged · no address group") is set in the sans at 11px, and the node's side padding is 8px, so it fits the 176px node. It ellipsizes with a tooltip if it ever cannot. |
| P12 | The map legend keeps short labels. "unmanaged peer" carries the full explanation as its tooltip, on hover and keyboard focus. |
| P13 | The map's evidence warning is the short form **"evidence incomplete · N workload(s)"** (warn glyph and tone). The full sentence and every gap are its tooltip, on hover and keyboard focus. |
| P13-a | *Ruled at lock, 2026-10-15.* The map's dropped-records warning takes the same short form: **"flow records dropped · N workload(s)"** (warn glyph and tone). The repo's sentence ("N workload(s) dropped flow records — map may be incomplete.") and each workload's dropped-record counter are its tooltip, on hover and keyboard focus. |
| P14 | Policy section headers are the mono-upper label alone ("SCOPE", "INBOUND RULES"), each with a text-tertiary sentence-case line under it ("Workloads this ruleset protects", "Who may connect to scoped workloads, on what"). |

## Polish round 2 (ruled 2026-10-15)

### Corrected deviations

A deviation the build introduced, recorded once it was corrected against the package. It is not a package erratum.

| # | Deviation | Correction |
| --- | --- | --- |
| D1 | **Narrow plate mapping.** The build gave the primary plate to the review's **Promote to enforced…** only when the verdict was safe, and drew it as a secondary button otherwise. The promote dialog's confirm overrode the plate's disabled state with a class the v3 vocabulary does not define, so a held-back confirm looked live. This read the accent as a verdict. | Per the button spec (`components/Controls-Badges-Chips.md`): the accent marks the view's primary action, never a verdict. Severity is carried by the verdict banner, the acknowledgment, and the disabled gate. The Simulation strip's **Promote to enforced…** and the dialog's enabled confirm (**Enforce anyway…** included) take the plate whatever the verdict. A held-back confirm is the plate's own disabled state (`surface-overlay`, text-disabled) and stays focusable so its reason can be reached. The mint dialog's confirm was already the plate. **Edit ruleset**, **Add rule** and **Revoke** stay neutral. |

## Copy this build adds

These strings are new to the repository; every other rendered string is the repo's.
- Tab: "Overview".
- State chip words:
  - "Credential expired", "Clock skew", "Evidence gap" (ruling 12);
  - "Enforcement removed", shortened from the repo's "Enforcement removed on host" (amendment 2).
- The offline refusal caption (amendment 3).
- Filter panel:
  - "Filters", "Reset";
  - search placeholders "Search flows" and "Search services";
  - group titles "Decision", "Port", "Source", "Matched rule", "Process", "Covered by rule";
  - "— no rule covers it" (ruling 8);
  - "address group" as a Source annotation.
- Result lines and empties:
  - "N of M loaded", "K match ·", "filter counts cover loaded rows" (P8; "· more not loaded" is retired);
  - "N of M services";
  - "No loaded flows match these filters."
- The Overview's history title, "Evidence gaps" (the repo's field name).
- Polish round 1:
  - the stat-tile labels (P1);
  - "evidence incomplete · N workload(s)" (P13);
  - "flow records dropped · N workload(s)" and the tooltip's "<host>: N dropped" lines (P13-a);
  - "unmanaged peer" as the legend's short label (P12);
  - the policy header split into "Scope" / "Inbound rules" with their sentence-case lines (P14).

## Screens the package has no board for

Recorded 2026-10-17 (INN-90). These screens are built from the package's own patterns and add no tokens, components, or icons; a package revision may draw them.

- **Definitions lists** (`/policy/services`, `/policy/address-groups`). The policy rail's two Definitions rows lead to them and take the ruleset rows' states (hover `wash-hover`, current `wash-selected` at 500). Each list is a data page: an in-content `h1` (E12) with a sentence in text-secondary and the view's primary action on the plate ("New service", "New address group", as "Mint token" is), then one table on `surface-card` with 40px rows (Name with its version, Entries or CIDRs as solid chips, Referenced by as ruleset links with each rule's description, Edit).
- **The definition dialog** is the standard dialog (`components/Pages-and-Patterns.md`): name field, the members as removable chips with the chip input below, findings under what they name, the plate confirm. The rule editor opens the same dialog from a "does not exist" finding, as a caption link under it.
- **A reference chip's title** is what it stands for (`tcp/5432`, a group's CIDRs), on hover.
- Copy added: the two list titles and their sentences, "Referenced by", "No rule references it.", the empty-list lines, the dialog titles ("New service", "Edit service *name*", and the address-group pair), its sentences and confirms, "Create service *name*…" / "Create address group *name*…", and the write notices.


## Design-package extensions: retirement and the credential's standing

Recorded 2026-10-21 (INN-63, INN-70, ADR-0023), **proposed for the maintainer's acceptance**. The package has no board for a retired workload, for a credential's standing against the renewal grace, or for the credential a live stream presented. These states are composed from the package's own patterns, as the sign-in screen was; they add no tokens, no components, and no icons (`circle-minus` is in the v3 subset). A package revision may draw them.

- **Retired is chrome, not status.** It is the operator's decision, not workload health, so it never takes a status glyph or tint: the Lucide `circle-minus` at icon-default and the word **Retired** in `status-neutral-fg`, as a provisioning token's status is drawn. It therefore reads apart from Offline (the health `offline` glyph in the critical tone). Where it stands:
  - **the fleet's chip row**: a **retired** chip after the state chips, behind a hairline and in its own group ("Lifecycle"), with its count from the control plane's counts. It lists the retired workloads in place of the active fleet (the surface lists them apart), most recently retired first. The column header reads "Lifecycle", the hostname is in text-secondary, the credential cell is "refused · retired" in text-tertiary, and the rows have no checkbox. The package's collapsible "Removed" bucket is not used for it: E8 gave "removed" its meaning in the console, and the surface never lists the two together.
  - **the workload's strip**: the icon in place of the health glyph, and "Retired *1d ago*" in place of the mode's "change…".
  - **the Overview**: a card above the cards on `surface-card` with a hairline (not an alert card: nothing is wrong and nothing is to fix), its lead the icon and "Retired *1d ago*", its sentence the instant and what retiring means. The Status card's health line becomes the caption "As last recorded before the retirement; nothing here is live."; its loss counters are plain; **Resend snapshot**, the label edit (its place reads "frozen"), and the mode change are gone. Of the state chips and alert cards only clock skew and evidence gap remain.
  - **the sidebar's open workload**: the icon in place of the health glyph.
- **The retire dialog** is the standard dialog in the acknowledgment pattern of the promote dialog (`components/Pages-and-Patterns.md`): a sentence on what retiring is for, the list of what it does, a caption that the token is not revoked, the sunk acknowledgment checkbox, and the **destructive** plate as the confirm (irreversible, as the token revocation's is), held in its disabled state and kept focusable until acknowledged. For a selection it lists the hostnames as the promote dialog's preview does. Refusals render per workload on the canvas well: "Already retired *ago* (*instant*); nothing changed." on the alert tone, "No workload is registered under this id; nothing was retired." on the error tone.
- **Retire…** is a secondary button in Host facts, under the identity it ends, on a hairline with a caption, as **Resend snapshot** sits in the Status card. **Retire selected…** is a secondary button beside **Change mode for selected…**.
- **The credential's standing** (Status card, credential row): the phrase in its tone as before, with a caption under it; past the grace, the caption link **Mint a provisioning token**, and the alert card's action is the same link as a secondary button. The fleet column keeps to its width: "expired 5h ago · in grace" and "expired 4d ago · re-enroll", each with its sentence as the cell's title.
- **The credential issued and the credential presented** are two more field rows of the Status card, "issued" and "in use" (or "presented" while the stream is down), each a mono serial with a caption. A presented serial that is not the one issued, on a live stream, takes the alert severity glyph and tone with "not the one issued", and a caption says what the divergence can mean.

Copy added: "Retired", "retired *ago*", "refused · retired", "refused · workload retired", "retired" (chip), "Lifecycle" (column and group), "No active workloads; *N* workloads are retired.", "Show retired", "Showing *N* · most recently retired first", "frozen", "Retire…", "Retire selected…", the retired card's sentence, the Status card's retired caption, the retire dialog's titles ("Retire *host*?", "Retire *N* workloads?"), sentences, acknowledgment, confirms ("Retire workload", "Retire *N* workloads"), nudge, and outcomes; the credential's phrases ("valid · renews by *instant*", "expired *ago* · in grace", "expired *ago* · re-enroll", "expired *ago* · will recover by renewal", "expired *ago* · needs re-enrollment") and their captions, "Mint a provisioning token", the field labels "issued", "in use", "presented", "not the one issued", and their captions.

## Design-package extensions: a change to enforced outside the review

Recorded 2026-11-25 (#94), **proposed for the maintainer's acceptance**. The package draws the promote dialog and its acknowledgment variant, and has no board for the mode-change dialog (the fleet's **Change mode for selected…** and the workload strip's **change…**). A change to enforced from that dialog is composed from the promote dialog's own patterns; it adds no tokens, no components, and no icons.

- **The verdict block** is the promote dialog's, under the target-mode chips, drawn only when the target is Enforced and the selection holds a workload not yet enforced: the flow `allowed` glyph with "Safe to enforce" and "No observed traffic would be dropped." when the review would call every such workload safe; otherwise the severity `error` glyph with "Not safe to enforce yet" and the counts as its line. Each verdict is the one the simulation review composes for a scope of that workload alone, over the review's default range.
- **The workloads behind the counts** sit under the block on the canvas well, rows on hairlines as in the promote dialog's partition: "Never simulated" with the hostnames as links, each workload not safe to enforce with its hostname as a link and the review's first failing condition on the alert tone, then "Safe to enforce" and "Already enforced" with their counts.
- **The acknowledgment** is the promote dialog's sunk checkbox and nudge, one for the whole selection. The confirm is the plate, held in its disabled state and kept focusable while the evidence is read and until acknowledged; it reads "Enforce anyway on *N* workloads" when the acknowledgment is asked, and keeps "Change to enforced" otherwise.
- **Visibility and Simulation** draw none of it: they only loosen, and stay one step.

Copy added: "No simulation evidence for *N* of *M* selected", "Not safe to enforce for *N* of *M* selected", "Never simulated", "no verdict", "+*N* more not safe to enforce", the caption ("Each verdict is the one the simulation review composes for the workload over the last *range*." and, when the acknowledgment is asked, "This enforces the selection as chosen; the review promotes only the workloads that earned it."), the acknowledgments ("I understand this workload is not safe to enforce yet", "I understand *N* of these *M* workloads are not safe to enforce yet"), and "Loading each workload's evidence…". The headlines, the safe line, the nudge, and "Enforce anyway on *N* workloads" are the promote dialog's.
