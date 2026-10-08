import { getRollup } from "@/api/fleet";
import type { RenderedPolicy, Rollup, Workload } from "@/api/schema";
import { ProblemNotice } from "@/components/Problem";
import { type FlowStatus, StatusGlyph, tone } from "@/components/StatusGlyph";
import { compact } from "@/lib/format";
import { useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
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

// ListeningTab is what listens on the host, paired with what connects to
// it over the screen's range (from the rollup by destination service)
// and with the rendered rules whose protocol and ports admit it. An
// exposed port nothing connects to is a candidate to leave out of policy.
export function ListeningTab({
	w,
	from,
	rangeDays,
	policy,
}: {
	w: Workload;
	from: string;
	rangeDays: number;
	policy: RenderedPolicy | null;
}) {
	const { resource: use, reload } = useResource(
		() =>
			Promise.all([
				getRollup({ group_by: "dst,service", workload: w.id, from }),
				getRollup({
					group_by: "dst,service",
					workload: w.id,
					from,
					verdict: "would_block",
				}),
			]).then(([all, wb]) => ({ all: byService(all), wb: byService(wb) })),
		[w.id, from],
	);

	return (
		<div>
			<p className="mb-4 max-w-[720px] type-ui text-secondary">
				What is listening on this host, paired with whether anything actually
				connects to it. Exposed-but-unused ports are candidates to leave out of
				policy.
			</p>
			{use.status === "error" ? (
				<ProblemNotice what="flow totals" error={use.error} onRetry={reload} />
			) : null}
			{w.listening_services.length === 0 ? (
				<p className="py-6 type-ui text-tertiary">
					The agent has reported no listening services.
				</p>
			) : (
				<div className="overflow-hidden rounded-lg border border-default">
					<table className="w-full border-separate border-spacing-0">
						<thead>
							<tr className="h-row-header bg-subtle">
								<th className={th}>Service</th>
								<th className={th}>Process</th>
								<th className={th}>Path</th>
								<th className={th}>Inbound flows</th>
								<th className={th}>Covered by rule</th>
							</tr>
						</thead>
						<tbody>
							{w.listening_services.map((s) => (
								<Row
									key={`${s.protocol}/${s.port}`}
									s={s}
									use={use.status === "ready" ? use.data : null}
									rangeDays={rangeDays}
									policy={policy}
								/>
							))}
						</tbody>
					</table>
				</div>
			)}
		</div>
	);
}

function Row({
	s,
	use,
	rangeDays,
	policy,
}: {
	s: Service;
	use: { all: Map<string, number>; wb: Map<string, number> } | null;
	rangeDays: number;
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
			text: `no inbound flows in ${rangeDays}d`,
		};
	return (
		<tr className="h-row-dense hover:bg-hover [&:last-child>td]:border-b-0">
			<td className={cn(td, "type-mono-ui text-primary")}>{key}</td>
			<td className={cn(td, "type-mono-ui text-primary")}>
				{s.process_name || "—"}
			</td>
			<td className={cn(td, "type-mono-sm text-tertiary")}>
				{s.process_path || "—"}
			</td>
			<td className={cn(td, "type-ui")}>
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
			<td className={cn(td, "type-ui text-secondary")}>
				{rules.length === 0
					? "—"
					: rules.map((r, i) => (
							<span key={r.id}>
								{i > 0 ? ", " : null}
								{r.description || (
									<span className="type-mono-xs">{ruleId(r)}</span>
								)}
							</span>
						))}
			</td>
		</tr>
	);
}

// The table's header and body cells.
const th =
	"border-b border-default px-3 text-left type-label whitespace-nowrap text-tertiary";
const td = "border-b border-subtle px-3 py-2";
