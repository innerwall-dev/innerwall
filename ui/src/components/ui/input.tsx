import type * as React from "react";
import { cn } from "@/lib/utils";

export function Input({ className, ...props }: React.ComponentProps<"input">) {
	return (
		<input
			data-slot="input"
			className={cn(
				"h-9 w-full min-w-0 rounded-md border border-strong bg-app px-3 text-[13px] text-primary placeholder:text-tertiary focus-visible:border-selection-fg focus-visible:outline-none aria-invalid:border-status-critical-fg",
				className,
			)}
			{...props}
		/>
	);
}
