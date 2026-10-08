import type * as React from "react";
import { cn } from "@/lib/utils";

// Field label: label type in sentence case, the caption the screens set
// above fields.
export function Label({ className, ...props }: React.ComponentProps<"label">) {
	return (
		// biome-ignore lint/a11y/noLabelWithoutControl: the caller associates it with its control through htmlFor
		<label
			data-slot="label"
			className={cn("block type-label text-secondary", className)}
			{...props}
		/>
	);
}
