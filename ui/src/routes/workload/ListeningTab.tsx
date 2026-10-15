import { useState } from "react";
import { getRollup } from "@/api/fleet";
import type { RenderedPolicy, Rollup, Workload } from "@/api/schema";
import { ProblemNotice } from "@/components/Problem";
import { type FlowStatus, StatusGlyph, tone } from "@/components/StatusGlyph";
import { compact, count } from "@/lib/format";
import { useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { FilterPanel, type Filters, type FilterValue } from "./FilterPanel";
import type { ScreenRange, ScreenRangeKey } from "./range";
import { coveringRules, ruleId } from "./rules";

type Service = Workload["listening_services"][number];

function byService(r: Rollup): Map<string, number> {
	const m = new Map<string, number>();
	for (const g of r.groups) {
		const s = g.keys.service;
		if (s) m.set(`${s.protocol}/${s.port}`, g.connection_count);
	}
	return m;
}

// The service facets the panel filters by.
const portOf = (s: Service) => `${s.protocol}/${s.port}`;
const processOf = (s: Service) => s.process_name || "—";
const uncovered = "none";

// ListeningTab is what listens on the host, paired with what connects to
// it over the screen's range (from the rollup by destination service)
// and with the rendered rules whose protocol and ports admit it. An
// exposed port nothing connects to is a candidate to leave out of policy.
// The filter panel narrows the services by port, process, and covering
// rule, and by text.
export function ListeningTab({
	w,
	range,
	onRange,
	policy,
}: {
	w: Workload;
	range: ScreenRange;
	onRange: (key: ScreenRangeKey) => void;
	policy: RenderedPolicy | null;
}) {
	const [filters, setFilters] = useState<Filters>({});
	const [search, setSearch] = useState("");
	const { resource: use, reload } = useResource(
		() =>
			Promise.all([
				getRollup({
					group_by: "dst,service",
					workload: w.id,
					from: range.from,
				}),
				getRollup({
					group_by: "dst,service",
					workload: w.id,
					from: range.from,
					verdict: "would_block",
				}),
			]).then(([all, wb]) => ({ all: byService(all), wb: byService(wb) })),
		[w.id, range.from],
	);

	const services = w.listening_services;
	const rulesOf = (s: Service) => {
		const r = coveringRules(policy, s);
		return r.length === 0 ? [uncovered] : r.map((x) => x.id);
	};
	const values = (
		keys: (s: Service) => string[],
		label: (k: string) => string,
	): FilterValue[] => {
		const m = new Map<string, number>();
		for (const s of services)
			for (const k of keys(s)) m.set(k, (m.get(k) ?? 0) + 1);
		return [...m.entries()].map(([k, n]) => ({
			key: k,
			label: label(k),
			mono: true,
			count: n,
		}));
	};
	const ruleLabel = new Map(
		(policy?.rules ?? []).map((r) => [
			r.id,
			[r.description, ruleId(r)].filter(Boolean).join(" "),
		]),
	);
	const covering = values(rulesOf, (k) =>
		k === uncovered ? "— no rule covers it" : (ruleLabel.get(k) ?? k),
	).sort((a, b) => Number(b.key === uncovered) - Number(a.key === uncovered));
	const facets: Record<string, (s: Service) => string[]> = {
		port: (s) => [portOf(s)],
		process: (s) => [processOf(s)],
		rule: rulesOf,
	};
	const q = search.trim().toLowerCase();
	const rows = services.filter((s) => {
		for (const [g, chosen] of Object.entries(filters)) {
			if (chosen.size > 0 && !(facets[g]?.(s) ?? []).some((k) => chosen.has(k)))
				return false;
		}
		if (q === "") return true;
		return [
			portOf(s),
			s.process_name,
			s.process_path,
			...rulesOf(s).map((k) => ruleLabel.get(k) ?? ""),
		].some((t) => t.toLowerCase().includes(q));
	});

	return (
		<div className="flex min-h-0 flex-1">
			<FilterPanel
				label="Filter listening services"
				search={search}
				onSearch={setSearch}
				placeholder="Search services"
				range={range}
				onRange={onRange}
				filters={filters}
				onFilters={setFilters}
				groups={[
					{
						key: "port",
						title: "Port",
						values: values(
							(s) => [portOf(s)],
							(k) => k,
						),
					},
					{
						key: "process",
						title: "Process",
						values: values(
							(s) => [processOf(s)],
							(k) => k,
						),
					},
					{ key: "rule", title: "Covered by rule", values: covering },
				]}
			/>
			<div className="flex min-w-0 flex-1 flex-col overflow-auto">
				<div className="flex h-row-pitch shrink-0 items-center border-b border-hairline px-5 type-dense text-secondary">
					<span>
						<span className="type-mono-data text-primary">
							{count(rows.length)}
						</span>{" "}
						of {count(services.length)} services
					</span>
				</div>
				<p className="max-w-[720px] px-5 pt-4 pb-3 type-dense text-secondary">
					What is listening on this host, paired with whether anything actually
					connects to it. Exposed-but-unused ports are candidates to leave out
					of policy.
				</p>
				{use.status === "error" ? (
					<div className="px-5">
						<ProblemNotice
							what="flow totals"
							error={use.error}
							onRetry={reload}
						/>
					</div>
				) : null}
				{services.length === 0 ? (
					<p className="px-5 py-6 type-dense text-tertiary">
						The agent has reported no listening services.
					</p>
				) : (
					<table className="w-full min-w-[724px] border-separate border-spacing-0 border-t border-hairline">
						<thead>
							<tr>
								<th className={cn(th, "pl-5")}>Service</th>
								<th className={th}>Process</th>
								<th className={th}>Path</th>
								<th className={th}>Inbound flows</th>
								<th className={cn(th, "pr-5")}>Covered by rule</th>
							</tr>
						</thead>
						<tbody>
							{rows.map((s) => (
								<Row
									key={portOf(s)}
									s={s}
									use={use.status === "ready" ? use.data : null}
									rangeShort={range.short}
									policy={policy}
								/>
							))}
						</tbody>
					</table>
				)}
			</div>
		</div>
	);
}

function Row({
	s,
	use,
	rangeShort,
	policy,
}: {
	s: Service;
	use: { all: Map<string, number>; wb: Map<string, number> } | null;
	rangeShort: string;
	policy: RenderedPolicy | null;
}) {
	const key = `${s.protocol}/${s.port}`;
	const conns = use?.all.get(key) ?? 0;
	const blocked = use?.wb.get(key) ?? 0;
	const rules = coveringRules(policy, s);
	let usage: { text: string; status?: FlowStatus };
	if (!use) usage = { text: "…" };
	else if (blocked > 0)
		usage = {
			text:
				blocked < conns
					? `${compact(conns)} conns · ${compact(blocked)} would block`
					: `${compact(conns)} conns · would block`,
			status: "would-block",
		};
	else if (conns > 0)
		usage = { text: `${compact(conns)} conns`, status: "allowed" };
	else
		usage = {
			text: `no inbound flows in ${rangeShort}`,
		};
	return (
		<tr className="hover:bg-wash-hover">
			<td className={cn(td, "pl-5 type-mono-data text-primary")}>{key}</td>
			<td className={cn(td, "type-mono-data text-primary")}>
				{s.process_name || "—"}
			</td>
			<td className={cn(td, "type-mono-sm text-tertiary")}>
				{s.process_path || "—"}
			</td>
			<td className={cn(td, "type-dense")}>
				{usage.status ? (
					<span
						className={cn(
							"inline-flex items-center gap-1.5",
							tone[usage.status],
						)}
					>
						<StatusGlyph status={usage.status} size="md" />
						<span>{usage.text}</span>
					</span>
				) : (
					<span className="text-tertiary">{usage.text}</span>
				)}
			</td>
			<td className={cn(td, "pr-5 type-dense text-secondary")}>
				{rules.length === 0
					? "—"
					: rules.map((r, i) => (
							<span key={r.id}>
								{i > 0 ? ", " : null}
								{r.description || (
									<span className="type-mono-sm">{ruleId(r)}</span>
								)}
							</span>
						))}
			</td>
		</tr>
	);
}

// The table's header and body cells.
const th =
	"h-row-header border-b border-hairline px-3 text-left type-meta-label whitespace-nowrap text-tertiary";
const td = "h-row-pitch border-b border-hairline px-3 py-2";
