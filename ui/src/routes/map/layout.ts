import dagre, { type graphlib } from "@dagrejs/dagre";
import { short } from "@/lib/format";
import type { MapEdge, MapModel } from "./model";

// The node box: 176 wide (the design's) by 64, for the third line the
// console draws, at the 6px radius.
export const nodeWidth = 176;
export const nodeHeight = 64;

// The label chip: 18 high, and as wide as its glyph and count.
export const chipHeight = 18;

// chipWidth is a label chip's width in mono-xs (11px, about 6.6px a
// figure): the 12px glyph, a 4px gap, and the count, with the chip's
// padding and border.
export function chipWidth(e: MapEdge): number {
	const n = e.byDecision[e.decision]?.connections ?? e.connections;
	return Math.ceil(12 + 4 + short(n).length * 6.6 + 10);
}

const root = "\u0000root";

// A Route is the way the layout found for one edge, from its source to
// its target: the places the edge passes between them, each with the
// half-width of the column it crosses there, and the slot kept for its
// label. Every place lies in a column's free space, clear of the nodes
// and of every other edge's place; between columns there is nothing.
export interface Route {
	points: { x: number; y: number; half: number }[];
	label: { x: number; y: number };
	// back is an edge that runs right to left, against the layers.
	back: boolean;
}

export interface Layout {
	positions: Map<string, { x: number; y: number }>;
	// order is the nodes in reading order, column by column and top to
	// bottom within a column; the matrix lists its rows and columns in
	// it, so the two takes read in the same order.
	order: string[];
	// routes are the edges' ways by edge id, or null when a layer was
	// wrapped into several columns, which moves the nodes off the places
	// the routes were found between.
	routes: Map<string, Route> | null;
	// bounds hold every node, route, and label slot.
	bounds: { x: number; y: number; width: number; height: number };
}

// layoutModel places the nodes in layers left to right, sources before
// what they reach, with crossings reduced within each layer. Unmanaged
// peers only ever send, so they land in the first layer, as the design
// draws them.
//
// Each edge carries its label chip's size into the layout, so a slot is
// kept for every chip, in the edge's own lane, where no node or other
// chip can be; and an edge that spans layers is given a lane through
// each layer it crosses, between that layer's nodes. The map draws every
// edge along its lane, so no edge passes under a node.
export function layoutModel(model: MapModel): Layout {
	const g = new dagre.graphlib.Graph({ multigraph: false });
	g.setGraph({
		rankdir: "LR",
		nodesep: 28,
		edgesep: 14,
		// The layout halves this around the chips' column between two
		// layers.
		ranksep: 130,
		marginx: 12,
		marginy: 12,
	});
	g.setDefaultEdgeLabel(() => ({}));
	const ids = model.nodes.map((n) => n.id).sort();
	for (const id of ids) {
		g.setNode(id, { width: nodeWidth, height: nodeHeight });
	}
	const edges = [...model.edges]
		.filter((e) => e.source !== e.target)
		.sort((a, b) => a.id.localeCompare(b.id));
	for (const e of edges) {
		g.setEdge(e.source, e.target, {
			weight: Math.max(1, Math.round(Math.log10(e.connections + 1))),
			width: chipWidth(e),
			height: chipHeight,
			labelpos: "c",
		});
	}
	// Unmanaged peers hang off one invisible root with a heavy, short
	// edge, so the ranking pulls them into the first layer: they only
	// ever send, and they read as where traffic enters. Managed sources
	// stay where the ranking puts them, beside what they reach, which
	// keeps the layers balanced. The root is left out of the result.
	g.setNode(root, { width: 1, height: 1 });
	for (const n of model.nodes) {
		if (n.kind === "address-group" || n.kind === "unknown")
			g.setEdge(root, n.id, { weight: 100, minlen: 1 });
	}
	dagre.layout(g);

	const ranked = new Map<number, { id: string; y: number }[]>();
	for (const id of ids) {
		const n = g.node(id);
		const x = Math.round(n.x ?? 0);
		ranked.set(x, [...(ranked.get(x) ?? []), { id, y: n.y ?? 0 }]);
	}
	const wrapped = wrapLayers(ranked, ids.length);
	const positions =
		wrapped ??
		new Map(
			ids.map((id) => {
				const n = g.node(id);
				return [
					id,
					{
						x: Math.round(n.x - nodeWidth / 2),
						y: Math.round(n.y - nodeHeight / 2),
					},
				];
			}),
		);
	const routes = wrapped ? null : routesOf(g, edges);
	const order = [...ids].sort((a, b) => {
		const pa = positions.get(a) ?? { x: 0, y: 0 };
		const pb = positions.get(b) ?? { x: 0, y: 0 };
		return pa.x - pb.x || pa.y - pb.y || a.localeCompare(b);
	});
	return { positions, order, routes, bounds: boundsOf(positions, routes) };
}

// routesOf reads each edge's way from the layout. The layout keeps a
// place for the edge in every column it crosses (the columns of nodes
// and the columns of chips between them); each place is widened to its
// column, so the edge crosses a column level and bends only in the free
// space between columns.
function routesOf(g: graphlib.Graph, edges: MapEdge[]): Map<string, Route> {
	const columns = new Map<number, number>();
	const widen = (x: number, half: number) => {
		const k = Math.round(x);
		columns.set(k, Math.max(columns.get(k) ?? 0, half));
	};
	for (const id of g.nodes()) {
		if (id === root) continue;
		widen(g.node(id).x, nodeWidth / 2);
	}
	const found = edges.map((e) => ({
		e,
		// The edge as laid out: its places and its label's centre.
		d: g.edge(e.source, e.target) as {
			points: { x: number; y: number }[];
			x: number;
			y: number;
		},
	}));
	for (const { e, d } of found) widen(d.x, chipWidth(e) / 2);

	const routes = new Map<string, Route>();
	for (const { e, d } of found) {
		// The layout's points start and end on the nodes' outlines; the
		// map attaches edges itself, so only the places between are kept.
		const inner = d.points.slice(1, -1);
		routes.set(e.id, {
			points: inner.map((p) => ({
				x: p.x,
				y: p.y,
				half: columns.get(Math.round(p.x)) ?? 0,
			})),
			label: { x: d.x, y: d.y },
			back: g.node(e.target).x < g.node(e.source).x,
		});
	}
	return routes;
}

function boundsOf(
	positions: Map<string, { x: number; y: number }>,
	routes: Map<string, Route> | null,
): Layout["bounds"] {
	let minX = Number.POSITIVE_INFINITY;
	let minY = Number.POSITIVE_INFINITY;
	let maxX = Number.NEGATIVE_INFINITY;
	let maxY = Number.NEGATIVE_INFINITY;
	const take = (x0: number, y0: number, x1: number, y1: number) => {
		minX = Math.min(minX, x0);
		minY = Math.min(minY, y0);
		maxX = Math.max(maxX, x1);
		maxY = Math.max(maxY, y1);
	};
	for (const p of positions.values())
		take(p.x, p.y, p.x + nodeWidth, p.y + nodeHeight);
	for (const r of routes?.values() ?? []) {
		for (const p of r.points) take(p.x - p.half, p.y, p.x + p.half, p.y);
		take(
			r.label.x,
			r.label.y - chipHeight / 2,
			r.label.x,
			r.label.y + chipHeight / 2,
		);
	}
	if (minX > maxX) return { x: 0, y: 0, width: 0, height: 0 };
	return { x: minX, y: minY, width: maxX - minX, height: maxY - minY };
}

const columnGap = 40;
const rowGap = 28;
const rankGap = 150;

// wrapLayers places each layer as a column, and a layer taller than the
// map is wide into several columns side by side, in its own order, so a
// group that reaches hundreds of others (one layer of hundreds of nodes)
// still fits a canvas at a legible zoom rather than as one sliver. The
// limit grows with the map, keeping the whole roughly as wide as tall.
// It answers null when no layer is past the limit: the layout's own
// places stand.
export function wrapLayers(
	ranked: Map<number, { id: string; y: number }[]>,
	total: number,
): Map<string, { x: number; y: number }> | null {
	const cap = Math.max(10, Math.ceil(Math.sqrt(total) * 1.2));
	const step = nodeHeight + rowGap;
	const layers = [...ranked.keys()]
		.sort((a, b) => a - b)
		.map((rank) => (ranked.get(rank) ?? []).sort((a, b) => a.y - b.y));
	if (!layers.some((l) => l.length > cap)) return null;
	const positions = new Map<string, { x: number; y: number }>();
	// Once a layer wraps, the layers beside it are restacked in their own
	// order and centred on its height: their places from the ranking were
	// spread along the unwrapped column and would sit far above or below.
	const height = Math.max(...layers.map((l) => Math.min(l.length, cap))) * step;
	let x = 0;
	for (const layer of layers) {
		if (layer.length <= cap) {
			const top = (height - layer.length * step) / 2;
			layer.forEach((n, i) => {
				positions.set(n.id, { x, y: Math.round(top + i * step) });
			});
			x += nodeWidth + rankGap;
			continue;
		}
		const columns = Math.ceil(layer.length / cap);
		layer.forEach((n, i) => {
			positions.set(n.id, {
				x: x + Math.floor(i / cap) * (nodeWidth + columnGap),
				y: (i % cap) * step,
			});
		});
		x += columns * (nodeWidth + columnGap) - columnGap + rankGap;
	}
	return positions;
}
