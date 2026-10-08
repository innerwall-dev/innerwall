import type * as React from "react";
import { cn } from "@/lib/utils";

export function Input({ className, ...props }: React.ComponentProps<"input">) {
	return (
		<input
			data-slot="input"
			className={cn(
				"h-control-md w-full min-w-0 rounded-md border border-strong bg-app px-3 type-ui text-primary placeholder:text-tertiary aria-invalid:border-status-critical-fg",
				className,
			)}
			{...props}
		/>
	);
}
