import { type KeyboardEvent, useState } from "react";
import type { Finding } from "@/api/schema";
import { LabelChip } from "@/components/fleet/status";
import { Icon } from "@/components/Icon";
import { SeverityNote } from "@/components/StatusGlyph";
import { cn } from "@/lib/utils";
import type { Chip } from "./model";

// ChipView is one authored element in a cell: its kind tag and its text,
// solid when it states a value inline, dashed when it names something
// defined elsewhere. A selector's labels draw as label chips. A refused
// write's finding on it draws it in the destructive color, struck through
// when it names a definition that does not exist.
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
	const input = onRemove !== undefined;
	const remove = onRemove ? (
		<button
			type="button"
			aria-label={`Remove ${chip.tag.toLowerCase()} ${chip.text}`.trim()}
			onClick={onRemove}
			className="group/rm -mr-0.5 inline-flex cursor-pointer items-center rounded-sm"
		>
			<Icon name="x" className="size-3.5 group-hover/rm:text-icon-active" />
		</button>
	) : null;
	if (chip.tag === "LABELS") {
		// The selector's requirements as label chips, one group: its
		// hairline only shows when a finding lands on it.
		return (
			<span
				className={cn(
					"inline-flex max-w-full flex-wrap items-center gap-1 rounded-sm border",
					chip.reference || failed ? "border-dashed" : "border-solid",
					failed
						? "border-status-critical-fg px-1 py-0.5"
						: "border-transparent",
				)}
				data-testid="chip"
				data-failed={failed || undefined}
			>
				<span className="type-mono-xs text-tertiary">{chip.tag}</span>
				{(chip.requirements ?? []).map((r, i) => (
					<span key={r.key} className="contents">
						{i > 0 ? " " : null}
						<LabelChip
							k={r.key}
							v={r.values}
							size={input ? "input" : "table"}
						/>
					</span>
				))}
				{remove}
			</span>
		);
	}
	return (
		<span
			className={cn(
				"inline-flex min-h-5 max-w-full items-center gap-1.5 rounded-sm border bg-subtle px-1.5 type-mono-sm",
				chip.reference || failed ? "border-dashed" : "border-solid",
				failed
					? "border-status-critical-fg"
					: input
						? "border-strong"
						: "border-default",
			)}
			data-testid="chip"
			data-failed={failed || undefined}
		>
			<span className="type-mono-xs text-tertiary">{chip.tag}</span>
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
			{remove}
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
					data-testid="finding"
					data-path={f.path}
				>
					<SeverityNote level="error" size="sm" className="type-caption">
						{f.message}
					</SeverityNote>
				</li>
			))}
		</ul>
	);
}

// AddInput is the field at the end of a chip list: what is typed becomes
// one or more elements on Enter, read as the placeholder describes, and
// with commitOnBlur also when the field loses focus, so text typed and
// left pending is committed before a button beside it submits. When
// onAdd refuses the text with a reason, the text stays, marked, with the
// reason under it, and nothing is added.
export function AddInput({
	label,
	placeholder,
	onAdd,
	commitOnBlur = false,
	className,
}: {
	label: string;
	placeholder: string;
	onAdd: (text: string) => string | undefined;
	commitOnBlur?: boolean;
	className?: string;
}) {
	const [text, setText] = useState("");
	const [refusal, setRefusal] = useState<string | null>(null);
	const commit = () => {
		if (text.trim() === "") return;
		const refused = onAdd(text);
		if (refused) {
			setRefusal(refused);
			return;
		}
		setText("");
		setRefusal(null);
	};
	const onKeyDown = (ev: KeyboardEvent<HTMLInputElement>) => {
		if (ev.key !== "Enter") return;
		ev.preventDefault();
		commit();
	};
	return (
		<span className={cn("inline-flex flex-col gap-1", className)}>
			<input
				aria-label={label}
				aria-invalid={refusal !== null || undefined}
				placeholder={placeholder}
				value={text}
				onChange={(ev) => {
					setText(ev.target.value);
					setRefusal(null);
				}}
				onKeyDown={onKeyDown}
				onBlur={commitOnBlur ? commit : undefined}
				className="h-control-sm min-w-[150px] rounded-md border border-strong bg-app px-2.5 type-mono-sm text-primary placeholder:text-tertiary aria-invalid:border-status-critical-fg"
			/>
			{refusal ? (
				<SeverityNote level="error" size="sm" className="type-caption">
					{refusal}
				</SeverityNote>
			) : null}
		</span>
	);
}
