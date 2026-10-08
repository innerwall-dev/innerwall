# Innerwall console design package v2

Provenance and verification record. Freeze pending maintainer confirmation.

## Sources (artifacts of record)
- Design system "Innerwall Console": https://claude.ai/artifact/5ecVdc3pkWNCC8NSA5tNGh (version 1791434754-84ee at packaging)
- Canvas "Innerwall Console Refresh" (16 artboards): https://claude.ai/artifact/1pzAJDRPmsZNzwP6cV93py (version 1791434823-11b9 at packaging)
- Brand mark geometry: verbatim from innerwall-dev/innerwall ui/public/favicon.svg (22×22), recolored to logo-outline / logo-fill.

## Contents
- tokens.json — token source of truth (verbatim from the design system).
- tokens.css — generated from tokens.json by the review track; both themes, @font-face included (adjust font paths to where ui/ serves them). Every token name machine-checked present.
- README.md — the brand book (principles, themes, status sub-palette + glyph pairings, type, patterns, iconography).
- components/ — Icon, Sidebar, StatusGlyph, StatusBadge, LabelChip specs.
- fonts/ — Geist + Geist Mono 1.7.2 variable woff2 (SIL OFL 1.1, OFL.txt). sha256:
  - Geist-Variable.woff2 a369fcf5628ea2aa4e1b9e2ec6a5b3624e365bda588e1f0f2f12b564f728fbb8
  - GeistMono-Variable.woff2 fba8f577f38a2bbcbe818efa6348dd58f36303a10b8737c42fefad275be563ab
- licenses/LUCIDE-LICENSE.txt — Lucide 1.47.0, ISC (chrome icons; the eight status glyphs are a custom set, exempt).
- reference/innerwall.css — the canvas's working stylesheet (tokens mirror + component CSS: sidebar, tables, chips, buttons). Reference, not source of truth; tokens.json governs.
- reference/Sidebar.reference.html — the canvas's shared Sidebar component markup (Lucide paths, brand mark, popover, collapsed behavior).

## Verified at packaging (independent re-computation)
- All status foregrounds >= 4.5:1 on bg-app, bg-subtle, bg-raised, bg-hover, bg-active, selection-bg and their own tints, both themes. Worst pair: light status-critical-fg on bg-active, 4.54:1.
- text-secondary on sidebar-bg: 7.31:1 dark, 8.10:1 light.
- Zero third-party runtime references in system or canvas (fonts self-hosted, Lucide paths bundled).
- Brand mark drawn only at native 22px (logo-size), tokens are aliases: logo-outline → text-secondary, logo-fill → text-primary.

## Known non-screens
- Policy nav entry has no designed screen (links nowhere by design).
- Promote dialogs live on a states board, per package convention.

## Amendment 1 (2026-10-08, maintainer-ruled): the severity glyph family

The console marks warnings and errors that are neither a flow decision nor workload health: evidence gaps, dropped records, stale renders, "no labels — matches no scope", the shown-once secret warning, promote-dialog caveats, credential renewal failed and expired, and every refusal, problem, and validation error. The eight status glyphs have no shape for these, and reusing one would collide meanings. So the package grows a third glyph family, **severity**, with two custom glyphs beside the eight:

| Glyph | Tone token | Shape |
| --- | --- | --- |
| `severity-alert` | `status-warn-fg` | outlined diamond with an interior `!` (vertical bar + dot) |
| `severity-error` | `status-critical-fg` | outlined octagon with an interior `×` |

Both are drawn on the 16×16 grid at a 1.5px stroke, at the `glyph-sm` / `glyph-md` / `glyph-lg` sizes, like the eight.

- **Family rule.** It is recorded alongside the flow/health rule:
  - flow decisions are outlined rings and a triangle;
  - workload health is solid or square-boxed;
  - severity is outlined pointed polygons.

  No shape repeats across the three families: the diamond is not would-block's triangle, and the octagon is not offline's square.
- **Status sentence.** "Status is color + glyph, always" now reads "status and severity are color + glyph, always".
- **Lucide rule.** Unchanged: a Lucide icon never carries a status color.
- **Mapping.** Severity glyphs take the tone column of the console's token map:
  - `alert`: evidence gaps, dropped records, stale renders, no-labels, the shown-once warning, promote-dialog caveats, credential renewal failed;
  - `error`: credential expired, and every refusal, problem, and validation error.
- **Design-system artifact.** The design-system artifact of record (StatusGlyph doc, README status section, legend board) is updated to match separately.

## Clarifications

- **2026-10-08, maintainer-ruled: `row-dense` is the row pitch, separator included.** The reference stylesheet gives `.iw-td` `height: var(--row-dense)` under `box-sizing: border-box`, so a body row's 1px `border-subtle` hairline sits inside the 36px. That leaves a 35px box for content and padding. In the fleet table, the 22px tinted mode pill takes 7px of padding above and 6px below. The token's value is unchanged; this records how the console measures it.

## Errata

- "Known non-screens" says the Policy nav entry "links nowhere by design". That sentence describes the canvas, not the repository. The repository's Policy entry links to `/policy`, the shipped policy editor.
- The README cites the Lucide license at `components/lib/LUCIDE-LICENSE.txt`. It ships at `licenses/LUCIDE-LICENSE.txt`.

## In this repository

| Package piece | Where it lives |
| --- | --- |
| `tokens.css` | `ui/src/tokens.css`, names and values verbatim; its font URLs resolve to `ui/src/fonts/` |
| Utility names | `ui/src/index.css` maps the tokens onto utility names |
| Fonts | `ui/src/fonts/` (`Geist-Variable.woff2`, `GeistMono-Variable.woff2`, `OFL.txt`), self-hosted and bundled; the build writes the OFL beside them in `dist/assets/` |
| Chrome icons | `lucide-react` pinned at 1.47.0, behind `ui/src/components/Icon.tsx` |
| Status and severity glyphs | `ui/src/components/StatusGlyph.tsx` |

`Icon.tsx` registers exactly the Icon spec's names plus `info` and `search`, added on the record. The status and severity glyphs are the custom set, never Lucide.
