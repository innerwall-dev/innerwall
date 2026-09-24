import { useEffect, useState } from "react";
import { Link, useLocation, useParams } from "react-router";
import { getRenderedPolicy, getRollup, getWorkload } from "@/api/fleet";
import { ProblemType, type Workload } from "@/api/schema";
import { Eyebrow, LabelChip, ModePill } from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { TabList, UnderlineTab } from "@/components/Tabs";
import { ago, count, labelPairs } from "@/lib/format";
import { useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { useShell } from "@/shell/Shell";
import { credential, credentialTone, lastSeen } from "../fleet/describe";
import { ModeChangeDialog } from "../fleet/ModeChangeDialog";
import { FlowsTab } from "./FlowsTab";
import { ListeningTab } from "./ListeningTab";
import { PolicyTab } from "./PolicyTab";
import { StatusCard } from "./StatusCard";

// The flow reads on this screen share one range, fixed when the screen
// opens so that a page and its cursor, the tab counts, and the listening
// services' pairing all describe the same windows.
const rangeDays = 14;

export type Tab = "flows" | "services" | "policy";

// WorkloadRoute opens a fresh screen, and a fresh range, per workload.
export function WorkloadRoute() {
	const { id = "" } = useParams();
	return <WorkloadDetail key={id} />;
}

// WorkloadDetail is one workload: identity, status, labels, and host
// facts in the rail, and its inbound flows, listening services, and
// rendered policy in the tabs.
export function WorkloadDetail() {
	const { id = "" } = useParams();
	const { pathname } = useLocation();
	const tab: Tab = pathname.endsWith("/services")
		? "services"
		: pathname.endsWith("/policy")
			? "policy"
			: "flows";
	const shell = useShell();
	const [from] = useState(() =>
		new Date(Date.now() - rangeDays * 86_400_000).toISOString(),
	);
	const { resource: wl, reload } = useResource(() => getWorkload(id), [id]);
	const { resource: policy } = useResource(() => getRenderedPolicy(id), [id]);
	const { resource: flowTotals } = useResource(
		() => getRollup({ group_by: "rule", workload: id, from, limit: 1 }),
		[id, from],
	);

	useEffect(() => {
		if (wl.status === "ready") {
			shell.openWorkload({
				id: wl.data.id,
				hostname: wl.data.hostname,
				state: wl.data.sync.state,
			});
		}
	}, [wl, shell]);

	if (wl.status === "loading") {
		return (
			<div className="px-6">
				<LoadingRow what="workload" />
			</div>
		);
	}
	if (wl.status === "error") {
		return (
			<div className="px-6">
				{wl.error.type === ProblemType.notFound ? (
					<div className="flex flex-col gap-2 py-6">
						<h1 className="text-[16px] font-semibold">No such workload</h1>
						<p className="text-[12px] text-foreground-tertiary">
							No workload is enrolled with this identity.{" "}
							<Link to="/workloads" className="text-link hover:text-link-hover">
								Back to the fleet
							</Link>
						</p>
					</div>
				) : (
					<ProblemNotice what="workload" error={wl.error} onRetry={reload} />
				)}
			</div>
		);
	}

	const w = wl.data;
	const rendered = policy.status === "ready" ? policy.data : null;
	return (
		<section className="flex min-h-0 flex-1 overflow-hidden">
			<Rail
				w={w}
				onReread={() => {
					reload();
				}}
				onModeChanged={() => {
					reload();
					shell.fleetChanged();
				}}
			/>
			<div className="flex min-w-0 flex-1 flex-col overflow-hidden">
				<TabList>
					<UnderlineTab
						to={`/workloads/${w.id}`}
						label="Inbound flows"
						count={
							flowTotals.status === "ready"
								? flowTotals.data.totals.flow_count
								: undefined
						}
						end
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
				<div className="min-h-0 flex-1 overflow-auto px-6 pt-4 pb-6">
					{tab === "flows" ? (
						<FlowsTab
							workload={w.id}
							from={from}
							total={
								flowTotals.status === "ready"
									? flowTotals.data.totals.flow_count
									: undefined
							}
							policy={rendered}
						/>
					) : null}
					{tab === "services" ? (
						<ListeningTab
							w={w}
							from={from}
							rangeDays={rangeDays}
							policy={rendered}
						/>
					) : null}
					{tab === "policy" ? <PolicyTab w={w} policy={policy} /> : null}
				</div>
			</div>
		</section>
	);
}

function Rail({
	w,
	onReread,
	onModeChanged,
}: {
	w: Workload;
	onReread: () => void;
	onModeChanged: () => void;
}) {
	const [changing, setChanging] = useState(false);
	const cred = credential(w);
	const labels = labelPairs(w.labels);
	const os = w.os;
	return (
		<aside className="flex w-[320px] shrink-0 flex-col gap-[18px] overflow-auto border-r border-border bg-surface-sidebar px-5 pt-5 pb-6">
			<div className="flex flex-col gap-1">
				<h1 className="font-mono text-[16px] font-semibold">{w.hostname}</h1>
				<div className="font-mono text-[11px] break-all text-muted-foreground">
					{w.id}
				</div>
				<div className="text-[11px] text-muted-foreground">
					Identity is assigned at enrollment and cannot be edited.
				</div>
			</div>

			<div className="flex flex-col gap-2">
				<Eyebrow>Status</Eyebrow>
				<StatusCard w={w} onReread={onReread} />
				<dl className="grid grid-cols-[auto_1fr] items-center gap-x-3 gap-y-1.5 text-[12px]">
					<dt className="text-muted-foreground">mode</dt>
					<dd className="flex items-center gap-2">
						<ModePill mode={w.mode} />
						<button
							type="button"
							onClick={() => setChanging(true)}
							className="cursor-pointer text-[11px] text-link hover:text-link-hover"
						>
							change…
						</button>
					</dd>
					<dt className="text-muted-foreground">last seen</dt>
					<dd className="font-mono">
						{w.health.last_seen_at ? `${lastSeen(w)} ago` : "never"}
					</dd>
					<dt className="text-muted-foreground">enrolled</dt>
					<dd className="font-mono">{ago(w.enrolled_at)}</dd>
					<dt className="text-muted-foreground">credential</dt>
					<dd className={cn("font-mono", credentialTone[cred.tone])}>
						{cred.text}
						{w.health.credential.last_error ? (
							<div className="font-sans text-[11px] text-muted-foreground">
								{w.health.credential.last_error}
							</div>
						) : null}
					</dd>
					<dt className="text-muted-foreground">dropped flows</dt>
					<dd
						className={cn(
							"font-mono",
							w.health.dropped_flow_records > 0 && "text-status-degraded",
						)}
					>
						{count(w.health.dropped_flow_records)}
						{w.health.dropped_flow_records > 0 ? (
							<div className="font-sans text-[11px] text-muted-foreground">
								Records the agent could not deliver; the flows shown are
								incomplete.
							</div>
						) : null}
					</dd>
				</dl>
			</div>

			<div className="flex flex-col gap-2">
				<div className="flex items-center">
					<Eyebrow>Labels</Eyebrow>
					<span
						aria-disabled="true"
						title="Editing labels arrives with its own session"
						className="ml-auto cursor-default text-[11px] text-link opacity-60"
					>
						edit
					</span>
				</div>
				<div className="flex flex-wrap gap-1.5">
					{labels.length === 0 ? (
						<span className="text-[11px] text-status-degraded">
							▲ no labels — matches no scope
						</span>
					) : (
						labels.map(([k, v]) => (
							<LabelChip key={k} k={k} v={v} size="rail" />
						))
					)}
				</div>
				<div className="text-[11px] text-muted-foreground">
					Assigned from token scope at enrollment; editable by operators only.
				</div>
			</div>

			<div className="flex flex-col gap-2">
				<Eyebrow>Host facts</Eyebrow>
				<dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-[5px] font-mono text-[12px]">
					<dt className="font-sans text-muted-foreground">hostname</dt>
					<dd>{w.hostname}</dd>
					<dt className="font-sans text-muted-foreground">os</dt>
					<dd>
						{os
							? [
									[os.name, os.version].filter(Boolean).join(" "),
									[os.family, os.kernel_version].filter(Boolean).join(" "),
									os.architecture,
								]
									.filter(Boolean)
									.join(" · ")
							: "not reported"}
					</dd>
					<dt className="font-sans text-muted-foreground">agent</dt>
					<dd>{w.agent.version || "not reported"}</dd>
					<dt className="font-sans text-muted-foreground">addresses</dt>
					<dd>
						{w.addresses.length === 0
							? "none reported"
							: w.addresses.map((a) => <div key={a}>{a}</div>)}
					</dd>
				</dl>
			</div>
			<ModeChangeDialog
				title="Change mode"
				open={changing}
				workloads={[w]}
				onOpenChange={setChanging}
				onChanged={() => {
					setChanging(false);
					onModeChanged();
				}}
				onReload={() => {
					setChanging(false);
					onModeChanged();
				}}
			/>
		</aside>
	);
}
