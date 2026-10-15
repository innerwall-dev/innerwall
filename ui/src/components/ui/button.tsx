import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";
import { cn } from "@/lib/utils";

// The design's button forms (Controls spec): the primary plate, one per
// view, whose icon takes the plate ink; the secondary, a raised control
// whose hover strengthens its border; ghost, for popover items and
// text-like controls; and destructive, the critical plate for an
// irreversible confirm. A control is 26px with 12px text at 500; block
// is the 34px standalone-form size (sign-in). Disabled and aria-disabled
// look alike: a confirm held disabled for a reason stays focusable.
const buttonVariants = cva(
	"inline-flex shrink-0 cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-control border type-control transition-colors disabled:pointer-events-none aria-disabled:cursor-default",
	{
		variants: {
			variant: {
				primary:
					"border-transparent bg-plate font-semibold text-plate-ink hover:bg-plate-hover active:bg-plate-pressed disabled:border-hairline disabled:bg-overlay disabled:text-disabled aria-disabled:border-hairline aria-disabled:bg-overlay aria-disabled:text-disabled [&_[data-slot=icon]]:text-current",
				secondary:
					"border-overlay bg-control-raised text-primary hover:border-control-hover active:border-control-hover active:bg-wash-selected disabled:border-hairline disabled:bg-transparent disabled:text-disabled aria-disabled:border-hairline aria-disabled:bg-transparent aria-disabled:text-disabled",
				ghost:
					"border-transparent bg-transparent text-secondary hover:bg-wash-hover hover:text-primary active:bg-wash-selected disabled:text-disabled aria-disabled:text-disabled",
				destructive:
					"border-transparent bg-destructive text-destructive-ink hover:bg-destructive-hover active:bg-destructive-hover disabled:border-hairline disabled:bg-overlay disabled:text-disabled aria-disabled:border-hairline aria-disabled:bg-overlay aria-disabled:text-disabled [&_[data-slot=icon]]:text-current",
			},
			size: {
				default: "h-control px-2.5",
				sm: "h-control px-2.5",
				block: "h-control-sm w-full px-3",
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
