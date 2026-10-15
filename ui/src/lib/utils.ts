import { type ClassValue, clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// The package's named sizes, radii and containers (index.css maps them from
// tokens.css), taught to the merge so that h-control-sm after h-control
// replaces it instead of both reaching the cascade.
const twMerge = extendTailwindMerge({
	extend: {
		theme: {
			spacing: [
				"control",
				"control-sm",
				"control-md",
				"chip",
				"label-chip",
				"row-pitch",
				"row-header",
				"header-strip",
				"tab-row",
				"sidebar-item",
				"filter-panel",
				"popover",
				"popover-account",
				"avatar",
				"icon",
				"logo",
				"glyph-sm",
				"glyph-md",
				"glyph-lg",
			],
			radius: ["inner", "control", "panel"],
			container: ["content", "page"],
		},
	},
});

// cn merges class lists the way the component primitives expect: later
// utilities win over earlier ones of the same kind.
export function cn(...inputs: ClassValue[]): string {
	return twMerge(clsx(inputs));
}
