# Controls, badges and chips

## v4 (proposed, INN-174)

What v4 changes; the tables below stand where this says nothing.

- **Buttons.** `control` is **28px**, 12/16, a 6px radius. **Primary:** `plate` fill on `plate-border` with `plate-ink` at 500. **Neutral (secondary):** `control-raised` (the control fill) on `border-strong`, text 1, hover `border-control-hover`. **Disabled** (and `aria-disabled`, kept focusable): the selected fill (`surface-overlay`) on the border, text 4, at 400. **Destructive:** `destructive` with white ink at 500. Ghost is unchanged.
- **The promote button** (Simulation review) is neutral while the verdict is unsafe and primary otherwise (structural change 3); its enabled state is unchanged.
- **Status badges.** Plain: the glyph in the mark color and the word in the text color. In drawers and dialogs: a 22px **pill** on the neutral border, no fill.
- **Chips.** Label chips, rule chips, scope requirements, filter chips and choice chips are **pills**. Label chips are 20px on the border with no fill, Inter 12px, the key in text 3 and the value in text 2; in an editable field they take the selected fill. Filter and choice chips are 26px on the control fill; pressed, the selected fill with text 1.
- **Status takes no fill.** `status-*-bg` are transparent and `status-*-border` are the neutral border. A notice with a severity sits on the selected fill behind the status mark's border; a nudged acknowledgment takes the warning mark as its border.
- **Checkboxes.** Unchecked on the control fill behind a text-4 border (`border-stronger`); checked is unchanged (inverted monochrome).
- **Light plate B** (v3) is superseded by the v4 primary values in `PACKAGE.md`.

## Buttons

Default size is `control` 26px, with 12/16 text at 500, 10px side padding and `radius-control` 6. The small (34px) and medium (38px) sizes are for standalone forms.

| Variant | Rest | Hover | Active / pressed | Focus | Disabled |
| --- | --- | --- | --- | --- | --- |
| Primary (plate) | `plate` + `plate-ink` (600) | `plate-hover` | `plate-pressed` | `accent-ring` 2px / 1px offset | `surface-overlay` fill, `border-hairline`, text-disabled |
| Secondary | `control-raised` + `border-overlay`, text-primary | `border-control-hover` | `wash-selected` + `border-control-hover` | ring | transparent, `border-hairline`, text-disabled |
| Ghost | transparent, text-secondary | `wash-hover`, text-primary | `wash-selected` | ring | text-disabled |
| Destructive | `destructive` + `destructive-ink` | `destructive-hover` | `destructive-hover` | ring | as primary |

**Plate values:**

| Theme | Rest + ink | Hover | Pressed |
| --- | --- | --- | --- |
| Dark | `#09af58` + `#071009` (6.70) | `#2fbc5b` (7.79) | `#00a14f` (5.71) |
| **Light (plate B, S8)** | `#007f35` + **white** (5.14) | `#006e2b` (6.43) | `#006125` (7.67) |

- Light plate A (`#09af58` + `#071009`) is **retired**.
- **One plate per view.** Icons inside a plate take `plate-ink`.

## Fields and choosers

- **Text field (sunk):**
  - 26px (34px on forms), `field-sunk` fill, `border-strong`, `radius-control`;
  - hover `border-control-hover`; focus `accent-ring`; disabled transparent with `border-hairline`.
  - Mono values for labels and ports.
- **Select (raised):**
  - `control-raised` with `border-overlay` and `chevron-down`;
  - open = `wash-selected` + `border-control-hover`, chevron rotated 180°.
- **Segmented control:** a sunk track (`border-overlay`) with the selected segment raised (`control-raised` + `border-overlay`).
- **Checkbox:** 14px, `radius-inner`.
  - Checked = `checkbox-on` (inverted mono) with a `check`.
  - **There is no blue (F6).**
- **Links:** underlined at a 4px offset; text-secondary changing to text-primary on hover.

## Badges and chips

| Element | Spec |
| --- | --- |
| **Status badge (tinted)** | 22px, `radius-inner`, `status-<tone>-bg` + `-border`; glyph + word in `-fg` at 12/500. For content: verdicts, inspector states, "shown once". **A badge, not a banner.** |
| **Status word (plain)** | glyph + word in `-fg` at 12/500, used in table cells. |
| **Label chip** | `label-chip` 20px, `surface-overlay` fill, `border-hairline`; mono 12 `key=` in text-tertiary, value in text-primary. |
| **Port chip** | 20px, `border-hairline`, no fill, mono 12 tabular. |
| **Count badge** | a pill, `border-overlay`, text-secondary; 13/500 in the header strip, mono 12 elsewhere. |
| **Mode pill** | 22px pill, `border-overlay`, chrome icon (`eye` / `flask-conical` / `shield`) + word in text-secondary. **Never a status color (S2).** |
| **"new in N" pill** | 18px pill, `border-overlay`, 11/500 text-secondary. |
| **Removed rows** | text-tertiary, word "removed", **no glyph** (F6). |

## Status glyphs

- **Shapes** are unchanged from v2 and Amendment 1.
- **Colors** come from `status-<tone>-fg`, through the aliases `flow-*`, `health-*` and `severity-*`.
- **Verdict pair (S1):** Not safe = severity `error`; Safe = flow `allowed`.
- **State mappings:** credential expired = severity `error`; evidence gap = severity `alert`.
