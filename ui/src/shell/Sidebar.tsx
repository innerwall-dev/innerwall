import { NavLink } from "react-router";
import { useMe } from "@/auth/SessionProvider";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import { LogoMark } from "./Logo";

// The four sections in product order. Glyphs are the mono decision and
// mode glyphs the screens use; counts are wired when the read screens
// land and are shown only when known.
export const sections = [
	{ to: "/simulation", label: "Simulation review", glyph: "◆", counted: false },
	{ to: "/map", label: "Flow map", glyph: "⇄", counted: false },
	{ to: "/workloads", label: "Workloads", glyph: "■", counted: true },
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
			<div className="flex h-11 items-center gap-2.5 border-b border-border pr-4 pl-4">
				<LogoMark />
				<span className="text-[15px] font-semibold leading-none tracking-[-0.01em]">
					Innerwall
				</span>
				{me.site ? (
					<span
						className="ml-auto font-mono text-[11px] text-muted-foreground"
						data-testid="site-label"
					>
						{me.site}
					</span>
				) : null}
			</div>
			<nav className="flex flex-col gap-0.5 p-2" aria-label="Sections">
				{sections.map((s) => (
					<NavLink
						key={s.to}
						to={s.to}
						className={({ isActive }) =>
							cn(
								"flex h-[34px] items-center gap-3 rounded px-3 text-[13px] text-foreground",
								isActive
									? "bg-surface-nav-active"
									: "hover:bg-surface-nav-active/60",
							)
						}
					>
						<span
							className="w-3 text-center font-mono text-[11px] text-foreground-glyph"
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
		<div className="mt-auto border-t border-border px-4 pt-5 pb-5">
			<Label>Fleet sync</Label>
			<div
				className="mt-[7px] h-1 w-full rounded-full bg-viz-sync-track"
				role="presentation"
			/>
			<div className="mt-2 font-mono text-[12px] text-foreground-secondary">
				no workloads
			</div>
			<div className="mt-1 text-[12px] text-muted-foreground">
				Enroll a workload to begin
			</div>
		</div>
	);
}
