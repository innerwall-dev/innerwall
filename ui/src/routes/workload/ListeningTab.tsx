import { getRollup } from "@/api/fleet";
import type { RenderedPolicy, Rollup, Workload } from "@/api/schema";
import { ProblemNotice } from "@/components/Problem";
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
			<p className="mb-3 text-[12px] text-foreground-tertiary">
				What is listening on this host, paired with whether anything actually
				connects to it. Exposed-but-unused ports are candidates to leave out of
				policy.
			</p>
			{use.status === "error" ? (
				<ProblemNotice what="flow totals" error={use.error} onRetry={reload} />
			) : null}
			{w.listening_services.length === 0 ? (
				<p className="py-6 text-[12px] text-muted-foreground">
					The agent has reported no listening services.
				</p>
			) : (
				<table className="w-full border-collapse text-[12.5px]">
					<thead>
						<tr className="text-left text-[11px] uppercase tracking-[0.05em] text-muted-foreground">
							<th className="py-1.5 pr-2 font-medium">Service</th>
							<th className="px-2 py-1.5 font-medium">Process</th>
							<th className="px-2 py-1.5 font-medium">Path</th>
							<th className="px-2 py-1.5 font-medium">Inbound flows</th>
							<th className="py-1.5 pl-2 font-medium">Covered by rule</th>
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
	const cell = "border-t border-border p-2";
	let usage: { text: string; cls: string };
	if (!use) usage = { text: "…", cls: "text-muted-foreground" };
	else if (blocked > 0)
		usage = {
			text:
				blocked < conns
					? `${compact(conns)} conns · ${compact(blocked)} would block`
					: `${compact(conns)} conns · would block`,
			cls: "text-status-would-block",
		};
	else if (conns > 0)
		usage = { text: `${compact(conns)} conns`, cls: "text-status-allowed" };
	else
		usage = {
			text: `no inbound flows in ${rangeDays}d`,
			cls: "text-muted-foreground",
		};
	return (
		<tr>
			<td className={cn(cell, "pl-0 font-mono")}>{key}</td>
			<td className={cn(cell, "font-mono")}>{s.process_name || "—"}</td>
			<td className={cn(cell, "font-mono text-muted-foreground")}>
				{s.process_path || "—"}
			</td>
			<td className={cell}>
				<span className={cn("text-[12px]", usage.cls)}>{usage.text}</span>
			</td>
			<td className={cn(cell, "pr-0 text-foreground-tertiary")}>
				{rules.length === 0
					? "—"
					: rules.map((r, i) => (
							<span key={r.id}>
								{i > 0 ? ", " : null}
								{r.description || (
									<span className="font-mono text-[11px]">{ruleId(r)}</span>
								)}
							</span>
						))}
			</td>
		</tr>
	);
}
