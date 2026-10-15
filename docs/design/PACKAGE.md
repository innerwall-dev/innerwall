# Innerwall Console — design package v3 (proposed)

**Dated:** 2026-10-14 / 2026-10-15.

**Status:** the **design of record, proposed.** The direction held on 2026-10-15 (Avi).

**Lock:** v3 **locks only on the lab-estate live preview against real data** (brief §8).
- The CC build is a preview vehicle on a branch.
- Avi deploys the branch with `./lab-deploy.sh` and rules the lock there.
- The PR then merges and ships in v0.2.0.

**Supersedes:** package v2, *on lock*. v2 stays the shipped console until then.

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
| E15 | **"Shown once" badge.** The package specifies a "shown once" warning badge before the mint dialog's sentence. The repo has no such string, so it is **not built** and is escalated as package-implied copy. The sentence is shown as prose with its glyph, and the secret sits in the sunk well. | escalated |
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
  - "unmanaged peer" as the legend's short label (P12);
  - the policy header split into "Scope" / "Inbound rules" with their sentence-case lines (P14).
