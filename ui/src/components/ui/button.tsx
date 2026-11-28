import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";
import { cn } from "@/lib/utils";

// The design's button forms (Controls spec, v4): the primary, green fill
// on its own border with white ink at 500, one per view; the secondary
// (neutral), the control fill on the border, whose hover strengthens the
// border; ghost, for popover items and text-like controls; and
// destructive, the red fill for an irreversible confirm. A control is
// 28px with 12px text; block is the 34px standalone-form size (sign-in).
// Disabled and aria-disabled look alike, the selected fill with text 4:
// a confirm held disabled for a reason stays focusable.
const buttonVariants = cva(
	"inline-flex shrink-0 cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-control border type-control transition-colors disabled:pointer-events-none aria-disabled:cursor-default",
	{
		variants: {
			variant: {
				primary:
					"border-plate bg-plate font-medium text-plate-ink hover:bg-plate-hover active:bg-plate-pressed disabled:border-strong disabled:bg-overlay disabled:font-normal disabled:text-disabled aria-disabled:border-strong aria-disabled:bg-overlay aria-disabled:font-normal aria-disabled:text-disabled [&_[data-slot=icon]]:text-current",
				secondary:
					"border-strong bg-control-raised text-primary hover:border-control-hover active:border-control-hover active:bg-wash-selected disabled:border-strong disabled:bg-overlay disabled:text-disabled aria-disabled:border-strong aria-disabled:bg-overlay aria-disabled:text-disabled",
				ghost:
					"border-transparent bg-transparent text-secondary hover:bg-wash-hover hover:text-primary active:bg-wash-selected disabled:text-disabled aria-disabled:text-disabled",
				destructive:
					"border-transparent bg-destructive font-medium text-destructive-ink hover:bg-destructive-hover active:bg-destructive-hover disabled:border-strong disabled:bg-overlay disabled:font-normal disabled:text-disabled aria-disabled:border-strong aria-disabled:bg-overlay aria-disabled:font-normal aria-disabled:text-disabled [&_[data-slot=icon]]:text-current",
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
