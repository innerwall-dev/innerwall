import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";
import { cn } from "@/lib/utils";

// The design's two button forms: the gold primary (with its hairline
// border, which the light theme draws in a darker gold and the dark
// theme in the fill color itself) and the outlined secondary. Ghost is
// for popover items and text-like controls.
const buttonVariants = cva(
	"inline-flex shrink-0 cursor-pointer items-center justify-center gap-2 whitespace-nowrap rounded border font-sans text-[13px] font-semibold leading-none transition-colors disabled:pointer-events-none disabled:opacity-50 aria-disabled:cursor-default",
	{
		variants: {
			variant: {
				primary:
					"border-primary-border bg-primary text-primary-foreground hover:brightness-95",
				secondary:
					"border-input-strong bg-transparent text-foreground hover:bg-muted",
				ghost:
					"border-transparent bg-transparent font-normal text-foreground hover:bg-muted",
			},
			size: {
				default: "h-[34px] px-3.5",
				sm: "h-7 px-2.5 text-xs",
				block: "h-9 w-full px-4",
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
