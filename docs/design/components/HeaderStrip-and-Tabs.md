# Header strip and underline tabs

## Header strip (W3)

- **Box:** 48px (`header-strip`), padding 0 20px, a bottom `border-hairline`. One quiet line.
- **Type:** every text item uses `header-register`: **13/20, weight 500.**
- **Hierarchy by color only:**
  - the title in text-primary; it stays the page's `h1` semantically;
  - secondary items in text-secondary;
  - tertiary items (for example `last seen <t>`) in text-tertiary.
- **Controls in the strip** (raised selects, plate, count badge) also set their labels at 13/500.
- **IA (E1):** no global breadcrumb bar, no renames.
- **Workload detail strip:** `Workloads /` (text-secondary, the page's own back-path) · health glyph (`glyph-md`) · workload name (text-primary) · mode pill (`chip` 22, pill radius, `border-overlay`, chrome icon and word in text-secondary) · spacer · `last seen <t>` (text-tertiary, time in mono).

## Underline tabs (round 3, neutral underline amended 2026-10-15)

- **Row:** `tab-row` 40px, padding 0 20px, 20px gap between tabs, a bottom `border-hairline`.
- **Tab:** a button with `role="tab"`, full row height, 0 2px padding, label in `header-register` 13/500.

| State | Treatment |
| --- | --- |
| Rest | text-secondary, no underline |
| Hover | text-primary |
| **Active** | text-primary and a **2px underline in `text-primary`**, overlapping the row hairline. **Never the accent:** the accent is reserved for the sidebar pill, focus rings and plates. |
| Focus | `accent-ring`, inset 2px, `radius-inner` |
| Disabled | text-disabled |

- **Right side of the tab row:** reserved for **state chips** (see StateChip.md). There is no extra chrome row.
- **Vocabulary rule:** the pill marks primary navigation; tabs are underlined.
