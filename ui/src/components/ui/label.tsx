import type * as React from "react";
import { cn } from "@/lib/utils";

// Eyebrow label: the small uppercase caption the screens set above
// fields, sections, and sidebar blocks.
export function Label({ className, ...props }: React.ComponentProps<"label">) {
	return (
		// biome-ignore lint/a11y/noLabelWithoutControl: the caller associates it with its control through htmlFor
		<label
			data-slot="label"
			className={cn(
				"block text-[11px] font-medium uppercase tracking-[0.08em] text-muted-foreground",
				className,
			)}
			{...props}
		/>
	);
}
