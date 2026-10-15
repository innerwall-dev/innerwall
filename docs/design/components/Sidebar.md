# Sidebar (E1 order)

The console's only global navigation. It is shadcn `Sidebar` with `collapsible="icon"`. The base behavior is unchanged: cookie persistence, ⌘B, and a mobile sheet below 768px. **Collapse is a user action only. Nothing auto-collapses, on any page (S7).**

## Anatomy

- **Rail:**
  - background `surface-canvas` (flat with the page), with a right hairline `border-hairline`;
  - padding 0 8px 8px;
  - width `sidebar-width` 232 / `sidebar-width-collapsed` 56 / `sidebar-sheet` 280 (mobile).
- **Brand row:** 48px tall, aligned with the header strip. The 22px mark (native size only, `logo-outline` and `logo-fill`, never the accent) plus "Innerwall" in `header-register` (13/20, 500, text-primary). Collapsed shows the mark only.
- **Items, in E1 order and without renames:**

| Item | Lucide icon |
| --- | --- |
| Simulation review | `flask-conical` |
| Flow map | `network` |
| Workloads | `server` |
| Policy | `scroll-text` |

  - Each item is `sidebar-item` 32px tall, `radius-control` 6, with 10px horizontal padding and a 10px icon–label gap.
  - Icons are 14px.
  - Labels use `dense` 14/20.
- **Footer:** a hairline above, then:
  - the Collapse button (`panel-left-close` / `panel-left-open`);
  - the account button (avatar 28 in `surface-overlay` with `border-overlay`, name at 13/500, site at 12/16 text-tertiary, `chevrons-up-down`).

## Item states

| State | Treatment |
| --- | --- |
| Rest | text-secondary, `icon-default` |
| Hover | `wash-hover` and text-primary |
| **Active** (`aria-current="page"`) | a **rounded outlined pill**: 1px `accent-edge` border, `accent-tint` fill, text-primary at 500, `icon-active`. **The pill marks primary navigation only.** Tabs use the underline. |
| Focus | `accent-ring`, 2px at a 1px offset |
| Collapsed | icon only; the accessible name and a `title` tooltip stay; the active pill stays |

## Account popover

- **Surface:** 240px on `surface-popover`, with `border-overlay` and `radius-panel`.
- **Contents:**
  - name and site;
  - a Theme segmented control (Light / Dark, using `sun` / `moon`) on a sunk track, with the selected segment raised;
  - **Sign out** (`log-out`) below a hairline.
- **Placement:** expanded, it opens above the footer; collapsed, it opens to the right.

## Notes

- The v2 bundled Sidebar (Fleet-first) is the v2 erratum under E1. This spec replaces it at lock.
