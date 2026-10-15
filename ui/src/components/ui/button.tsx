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
	"inline-flex shrink-0 cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-control border type-header-register transition-colors disabled:pointer-events-none disabled:opacity-disabled aria-disabled:cursor-default",
	{
		variants: {
			variant: {
				primary:
					"border-action-primary-bg bg-plate text-plate-ink hover:border-action-primary-hover hover:bg-plate-hover [&_[data-slot=icon]]:text-plate-ink",
				secondary:
					"border-hairline bg-canvas text-primary hover:border-strong hover:bg-wash-hover",
				ghost:
					"border-transparent bg-transparent text-secondary hover:bg-wash-hover hover:text-primary",
			},
			size: {
				default: "h-control px-3",
				sm: "h-control px-2.5 text-[12px]",
				lg: "h-control-md px-4",
				block: "h-control w-full px-3",
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
