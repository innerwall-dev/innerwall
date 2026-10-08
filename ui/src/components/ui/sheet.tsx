import * as SheetPrimitive from "@radix-ui/react-dialog";
import type * as React from "react";
import { Icon } from "@/components/Icon";
import { cn } from "@/lib/utils";

// From shadcn/ui (shadcn@3.5.0, new-york-v4, MIT), trimmed to what the
// sidebar's narrow-viewport form uses and restyled to the design
// tokens: the scrim behind a flat panel on the default hairline.
function Sheet({ ...props }: React.ComponentProps<typeof SheetPrimitive.Root>) {
	return <SheetPrimitive.Root data-slot="sheet" {...props} />;
}

function SheetContent({
	className,
	children,
	side = "right",
	...props
}: React.ComponentProps<typeof SheetPrimitive.Content> & {
	side?: "left" | "right";
}) {
	return (
		<SheetPrimitive.Portal>
			<SheetPrimitive.Overlay
				data-slot="sheet-overlay"
				className="fixed inset-0 z-50 bg-scrim"
			/>
			<SheetPrimitive.Content
				data-slot="sheet-content"
				className={cn(
					"fixed inset-y-0 z-50 flex h-full flex-col gap-4 border-default bg-app",
					side === "right" ? "right-0 border-l" : "left-0 border-r",
					className,
				)}
				{...props}
			>
				{children}
				<SheetPrimitive.Close className="absolute top-3 right-3 rounded-md p-1 hover:bg-hover">
					<Icon name="x" label="Close" />
				</SheetPrimitive.Close>
			</SheetPrimitive.Content>
		</SheetPrimitive.Portal>
	);
}

function SheetHeader({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="sheet-header"
			className={cn("flex flex-col gap-1.5 p-4", className)}
			{...props}
		/>
	);
}

function SheetTitle({
	className,
	...props
}: React.ComponentProps<typeof SheetPrimitive.Title>) {
	return (
		<SheetPrimitive.Title
			data-slot="sheet-title"
			className={cn("type-title-section", className)}
			{...props}
		/>
	);
}

function SheetDescription({
	className,
	...props
}: React.ComponentProps<typeof SheetPrimitive.Description>) {
	return (
		<SheetPrimitive.Description
			data-slot="sheet-description"
			className={cn("type-caption text-tertiary", className)}
			{...props}
		/>
	);
}

export { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle };
