import { VerdictPill, verdicts } from "@/components/fleet/status";
import type { RangeKey } from "@/components/RangeControl";
import { StatusGlyph } from "@/components/StatusGlyph";
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
				"inline-flex h-5 shrink-0 items-center whitespace-nowrap rounded-sm border border-strong px-1.5 type-label text-secondary",
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

// The connection-volume bar: the track is the pressed surface, the fill
// the decision's flow tone.
const barFill = {
	would_block: "bg-flow-would-block",
	allowed: "bg-flow-allowed",
} as const;

const cellFill = {
	would_block: "bg-status-warn-bg",
	allowed: "bg-status-ok-bg",
} as const;

// The table pattern every take shares: a bordered panel, a subtle
// header band, dense rows on subtle hairlines.
const panel = "overflow-x-auto rounded-lg border border-default";
const th =
	"h-row-header whitespace-nowrap border-b border-default bg-subtle px-2.5 text-left type-label text-tertiary first:pl-3 last:pr-3";
const td = "border-b border-subtle px-2.5 py-2 type-ui first:pl-3 last:pr-3";

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
	return (
		<div className={panel}>
			<table className="w-full border-collapse">
				<caption className="sr-only">
					Peer and service pairs, grouped by peer
				</caption>
				<thead>
					<tr>
						<th className={th}>Decision</th>
						<th className={cn(th, "min-w-[200px]")}>Peer</th>
						<th className={th}>Service</th>
						<th className={th}>Rule</th>
						<th className={cn(th, "text-right")}>Workloads</th>
						<th className={cn(th, "text-right")}>Connections</th>
						<th className={cn(th, "text-right")}>Last seen</th>
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
									"h-row-dense cursor-pointer [&:last-child>td]:border-b-0",
									on ? "bg-selection-bg" : "hover:bg-hover",
								)}
							>
								<td className={td}>
									<VerdictPill verdict={r.verdict} />
								</td>
								<td className={td}>
									{/* The kind tag reads first but sits on the second line,
									    beside the note, so the identifier keeps the width. */}
									<div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-x-2 gap-y-1">
										<span className="col-start-1 row-start-2 flex">
											<PeerKind peer={r.peer} />
										</span>
										<button
											type="button"
											aria-pressed={on}
											onClick={(ev) => {
												ev.stopPropagation();
												onSelect(on ? null : r.id);
											}}
											title={r.peer.full ?? r.peer.title}
											className="col-span-2 col-start-1 row-start-1 cursor-pointer justify-self-start whitespace-nowrap text-left type-mono-ui font-medium text-primary"
										>
											{r.peer.title}
										</button>
										<span className="col-start-2 row-start-2 type-caption text-tertiary">
											{peerNote(r.peer, r.members.length)}
										</span>
									</div>
								</td>
								<td className={cn(td, "type-mono-ui whitespace-nowrap")}>
									{r.service}
								</td>
								<td className={cn(td, "text-secondary")}>
									{r.verdict === "would_block"
										? "no rule matched"
										: "matched a rule"}
								</td>
								<td
									className={cn(td, "text-right type-mono-ui")}
									title={
										r.workloadsExact
											? undefined
											: "At least this many: open the row for the exact count"
									}
								>
									{workloadsText(r)}
								</td>
								<td className={td}>
									<div className="flex items-center justify-end gap-2">
										<div
											className="h-1 w-12 shrink-0 overflow-hidden rounded-full bg-active"
											aria-hidden="true"
										>
											<div
												className={cn(
													"h-full rounded-full",
													barFill[r.verdict],
												)}
												style={{
													width: `${barWidth(r.connections, busiest)}%`,
												}}
											/>
										</div>
										<span className="min-w-[48px] text-right type-mono-ui">
											{count(r.connections)}
										</span>
									</div>
								</td>
								<td
									className={cn(
										td,
										"whitespace-nowrap text-right type-mono-ui text-secondary",
									)}
								>
									{since(r.lastSeen)}
								</td>
							</tr>
						);
					})}
				</tbody>
			</table>
		</div>
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
				<StatusGlyph status="allowed" size="lg" className="size-6" />
				<div className="type-title-section text-primary">
					No would-block flows
				</div>
				<p className="max-w-[440px] text-center type-body">
					Every inbound connection observed on the {simulating} simulating{" "}
					{simulating === 1 ? "workload" : "workloads"} in the last {range}{" "}
					matched an enabled rule. Enforcing now changes nothing for the traffic
					seen so far.
				</p>
			</div>
		);
	}
	return (
		<p className="py-10 text-center type-body text-secondary">
			{all.length === 0
				? `No inbound flows into this scope's simulating workloads were stored in the last ${range}. Widen the range, or give the scope time in simulation.`
				: `No allowed flows in the last ${range}.`}
		</p>
	);
}

export function MatrixTake({ rows, range, selected, onSelect }: TakeProps) {
	if (rows.length === 0) {
		return (
			<p className="py-10 text-center type-body text-secondary">
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
	// border-hidden suppresses the cells' outer hairlines, so the panel's
	// own border is the only frame.
	const cellBorder = "border border-subtle";
	return (
		<>
			<div className={panel}>
				<table className="w-full table-fixed border-collapse border-hidden">
					<caption className="sr-only">Peer by service</caption>
					<colgroup>
						<col className="w-[260px]" />
						{m.services.map((s) => (
							<col key={s} className="min-w-[120px]" />
						))}
					</colgroup>
					<thead>
						<tr>
							<th
								className={cn(
									cellBorder,
									"h-row-header bg-subtle px-3 text-left type-label text-tertiary",
								)}
							>
								Peer ↓ · Service →
							</th>
							{m.services.map((s) => (
								<th
									key={s}
									scope="col"
									className={cn(
										cellBorder,
										"h-row-header bg-subtle px-3 text-left type-mono-sm font-medium text-secondary",
									)}
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
									className={cn(
										cellBorder,
										"bg-subtle px-3 py-2 text-left font-normal",
									)}
								>
									<div className="flex min-w-0 flex-col gap-0.5">
										<span className="type-label text-tertiary">
											{kindLabel[p.kind]}
										</span>
										<span
											className="truncate type-mono-ui font-medium text-primary"
											title={p.full ?? p.title}
										>
											{p.title}
										</span>
										<span className="type-caption text-tertiary">
											{peerNote(p, members(p))}
										</span>
									</div>
								</th>
								{m.services.map((s) => {
									const cell = m.cells.get(cellKey(p.id, s));
									if (!cell) {
										return <td key={s} className={cn(cellBorder, "bg-app")} />;
									}
									const r = cell.row;
									const v = verdicts[r.verdict];
									const on = selected === r.id;
									return (
										<td key={s} className={cn(cellBorder, "p-0")}>
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
													"flex w-full cursor-pointer flex-col gap-0.5 px-3 py-2 text-left",
													on ? "bg-selection-bg" : cellFill[r.verdict],
												)}
											>
												<span className="inline-flex items-center gap-1.5 type-mono-ui text-primary">
													<StatusGlyph status={v.status} size="md" />
													{count(r.connections)}
													<span className="sr-only"> {v.label}</span>
												</span>
												<span className="type-mono-xs text-secondary">
													{v.label} · {workloadsText(r)} wl ·{" "}
													{since(r.lastSeen)}
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
			</div>
			<p className="mt-2.5 type-caption text-tertiary">
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
			<p className="py-10 text-center type-body text-secondary">
				No peer and service pairs in the last {range}.
			</p>
		);
	}
	const groups = recencyOf(rows, Date.now());
	return (
		<>
			<ol className={cn(panel, "flex flex-col overflow-hidden")}>
				{groups.map((g) => (
					<li
						key={g.bucket}
						className="grid grid-cols-[140px_1fr] border-b border-default last:border-b-0"
					>
						<span className="border-r border-subtle bg-subtle px-3 py-2.5 type-label text-secondary">
							{g.bucket}
						</span>
						{g.rows.length === 0 ? (
							<span className="flex h-row-dense items-center px-3 type-ui text-disabled">
								—
							</span>
						) : (
							<ul className="flex min-w-0 flex-col">
								{g.rows.map((r) => {
									const on = selected === r.id;
									return (
										<li
											key={r.id}
											className="border-b border-subtle last:border-b-0"
										>
											<button
												type="button"
												aria-pressed={on}
												onClick={() => onSelect(on ? null : r.id)}
												className={cn(
													"flex min-h-row-dense w-full cursor-pointer flex-wrap items-center gap-x-2.5 gap-y-0.5 px-3 py-2 text-left type-ui",
													on ? "bg-selection-bg" : "hover:bg-hover",
												)}
											>
												<span className="w-[104px] shrink-0">
													<VerdictPill verdict={r.verdict} />
												</span>
												<span className="type-mono-ui font-medium text-primary">
													{r.peer.title}
												</span>
												<span className="text-tertiary" aria-hidden="true">
													→
												</span>
												<span className="sr-only">on</span>
												<span className="type-mono-ui">{r.service}</span>
												<span className="ml-auto type-caption text-tertiary">
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
			<p className="mt-2.5 type-caption text-tertiary">
				Buckets use each pair's last-seen instant. Fresh would-block traffic at
				the top is the strongest sign that enforcing would break something live.
			</p>
		</>
	);
}
