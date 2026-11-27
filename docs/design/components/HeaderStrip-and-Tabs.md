# Header strip and underline tabs

## v4 (proposed, INN-174)

What v4 changes; the spec below stands where this says nothing.

- **Header.** 48px across the whole width, above the sidebar and the screen, on the subtle rule, padded 13px left and 16px right. It leads with the logo and the wordmark (Manrope 14/20 at 600), then a text-4 separator and the screen's path, or a workload's identity.
- **Type.** The path is `header-register` at **400**: the section in text 3, the current screen in text 1, separators in text 4. Hierarchy stays color only.
- **Search.** The existing trigger moves to the right as a 28px pill on the control fill behind the border, its ⌘K hint in mono without a box.
- **Account.** At the far right, 10px after the search pill: a 26px circle on the selected fill behind the border, the operator's initials at 10.5px / 500 in text 2, the name as its title. Its popover (identity, control plane version, theme, sign out) opens below it, anchored to its right edge, on the surface with `shadow-overlay` and a 10px radius, kept 8px clear of the viewport's edge. It stays in the header on a phone.
- **Tabs.** Text 3 at rest and text 1 when active, 24px apart, on a 34px row (`tab-row`) with the subtle rule; the active tab's underline is a 1.5px text-1 inset (`tab-underline`). The row keeps the page's 32px gutter. Counts are captions in text 3.

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
