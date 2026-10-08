import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// The console's status glyphs: a custom set, never Lucide, and the only
// colored icons. Three families, no shape repeated across them, so the
// meaning survives grayscale, color-blindness, and print:
//
//   flow decisions are outlined rings and a triangle: observed (dotted
//   ring, center dot), allowed (check in ring), would-block (outlined
//   triangle with !), blocked (ring with slash);
//   workload health is solid or square-boxed: synced (solid disc),
//   pending (half-filled ring), degraded (solid triangle), offline
//   (× in a square);
//   severity is outlined pointed polygons: alert (diamond with !) and
//   error (octagon with ×), for warnings and errors that are neither a
//   flow decision nor workload health (design package v2, amendment 1).
//
// Each is drawn on a 16-unit grid at a 1.5 stroke and takes its tone
// from the status palette.
export type FlowStatus = "observed" | "allowed" | "would-block" | "blocked";
export type HealthStatus = "synced" | "pending" | "degraded" | "offline";
export type Severity = "alert" | "error";
export type GlyphStatus = FlowStatus | HealthStatus | Severity;

// tone is each glyph's color: the flow and health aliases of the status
// palette, and the severity tones the amendment names.
export const tone: Record<GlyphStatus, string> = {
	observed: "text-flow-observed",
	allowed: "text-flow-allowed",
	"would-block": "text-flow-would-block",
	blocked: "text-flow-blocked",
	synced: "text-health-synced",
	pending: "text-health-pending",
	degraded: "text-health-degraded",
	offline: "text-health-offline",
	alert: "text-status-warn-fg",
	error: "text-status-critical-fg",
};

// tint is the tinted badge's fill and hairline for each glyph's tone.
export const tint: Record<GlyphStatus, string> = {
	observed: "bg-status-neutral-bg border-status-neutral-border",
	allowed: "bg-status-ok-bg border-status-ok-border",
	"would-block": "bg-status-warn-bg border-status-warn-border",
	blocked: "bg-status-critical-bg border-status-critical-border",
	synced: "bg-status-ok-bg border-status-ok-border",
	pending: "bg-status-neutral-bg border-status-neutral-border",
	degraded: "bg-status-warn-bg border-status-warn-border",
	offline: "bg-status-critical-bg border-status-critical-border",
	alert: "bg-status-warn-bg border-status-warn-border",
	error: "bg-status-critical-bg border-status-critical-border",
};

const stroke = {
	fill: "none",
	stroke: "currentColor",
	strokeWidth: 1.5,
	strokeLinecap: "round",
	strokeLinejoin: "round",
} as const;

const shapes: Record<GlyphStatus, ReactNode> = {
	observed: (
		<>
			<circle cx="8" cy="8" r="6.25" {...stroke} strokeDasharray="0.01 2.45" />
			<circle cx="8" cy="8" r="1.75" fill="currentColor" />
		</>
	),
	allowed: (
		<>
			<circle cx="8" cy="8" r="6.25" {...stroke} />
			<path d="M5.25 8.25 7.25 10.25 10.75 6.25" {...stroke} />
		</>
	),
	"would-block": (
		<>
			<path d="M8 1.75 14.5 13.5H1.5Z" {...stroke} />
			<path d="M8 6v3.5" {...stroke} />
			<circle cx="8" cy="11.5" r="0.85" fill="currentColor" />
		</>
	),
	blocked: (
		<>
			<circle cx="8" cy="8" r="6.25" {...stroke} />
			<path d="M3.6 12.4 12.4 3.6" {...stroke} />
		</>
	),
	synced: <circle cx="8" cy="8" r="6.25" fill="currentColor" />,
	pending: (
		<>
			<circle cx="8" cy="8" r="6.25" {...stroke} />
			<path d="M8 1.75a6.25 6.25 0 0 1 0 12.5Z" fill="currentColor" />
		</>
	),
	degraded: (
		<path
			d="M8 1.5 14.75 13.75H1.25Z"
			fill="currentColor"
			stroke="currentColor"
			strokeWidth={1.5}
			strokeLinejoin="round"
		/>
	),
	offline: (
		<>
			<rect x="1.75" y="1.75" width="12.5" height="12.5" rx="2" {...stroke} />
			<path d="M5.5 5.5 10.5 10.5M10.5 5.5 5.5 10.5" {...stroke} />
		</>
	),
	alert: (
		<>
			<path d="M8 1.25 14.75 8 8 14.75 1.25 8Z" {...stroke} />
			<path d="M8 4.75v3.75" {...stroke} />
			<circle cx="8" cy="10.9" r="0.85" fill="currentColor" />
		</>
	),
	error: (
		<>
			<path
				d="M5.2 1.25h5.6l3.95 3.95v5.6l-3.95 3.95H5.2l-3.95-3.95V5.2Z"
				{...stroke}
			/>
			<path d="M5.9 5.9 10.1 10.1M10.1 5.9 5.9 10.1" {...stroke} />
		</>
	),
};

// The glyph sizes: glyph-sm in 12px text, glyph-md in table rows,
// glyph-lg in banners and legends.
const sizes = { sm: "size-glyph-sm", md: "size-glyph-md", lg: "size-glyph-lg" };

// StatusGlyph is one glyph in its tone. It is hidden from assistive
// technology unless `label` names it; the status word belongs beside it
// (StatusBadge), except in dense graph and matrix cells, whose accessible
// name carries the word.
export function StatusGlyph({
	status,
	size = "md",
	label,
	className,
}: {
	status: GlyphStatus;
	size?: keyof typeof sizes;
	label?: string;
	className?: string;
}) {
	return (
		<svg
			viewBox="0 0 16 16"
			data-slot="status-glyph"
			data-status={status}
			className={cn("shrink-0", sizes[size], tone[status], className)}
			aria-hidden={label ? undefined : true}
			role={label ? "img" : undefined}
			aria-label={label}
			focusable="false"
		>
			{shapes[status]}
		</svg>
	);
}

// StatusBadge is glyph and status word, the one way a flow decision or
// a workload's health is shown in text: plain (the word at the status
// color, for table cells) or tinted (the tone's fill and hairline, for
// banners, drawers, and dialogs). children carry the word.
export function StatusBadge({
	status,
	variant = "plain",
	className,
	children,
}: {
	status: GlyphStatus;
	variant?: "plain" | "tinted";
	className?: string;
	children: ReactNode;
}) {
	return (
		<span
			className={cn(
				"inline-flex items-center gap-1.5 whitespace-nowrap type-label",
				tone[status],
				variant === "tinted" && [
					"h-[22px] rounded-sm border px-2",
					tint[status],
				],
				className,
			)}
		>
			<StatusGlyph status={status} size="sm" />
			<span>{children}</span>
		</span>
	);
}

// SeverityNote is a warning or an error in running text: its severity
// glyph, then the words, both in the severity's tone. The glyph sits on
// the first line when the text wraps.
export function SeverityNote({
	level,
	className,
	children,
	role,
	...rest
}: {
	level: Severity;
	className?: string;
	children: ReactNode;
	role?: "alert" | "status" | "note";
	"data-testid"?: string;
}) {
	return (
		<span
			role={role}
			className={cn("inline-flex items-start gap-1.5", tone[level], className)}
			{...rest}
		>
			<StatusGlyph status={level} size="md" className="mt-[3px]" />
			<span className="min-w-0">{children}</span>
		</span>
	);
}
