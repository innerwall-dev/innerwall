import { useEffect, useMemo, useState } from "react";
import { getRollup, listFlows } from "@/api/fleet";
import type {
	FlowsPage,
	RenderedPolicy,
	Verdict,
	Workload,
} from "@/api/schema";
import { FilterChip, VerdictPill, verdicts } from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { SeverityNote } from "@/components/StatusGlyph";
import { Button } from "@/components/ui/button";
import { ago, count, offset, since } from "@/lib/format";
import { asProblem, useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { indexRules, matchedRule, peerNote } from "./rules";

const order: Verdict[] = ["allowed", "would_block", "blocked", "observed"];

// FlowsTab is the workload's stored flow windows, newest first, a page
// at a time. Each row is one reporting window of one source and service
// as ingestion stored it; the counts on the chips are stored records
// per decision over the same range, from the rollup.
export function FlowsTab({
	workload,
	from,
	total,
	policy,
	clockSkew,
}: {
	workload: string;
	from: string;
	total: number | undefined;
	policy: RenderedPolicy | null;
	clockSkew: ClockSkew | null;
}) {
	const [verdict, setVerdict] = useState<Verdict | null>(null);
	const [pages, setPages] = useState<FlowsPage[]>([]);
	const [more, setMore] = useState<{ loading: boolean; error?: string }>({
		loading: false,
	});
	const rules = useMemo(() => indexRules(policy), [policy]);

	const { resource: counts } = useResource(
		() =>
			Promise.all(
				order.map((v) =>
					getRollup({
						group_by: "rule",
						workload,
						from,
						verdict: v,
						limit: 1,
					}).then((r) => [v, r.totals.flow_count] as const),
				),
			).then((pairs) => new Map<Verdict, number>(pairs)),
		[workload, from],
	);
	const filter = useMemo(
		() => ({ workload, from, verdict: verdict ?? undefined }),
		[workload, from, verdict],
	);
	const { resource, reload } = useResource(() => listFlows(filter), [filter]);
	useEffect(() => {
		if (resource.status === "ready") setPages([resource.data]);
	}, [resource]);

	const rows = pages.flatMap((p) => p.flows);
	const cursor = pages.at(-1)?.next_cursor ?? null;

	async function loadMore() {
		if (!cursor) return;
		setMore({ loading: true });
		try {
			const page = await listFlows(filter, cursor);
			setPages((p) => [...p, page]);
			setMore({ loading: false });
		} catch (err) {
			setMore({ loading: false, error: asProblem(err).message });
		}
	}

	const shown = order.filter(
		(v) =>
			counts.status === "ready" &&
			((counts.data.get(v) ?? 0) > 0 || verdict === v),
	);

	return (
		<div>
			<fieldset className="m-0 mb-4 flex min-w-0 items-center gap-2 border-0 p-0">
				<legend className="sr-only">Decision</legend>
				<FilterChip
					on={verdict === null}
					label="all"
					count={total}
					onClick={() => setVerdict(null)}
				/>
				{shown.map((v) => (
					<FilterChip
						key={v}
						on={verdict === v}
						glyph={verdicts[v].glyph}
						label={verdicts[v].label}
						count={counts.status === "ready" ? counts.data.get(v) : undefined}
						onClick={() => setVerdict(verdict === v ? null : v)}
					/>
				))}
			</fieldset>
			<ClockSkewNote skew={clockSkew} from={from} />
			{resource.status === "error" ? (
				<ProblemNotice what="flows" error={resource.error} onRetry={reload} />
			) : (
				<div className="overflow-hidden rounded-panel border border-hairline">
					<table className="w-full border-separate border-spacing-0">
						<thead>
							<tr className="h-row-header bg-card">
								<th className={th}>Decision</th>
								<th className={th}>Source</th>
								<th className={th}>Port</th>
								<th className={th}>Process</th>
								<th className={th}>Matched rule</th>
								<th className={cn(th, "text-right")}>Conns</th>
								<th className={cn(th, "text-right")}>Last seen</th>
							</tr>
						</thead>
						<tbody>
							{rows.map((f) => {
								const rule = matchedRule(f, rules);
								return (
									<tr
										key={f.id}
										title={`window ${f.window_start} – ${f.window_end}`}
										className="h-row-pitch hover:bg-wash-hover [&:last-child>td]:border-b-0"
									>
										<td className={td}>
											<VerdictPill verdict={f.verdict} />
										</td>
										<td className={cn(td, "type-dense")}>
											<div className="flex flex-wrap items-baseline gap-x-2">
												<span className="type-mono-data text-primary">
													{f.src_address}
												</span>
												<span className="type-caption text-tertiary">
													{peerNote(f.peer)}
												</span>
											</div>
										</td>
										<td className={cn(td, "type-mono-data")}>
											{f.service.protocol === "icmp"
												? "icmp"
												: `${f.service.protocol}/${f.service.port}`}
										</td>
										<td className={cn(td, "type-mono-data text-secondary")}>
											{f.process_name || "—"}
										</td>
										<td className={cn(td, "type-dense text-secondary")}>
											{rule.text}
											{rule.id ? (
												<span
													className={cn(
														"type-mono-xs whitespace-nowrap text-tertiary",
														rule.text && "ml-2",
													)}
												>
													{rule.id}
												</span>
											) : null}
										</td>
										<td className={cn(td, "text-right type-mono-data")}>
											{count(f.connection_count)}
										</td>
										<td
											className={cn(
												td,
												"text-right type-mono-data text-secondary",
											)}
										>
											{since(f.last_seen)}
										</td>
									</tr>
								);
							})}
						</tbody>
					</table>
				</div>
			)}
			{resource.status === "loading" ? <LoadingRow what="flows" /> : null}
			{resource.status === "ready" && rows.length === 0 ? (
				<p className="py-6 type-dense text-tertiary">
					{verdict
						? `No ${verdicts[verdict].label} flows in the last 14 days.`
						: "No inbound flows recorded in the last 14 days."}
				</p>
			) : null}
			{cursor || more.error ? (
				<div className="flex items-center gap-3 pt-4">
					{cursor ? (
						<Button
							variant="secondary"
							size="sm"
							disabled={more.loading}
							onClick={loadMore}
						>
							{more.loading ? "Loading…" : "Load more"}
						</Button>
					) : null}
					{more.error ? (
						<SeverityNote level="error" role="alert" className="type-dense">
							{more.error}
						</SeverityNote>
					) : null}
				</div>
			) : null}
		</div>
	);
}

// The table's header and body cells.
const th =
	"border-b border-hairline px-3 text-left type-control whitespace-nowrap text-tertiary";
const td = "border-b border-hairline px-3 py-2 text-primary";

type ClockSkew = NonNullable<Workload["health"]["clock_skew"]>;

// ClockSkewNote says the agent's clock disagreed with the control
// plane's: its windows are stamped with its own clock, so the times in
// the table are off by about the offset. It shows while the skew is
// current, and after the clock came back for as long as the last skewed
// report falls inside the table's range, since the windows stamped by the
// wrong clock are still in it.
function ClockSkewNote({
	skew,
	from,
}: {
	skew: ClockSkew | null;
	from: string;
}) {
	if (!skew) return null;
	if (!skew.current && Date.parse(skew.observed_at) < Date.parse(from)) {
		return null;
	}
	const by = offset(skew.offset_seconds * 1000);
	const way = skew.offset_seconds < 0 ? "behind" : "ahead of";
	return (
		<p className="mb-4" data-testid="clock-skew">
			<SeverityNote level="alert" prose className="type-dense">
				{skew.current ? (
					<span>
						This agent's clock is {by} {way} the control plane's, as of its
						latest flow report {ago(skew.observed_at)}. It stamps its windows
						with its own clock, so the times below are off by about that much.
						Re-sync the host's clock, for example with{" "}
						<code className="type-mono-data">
							systemctl restart systemd-timesyncd
						</code>
						.
					</span>
				) : (
					<span>
						This agent's clock was {by} {way} the control plane's in a flow
						report {ago(skew.observed_at)}; its reports since are within
						tolerance. Windows it reported until then were stamped with the
						wrong clock, and their times below are off by up to that much.
					</span>
				)}
			</SeverityNote>
		</p>
	);
}
