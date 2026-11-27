import type * as React from "react";
import { cn } from "@/lib/utils";

export function Input({ className, ...props }: React.ComponentProps<"input">) {
	return (
		<input
			data-slot="input"
			className={cn(
				"h-control w-full min-w-0 rounded-control border border-strong bg-field-sunk px-2.5 type-dense text-primary placeholder:text-disabled hover:border-control-hover disabled:border-strong disabled:bg-overlay disabled:text-disabled aria-invalid:border-status-critical-fg",
				className,
			)}
			{...props}
		/>
	);
}
