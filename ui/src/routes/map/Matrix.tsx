import { type ReactNode, useEffect, useRef } from "react";
import { verdicts } from "@/components/fleet/status";
import { short } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Layout } from "./layout";
import type { MapEdge, MapModel, MapNode, Selection } from "./model";

// The matrix take: sources down, destination groups across, one cell per
// edge carrying its decision glyph and connections. Rows and columns
// follow the graph's reading order, so the two takes read alike, and
// selection is shared: a cell is an edge, a header is a node.

const unmanaged = new Set(["address-group", "unknown"]);

const cellTone = {
	observed: "bg-status-observed-bg text-status-observed",
	allowed: "bg-status-allowed-bg text-status-allowed",
	would_block: "bg-status-would-block-bg text-status-would-block",
	blocked: "bg-status-blocked-bg text-status-blocked",
} as const;

function rowEdge(n: MapNode): string {
	if (unmanaged.has(n.kind))
		return "border-l-2 border-dashed border-l-[var(--viz-node-unmanaged-stroke)]";
	if (n.kind === "unlabeled")
		return "border-l-2 border-dotted border-l-[var(--viz-node-unlabeled-stroke)]";
	return "border-l-2 border-solid border-l-[var(--viz-node-managed-stroke)]";
}

export function Matrix({
	model,
	layout,
	selection,
	onSelect,
	range,
}: {
	model: MapModel;
	layout: Layout;
	selection: Selection;
	onSelect: (s: Selection) => void;
	range: ReactNode;
}) {
	const byId = new Map(model.nodes.map((n) => [n.id, n]));
	const ordered = layout.order
		.map((id) => byId.get(id))
		.filter((n): n is MapNode => n !== undefined);
	const rows = ordered.filter((n) =>
		model.edges.some((e) => e.source === n.id),
	);
	const cols = ordered.filter((n) => !unmanaged.has(n.kind));
	const edges = new Map<string, MapEdge>(model.edges.map((e) => [e.id, e]));
	const nodeSelected = (id: string) =>
		selection?.kind === "node" && selection.id === id;
	const pickNode = (id: string) =>
		onSelect(nodeSelected(id) ? null : { kind: "node", id });

	// A selection made in the graph, or carried in the address, may name
	// a cell outside the scrolled view; bring it into view.
	const box = useRef<HTMLDivElement>(null);
	const selected = selection ? `${selection.kind}:${selection.id}` : null;
	useEffect(() => {
		if (!selected) return;
		const el = box.current?.querySelector<HTMLElement>('[aria-pressed="true"]');
		el?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
	}, [selected]);

	return (
		<div className="flex min-h-0 flex-col px-6 py-5" data-testid="flow-matrix">
			<div className="mb-3">{range}</div>
			<div ref={box} className="min-h-0 overflow-auto">
				{/* The grid is the table's own spacing over the grid color, so
			    each 1px gap is a grid line; a filled cell's tint sits on that
			    color, as the design draws it, and an empty cell is opaque. */}
				<table
					className="w-full border-separate border-spacing-px border border-[var(--viz-matrix-grid)] bg-[var(--viz-matrix-grid)]"
					style={{ minWidth: 150 + cols.length * 84 }}
				>
					<caption className="sr-only">
						Connections by source and destination group
					</caption>
					<colgroup>
						<col style={{ width: 150 }} />
						{cols.map((c) => (
							<col key={c.id} style={{ minWidth: 84 }} />
						))}
					</colgroup>
					<thead>
						<tr>
							<th
								scope="col"
								className="sticky left-0 bg-[var(--viz-matrix-empty)] px-2.5 py-2 text-left align-top text-[11px] font-normal uppercase tracking-[0.05em] text-muted-foreground"
							>
								source ↓ · destination →
							</th>
							{cols.map((c) => (
								<th
									key={c.id}
									scope="col"
									className={cn(
										"bg-[var(--viz-matrix-empty)] p-0 align-top font-normal",
										nodeSelected(c.id) && "bg-[var(--viz-matrix-selected)]",
									)}
								>
									<button
										type="button"
										aria-pressed={nodeSelected(c.id)}
										onClick={() => pickNode(c.id)}
										className="w-full cursor-pointer px-1.5 py-2 text-center text-[11px] leading-[1.2] break-words text-foreground-secondary"
									>
										{c.title}
									</button>
								</th>
							))}
						</tr>
					</thead>
					<tbody>
						{rows.map((r) => (
							<tr key={r.id}>
								<th
									scope="row"
									className={cn(
										"sticky left-0 bg-surface-sidebar p-0 text-left font-normal",
										rowEdge(r),
										nodeSelected(r.id) && "bg-[var(--viz-matrix-selected)]",
									)}
								>
									<button
										type="button"
										aria-pressed={nodeSelected(r.id)}
										onClick={() => pickNode(r.id)}
										className="w-full cursor-pointer truncate px-2.5 py-1.5 text-left font-mono text-[11.5px] whitespace-nowrap text-foreground-secondary"
									>
										{r.title}
									</button>
								</th>
								{cols.map((c) => {
									const e = edges.get(`${r.id}>${c.id}`);
									if (!e) {
										return (
											<td key={c.id} className="bg-[var(--viz-matrix-empty)]" />
										);
									}
									const selected =
										selection?.kind === "edge" && selection.id === e.id;
									const v = verdicts[e.decision];
									const shown =
										e.byDecision[e.decision]?.connections ?? e.connections;
									return (
										<td key={c.id} className="p-0">
											<button
												type="button"
												aria-pressed={selected}
												aria-label={`${r.title} to ${c.title}: ${v.label} ${shown} connections`}
												data-edge={e.id}
												onClick={() =>
													onSelect(selected ? null : { kind: "edge", id: e.id })
												}
												className={cn(
													"size-full cursor-pointer px-1.5 py-1.5 text-center font-mono text-[11.5px] whitespace-nowrap",
													cellTone[e.decision],
													selected && "bg-[var(--viz-matrix-selected)]",
												)}
											>
												<span aria-hidden="true">{v.glyph}</span> {short(shown)}
											</button>
										</td>
									);
								})}
							</tr>
						))}
					</tbody>
				</table>
			</div>
			<p className="mt-2.5 text-[11px] text-muted-foreground">
				Glyph encodes decision (○ observed · ✓ allowed · ◆ would block · ✕
				blocked); number is connections under that decision, the one that takes
				precedence among the cell's pairs. Row headers with dashed border are
				unmanaged sources.
			</p>
		</div>
	);
}
