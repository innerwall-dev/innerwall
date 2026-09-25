import { matchPath, NavLink, useLocation } from "react-router";
import { useMe } from "@/auth/SessionProvider";
import { syncStates } from "@/components/fleet/status";
import { cn } from "@/lib/utils";
import { Lockup } from "./Logo";
import type { OpenWorkload } from "./Shell";

// The four sections in product order. Glyphs are the mono glyphs the
// screens use; a count is shown only for a section that counts
// something, and only when the frame knows it exactly.
export const sections = [
	{ to: "/simulation", label: "Simulation review", glyph: "◆", counted: false },
	{ to: "/map", label: "Flow map", glyph: "⇄", counted: false },
	{ to: "/workloads", label: "Workloads", glyph: "▦", counted: true },
	{ to: "/policy", label: "Policy", glyph: "≡", counted: true },
] as const;

const navRow = ({ isActive }: { isActive: boolean }) =>
	cn(
		"flex w-full items-center gap-2.5 rounded px-2.5 py-[7px] text-[13px]",
		isActive
			? "bg-surface-nav-active text-foreground"
			: "text-foreground-secondary hover:bg-surface-nav-active/60",
	);

export function Sidebar({
	counts = {},
	fleetEmpty = null,
	open = null,
}: {
	counts?: Partial<Record<string, number>>;
	fleetEmpty?: boolean | null;
	open?: OpenWorkload | null;
}) {
	const me = useMe();
	// A workload's detail has its own row; the fleet row is the fleet's.
	const { pathname } = useLocation();
	const detail = matchPath("/workloads/:id/*", pathname);
	const onDetail = detail !== null && detail.params.id !== "tokens";
	return (
		<aside className="flex h-full w-[212px] shrink-0 flex-col border-r border-border bg-surface-sidebar">
			<div className="flex items-center gap-2.5 border-b border-border px-4 pt-4 pb-3.5">
				<Lockup />
				{me.site ? (
					<span
						className="ml-auto font-mono text-[10px] text-muted-foreground"
						data-testid="site-label"
					>
						{me.site}
					</span>
				) : null}
			</div>
			<nav className="flex flex-col gap-0.5 px-2 py-2.5" aria-label="Sections">
				{sections.map((s) => (
					<NavLink
						key={s.to}
						to={s.to}
						className={
							s.to === "/workloads"
								? ({ isActive }) => navRow({ isActive: isActive && !onDetail })
								: navRow
						}
					>
						<span
							className="w-4 text-center font-mono text-[12px] text-foreground-glyph"
							aria-hidden="true"
						>
							{s.glyph}
						</span>
						<span>{s.label}</span>
						{s.counted && counts[s.to] !== undefined ? (
							<span className="ml-auto font-mono text-[11px] text-muted-foreground">
								{counts[s.to]}
							</span>
						) : null}
					</NavLink>
				))}
				{open ? (
					<NavLink to={`/workloads/${open.id}`} className={navRow}>
						<span
							className="w-4 text-center font-mono text-[12px] text-foreground-glyph"
							aria-hidden="true"
						>
							▫
						</span>
						<span className="truncate">{open.hostname}</span>
						<span
							className={cn(
								"ml-auto font-mono text-[11px]",
								syncStates[open.state].cls,
							)}
							title={syncStates[open.state].label}
						>
							{syncStates[open.state].glyph}
						</span>
					</NavLink>
				) : null}
			</nav>
			{fleetEmpty ? <FleetSync /> : null}
		</aside>
	);
}

// The fleet sync block at the foot of the sidebar, in its fresh-install
// form: the track alone. The populated form needs the fleet's per-state
// totals, which no read supplies, so a fleet with workloads shows no
// block rather than numbers the console cannot know.
function FleetSync() {
	return (
		<div className="mt-auto flex flex-col gap-1.5 border-t border-border px-4 py-3">
			<div className="text-[11px] uppercase tracking-[0.06em] text-muted-foreground">
				Fleet sync
			</div>
			<div
				className="flex h-1.5 overflow-hidden rounded-[3px] bg-viz-sync-track"
				role="presentation"
			/>
			<div className="font-mono text-[11px] text-foreground-tertiary">
				no workloads
			</div>
			<div className="text-[11px] text-muted-foreground">
				Enroll a workload to begin
			</div>
		</div>
	);
}
