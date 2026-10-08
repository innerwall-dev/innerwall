import { matchPath, NavLink, useLocation } from "react-router";
import { syncStates } from "@/components/fleet/status";
import { Icon, type IconName } from "@/components/Icon";
import { StatusGlyph } from "@/components/StatusGlyph";
import {
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarHeader,
	SidebarMenu,
	SidebarMenuBadge,
	SidebarMenuButton,
	SidebarMenuItem,
	Sidebar as SidebarRail,
	SidebarTrigger,
} from "@/components/ui/sidebar";
import { AccountPopover } from "./AccountPopover";
import { LogoMark, Wordmark } from "./Logo";
import type { OpenWorkload } from "./Shell";

// The four sections in product order, each with its Lucide icon. A
// count is shown only for a section that counts something, and only
// when the frame knows it exactly.
export const sections: {
	to: string;
	label: string;
	icon: IconName;
	counted: boolean;
}[] = [
	{
		to: "/simulation",
		label: "Simulation review",
		icon: "flask-conical",
		counted: false,
	},
	{ to: "/map", label: "Flow map", icon: "network", counted: false },
	{ to: "/workloads", label: "Workloads", icon: "server", counted: true },
	{ to: "/policy", label: "Policy", icon: "scroll-text", counted: true },
];

// Sidebar is the console's only global navigation: the brand mark and
// name, the sections, the workload a detail screen last showed, and in
// the footer the collapse control and the operator's account. It
// collapses to an icon rail, where every entry keeps its accessible name
// and shows its name as a tooltip.
export function Sidebar({
	counts = {},
	fleetEmpty = null,
	open = null,
}: {
	counts?: Partial<Record<string, number>>;
	fleetEmpty?: boolean | null;
	open?: OpenWorkload | null;
}) {
	// A workload's detail has its own entry; the fleet entry is the fleet's.
	const { pathname } = useLocation();
	const detail = matchPath("/workloads/:id/*", pathname);
	const onDetail = detail !== null && detail.params.id !== "tokens";
	const current = (to: string) =>
		matchPath({ path: to, end: false }, pathname) !== null &&
		!(to === "/workloads" && onDetail);
	return (
		<SidebarRail collapsible="icon">
			<SidebarHeader>
				<div className="mb-1 flex h-sidebar-item items-center gap-2 px-2 group-data-[collapsible=icon]:px-[9px]">
					<LogoMark />
					<Wordmark className="truncate group-data-[collapsible=icon]:hidden" />
				</div>
			</SidebarHeader>
			<SidebarContent>
				<SidebarGroup>
					<nav aria-label="Sections">
						<SidebarMenu>
							{sections.map((s) => (
								<SidebarMenuItem key={s.to}>
									<SidebarMenuButton
										asChild
										isActive={current(s.to)}
										tooltip={s.label}
									>
										<NavLink to={s.to}>
											<Icon name={s.icon} />
											<span>{s.label}</span>
										</NavLink>
									</SidebarMenuButton>
									{s.counted && counts[s.to] !== undefined ? (
										<SidebarMenuBadge>{counts[s.to]}</SidebarMenuBadge>
									) : null}
								</SidebarMenuItem>
							))}
							{open ? (
								<SidebarMenuItem>
									<SidebarMenuButton
										asChild
										isActive={onDetail && detail?.params.id === open.id}
										tooltip={`${open.hostname} · ${syncStates[open.state].label}`}
									>
										<NavLink to={`/workloads/${open.id}`}>
											<StatusGlyph
												status={syncStates[open.state].status}
												size="lg"
												label={syncStates[open.state].label}
											/>
											<span>{open.hostname}</span>
										</NavLink>
									</SidebarMenuButton>
								</SidebarMenuItem>
							) : null}
						</SidebarMenu>
					</nav>
				</SidebarGroup>
			</SidebarContent>
			{fleetEmpty ? <FleetSync /> : null}
			<SidebarFooter className="border-t border-subtle">
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarTrigger />
					</SidebarMenuItem>
					<SidebarMenuItem>
						<AccountPopover />
					</SidebarMenuItem>
				</SidebarMenu>
			</SidebarFooter>
		</SidebarRail>
	);
}

// The fleet sync block above the sidebar's footer, in its fresh-install
// form: the track alone. The populated form needs the fleet's per-state
// totals, which no read supplies, so a fleet with workloads shows no
// block rather than numbers the console cannot know. The icon rail has
// no room for it.
function FleetSync() {
	return (
		<div className="flex flex-col gap-1.5 border-t border-subtle px-4 py-3 group-data-[collapsible=icon]:hidden">
			<div className="type-label text-tertiary">Fleet sync</div>
			<div
				className="flex h-1.5 overflow-hidden rounded-full bg-active"
				role="presentation"
			/>
			<div className="type-mono-xs text-secondary">no workloads</div>
			<div className="type-caption text-tertiary">
				Enroll a workload to begin
			</div>
		</div>
	);
}
