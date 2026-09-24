import { NavLink } from "react-router";
import { useMe } from "@/auth/SessionProvider";
import { cn } from "@/lib/utils";
import { Lockup } from "./Logo";

// The four sections in product order. Glyphs are the mono glyphs the
// screens use; counts are wired when the read screens land and are
// shown only for the sections that count something.
export const sections = [
	{ to: "/simulation", label: "Simulation review", glyph: "◆", counted: false },
	{ to: "/map", label: "Flow map", glyph: "⇄", counted: false },
	{ to: "/workloads", label: "Workloads", glyph: "▦", counted: true },
	{ to: "/policy", label: "Policy", glyph: "≡", counted: true },
] as const;

export function Sidebar({
	counts = {},
}: {
	counts?: Partial<Record<string, number>>;
}) {
	const me = useMe();
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
						className={({ isActive }) =>
							cn(
								"flex w-full items-center gap-2.5 rounded px-2.5 py-[7px] text-[13px]",
								isActive
									? "bg-surface-nav-active text-foreground"
									: "text-foreground-secondary hover:bg-surface-nav-active/60",
							)
						}
					>
						<span
							className="w-4 text-center font-mono text-[12px] text-foreground-glyph"
							aria-hidden="true"
						>
							{s.glyph}
						</span>
						<span>{s.label}</span>
						{s.counted ? (
							<span className="ml-auto font-mono text-[11px] text-muted-foreground">
								{counts[s.to] ?? 0}
							</span>
						) : null}
					</NavLink>
				))}
			</nav>
			<FleetSync />
		</aside>
	);
}

// The fleet sync block at the foot of the sidebar. Until the workload
// read screens land it reports the fresh-install state; the bar is the
// track alone.
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
