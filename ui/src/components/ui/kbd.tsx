import type * as React from "react";
import { cn } from "@/lib/utils";

export function Kbd({ className, ...props }: React.ComponentProps<"kbd">) {
	return (
		<kbd
			className={cn(
				"rounded-sm border border-strong px-[5px] font-mono text-[10px]",
				className,
			)}
			{...props}
		/>
	);
}
