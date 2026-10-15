# State chip, popover and alert card (R4.1)

These present a workload's **exceptional states**:

| State | Glyph | Tone |
| --- | --- | --- |
| offline | health `offline` | critical |
| credential expired | severity `error` | critical |
| clock skew | severity `alert` | warn |
| evidence gap | severity `alert` | warn |

They replace v2's stacked banners and the clock-skew banner. Stacked banners are a **rejected** presentation.

## State chip

- **Placement:** right-aligned in the tab row, 6px apart, in **severity order (critical first)**. Shown on every page of the workload, Overview included, while the state is active.
- **Anatomy:**
  - a `<button>` with `aria-haspopup="dialog"` and `aria-expanded`;
  - `chip` 22px, `radius-inner` 4, 0 8px padding;
  - border `status-<tone>-border`, fill `status-<tone>-bg`;
  - glyph (`glyph-md`) plus word in `status-<tone>-fg`, `control` 12/16, 500.
- **Content limit:** glyph and word only. There is never a sentence in the chip.

| State | Treatment |
| --- | --- |
| Rest | tinted badge |
| Hover | an inset 1px `border-control-hover` (edge strengthens) |
| Open | `aria-expanded="true"`, `accent-ring` outline, popover shown |
| Focus | `accent-ring`, 2px at a 1px offset |

- **Overflow:**
  - If every active chip can't fit beside the tabs, the remainder collapses into one **"+N"** chip.
  - The "+N" chip has no glyph and uses the highest tone among the states it hides.
  - It opens the same popover, listing the hidden states.
  - At 1280 with all four states active, all four fit.

## Popover

- **Opened by** click or Enter on the chip.
- **Hover** may preview the popover but is never the only path. Escape closes it.
- **Only one popover is open at a time.**
- **Surface:**
  - `popover-width` 320 on `surface-popover`;
  - `border-overlay`, `radius-panel` 6, padding 12;
  - anchored below the chip, right-aligned, 8px gap.
- **Content, per state:**
  - glyph plus lead word in the tone (13/20, 500);
  - **one sentence in text-primary** (13/20);
  - the action where there is one. **Resend snapshot** (a secondary button) lives in the **offline** popover.

## Alert card (Overview inline)

- **Placement:** at the top of the Overview content column, above Status and sync. One card per active state, in severity order, 8px apart.
- **Anatomy:**
  - padding 10px 14px, `radius-panel`;
  - border `status-<tone>-border`, fill `status-<tone>-bg`;
  - glyph plus lead word in the tone (500);
  - body sentence in **text-primary**;
  - action on the right.
- The evidence-gap summary fact stays on the status card.

## Rules

- **Body text is always text-primary.** The light-warning waiver (W1) covers only the warning word and glyph.
- **Tints never sit on the selection wash.**
- **Rationale:** these states change how visible data must be read (stale last-seen, shifted timestamps, missing rows). The signal therefore stays persistently visible on data pages, and there are no keyboard or touch dead ends.
