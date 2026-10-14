import { matchPath, NavLink, useLocation } from "react-router";
import type { WorkloadCounts } from "@/api/schema";
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
import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { AccountPopover } from "./AccountPopover";
import { LogoMark, Wordmark } from "./Logo";
import type { OpenWorkload } from "./Shell";

// Badge is a section's count, and what it counts when the number alone
// does not say.
export interface Badge {
	n: number;
	hint?: string;
}

// attentionBadge is the Workloads count: the workloads needing the
// operator, those pending, degraded, or offline, and those whose
// enforcement was removed on the host, from the control plane's counts.
// The control plane counts a removed workload in removed alone, so the
// sum counts no workload twice (issue #96). A fleet with none needing
// attention shows no count; a fleet with no workloads at all shows its
// zero, the fresh install's cue to enroll the first.
export function attentionBadge(c: WorkloadCounts): Badge | null {
	if (c.total === 0) {
		return { n: 0, hint: "No workloads enrolled" };
	}
	const s = c.by_sync_state;
	const n = s.pending + s.degraded + s.offline + c.removed;
	if (n === 0) return null;
	const parts = [
		[s.pending, "pending"],
		[s.degraded, "degraded"],
		[s.offline, "offline"],
		[c.removed, "enforcement removed"],
	]
		.filter(([k]) => (k as number) > 0)
		.map(([k, label]) => `${k} ${label}`);
	return {
		n,
		hint: `${n} ${n === 1 ? "workload needs" : "workloads need"} attention: ${parts.join(", ")}. Counts workloads whose sync state is pending, degraded, or offline, and those whose enforcement was removed on the host.`,
	};
}

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
	counts?: Partial<Record<string, Badge>>;
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
							{sections.map((s) => {
								const badge = s.counted ? counts[s.to] : undefined;
								return (
									<SidebarMenuItem key={s.to}>
										<SidebarMenuButton
											asChild
											isActive={current(s.to)}
											tooltip={
												badge?.hint && badge.n > 0
													? `${s.label} · ${badge.n} needing attention`
													: s.label
											}
										>
											<NavLink to={s.to}>
												<Icon name={s.icon} />
												<span>{s.label}</span>
											</NavLink>
										</SidebarMenuButton>
										{badge === undefined ? null : badge.hint ? (
											<Tooltip>
												<TooltipTrigger asChild>
													<SidebarMenuBadge
														data-testid={`count-${s.label}`}
														className="pointer-events-auto cursor-default"
														tabIndex={0}
														aria-label={badge.hint}
													>
														{badge.n}
													</SidebarMenuBadge>
												</TooltipTrigger>
												<TooltipContent side="right" className="max-w-[260px]">
													{badge.hint}
												</TooltipContent>
											</Tooltip>
										) : (
											<SidebarMenuBadge data-testid={`count-${s.label}`}>
												{badge.n}
											</SidebarMenuBadge>
										)}
									</SidebarMenuItem>
								);
							})}
							{open ? (
								<SidebarMenuItem>
									<SidebarMenuButton
										asChild
										isActive={onDetail && detail?.params.id === open.id}
										tooltip={`${open.hostname} · ${openLabel(open)}`}
									>
										<NavLink to={`/workloads/${open.id}`}>
											<StatusGlyph
												status={
													open.removed ? "error" : syncStates[open.state].status
												}
												size="lg"
												label={openLabel(open)}
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
// form: the track alone. Its populated form is not drawn in this
// version, so a fleet with workloads shows no block. The icon rail has
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

// openLabel names the open workload's state beside its glyph: a removed
// table outranks the sync state it still holds (issue #64).
function openLabel(open: OpenWorkload): string {
	if (open.removed) return "Enforcement removed on host";
	if (open.reconnecting) return "Reconnecting";
	return syncStates[open.state].label;
}
