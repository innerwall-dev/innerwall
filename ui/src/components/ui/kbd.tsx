import type * as React from "react";
import { cn } from "@/lib/utils";

export function Kbd({ className, ...props }: React.ComponentProps<"kbd">) {
	return (
		<kbd
			className={cn(
				"rounded-sm border border-default px-1 type-mono-xs text-tertiary",
				className,
			)}
			{...props}
		/>
	);
}
