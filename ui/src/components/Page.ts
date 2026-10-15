// Page containment (W5 as corrected by W5-a). A page is chrome, rails,
// and content:
//
//   - Chrome rows (the header strip, tab rows, toolbar rows) span the
//     region they sit over, background and hairline; their contents align
//     to the page container, so controls sit over what they act on.
//   - Rails (a filter panel, the policy rail) dock against the primary
//     sidebar at full height; they are navigation, never centered.
//   - Content blocks (banners, tiles, tables, cards, forms, empty states)
//     sit in the page container: page-max, including the page's 20px
//     gutters, centered in the space right of any docked rail.
//
// Below page-max of available width the container is the full width, so
// a narrower screen renders as it did before containment. The Flow map is
// the one full-bleed page.

// page is the page container with its gutters: a content block, or a
// chrome row's contents.
export const page = "mx-auto w-full max-w-page px-5";

// pageBox is the page container for a table whose edge cells carry the
// gutters themselves, so its row hairlines run the container's width.
export const pageBox = "mx-auto w-full max-w-page";

// pageInner is the container inside a row that already sits within the
// page gutters (a hairline inset by them): the same content edges as page.
export const pageInner = "mx-auto w-full max-w-page-inner";
