import type { ReactNode } from "react";
import type { Mode, SyncState, Verdict } from "@/api/schema";
import { Icon, type IconName } from "@/components/Icon";
import {
	type FlowStatus,
	type HealthStatus,
	StatusBadge,
	StatusGlyph,
	tint,
	tone,
} from "@/components/StatusGlyph";
import { cn } from "@/lib/utils";

// The design's status vocabulary. Color is never the only encoding:
// every sync state and verdict carries its status glyph, and every mode
// its Lucide icon. Modes are not status, so they stay gray.

export const modes: Record<
	Mode,
	{ icon: IconName; glyph: ReactNode; label: string; cls: string }
> = {
	visibility: {
		icon: "eye",
		glyph: <Icon name="eye" className="size-3.5" />,
		label: "Visibility",
		cls: "text-secondary border-default",
	},
	simulation: {
		icon: "flask-conical",
		glyph: <Icon name="flask-conical" className="size-3.5" />,
		label: "Simulation",
		cls: "text-primary border-strong",
	},
	enforced: {
		icon: "shield",
		glyph: <Icon name="shield" className="size-3.5" />,
		label: "Enforced",
		cls: "text-primary border-strong",
	},
};

const health = (status: HealthStatus, label: string) => ({
	status,
	glyph: <StatusGlyph status={status} size="sm" />,
	label,
	cls: tone[status],
});

export const syncStates: Record<
	SyncState,
	{ status: HealthStatus; glyph: ReactNode; label: string; cls: string }
> = {
	synced: health("synced", "Synced"),
	pending: health("pending", "Pending"),
	degraded: health("degraded", "Degraded"),
	offline: health("offline", "Offline"),
};

const flow = (status: FlowStatus, label: string) => ({
	status,
	glyph: <StatusGlyph status={status} size="sm" />,
	label,
	cls: cn(tone[status], tint[status]),
	text: tone[status],
});

export const verdicts: Record<
	Verdict,
	{
		status: FlowStatus;
		glyph: ReactNode;
		label: string;
		cls: string;
		text: string;
	}
> = {
	observed: flow("observed", "observed"),
	allowed: flow("allowed", "allowed"),
	would_block: flow("would-block", "would block"),
	blocked: flow("blocked", "blocked"),
};

// ModePill is a workload's mode: its icon and word in a hairline pill,
// gray in every mode.
export function ModePill({ mode }: { mode: Mode }) {
	const m = modes[mode];
	return (
		<span
			className={cn(
				"inline-flex h-[22px] items-center gap-1.5 whitespace-nowrap rounded-sm border px-2 type-label",
				m.cls,
			)}
		>
			{m.glyph}
			<span>{m.label}</span>
		</span>
	);
}

// SyncLabel is a workload's health in a table cell.
export function SyncLabel({ state }: { state: SyncState }) {
	const s = syncStates[state];
	return <StatusBadge status={s.status}>{s.label}</StatusBadge>;
}

// VerdictPill is a flow decision: plain in a table cell, tinted in a
// drawer or a dialog.
export function VerdictPill({
	verdict,
	variant = "plain",
}: {
	verdict: Verdict;
	variant?: "plain" | "tinted";
}) {
	const v = verdicts[verdict];
	return (
		<StatusBadge status={v.status} variant={variant}>
			{v.label}
		</StatusBadge>
	);
}

// LabelChip is one `key=value` label, always mono and never truncated
// mid-key: the key in text-tertiary, the value in text-primary, on
// bg-subtle behind a hairline. The input form sits inside an editable
// field, on its stronger border.
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
			data-slot="label-chip"
			className={cn(
				"inline-flex h-5 items-center whitespace-nowrap rounded-sm border bg-subtle px-1.5 type-mono-sm text-primary",
				size === "input" ? "border-strong" : "border-default",
			)}
		>
			<span className="text-tertiary">{k}=</span>
			{v}
		</span>
	);
}

// FilterChip is a toggle in a chip row: the fleet's sync states, the
// flow tab's verdicts. Pressed, it takes the pressed-segment fill.
export function FilterChip({
	on,
	glyph,
	label,
	count,
	onClick,
}: {
	on: boolean;
	glyph?: ReactNode;
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
				"inline-flex h-control-sm cursor-pointer items-center gap-1.5 rounded-md border px-2.5 type-caption",
				on
					? "border-strong bg-active font-medium text-primary"
					: "border-default text-secondary hover:bg-hover hover:text-primary",
			)}
		>
			{glyph}
			<span>{label}</span>
			{count !== undefined ? (
				<span className="type-mono-xs text-tertiary">{count}</span>
			) : null}
		</button>
	);
}

// Eyebrow is a section's small heading: label type, sentence case.
export function Eyebrow({
	children,
	className,
}: {
	children: ReactNode;
	className?: string;
}) {
	return (
		<div className={cn("type-label text-tertiary", className)}>{children}</div>
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
						"inline-flex h-control-sm cursor-pointer items-center gap-1.5 rounded-md border px-2.5 type-caption has-[:focus-visible]:focus-ring",
						value === o.value
							? "border-strong bg-active font-medium text-primary"
							: "border-default text-secondary hover:bg-hover hover:text-primary",
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
