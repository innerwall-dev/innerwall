import type * as React from "react";
import { cn } from "@/lib/utils";

export function Input({ className, ...props }: React.ComponentProps<"input">) {
	return (
		<input
			data-slot="input"
			className={cn(
				"h-9 w-full min-w-0 rounded border border-input bg-background px-3 text-[13px] text-foreground placeholder:text-muted-foreground focus-visible:border-ring focus-visible:outline-none aria-invalid:border-destructive",
				className,
			)}
			{...props}
		/>
	);
}
