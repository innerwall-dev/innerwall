import { useEffect, useMemo, useState } from "react";
import { getRollup, listFlows } from "@/api/fleet";
import type { FlowsPage, RenderedPolicy, Verdict } from "@/api/schema";
import { FilterChip, VerdictPill, verdicts } from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { Button } from "@/components/ui/button";
import { count, since } from "@/lib/format";
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
}: {
	workload: string;
	from: string;
	total: number | undefined;
	policy: RenderedPolicy | null;
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
			<fieldset className="m-0 mb-3 flex min-w-0 gap-1.5 border-0 p-0">
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
						glyphClass={verdicts[v].text}
						label={verdicts[v].label}
						count={counts.status === "ready" ? counts.data.get(v) : undefined}
						onClick={() => setVerdict(verdict === v ? null : v)}
					/>
				))}
			</fieldset>
			{resource.status === "error" ? (
				<ProblemNotice what="flows" error={resource.error} onRetry={reload} />
			) : (
				<table className="w-full border-collapse text-[12.5px]">
					<thead>
						<tr className="text-left text-[11px] uppercase tracking-[0.05em] text-muted-foreground">
							<th className="py-1.5 pr-2 font-medium">Decision</th>
							<th className="px-2 py-1.5 font-medium">Source</th>
							<th className="px-2 py-1.5 font-medium">Port</th>
							<th className="px-2 py-1.5 font-medium">Process</th>
							<th className="px-2 py-1.5 font-medium">Matched rule</th>
							<th className="px-2 py-1.5 text-right font-medium">Conns</th>
							<th className="py-1.5 pl-2 text-right font-medium">Last seen</th>
						</tr>
					</thead>
					<tbody>
						{rows.map((f) => {
							const rule = matchedRule(f, rules);
							const cell = "border-t border-border p-2";
							return (
								<tr
									key={f.id}
									title={`window ${f.window_start} – ${f.window_end}`}
								>
									<td className={cn(cell, "pl-0")}>
										<VerdictPill verdict={f.verdict} />
									</td>
									<td className={cell}>
										<div className="flex items-center gap-1.5">
											<span className="font-mono">{f.src_address}</span>
											<span className="text-[11px] text-muted-foreground">
												{peerNote(f.peer)}
											</span>
										</div>
									</td>
									<td className={cn(cell, "font-mono")}>
										{f.service.protocol === "icmp"
											? "icmp"
											: `${f.service.protocol}/${f.service.port}`}
									</td>
									<td
										className={cn(cell, "font-mono text-foreground-tertiary")}
									>
										{f.process_name || "—"}
									</td>
									<td className={cn(cell, "text-foreground-tertiary")}>
										{rule.text}
										{rule.id ? (
											<span
												className={cn(
													"font-mono text-[11px] text-muted-foreground",
													rule.text && "ml-1.5",
												)}
											>
												{rule.id}
											</span>
										) : null}
									</td>
									<td className={cn(cell, "text-right font-mono")}>
										{count(f.connection_count)}
									</td>
									<td
										className={cn(
											cell,
											"pr-0 text-right font-mono text-foreground-tertiary",
										)}
									>
										{since(f.last_seen)}
									</td>
								</tr>
							);
						})}
					</tbody>
				</table>
			)}
			{resource.status === "loading" ? <LoadingRow what="flows" /> : null}
			{resource.status === "ready" && rows.length === 0 ? (
				<p className="py-6 text-[12px] text-muted-foreground">
					{verdict
						? `No ${verdicts[verdict].label} flows in the last 14 days.`
						: "No inbound flows recorded in the last 14 days."}
				</p>
			) : null}
			{cursor || more.error ? (
				<div className="flex items-center gap-3 py-3 text-[12px]">
					{cursor ? (
						<Button
							variant="secondary"
							size="sm"
							className="rounded-chip"
							disabled={more.loading}
							onClick={loadMore}
						>
							{more.loading ? "Loading…" : "Load more"}
						</Button>
					) : null}
					{more.error ? (
						<span role="alert" className="text-destructive">
							{more.error}
						</span>
					) : null}
				</div>
			) : null}
		</div>
	);
}
