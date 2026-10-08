import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";
import { cn } from "@/lib/utils";

// The design's button forms: the primary, inverted monochrome and one
// per view, whose icon takes its label color; the secondary, a hairline
// control on the canvas; and ghost, for popover items and text-like
// controls. Heights are the control sizes: control-md by default,
// control-sm in toolbars, control-lg for a dialog's primary action.
const buttonVariants = cva(
	"inline-flex shrink-0 cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-md border type-ui-strong transition-colors disabled:pointer-events-none disabled:opacity-disabled aria-disabled:cursor-default",
	{
		variants: {
			variant: {
				primary:
					"border-action-primary-bg bg-action-primary-bg text-action-primary-fg hover:border-action-primary-hover hover:bg-action-primary-hover [&_[data-slot=icon]]:text-action-primary-fg",
				secondary:
					"border-default bg-app text-primary hover:border-strong hover:bg-hover",
				ghost:
					"border-transparent bg-transparent text-secondary hover:bg-hover hover:text-primary",
			},
			size: {
				default: "h-control-md px-3",
				sm: "h-control-sm px-2.5 text-[12px]",
				lg: "h-control-lg px-4",
				block: "h-control-md w-full px-3",
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
