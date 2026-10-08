import type { ReactNode } from "react";
import type { Mode, SyncState, Verdict } from "@/api/schema";
import { cn } from "@/lib/utils";

// The design's status vocabulary. Color is never the only encoding:
// every mode, sync state, and verdict carries its glyph in text.

export const modes: Record<
	Mode,
	{ glyph: string; label: string; cls: string }
> = {
	visibility: {
		glyph: "◌",
		label: "Visibility",
		cls: "text-secondary border-default",
	},
	simulation: {
		glyph: "◐",
		label: "Simulation",
		cls: "text-primary border-strong",
	},
	enforced: {
		glyph: "●",
		label: "Enforced",
		cls: "text-primary border-strong",
	},
};

export const syncStates: Record<
	SyncState,
	{ glyph: string; label: string; cls: string }
> = {
	synced: { glyph: "●", label: "Synced", cls: "text-health-synced" },
	pending: { glyph: "◔", label: "Pending", cls: "text-health-pending" },
	degraded: { glyph: "▲", label: "Degraded", cls: "text-status-warn-fg" },
	offline: { glyph: "○", label: "Offline", cls: "text-health-offline" },
};

export const verdicts: Record<
	Verdict,
	{ glyph: string; label: string; cls: string; text: string }
> = {
	observed: {
		glyph: "○",
		label: "observed",
		cls: "text-flow-observed bg-status-neutral-bg border-status-neutral-border",
		text: "text-flow-observed",
	},
	allowed: {
		glyph: "✓",
		label: "allowed",
		cls: "text-flow-allowed bg-status-ok-bg border-status-ok-border",
		text: "text-flow-allowed",
	},
	would_block: {
		glyph: "◆",
		label: "would block",
		cls: "text-flow-would-block bg-status-warn-bg border-status-warn-border",
		text: "text-flow-would-block",
	},
	blocked: {
		glyph: "✕",
		label: "blocked",
		cls: "text-flow-blocked bg-status-critical-bg border-status-critical-border",
		text: "text-flow-blocked",
	},
};

export function ModePill({ mode }: { mode: Mode }) {
	const m = modes[mode];
	return (
		<span
			className={cn(
				"inline-flex items-center gap-[5px] whitespace-nowrap rounded-sm border px-2 py-0.5 text-[11.5px]",
				m.cls,
			)}
		>
			<span aria-hidden="true">{m.glyph}</span>
			<span>{m.label}</span>
		</span>
	);
}

export function SyncLabel({ state }: { state: SyncState }) {
	const s = syncStates[state];
	return (
		<span
			className={cn(
				"inline-flex items-center gap-[5px] whitespace-nowrap text-[12px]",
				s.cls,
			)}
		>
			<span aria-hidden="true">{s.glyph}</span>
			<span>{s.label}</span>
		</span>
	);
}

export function VerdictPill({ verdict }: { verdict: Verdict }) {
	const v = verdicts[verdict];
	return (
		<span
			className={cn(
				"inline-flex items-center gap-[5px] whitespace-nowrap rounded-sm border px-2 py-0.5 text-[11.5px]",
				v.cls,
			)}
		>
			<span aria-hidden="true">{v.glyph}</span>
			<span>{v.label}</span>
		</span>
	);
}

// LabelChip is one `key=value` label. The table form is the compact one;
// the detail rail's is larger and dims the separator.
export function LabelChip({
	k,
	v,
	size = "table",
}: {
	k: string;
	v: string;
	size?: "table" | "rail" | "input";
}) {
	return (
		<span
			className={cn(
				"whitespace-nowrap border bg-raised font-mono",
				size === "table" &&
					"rounded-[3px] border-strong px-1.5 py-px text-[11px] text-secondary",
				size === "rail" &&
					"rounded-sm border-strong px-2 py-[3px] text-[11.5px]",
				size === "input" &&
					"rounded-sm border-strong px-[7px] py-0.5 text-[12px]",
			)}
		>
			{size === "rail" ? (
				<>
					{k}
					<span className="text-tertiary">=</span>
					{v}
				</>
			) : (
				`${k}=${v}`
			)}
		</span>
	);
}

// FilterChip is a toggle in a chip row: the fleet's sync states, the
// flow tab's verdicts.
export function FilterChip({
	on,
	glyph,
	glyphClass,
	label,
	count,
	onClick,
}: {
	on: boolean;
	glyph?: string;
	glyphClass?: string;
	label: string;
	count?: number;
	onClick: () => void;
}) {
	return (
		<button
			type="button"
			aria-pressed={on}
			onClick={onClick}
			className={cn(
				"inline-flex cursor-pointer items-center gap-1.5 rounded-sm border px-[9px] py-[3px] text-[12px]",
				on
					? "border-strong bg-active text-primary"
					: "border-strong text-secondary hover:text-primary",
			)}
		>
			{glyph ? (
				<span aria-hidden="true" className={glyphClass}>
					{glyph}
				</span>
			) : null}
			<span>{label}</span>
			{count !== undefined ? (
				<span className="font-mono text-tertiary">{count}</span>
			) : null}
		</button>
	);
}

// Eyebrow is a section's small uppercase heading.
export function Eyebrow({
	children,
	className,
}: {
	children: ReactNode;
	className?: string;
}) {
	return (
		<div
			className={cn(
				"text-[11px] uppercase tracking-[0.05em] text-tertiary",
				className,
			)}
		>
			{children}
		</div>
	);
}

// ChoiceChips is a single choice drawn as a row of chips: native radio
// inputs, so the group is one tab stop and arrows move the choice.
export function ChoiceChips<T extends string>({
	legend,
	name,
	options,
	value,
	onChange,
	className,
}: {
	legend: string;
	name: string;
	options: { value: T; label: ReactNode }[];
	value: T | null;
	onChange: (v: T) => void;
	className?: string;
}) {
	return (
		<fieldset
			className={cn("m-0 flex min-w-0 gap-1.5 border-0 p-0", className)}
		>
			<legend className="sr-only">{legend}</legend>
			{options.map((o) => (
				<label
					key={o.value}
					className={cn(
						"inline-flex cursor-pointer items-center gap-[5px] rounded-sm border px-2.5 py-[5px] has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-selection-fg",
						value === o.value
							? "border-strong bg-active text-primary"
							: "border-strong text-secondary hover:text-primary",
					)}
				>
					<input
						type="radio"
						name={name}
						value={o.value}
						checked={value === o.value}
						onChange={() => onChange(o.value)}
						className="sr-only"
					/>
					{o.label}
				</label>
			))}
		</fieldset>
	);
}
