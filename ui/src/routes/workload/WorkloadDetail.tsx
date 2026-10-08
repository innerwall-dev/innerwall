import { useEffect, useState } from "react";
import { Link, useLocation, useParams } from "react-router";
import {
	getGaps,
	getRenderedPolicy,
	getRollup,
	getWorkload,
} from "@/api/fleet";
import { type EvidenceGaps, ProblemType, type Workload } from "@/api/schema";
import { EmptyState } from "@/components/EmptyState";
import { Eyebrow, LabelChip, ModePill } from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { SeverityNote, StatusGlyph } from "@/components/StatusGlyph";
import { TabList, UnderlineTab } from "@/components/Tabs";
import { ago, count, labelPairs } from "@/lib/format";
import { between, kindText } from "@/lib/gaps";
import { type Resource, useResource } from "@/lib/resource";
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
	const { resource: gaps } = useResource(
		() => getGaps({ workload: id, from, limit: gapsShown + 1 }),
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
			<div className="px-6 pt-6">
				{wl.error.type === ProblemType.notFound ? (
					<EmptyState title="No such workload" width={480}>
						No workload is enrolled with this identity.{" "}
						<Link to="/workloads" className="text-link hover:underline">
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
	return (
		<section className="flex min-h-0 flex-1 overflow-hidden">
			<Rail
				w={w}
				gaps={gaps}
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
				<div className="min-h-0 flex-1 overflow-auto px-6 pt-5 pb-6">
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

// The rail's field rows: a sentence-case label beside a mono value, and
// the caption that explains a value under it.
const field = "type-label text-tertiary";
const value = "min-w-0 type-mono-sm break-words text-primary";
const note = "mt-0.5 type-caption text-tertiary";
// A severity note in the rail's 16px lines centres its glyph on them.
const dense = "type-mono-sm";

// Counted is a loss counter: plain at zero, an alert past it.
function Counted({ n }: { n: number }) {
	if (n === 0) return <span>{count(n)}</span>;
	return (
		<SeverityNote level="alert" size="sm" className={dense}>
			{count(n)}
		</SeverityNote>
	);
}

// The most evidence gaps the rail lists; past it the rail says there are
// more.
const gapsShown = 3;

function Rail({
	w,
	gaps,
	onReread,
	onModeChanged,
}: {
	w: Workload;
	gaps: Resource<EvidenceGaps>;
	onReread: () => void;
	onModeChanged: () => void;
}) {
	const [changing, setChanging] = useState(false);
	const cred = credential(w, Date.now(), "rail");
	const labels = labelPairs(w.labels);
	const os = w.os;
	return (
		<aside className="flex w-[320px] shrink-0 flex-col gap-6 overflow-auto border-r border-default bg-subtle px-5 pt-5 pb-6">
			<div className="flex flex-col gap-1">
				<h1 className="type-title-page font-mono break-all text-primary">
					{w.hostname}
				</h1>
				<div className="type-mono-xs break-all text-tertiary">{w.id}</div>
				<div className="type-caption text-tertiary">
					Identity is assigned at enrollment and cannot be edited.
				</div>
			</div>

			<div className="flex flex-col gap-2">
				<Eyebrow>Status</Eyebrow>
				<StatusCard w={w} onReread={onReread} />
				<dl className="mt-1 grid grid-cols-[auto_1fr] items-baseline gap-x-4 gap-y-2.5">
					<dt className={field}>mode</dt>
					<dd className="flex items-center gap-2">
						<ModePill mode={w.mode} />
						<button
							type="button"
							onClick={() => setChanging(true)}
							className="cursor-pointer type-caption text-link hover:underline"
						>
							change…
						</button>
					</dd>
					<dt className={field}>last seen</dt>
					<dd className={value}>
						{w.health.last_seen_at ? `${lastSeen(w)} ago` : "never"}
					</dd>
					<dt className={field}>enrolled</dt>
					<dd className={value}>{ago(w.enrolled_at)}</dd>
					<dt className={field}>credential</dt>
					<dd className={value}>
						{cred.tone === "ok" ? (
							<span className={credentialTone.ok}>{cred.text}</span>
						) : (
							<SeverityNote
								level={cred.tone === "bad" ? "error" : "alert"}
								size="sm"
								className={dense}
							>
								{cred.text}
							</SeverityNote>
						)}
						{w.health.credential.last_error ? (
							<div className={note}>{w.health.credential.last_error}</div>
						) : null}
					</dd>
					<dt className={field}>dropped flows</dt>
					<dd className={value}>
						<Counted n={w.health.dropped_flow_records} />
						{w.health.dropped_flow_records > 0 ? (
							<div className={note}>
								Records the agent could not deliver; the flows shown are
								incomplete.
							</div>
						) : null}
					</dd>
					<dt className={field}>overruns</dt>
					<dd className={value}>
						<Counted n={w.health.source_overruns} />
						{w.health.source_overruns > 0 ? (
							<div className={note}>
								Times the kernel dropped events because a flow source fell
								behind, since the agent started.
							</div>
						) : null}
					</dd>
					<dt className={field}>evidence gaps</dt>
					<dd>
						<EvidenceGapsCell gaps={gaps} />
					</dd>
				</dl>
			</div>

			<div className="flex flex-col gap-2">
				<div className="flex items-center">
					<Eyebrow>Labels</Eyebrow>
					<span
						aria-disabled="true"
						title="Editing labels arrives with its own session"
						className="ml-auto cursor-default type-caption text-link opacity-disabled"
					>
						edit
					</span>
				</div>
				<div className="flex flex-wrap gap-1">
					{labels.length === 0 ? (
						<SeverityNote level="alert" size="sm" className="type-caption">
							no labels — matches no scope
						</SeverityNote>
					) : (
						labels.map(([k, v]) => (
							<LabelChip key={k} k={k} v={v} size="rail" />
						))
					)}
				</div>
				<div className="type-caption text-tertiary">
					Assigned from token scope at enrollment; editable by operators only.
				</div>
			</div>

			<div className="flex flex-col gap-2">
				<Eyebrow>Host facts</Eyebrow>
				<dl className="grid grid-cols-[auto_1fr] items-baseline gap-x-4 gap-y-2">
					<dt className={field}>hostname</dt>
					<dd className={value}>{w.hostname}</dd>
					<dt className={field}>os</dt>
					<dd className={value}>
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
					<dt className={field}>agent</dt>
					<dd className={value}>{w.agent.version || "not reported"}</dd>
					<dt className={field}>addresses</dt>
					<dd className={value}>
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

// EvidenceGapsCell lists the newest intervals over the screen's range in
// which the agent knows it lost evidence: flows in them are incomplete.
function EvidenceGapsCell({ gaps }: { gaps: Resource<EvidenceGaps> }) {
	if (gaps.status === "loading") {
		return <span className="type-mono-sm text-tertiary">…</span>;
	}
	if (gaps.status === "error") {
		return (
			<span className="type-caption text-tertiary">could not be read</span>
		);
	}
	const shown = gaps.data.gaps.slice(0, gapsShown);
	const more = gaps.data.gaps.length > gapsShown || gaps.data.truncated;
	if (shown.length === 0) {
		return (
			<span className="type-mono-sm text-primary">
				none{" "}
				<span className="type-caption text-tertiary">in {rangeDays} days</span>
			</span>
		);
	}
	return (
		<div className="flex flex-col gap-1.5" data-testid="evidence-gaps">
			<span className="type-mono-sm text-primary">
				{count(gaps.data.gaps.length)}
				{more ? "+" : ""}{" "}
				<span className="type-caption text-tertiary">in {rangeDays} days</span>
			</span>
			<ul className="flex flex-col gap-1 type-caption text-secondary">
				{shown.map((g) => (
					<li
						key={`${g.kind}|${g.source}|${g.from}|${g.to}`}
						className="flex items-start gap-1.5"
					>
						<StatusGlyph status="alert" size="sm" className="mt-0.5" />
						<span className="min-w-0">
							{kindText(g.kind)} between {between(g.from, g.to)}
							{g.count !== null ? ` · ${count(g.count)} lost` : ""}
						</span>
					</li>
				))}
			</ul>
			<div className="type-caption text-tertiary">
				Intervals the agent knows it lost evidence in; the flows shown in them
				are incomplete.
			</div>
		</div>
	);
}
