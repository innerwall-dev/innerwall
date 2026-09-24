import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";
import { cn } from "@/lib/utils";

// The design's two button forms: the gold primary (with its hairline
// border, which the light theme draws in a darker gold and the dark
// theme in the fill color itself) and the outlined secondary. Ghost is
// for popover items and text-like controls. Buttons size by padding
// over the body line, as the design's do, and only the primary is
// semibold.
const buttonVariants = cva(
	"inline-flex shrink-0 cursor-pointer items-center justify-center gap-2 whitespace-nowrap rounded border font-sans text-[13px] transition-colors disabled:pointer-events-none disabled:opacity-50 aria-disabled:cursor-default",
	{
		variants: {
			variant: {
				primary:
					"border-primary-border bg-primary font-semibold text-primary-foreground hover:brightness-95",
				secondary:
					"border-input-strong bg-transparent text-foreground hover:bg-muted",
				ghost:
					"border-transparent bg-transparent text-foreground hover:bg-muted",
			},
			size: {
				default: "px-3 py-[7px]",
				sm: "px-2.5 py-[5px] text-[12px]",
				block: "w-full px-3 py-[7px]",
			},
		},
		defaultVariants: { variant: "primary", size: "default" },
	},
);

type ButtonProps = React.ComponentProps<"button"> &
	VariantProps<typeof buttonVariants> & {
		asChild?: boolean;
	};

export function Button({
	className,
	variant,
	size,
	asChild = false,
	type,
	...props
}: ButtonProps) {
	const Comp = asChild ? Slot : "button";
	return (
		<Comp
			data-slot="button"
			type={asChild ? undefined : (type ?? "button")}
			className={cn(buttonVariants({ variant, size }), className)}
			{...props}
		/>
	);
}

export { buttonVariants };
