import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { Link, useLocation, useParams } from "react-router";
import {
	getGaps,
	getRenderedPolicy,
	getRollup,
	getWorkload,
} from "@/api/fleet";
import { ProblemType, type Workload } from "@/api/schema";
import { EmptyState } from "@/components/EmptyState";
import { ModePill, syncStates } from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { StatusGlyph } from "@/components/StatusGlyph";
import { TabList, UnderlineTab } from "@/components/Tabs";
import { useResource } from "@/lib/resource";
import { useShell } from "@/shell/Shell";
import { enforcementRemoved, lastSeen, reconnecting } from "../fleet/describe";
import { ModeChangeDialog } from "../fleet/ModeChangeDialog";
import { FlowsTab } from "./FlowsTab";
import { LabelsDialog } from "./LabelsDialog";
import { ListeningTab } from "./ListeningTab";
import { Overview } from "./Overview";
import { PolicyTab } from "./PolicyTab";
import {
	defaultScreenRange,
	type ScreenRange,
	type ScreenRangeKey,
	screenRange,
} from "./range";
import { exceptionalStates, gapsShown, StateChips } from "./states";

export type Tab = "overview" | "flows" | "services" | "policy";

function tabOf(pathname: string): Tab {
	if (pathname.endsWith("/flows")) return "flows";
	if (pathname.endsWith("/services")) return "services";
	if (pathname.endsWith("/policy")) return "policy";
	return "overview";
}

// WorkloadRoute opens a fresh screen, and a fresh range, per workload.
export function WorkloadRoute() {
	const { id = "" } = useParams();
	return <WorkloadDetail key={id} />;
}

// WorkloadDetail is one workload. Its identity is in the header strip
// (the way back to Workloads, the health glyph, the name, the mode, and
// when it was last seen); its pages are tabs, the Overview first, and
// its exceptional states are chips at the right of the tab row on every
// page. Every read on the screen covers one range (range.ts).
export function WorkloadDetail() {
	const { id = "" } = useParams();
	const { pathname } = useLocation();
	const tab = tabOf(pathname);
	const shell = useShell();
	const [range, setRange] = useState<ScreenRange>(() =>
		screenRange(defaultScreenRange),
	);
	const [changing, setChanging] = useState(false);
	const [editingLabels, setEditingLabels] = useState(false);
	const { resource: wl, reload } = useResource(() => getWorkload(id), [id]);
	const { resource: policy, reload: reloadPolicy } = useResource(
		() => getRenderedPolicy(id),
		[id],
	);
	const { resource: flowTotals } = useResource(
		() =>
			getRollup({ group_by: "rule", workload: id, from: range.from, limit: 1 }),
		[id, range.from],
	);
	const { resource: gaps } = useResource(
		() => getGaps({ workload: id, from: range.from, limit: gapsShown + 1 }),
		[id, range.from],
	);

	useEffect(() => {
		if (wl.status === "ready") {
			shell.openWorkload({
				id: wl.data.id,
				hostname: wl.data.hostname,
				state: wl.data.sync.state,
				removed: enforcementRemoved(wl.data),
				reconnecting: reconnecting(wl.data),
			});
		}
	}, [wl, shell]);

	if (wl.status === "loading") {
		return (
			<div className="px-5">
				<LoadingRow what="workload" />
			</div>
		);
	}
	if (wl.status === "error") {
		return (
			<div className="px-5 pt-5">
				{wl.error.type === ProblemType.notFound ? (
					<EmptyState title="No such workload" icon="server" width={480}>
						No workload is enrolled with this identity.{" "}
						<Link to="/workloads" className="link">
							Back to the fleet
						</Link>
					</EmptyState>
				) : (
					<ProblemNotice what="workload" error={wl.error} onRetry={reload} />
				)}
			</div>
		);
	}

	const w = wl.data;
	const rendered = policy.status === "ready" ? policy.data : null;
	const states = exceptionalStates(w, gaps, range);
	const onRange = (key: ScreenRangeKey) => setRange(screenRange(key));
	return (
		<section className="flex min-h-0 flex-1 flex-col overflow-hidden">
			{shell.strip
				? createPortal(
						<Identity w={w} onChangeMode={() => setChanging(true)} />,
						shell.strip,
					)
				: null}
			<TabList aside={<StateChips states={states} />}>
				<UnderlineTab to={`/workloads/${w.id}`} label="Overview" end />
				<UnderlineTab
					to={`/workloads/${w.id}/flows`}
					label="Inbound flows"
					count={
						flowTotals.status === "ready"
							? flowTotals.data.totals.flow_count
							: undefined
					}
				/>
				<UnderlineTab
					to={`/workloads/${w.id}/services`}
					label="Listening services"
					count={w.listening_services.length}
				/>
				<UnderlineTab
					to={`/workloads/${w.id}/policy`}
					label="Applied policy"
					count={rendered ? rendered.rules.length : undefined}
				/>
			</TabList>
			{tab === "overview" ? (
				<Overview
					w={w}
					gaps={gaps}
					range={range}
					states={states}
					onReread={reload}
					onEditLabels={() => setEditingLabels(true)}
				/>
			) : null}
			{tab === "flows" ? (
				<FlowsTab
					workload={w.id}
					range={range}
					onRange={onRange}
					total={
						flowTotals.status === "ready"
							? flowTotals.data.totals.flow_count
							: undefined
					}
					policy={rendered}
				/>
			) : null}
			{tab === "services" ? (
				<ListeningTab w={w} range={range} onRange={onRange} policy={rendered} />
			) : null}
			{tab === "policy" ? (
				<div className="min-h-0 flex-1 overflow-auto px-5 pt-5 pb-6">
					<PolicyTab w={w} policy={policy} />
				</div>
			) : null}
			<LabelsDialog
				workload={w}
				open={editingLabels}
				onOpenChange={setEditingLabels}
				onSaved={() => {
					setEditingLabels(false);
					reload();
					reloadPolicy();
					shell.fleetChanged();
				}}
			/>
			<ModeChangeDialog
				title="Change mode"
				open={changing}
				workloads={[w]}
				onOpenChange={setChanging}
				onChanged={() => {
					setChanging(false);
					reload();
					shell.fleetChanged();
				}}
				onReload={() => {
					setChanging(false);
					reload();
					shell.fleetChanged();
				}}
			/>
		</section>
	);
}

// Identity is the workload in the header strip, in its one register:
// "Workloads /" in text-secondary, the page's own way back; the health
// glyph and the name in text-primary, the page's title; the mode pill
// with the way to change it; and, at the right, when it was last seen
// on its control channel, in text-tertiary.
function Identity({
	w,
	onChangeMode,
}: {
	w: Workload;
	onChangeMode: () => void;
}) {
	const removed = enforcementRemoved(w);
	const health = removed
		? { status: "error" as const, label: "Enforcement removed on host" }
		: reconnecting(w)
			? { status: "offline" as const, label: "Reconnecting" }
			: {
					status: syncStates[w.sync.state].status,
					label: syncStates[w.sync.state].label,
				};
	return (
		<div className="flex min-w-0 flex-1 items-center gap-3">
			<nav
				aria-label="Breadcrumb"
				className="flex min-w-0 items-center gap-1.5 text-secondary"
			>
				<Link to="/workloads" className="hover:text-primary">
					Workloads
				</Link>
				<span className="text-tertiary" aria-hidden="true">
					/
				</span>
				<StatusGlyph
					status={health.status}
					size="md"
					label={health.label}
					className="ml-0.5"
				/>
				<h1 className="min-w-16 truncate text-primary">{w.hostname}</h1>
			</nav>
			<ModePill mode={w.mode} />
			<button
				type="button"
				onClick={onChangeMode}
				className="shrink-0 type-caption link"
			>
				change…
			</button>
			<span
				className="ml-auto shrink-0 pr-2 text-tertiary"
				title="On the control channel, the sync stream."
			>
				last seen{" "}
				<span className="type-mono-sm">
					{w.health.last_seen_at ? `${lastSeen(w)} ago` : "never"}
				</span>
			</span>
		</div>
	);
}
