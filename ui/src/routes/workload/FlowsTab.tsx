import { useEffect, useMemo, useState } from "react";
import { listFlows } from "@/api/fleet";
import type { Flow, FlowsPage, RenderedPolicy, Verdict } from "@/api/schema";
import { LabelChip, VerdictPill, verdicts } from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { SeverityNote, StatusGlyph } from "@/components/StatusGlyph";
import { Button } from "@/components/ui/button";
import { count, labelPairs, since } from "@/lib/format";
import { asProblem, useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { FilterPanel, type Filters, type FilterValue } from "./FilterPanel";
import type { ScreenRange, ScreenRangeKey } from "./range";
import { indexRules, matchedRule, peerNote, type RuleIndex } from "./rules";

const order: Verdict[] = ["allowed", "would_block", "blocked", "observed"];

// The flow facets the panel filters by, read from a row as the table
// shows it.
function portOf(f: Flow): string {
	return f.service.protocol === "icmp"
		? "icmp"
		: `${f.service.protocol}/${f.service.port}`;
}

function sourceOf(f: Flow): string {
	return f.peer.workload_id ?? f.peer.address_group_id ?? f.src_address;
}

function ruleOf(f: Flow, rules: RuleIndex): { key: string; label: string } {
	const r = matchedRule(f, rules);
	if (!f.rule)
		return { key: `none:${f.verdict === "observed"}`, label: r.text };
	return {
		key: f.rule.id,
		label: [r.text, r.id].filter(Boolean).join(" "),
	};
}

// tally counts the rows per facet value, keeping the first row's
// presentation of each.
function tally<T extends { key: string }>(
	rows: Flow[],
	facet: (f: Flow) => T,
): (T & { count: number })[] {
	const m = new Map<string, T & { count: number }>();
	for (const f of rows) {
		const v = facet(f);
		const had = m.get(v.key);
		if (had) had.count += 1;
		else m.set(v.key, { ...v, count: 1 });
	}
	return [...m.values()].sort(
		(a, b) => b.count - a.count || a.key.localeCompare(b.key),
	);
}

// FlowsTab is the workload's stored flow windows over the screen's
// range, newest first, a page at a time. Each row is one reporting window
// of one source and service as ingestion stored it. The filter panel
// narrows the rows loaded so far, by decision, port, source, and matched
// rule, and by text; its counts, like the result line, are of the loaded
// rows, which the result line says. The tab's own count is the range's
// total, from the rollup.
export function FlowsTab({
	workload,
	range,
	onRange,
	policy,
}: {
	workload: string;
	range: ScreenRange;
	onRange: (key: ScreenRangeKey) => void;
	policy: RenderedPolicy | null;
}) {
	const [pages, setPages] = useState<FlowsPage[]>([]);
	const [more, setMore] = useState<{ loading: boolean; error?: string }>({
		loading: false,
	});
	const [filters, setFilters] = useState<Filters>({});
	const [search, setSearch] = useState("");
	const rules = useMemo(() => indexRules(policy), [policy]);

	const filter = useMemo(
		() => ({ workload, from: range.from }),
		[workload, range.from],
	);
	const { resource, reload } = useResource(() => listFlows(filter), [filter]);
	useEffect(() => {
		if (resource.status === "ready") setPages([resource.data]);
	}, [resource]);

	const loaded = pages.flatMap((p) => p.flows);
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

	const decisions: FilterValue[] = order.map((v) => ({
		key: v,
		label: verdicts[v].label,
		glyph: <StatusGlyph status={verdicts[v].status} size="md" />,
		count: loaded.filter((f) => f.verdict === v).length,
	}));
	const ports = tally(loaded, (f) => ({ key: portOf(f), label: portOf(f) }));
	const sources = tally(loaded, (f) => ({
		key: sourceOf(f),
		label: f.peer.name ?? f.src_address,
		annotation: f.peer.workload_id
			? undefined
			: f.peer.address_group_id
				? "address group"
				: "not managed",
	}));
	const matched = tally(loaded, (f) => ruleOf(f, rules)).sort(
		(a, b) =>
			Number(b.key.startsWith("none:")) - Number(a.key.startsWith("none:")),
	);

	const q = search.trim().toLowerCase();
	const facets: Record<string, (f: Flow) => string> = {
		decision: (f) => f.verdict,
		port: portOf,
		source: sourceOf,
		rule: (f) => ruleOf(f, rules).key,
	};
	const rows = loaded.filter((f) => {
		for (const [g, chosen] of Object.entries(filters)) {
			if (chosen.size > 0 && !chosen.has(facets[g]?.(f) ?? "")) return false;
		}
		if (q === "") return true;
		const rule = matchedRule(f, rules);
		return [
			f.src_address,
			peerNote(f.peer),
			portOf(f),
			f.process_name ?? "",
			rule.text,
			rule.id ?? "",
			verdicts[f.verdict].label,
		].some((t) => t.toLowerCase().includes(q));
	});
	const chosenDecisions = order.filter((v) => filters.decision?.has(v));

	return (
		<div className="flex min-h-0 flex-1">
			<FilterPanel
				label="Filter inbound flows"
				search={search}
				onSearch={setSearch}
				placeholder="Search flows"
				range={range}
				onRange={onRange}
				filters={filters}
				onFilters={setFilters}
				groups={[
					{ key: "decision", title: "Decision", values: decisions },
					{
						key: "port",
						title: "Port",
						values: ports.map((p) => ({ ...p, mono: true })),
					},
					{
						key: "source",
						title: "Source",
						values: sources.map((s) => ({ ...s, mono: true })),
					},
					{
						key: "rule",
						title: "Matched rule",
						values: matched.map((r) => ({ ...r, mono: true })),
					},
				]}
			/>
			<div className="flex min-w-0 flex-1 flex-col overflow-auto">
				<div
					className="flex h-row-pitch shrink-0 items-center gap-2 border-b border-hairline px-5 type-dense text-secondary"
					data-testid="flows-result"
				>
					<span>
						<span className="type-mono-data text-primary">
							{count(rows.length)}
						</span>{" "}
						of {count(loaded.length)} loaded
						{cursor ? " · more not loaded" : ""}
					</span>
					{chosenDecisions.map((v) => (
						<VerdictPill key={v} verdict={v} />
					))}
				</div>
				{resource.status === "error" ? (
					<div className="px-5">
						<ProblemNotice
							what="flows"
							error={resource.error}
							onRetry={reload}
						/>
					</div>
				) : (
					<table className="w-full min-w-[724px] border-separate border-spacing-0">
						<thead>
							<tr>
								<th className={cn(th, "pl-5")}>Decision</th>
								<th className={th}>Source</th>
								<th className={th}>Port</th>
								<th className={th}>Process</th>
								<th className={th}>Matched rule</th>
								<th className={cn(th, "text-right")}>Conns</th>
								<th className={cn(th, "pr-5 text-right")}>Last seen</th>
							</tr>
						</thead>
						<tbody>
							{rows.map((f) => (
								<Row key={f.id} f={f} rules={rules} />
							))}
						</tbody>
					</table>
				)}
				{resource.status === "loading" ? (
					<div className="px-5">
						<LoadingRow what="flows" />
					</div>
				) : null}
				{resource.status === "ready" && loaded.length === 0 ? (
					<p className="px-5 py-6 type-dense text-tertiary">
						No inbound flows recorded in the last {range.text}.
					</p>
				) : null}
				{resource.status === "ready" &&
				loaded.length > 0 &&
				rows.length === 0 ? (
					<p className="px-5 py-6 type-dense text-tertiary">
						No loaded flows match these filters.
					</p>
				) : null}
				{cursor || more.error ? (
					<div className="flex items-center gap-3 px-5 py-4">
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
							<SeverityNote
								level="error"
								role="alert"
								prose
								className="type-dense"
							>
								{more.error}
							</SeverityNote>
						) : null}
					</div>
				) : null}
			</div>
		</div>
	);
}

function Row({ f, rules }: { f: Flow; rules: RuleIndex }) {
	const rule = matchedRule(f, rules);
	const labels = f.peer.workload_id ? labelPairs(f.peer.labels) : [];
	return (
		<tr
			title={`window ${f.window_start} – ${f.window_end}`}
			className="hover:bg-wash-hover"
		>
			<td className={cn(td, "pl-5")}>
				<VerdictPill verdict={f.verdict} />
			</td>
			<td className={td}>
				<div className="flex flex-wrap items-center gap-x-2 gap-y-1">
					<span className="type-mono-data text-primary">{f.src_address}</span>
					{f.peer.workload_id ? (
						<>
							<span className="type-dense text-secondary">
								{f.peer.name ?? "workload"}
							</span>
							{labels.map(([k, v]) => (
								<LabelChip key={k} k={k} v={v} />
							))}
						</>
					) : (
						<span className="type-caption text-tertiary">
							{peerNote(f.peer)}
						</span>
					)}
				</div>
			</td>
			<td className={cn(td, "type-mono-data")}>{portOf(f)}</td>
			<td className={cn(td, "type-mono-data text-secondary")}>
				{f.process_name || "—"}
			</td>
			<td className={cn(td, "type-dense text-secondary")}>
				{rule.text}
				{rule.id ? (
					<span
						className={cn(
							"type-mono-sm whitespace-nowrap text-tertiary",
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
			<td className={cn(td, "pr-5 text-right type-mono-data text-secondary")}>
				{since(f.last_seen)}
			</td>
		</tr>
	);
}

// The table's header and body cells: the meta label over 40px rows on
// hairlines.
const th =
	"h-row-header border-b border-hairline px-3 text-left type-meta-label whitespace-nowrap text-tertiary";
const td = "h-row-pitch border-b border-hairline px-3 py-2 text-primary";
