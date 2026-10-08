import { type KeyboardEvent, useState } from "react";
import type { Finding } from "@/api/schema";
import { cn } from "@/lib/utils";
import type { Chip } from "./model";

// ChipView is one authored element in a cell: its kind tag and its text,
// solid when it states a value inline, dashed when it names something
// defined elsewhere. A refused write's finding on it draws it in the
// destructive color, struck through when it names a definition that does
// not exist.
export function ChipView({
	chip,
	findings,
	onRemove,
}: {
	chip: Chip;
	findings?: readonly Finding[];
	onRemove?: () => void;
}) {
	const failed = (findings?.length ?? 0) > 0;
	const unknown = findings?.some(
		(f) => f.rule === "service-unknown" || f.rule === "address-group-unknown",
	);
	return (
		<span
			className={cn(
				"inline-flex max-w-full items-center gap-1.5 rounded-[3px] border bg-raised px-1.5 py-[3px] font-mono text-[12px]",
				chip.reference || failed ? "border-dashed" : "border-solid",
				failed ? "border-status-critical-fg" : "border-strong",
			)}
			data-testid="chip"
			data-failed={failed || undefined}
		>
			<span className="text-[10px] tracking-[0.04em] text-tertiary">
				{chip.tag}
			</span>
			{chip.text ? (
				<span
					className={cn(
						"break-all text-primary",
						unknown && "text-tertiary line-through",
					)}
				>
					{chip.text}
				</span>
			) : null}
			{onRemove ? (
				<button
					type="button"
					aria-label={`Remove ${chip.tag.toLowerCase()} ${chip.text}`.trim()}
					onClick={onRemove}
					className="cursor-pointer text-tertiary hover:text-primary"
				>
					×
				</button>
			) : null}
		</span>
	);
}

// FindingLines are the findings on one element or column, in the
// surface's own words, under it.
export function FindingLines({ findings }: { findings?: readonly Finding[] }) {
	if (!findings || findings.length === 0) return null;
	return (
		<ul className="flex flex-col gap-0.5">
			{findings.map((f) => (
				<li
					key={`${f.path}|${f.rule}`}
					className="flex gap-1.5 text-[12px] text-status-critical-fg"
					data-testid="finding"
					data-path={f.path}
				>
					<span aria-hidden="true">✕</span>
					<span>{f.message}</span>
				</li>
			))}
		</ul>
	);
}

// AddInput is the dashed field at the end of a chip list: what is typed
// becomes one element on Enter, read as the placeholder describes.
export function AddInput({
	label,
	placeholder,
	onAdd,
	className,
}: {
	label: string;
	placeholder: string;
	onAdd: (text: string) => void;
	className?: string;
}) {
	const [text, setText] = useState("");
	const onKeyDown = (ev: KeyboardEvent<HTMLInputElement>) => {
		if (ev.key !== "Enter") return;
		ev.preventDefault();
		if (text.trim() === "") return;
		onAdd(text);
		setText("");
	};
	return (
		<input
			aria-label={label}
			placeholder={placeholder}
			value={text}
			onChange={(ev) => setText(ev.target.value)}
			onKeyDown={onKeyDown}
			className={cn(
				"min-w-[150px] rounded-[3px] border border-dashed border-strong bg-transparent px-2 py-[3px] font-mono text-[12px] text-primary placeholder:text-tertiary focus:outline-none focus-visible:border-selection-fg",
				className,
			)}
		/>
	);
}
