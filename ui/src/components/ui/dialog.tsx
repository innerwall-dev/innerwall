import * as DialogPrimitive from "@radix-ui/react-dialog";
import type * as React from "react";
import { cn } from "@/lib/utils";

// The design's dialog: a scrim in the overlay token, a muted panel on
// the strong input border at the dialog radius, and header, body, and
// footer bands. Focus is held in the dialog while it is open and Escape
// closes it.
export const Dialog = DialogPrimitive.Root;

export function DialogContent({
	className,
	children,
	width = 560,
	...props
}: React.ComponentProps<typeof DialogPrimitive.Content> & { width?: number }) {
	return (
		<DialogPrimitive.Portal>
			<DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-[var(--overlay)]" />
			<div className="pointer-events-none fixed inset-0 z-50 flex items-center justify-center p-6">
				<DialogPrimitive.Content
					data-slot="dialog-content"
					aria-describedby={undefined}
					className={cn(
						"pointer-events-auto flex max-h-[88vh] flex-col overflow-auto rounded-dialog border border-input-strong bg-muted text-foreground shadow-[0_24px_60px_var(--overlay)] outline-none",
						className,
					)}
					style={{ width }}
					{...props}
				>
					{children}
				</DialogPrimitive.Content>
			</div>
		</DialogPrimitive.Portal>
	);
}

export function DialogHeader({
	title,
	children,
}: {
	title: string;
	children?: React.ReactNode;
}) {
	return (
		<div className="flex flex-col gap-1 px-5 pt-[18px] pb-3">
			<DialogPrimitive.Title className="text-[16px] font-semibold">
				{title}
			</DialogPrimitive.Title>
			{children}
		</div>
	);
}

export function DialogBody({
	className,
	...props
}: React.ComponentProps<"div">) {
	return (
		<div
			className={cn("flex flex-col gap-3.5 px-5 pb-4", className)}
			{...props}
		/>
	);
}

export function DialogFooter({
	className,
	...props
}: React.ComponentProps<"div">) {
	return (
		<div
			className={cn(
				"flex items-center justify-end gap-2 border-t border-border px-5 pt-3 pb-4",
				className,
			)}
			{...props}
		/>
	);
}

export const DialogClose = DialogPrimitive.Close;
