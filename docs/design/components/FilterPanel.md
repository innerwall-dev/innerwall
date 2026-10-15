# Filter panel

- **Where:** Inbound flows and Listening services only. It sits left of the table.
- **Where not:** Overview and Applied policy have no panel.
- **Filtering:** client-side over columns the console already serves.

## Anatomy

- **Panel:**
  - `filter-panel` 280px on `surface-canvas`, with a right `border-hairline`;
  - padding 12, a 10px gap between parts;
  - scrolls vertically when the page is short.
- **Header:** "Filters" (13/20, 600) plus a ghost **Reset** button (`x`).
- **In-panel search:** a sunk field, 26px, with `search` and a placeholder.
- **Time range:** a raised select, 26px, with `clock` and `chevron-down`.
- **Groups:** collapsible titled sections.
  - Each group sits on a `border-hairline`, `radius-panel` box with 4px padding.
  - The title row is 30px: title at 13/500, an active-count pill "× N" (mono 11, `border-overlay`), and a chevron rotated −90° when collapsed.
- **Rows:** 30px `<label>` checkbox rows.

| Part | Treatment |
| --- | --- |
| Box | 14px; checked = `checkbox-on` with a `check` in `checkbox-ink`; unchecked = sunk with `border-stronger` |
| Glyph | optional, for Decision rows only |
| Label | 13px; mono 12px for ports, labels and rules |
| Annotation | optional; for example, "not managed" at 11px in text-tertiary |
| Count | right-aligned, mono 12 tabular, text-tertiary |

- **Row states:** hover `wash-hover`; zero-count rows in text-tertiary.

## Groups

| Group | Values |
| --- | --- |
| **Decision** | allowed, would-block, blocked, observed, each with its flow glyph. Replaces v2's all/allowed/would-block/blocked/observed chips. |
| **Port** | observed ports, e.g. `443/tcp` |
| **Source** | peer labels and unmanaged addresses, with the **"not managed"** annotation |
| **Matched rule** | **"— no rule matched"** first, then rule IDs, e.g. `bastion to nginx 99ffd253/tcp` |

## Table beside it

- **Result line:** 40px, showing the count and the active decisions.
- **Table:** 40px rows. Columns: Time · Decision (glyph and word) · Source (name and label chip) · Port (port chip) · Matched rule (mono 12; "— (no rule matched)" in text-tertiary) · Flows.
- **Width:** a 724px minimum; it scrolls sideways below that.
- **Table area at 1440 / 1280:** 926 / 766px.
