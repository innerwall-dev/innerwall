import * as DialogPrimitive from "@radix-ui/react-dialog";
import type * as React from "react";
import { cn } from "@/lib/utils";

// The design's dialog: the scrim behind a raised panel on the default
// hairline at the dialog radius, flat (no shadow), and header, body,
// and footer bands. Focus is held in the dialog while it is open and Escape
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
			<DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-scrim" />
			<div className="pointer-events-none fixed inset-0 z-50 flex items-center justify-center p-6">
				<DialogPrimitive.Content
					data-slot="dialog-content"
					aria-describedby={undefined}
					className={cn(
						"pointer-events-auto flex max-h-[88vh] flex-col overflow-auto rounded-xl border border-default bg-raised text-primary outline-none",
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
		<div className="flex flex-col gap-1 px-6 pt-5 pb-3">
			<DialogPrimitive.Title className="type-title-section">
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
			className={cn("flex flex-col gap-4 px-6 pb-5 type-body", className)}
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
				"flex items-center justify-end gap-2 border-t border-default px-6 py-4",
				className,
			)}
			{...props}
		/>
	);
}

export const DialogClose = DialogPrimitive.Close;
