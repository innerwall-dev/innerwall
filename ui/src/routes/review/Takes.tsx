import { VerdictPill, verdicts } from "@/components/fleet/status";
import type { RangeKey } from "@/components/RangeControl";
import { count, since } from "@/lib/format";
import { cn } from "@/lib/utils";
import {
	barWidth,
	cellKey,
	type Filter,
	matrixOf,
	type PeerGroup,
	type ReviewRow,
	recencyOf,
} from "./model";

// The review's three takes over the same rows: a table grouped by peer,
// a peer-by-service matrix, and the rows bucketed by when each was last
// seen. Every take selects a row the same way, and the selection lives
// in the address, so switching takes keeps it.

interface TakeProps {
	rows: ReviewRow[];
	range: RangeKey;
	selected: string | null;
	onSelect: (id: string | null) => void;
}

const kindLabel: Record<PeerGroup["kind"], string> = {
	workloads: "labels",
	unlabeled: "unlabeled",
	group: "address group",
	address: "unknown",
	unrecognized: "unrecognized",
};

// PeerKind is the tag before a peer: solid for workloads the scope's
// policy can name by label, dashed for peers that are not workloads.
export function PeerKind({ peer }: { peer: PeerGroup }) {
	const managed = peer.kind === "workloads" || peer.kind === "unlabeled";
	return (
		<span
			className={cn(
				"shrink-0 rounded-[3px] border border-strong px-[5px] py-px text-[10px] uppercase tracking-[0.04em] whitespace-nowrap text-icon-default",
				managed ? "border-solid" : "border-dashed",
			)}
		>
			{kindLabel[peer.kind]}
		</span>
	);
}

// peerNote is what a row says after its peer: how many workloads a label
// group folds, an address group's first CIDR, or that an address is known
// to nothing.
export function peerNote(peer: PeerGroup, members: number): string {
	switch (peer.kind) {
		case "workloads":
		case "unlabeled": {
			const n = `${members} ${members === 1 ? "workload" : "workloads"}`;
			return peer.rest ? `${n}, ${peer.rest}` : n;
		}
		case "group": {
			const c = peer.cidrs ?? [];
			return c.length === 0
				? ""
				: c.length === 1
					? c[0]
					: `${c[0]} (+${c.length - 1})`;
		}
		case "address":
			return "not managed, in no address group";
		default:
			return "a stored peer this console does not recognize";
	}
}

// workloadsText is a row's workload count, marked when it is a lower
// bound.
export function workloadsText(r: ReviewRow): string {
	return `${count(r.workloads)}${r.workloadsExact ? "" : "+"}`;
}

const barFill = {
	would_block: "bg-flow-would-block",
	allowed: "bg-flow-allowed",
} as const;

const cellFill = {
	would_block: "bg-status-warn-bg",
	allowed: "bg-status-ok-bg",
} as const;

export function Grouped({
	rows,
	all,
	filter,
	simulating,
	range,
	selected,
	onSelect,
}: TakeProps & { all: ReviewRow[]; filter: Filter; simulating: number }) {
	if (rows.length === 0) {
		return (
			<NoRows filter={filter} all={all} simulating={simulating} range={range} />
		);
	}
	const busiest = Math.max(...all.map((r) => r.connections));
	const th =
		"pb-2 pt-2.5 font-medium text-[11px] uppercase tracking-[0.05em] text-tertiary";
	const td = "border-t border-default py-[9px]";
	return (
		<table className="w-full border-collapse text-[12.5px]">
			<caption className="sr-only">
				Peer and service pairs, grouped by peer
			</caption>
			<thead>
				<tr className="text-left">
					<th className={cn(th, "w-[112px] pr-2")}>Decision</th>
					<th className={cn(th, "px-2")}>Peer</th>
					<th className={cn(th, "px-2")}>Service</th>
					<th className={cn(th, "px-2")}>Rule</th>
					<th className={cn(th, "px-2 text-right")}>Workloads</th>
					<th className={cn(th, "px-2 text-right")}>Connections</th>
					<th className={cn(th, "pl-2 text-right")}>Last seen</th>
				</tr>
			</thead>
			<tbody>
				{rows.map((r) => {
					const on = r.id === selected;
					return (
						// The whole row selects for a pointer; the peer's button
						// is the keyboard's way in.
						<tr
							key={r.id}
							onClick={() => onSelect(on ? null : r.id)}
							className={cn(
								"cursor-pointer hover:bg-selection-bg",
								on && "bg-selection-bg",
							)}
						>
							<td className={cn(td, "pr-2")}>
								<VerdictPill verdict={r.verdict} />
							</td>
							<td className={cn(td, "px-2")}>
								<div className="flex min-w-0 items-center gap-1.5">
									<PeerKind peer={r.peer} />
									<button
										type="button"
										aria-pressed={on}
										onClick={(ev) => {
											ev.stopPropagation();
											onSelect(on ? null : r.id);
										}}
										title={r.peer.full ?? r.peer.title}
										className="cursor-pointer text-left font-mono text-primary"
									>
										{r.peer.title}
									</button>
									<span className="max-w-[100px] text-[11px] text-tertiary">
										{peerNote(r.peer, r.members.length)}
									</span>
								</div>
							</td>
							<td className={cn(td, "px-2 font-mono")}>{r.service}</td>
							<td className={cn(td, "px-2 text-secondary")}>
								{r.verdict === "would_block"
									? "no rule matched"
									: "matched a rule"}
							</td>
							<td
								className={cn(td, "px-2 text-right font-mono")}
								title={
									r.workloadsExact
										? undefined
										: "At least this many: open the row for the exact count"
								}
							>
								{workloadsText(r)}
							</td>
							<td className={cn(td, "px-2")}>
								<div className="flex items-center justify-end gap-2">
									<div
										className="h-1 w-[72px] overflow-hidden rounded-[2px] bg-active"
										aria-hidden="true"
									>
										<div
											className={cn("h-full", barFill[r.verdict])}
											style={{
												width: `${barWidth(r.connections, busiest)}%`,
											}}
										/>
									</div>
									<span className="min-w-[52px] text-right font-mono">
										{count(r.connections)}
									</span>
								</div>
							</td>
							<td
								className={cn(td, "pl-2 text-right font-mono text-secondary")}
							>
								{since(r.lastSeen)}
							</td>
						</tr>
					);
				})}
			</tbody>
		</table>
	);
}

// NoRows is a take with nothing under its filter: the design's clean
// state when no traffic would be dropped, and plain statements otherwise.
function NoRows({
	filter,
	all,
	simulating,
	range,
}: {
	filter: Filter;
	all: ReviewRow[];
	simulating: number;
	range: RangeKey;
}) {
	if (filter === "would_block") {
		return (
			<div className="flex flex-col items-center gap-2 py-10 text-secondary">
				<div className="text-[28px] text-flow-allowed" aria-hidden="true">
					✓
				</div>
				<div className="font-semibold text-primary">No would-block flows</div>
				<p className="max-w-[440px] text-center text-[12px]">
					Every inbound connection observed on the {simulating} simulating{" "}
					{simulating === 1 ? "workload" : "workloads"} in the last {range}{" "}
					matched an enabled rule. Enforcing now changes nothing for the traffic
					seen so far.
				</p>
			</div>
		);
	}
	return (
		<p className="py-10 text-center text-[12px] text-secondary">
			{all.length === 0
				? `No inbound flows into this scope's simulating workloads were stored in the last ${range}. Widen the range, or give the scope time in simulation.`
				: `No allowed flows in the last ${range}.`}
		</p>
	);
}

export function MatrixTake({ rows, range, selected, onSelect }: TakeProps) {
	if (rows.length === 0) {
		return (
			<p className="py-10 text-center text-[12px] text-secondary">
				No peer and service pairs in the last {range}.
			</p>
		);
	}
	const m = matrixOf(rows);
	const members = (p: PeerGroup) =>
		new Set(
			rows
				.filter((r) => r.peer.id === p.id)
				.flatMap((r) => r.members.map((mm) => mm.key)),
		).size;
	return (
		<>
			<table className="mt-4 w-full table-fixed border-collapse border border-subtle text-[12px]">
				<caption className="sr-only">Peer by service</caption>
				<colgroup>
					<col className="w-[260px]" />
					{m.services.map((s) => (
						<col key={s} className="min-w-[120px]" />
					))}
				</colgroup>
				<thead>
					<tr>
						<th className="border border-subtle bg-app px-3 py-2.5 text-left text-[11px] font-normal uppercase tracking-[0.05em] text-tertiary">
							Peer ↓ · Service →
						</th>
						{m.services.map((s) => (
							<th
								key={s}
								scope="col"
								className="border border-subtle bg-app px-3 py-2.5 text-left font-mono font-normal text-secondary"
							>
								{s}
							</th>
						))}
					</tr>
				</thead>
				<tbody>
					{m.peers.map((p) => (
						<tr key={p.id}>
							<th
								scope="row"
								className="border border-subtle bg-subtle px-3 py-2.5 text-left font-normal"
							>
								<div className="flex min-w-0 flex-col gap-0.5">
									<span className="text-[10px] uppercase tracking-[0.04em] text-tertiary">
										{kindLabel[p.kind]}
									</span>
									<span
										className="truncate font-mono text-primary"
										title={p.full ?? p.title}
									>
										{p.title}
									</span>
									<span className="font-mono text-[11px] text-tertiary">
										{peerNote(p, members(p))}
									</span>
								</div>
							</th>
							{m.services.map((s) => {
								const cell = m.cells.get(cellKey(p.id, s));
								if (!cell) {
									return <td key={s} className="border border-subtle bg-app" />;
								}
								const r = cell.row;
								const v = verdicts[r.verdict];
								const on = selected === r.id;
								return (
									<td key={s} className="border border-subtle p-0">
										<button
											type="button"
											aria-pressed={on}
											onClick={() => onSelect(on ? null : r.id)}
											title={[r, ...cell.others]
												.map(
													(x) =>
														`${verdicts[x.verdict].label}: ${count(x.connections)} connections`,
												)
												.join("; ")}
											className={cn(
												"flex w-full cursor-pointer flex-col gap-0.5 px-3 py-2.5 text-left",
												on ? "bg-selection-bg" : cellFill[r.verdict],
											)}
										>
											<span className={cn("font-mono text-[13px]", v.text)}>
												<span aria-hidden="true">{v.glyph}</span>{" "}
												{count(r.connections)}
												<span className="sr-only"> {v.label}</span>
											</span>
											<span className="font-mono text-[11px] text-tertiary">
												{v.label} · {workloadsText(r)} wl · {since(r.lastSeen)}
												{cell.others.length > 0
													? ` · +${cell.others.map((x) => verdicts[x.verdict].label).join(", ")}`
													: ""}
											</span>
										</button>
									</td>
								);
							})}
						</tr>
					))}
				</tbody>
			</table>
			<p className="mt-2.5 text-[11px] text-tertiary">
				Each cell: decision glyph and connection count; where a pair received
				both decisions, the cell shows what enforcing would drop. Empty cells:
				no traffic observed for that pair.
			</p>
		</>
	);
}

export function Recency({ rows, range, selected, onSelect }: TakeProps) {
	if (rows.length === 0) {
		return (
			<p className="py-10 text-center text-[12px] text-secondary">
				No peer and service pairs in the last {range}.
			</p>
		);
	}
	const groups = recencyOf(rows, Date.now());
	return (
		<>
			<ol className="mt-4 flex flex-col">
				{groups.map((g) => (
					<li
						key={g.bucket}
						className="grid grid-cols-[110px_1fr] gap-4 border-t border-default py-3"
					>
						<span className="font-mono text-[12px] text-secondary">
							{g.bucket}
						</span>
						{g.rows.length === 0 ? (
							<span className="text-[12px] text-disabled">—</span>
						) : (
							<ul className="flex flex-col gap-1.5">
								{g.rows.map((r) => {
									const on = selected === r.id;
									return (
										<li key={r.id}>
											<button
												type="button"
												aria-pressed={on}
												onClick={() => onSelect(on ? null : r.id)}
												className={cn(
													"-mx-1.5 flex cursor-pointer items-center gap-2.5 rounded-md px-1.5 py-0.5 text-left",
													on && "bg-selection-bg",
												)}
											>
												<VerdictPill verdict={r.verdict} />
												<span className="font-mono">{r.peer.title}</span>
												<span className="text-disabled" aria-hidden="true">
													→
												</span>
												<span className="sr-only">on</span>
												<span className="font-mono">{r.service}</span>
												<span className="text-[11px] text-tertiary">
													{count(r.connections)} conns · {workloadsText(r)}{" "}
													workloads · {since(r.lastSeen)}
												</span>
											</button>
										</li>
									);
								})}
							</ul>
						)}
					</li>
				))}
			</ol>
			<p className="mt-2.5 text-[11px] text-tertiary">
				Buckets use each pair's last-seen instant. Fresh would-block traffic at
				the top is the strongest sign that enforcing would break something live.
			</p>
		</>
	);
}
