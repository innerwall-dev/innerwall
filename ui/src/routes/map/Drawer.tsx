import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import { listFlows } from "@/api/fleet";
import type { Flow, FlowsPage, PeerRef, Workload } from "@/api/schema";
import {
	Eyebrow,
	LabelChip,
	ModePill,
	verdicts,
} from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { Button } from "@/components/ui/button";
import { count, since } from "@/lib/format";
import { asProblem, useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { ModeLine, subtitle } from "./Graph";
import {
	keylessId,
	type MapEdge,
	type MapModel,
	type MapNode,
	type Pair,
	precedence,
	type Selection,
	unlabeledId,
} from "./model";

// The drawer beside either take. An edge opens its decisions, the rule
// it suggests, and its pairs; a pair opens its stored windows from
// GET /flows. A node opens its members and its edges.

export function Drawer({
	model,
	workloads,
	selection,
	groupKey,
	scope,
	from,
	to,
	onSelect,
}: {
	model: MapModel;
	workloads: Workload[];
	selection: Exclude<Selection, null>;
	groupKey: string;
	scope: readonly string[];
	from: string;
	to: string;
	onSelect: (s: Selection) => void;
}) {
	const nodes = new Map(model.nodes.map((n) => [n.id, n]));
	return (
		<aside
			aria-label={
				selection.kind === "edge" ? "Traffic between groups" : "Group"
			}
			className="flex w-[380px] shrink-0 flex-col overflow-hidden border-l border-border bg-surface-sidebar"
		>
			{selection.kind === "edge" ? (
				<EdgeDrawer
					key={selection.id}
					edge={model.edges.find((e) => e.id === selection.id) as MapEdge}
					nodes={nodes}
					groupKey={groupKey}
					scope={scope}
					from={from}
					to={to}
					onClose={() => onSelect(null)}
				/>
			) : (
				<NodeDrawer
					key={selection.id}
					node={nodes.get(selection.id) as MapNode}
					model={model}
					nodes={nodes}
					workloads={workloads}
					groupKey={groupKey}
					onSelect={onSelect}
				/>
			)}
		</aside>
	);
}

function Header({
	eyebrow,
	onClose,
	children,
}: {
	eyebrow: string;
	onClose: () => void;
	children: React.ReactNode;
}) {
	return (
		<div className="flex flex-col gap-2 border-b border-border px-4 py-3.5">
			<div className="flex items-center gap-2">
				<Eyebrow>{eyebrow}</Eyebrow>
				<button
					type="button"
					onClick={onClose}
					aria-label="Close"
					className="ml-auto cursor-pointer text-[16px] leading-none text-muted-foreground hover:text-foreground"
				>
					×
				</button>
			</div>
			{children}
		</div>
	);
}

function DecisionCount({
	verdict,
	n,
}: {
	verdict: Pair["verdict"];
	n: number;
}) {
	const v = verdicts[verdict];
	return (
		<span
			className={cn(
				"inline-flex items-center gap-[5px] whitespace-nowrap rounded-pill border px-2 py-0.5 text-[11.5px]",
				v.cls,
			)}
		>
			<span aria-hidden="true">{v.glyph}</span>
			<span>{v.label}</span>
			<span className="font-mono opacity-80">{count(n)}</span>
		</span>
	);
}

// note is the edge's one-line reading, by the decision that takes
// precedence and what the destination group's listed members run in.
function note(e: MapEdge, to: MapNode): string {
	const only = (m: string) => {
		const entries = Object.entries(to.modes).filter(([, c]) => (c ?? 0) > 0);
		return entries.length === 1 && entries[0][0] === m;
	};
	switch (e.decision) {
		case "observed":
			return only("visibility")
				? `${to.title} is in visibility mode — no policy was evaluated. Drawing a rule now prepares for simulation.`
				: "No policy was evaluated for these connections: their workloads were in visibility mode when they were recorded.";
		case "would_block":
			return only("simulation")
				? `${to.title} is simulating. This traffic still flows, but the current ruleset would drop it if enforced.`
				: "This traffic still flowed, but the ruleset in force would have dropped it had its workloads been enforced.";
		case "blocked":
			return only("enforced")
				? `${to.title} is enforced. These connections were dropped on the host.`
				: "These connections were dropped on the host.";
		default:
			return `Permitted by an enabled rule in ${to.title}'s rendered policy.`;
	}
}

function hostBits(addr: string): string {
	return addr.includes(":") ? "128" : "32";
}

// peerSelector is how a rule would name the edge's source, derived from
// the edge alone: the group's requirement, with the map's scope
// requirements when every source workload carries them too; the address
// group; or the addresses themselves.
export function peerSelector(
	e: MapEdge,
	from: MapNode,
	groupKey: string,
	scope: readonly string[],
): string {
	switch (from.kind) {
		case "managed": {
			const shared = scope.filter((req) => {
				const [k, v] = req.split("=");
				return (
					k !== groupKey &&
					e.pairs.every((p) => p.src.labels[k as string] === v)
				);
			});
			return [from.selector ?? "", ...shared].join(" AND ");
		}
		case "address-group":
			return `address group ${from.title}`;
		case "unknown": {
			const addrs = [
				...new Set(
					e.pairs.filter((p) => p.src.kind === "address").map((p) => p.peerKey),
				),
			];
			if (addrs.length === 0)
				return "none: the sources' stored kind is not recognized";
			const first = `cidr ${addrs[0]}/${hostBits(addrs[0])}`;
			return addrs.length > 1 ? `${first} (+${addrs.length - 1})` : first;
		}
		case "keyless":
			return `none: the sources carry no ${groupKey} label`;
		default:
			return "none: the sources carry no labels";
	}
}

// peerName is a pair's source as a row names it.
export function peerName(p: PeerRef): string {
	switch (p.kind) {
		case "workload":
			return p.name ?? p.workload_id ?? "workload";
		case "group":
			return p.name ?? "address group";
		case "address":
			return p.address ?? "address";
		default:
			return `unrecognized · ${p.address ?? ""}`;
	}
}

function service(f: Flow): string {
	return f.service.port === 0
		? f.service.protocol
		: `${f.service.protocol}/${f.service.port}`;
}

function EdgeDrawer({
	edge: e,
	nodes,
	groupKey,
	scope,
	from,
	to,
	onClose,
}: {
	edge: MapEdge;
	nodes: Map<string, MapNode>;
	groupKey: string;
	scope: readonly string[];
	from: string;
	to: string;
	onClose: () => void;
}) {
	const src = nodes.get(e.source) as MapNode;
	const dst = nodes.get(e.target) as MapNode;
	const pairKey = (p: Pair) => `${p.dst.id}|${p.peerKey}|${p.verdict}`;
	const [open, setOpen] = useState<string | null>(
		e.pairs.length === 1 ? pairKey(e.pairs[0]) : null,
	);
	const [services, setServices] = useState<string[] | null>(null);
	const opened = e.pairs.find((p) => pairKey(p) === open) ?? null;

	return (
		<>
			<Header eyebrow="Traffic between groups" onClose={onClose}>
				<div className="flex items-center gap-2 font-mono text-[13px]">
					<span>{src.title}</span>
					<span className="text-foreground-separator" aria-hidden="true">
						→
					</span>
					<span className="sr-only">to</span>
					<span>{dst.title}</span>
				</div>
				<div className="flex flex-wrap gap-1.5">
					{precedence
						.filter((v) => e.byDecision[v])
						.map((v) => (
							<DecisionCount
								key={v}
								verdict={v}
								n={e.byDecision[v]?.connections ?? 0}
							/>
						))}
				</div>
				<p className="text-[12px] text-foreground-tertiary">{note(e, dst)}</p>
			</Header>
			<div className="flex flex-col gap-2 border-b border-border px-4 py-3">
				<Eyebrow>Draw a rule from this</Eyebrow>
				<dl className="flex flex-col gap-1.5 rounded border border-input bg-background px-3 py-2.5 text-[12px]">
					<div className="flex gap-2">
						<dt className="w-[60px] shrink-0 text-muted-foreground">peers</dt>
						<dd className="font-mono break-words" data-testid="rule-peers">
							{peerSelector(e, src, groupKey, scope)}
						</dd>
					</div>
					<div className="flex gap-2">
						<dt className="w-[60px] shrink-0 text-muted-foreground">
							services
						</dt>
						<dd className="font-mono" data-testid="rule-services">
							{services && services.length > 0 ? (
								services.join(", ")
							) : (
								<span className="font-sans text-muted-foreground">
									open a pair below to read its services
								</span>
							)}
						</dd>
					</div>
				</dl>
				<div className="flex items-center gap-1.5">
					<Button
						size="sm"
						aria-disabled="true"
						title="The policy editor arrives with the write screens"
						className="cursor-default rounded-chip"
						onClick={(ev) => ev.preventDefault()}
					>
						Open in policy editor
					</Button>
					<span className="text-[11px] text-muted-foreground">
						The policy editor arrives with the write screens.
					</span>
				</div>
			</div>
			<div className="px-4 pt-2.5 pb-1">
				<Eyebrow>
					Workload pairs on this edge ·{" "}
					<span className="font-mono">{e.pairs.length}</span>
				</Eyebrow>
			</div>
			<div className="min-h-0 flex-1 overflow-auto px-4 pb-4">
				<ul className="flex flex-col">
					{e.pairs.map((p) => {
						const k = pairKey(p);
						const isOpen = k === open;
						const v = verdicts[p.verdict];
						return (
							<li key={k} className="border-t border-border">
								<button
									type="button"
									aria-expanded={isOpen}
									onClick={() => {
										setOpen(isOpen ? null : k);
										setServices(null);
									}}
									className={cn(
										"grid w-full cursor-pointer grid-cols-[1fr_auto_auto] items-center gap-x-2 py-[7px] text-left text-[12px]",
										isOpen && "bg-surface-row-selected",
									)}
								>
									<span className="min-w-0 truncate font-mono">
										{peerName(p.src)}
										<span className="text-foreground-separator"> → </span>
										{p.dst.hostname}
									</span>
									<span className={cn("font-mono", v.text)}>
										<span aria-hidden="true">{v.glyph}</span>{" "}
										{count(p.connections)}
										<span className="sr-only"> {v.label}</span>
									</span>
									<span className="w-[34px] text-right font-mono text-foreground-tertiary">
										{since(p.lastSeen)}
									</span>
								</button>
								{isOpen && opened ? (
									<PairFlows
										pair={opened}
										from={from}
										to={to}
										onServices={setServices}
									/>
								) : null}
							</li>
						);
					})}
				</ul>
			</div>
		</>
	);
}

// PairFlows is one pair's stored windows under the pair's decision,
// newest first, from GET /flows with the workload and peer filters.
function PairFlows({
	pair,
	from,
	to,
	onServices,
}: {
	pair: Pair;
	from: string;
	to: string;
	onServices: (s: string[]) => void;
}) {
	const filter = useMemo(
		() => ({
			workload: pair.dst.id,
			peer: pair.peerKey,
			verdict: pair.verdict,
			from,
			to,
		}),
		[pair, from, to],
	);
	const { resource, reload } = useResource(() => listFlows(filter), [filter]);
	const [pages, setPages] = useState<FlowsPage[]>([]);
	const [more, setMore] = useState<{ loading: boolean; error?: string }>({
		loading: false,
	});
	useEffect(() => {
		if (resource.status === "ready") setPages([resource.data]);
	}, [resource]);
	const rows = useMemo(() => pages.flatMap((p) => p.flows), [pages]);
	const cursor = pages.at(-1)?.next_cursor ?? null;
	useEffect(() => {
		if (rows.length > 0) onServices([...new Set(rows.map(service))].sort());
	}, [rows, onServices]);

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

	if (resource.status === "loading") return <LoadingRow what="flows" />;
	if (resource.status === "error") {
		return (
			<ProblemNotice what="flows" error={resource.error} onRetry={reload} />
		);
	}
	return (
		<div className="flex flex-col gap-1 pb-2 pl-2" data-testid="pair-flows">
			<table className="w-full border-collapse text-[12px]">
				<caption className="sr-only">Stored windows of this pair</caption>
				<thead>
					<tr className="text-left text-[11px] text-muted-foreground">
						<th className="py-1 font-normal">service</th>
						<th className="py-1 font-normal">decision</th>
						<th className="py-1 text-right font-normal">conns</th>
						<th className="py-1 text-right font-normal">last seen</th>
					</tr>
				</thead>
				<tbody>
					{rows.map((f) => (
						<tr key={f.id} className="border-t border-border">
							<td className="py-[5px] font-mono">{service(f)}</td>
							<td className="py-[5px]">
								<span
									className={cn(
										"inline-flex items-center gap-[5px] whitespace-nowrap rounded-pill border px-1.5 py-px text-[11px]",
										verdicts[f.verdict].cls,
									)}
								>
									<span aria-hidden="true">{verdicts[f.verdict].glyph}</span>
									{verdicts[f.verdict].label}
								</span>
							</td>
							<td className="py-[5px] text-right font-mono">
								{count(f.connection_count)}
							</td>
							<td className="py-[5px] text-right font-mono text-foreground-tertiary">
								{since(f.last_seen)}
							</td>
						</tr>
					))}
				</tbody>
			</table>
			<div className="flex items-center gap-3 text-[11px]">
				<Link
					to={`/workloads/${pair.dst.id}`}
					className="text-link hover:text-link-hover"
				>
					Open {pair.dst.hostname}
				</Link>
				{cursor ? (
					<button
						type="button"
						onClick={loadMore}
						disabled={more.loading}
						className="cursor-pointer text-link hover:text-link-hover"
					>
						{more.loading ? "Loading…" : "Load more windows"}
					</button>
				) : null}
				{more.error ? (
					<span role="alert" className="text-destructive">
						{more.error}
					</span>
				) : null}
			</div>
		</div>
	);
}

function NodeDrawer({
	node: n,
	model,
	nodes,
	workloads,
	groupKey,
	onSelect,
}: {
	node: MapNode;
	model: MapModel;
	nodes: Map<string, MapNode>;
	workloads: Workload[];
	groupKey: string;
	onSelect: (s: Selection) => void;
}) {
	const eyebrow = {
		managed: "Label group",
		keyless: `Workloads without a ${groupKey} label`,
		unlabeled: "Unlabeled workloads",
		"address-group": "Address group",
		unknown: "Unknown peers",
	}[n.kind];
	const inbound = model.edges.filter((e) => e.target === n.id);
	const outbound = model.edges.filter((e) => e.source === n.id);
	const listed = new Map(workloads.map((w) => [w.id, w]));
	// Members seen only as sources outside the scope are named as their
	// pairs name them.
	const sourceNames = new Map<string, string>();
	for (const e of outbound) {
		for (const p of e.pairs) {
			if (p.src.kind === "workload" && p.src.workload_id)
				sourceNames.set(p.src.workload_id, peerName(p.src));
		}
	}
	for (const e of inbound) {
		for (const p of e.pairs) sourceNames.set(p.dst.id, p.dst.hostname);
	}
	const edgeRow = (e: MapEdge, dir: "in" | "out") => {
		const other = nodes.get(dir === "in" ? e.source : e.target);
		const v = verdicts[e.decision];
		return (
			<li key={e.id} className="border-t border-border">
				<button
					type="button"
					onClick={() => onSelect({ kind: "edge", id: e.id })}
					className="grid w-full cursor-pointer grid-cols-[1fr_auto] gap-x-2 py-[6px] text-left text-[12px]"
				>
					<span className="truncate font-mono">
						<span className="text-foreground-separator">
							{dir === "in" ? "← " : "→ "}
						</span>
						{other?.title}
					</span>
					<span className={cn("font-mono", v.text)}>
						<span aria-hidden="true">{v.glyph}</span>{" "}
						{count(e.byDecision[e.decision]?.connections ?? e.connections)}
					</span>
				</button>
			</li>
		);
	};

	return (
		<>
			<Header eyebrow={eyebrow} onClose={() => onSelect(null)}>
				<div className="font-mono text-[13px]">{n.title}</div>
				{n.selector ? (
					<div>
						<LabelChip
							k={n.selector.split("=")[0] as string}
							v={n.selector.slice(n.selector.indexOf("=") + 1)}
						/>
					</div>
				) : null}
				<div className="flex flex-col gap-0.5 font-mono text-[11px]">
					<span className="text-foreground-tertiary">{subtitle(n)}</span>
					<ModeLine n={n} />
				</div>
				{n.id === unlabeledId ? (
					<p className="text-[12px] text-foreground-tertiary">
						These workloads carry no labels, so no ruleset's scope selects them
						and no rule can name them as peers.
					</p>
				) : null}
				{n.id === keylessId ? (
					<p className="text-[12px] text-foreground-tertiary">
						These workloads carry labels, but none under {groupKey}.
					</p>
				) : null}
			</Header>
			<div className="min-h-0 flex-1 overflow-auto px-4 pb-4">
				{inbound.length > 0 ? (
					<section className="pt-3">
						<Eyebrow className="pb-1">Inbound · {inbound.length}</Eyebrow>
						<ul>{inbound.map((e) => edgeRow(e, "in"))}</ul>
					</section>
				) : null}
				{outbound.length > 0 ? (
					<section className="pt-3">
						<Eyebrow className="pb-1">Outbound · {outbound.length}</Eyebrow>
						<ul>{outbound.map((e) => edgeRow(e, "out"))}</ul>
					</section>
				) : null}
				{n.workloadIds.length > 0 ? (
					<section className="pt-3">
						<Eyebrow className="pb-1">
							Workloads · {n.workloadIds.length}
						</Eyebrow>
						<ul>
							{n.workloadIds.map((id) => {
								const w = listed.get(id);
								return (
									<li
										key={id}
										className="flex items-center gap-2 border-t border-border py-[6px] text-[12px]"
									>
										<Link
											to={`/workloads/${id}`}
											className="min-w-0 flex-1 truncate font-mono text-link hover:text-link-hover"
										>
											{w?.hostname ?? sourceNames.get(id) ?? id}
										</Link>
										{w && w.health.dropped_flow_records > 0 ? (
											<span
												className="font-mono text-[11px] text-status-degraded"
												title="This workload's agent dropped flow records"
											>
												▲ {count(w.health.dropped_flow_records)} dropped
											</span>
										) : null}
										{w ? (
											<ModePill mode={w.mode} />
										) : (
											<span className="text-[11px] text-muted-foreground">
												outside scope
											</span>
										)}
									</li>
								);
							})}
						</ul>
					</section>
				) : null}
				{n.kind === "address-group" ? (
					<section className="pt-3">
						<Eyebrow className="pb-1">CIDRs</Eyebrow>
						<ul className="font-mono text-[12px]">
							{(n.cidrs ?? []).map((c) => (
								<li key={c} className="border-t border-border py-[6px]">
									{c}
								</li>
							))}
						</ul>
					</section>
				) : null}
				{n.kind === "unknown" ? (
					<section className="pt-3">
						<Eyebrow className="pb-1">Sources</Eyebrow>
						<ul className="font-mono text-[12px]">
							{n.addresses.map((a) => (
								<li key={a} className="border-t border-border py-[6px]">
									{a}
								</li>
							))}
							{n.unrecognized.map((a) => (
								<li
									key={`u:${a}`}
									className="flex gap-2 border-t border-border py-[6px]"
								>
									<span>{a}</span>
									<span className="font-sans text-[11px] text-muted-foreground">
										stored with a peer kind this console does not recognize
									</span>
								</li>
							))}
						</ul>
					</section>
				) : null}
			</div>
		</>
	);
}
