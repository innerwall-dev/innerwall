import dagre from "@dagrejs/dagre";
import type { MapModel } from "./model";

// The node box, in the design's geometry: 170 by 64 with a 6px radius.
export const nodeWidth = 170;
export const nodeHeight = 64;

const root = "\u0000root";

export interface Layout {
	positions: Map<string, { x: number; y: number }>;
	// order is the nodes in reading order, column by column and top to
	// bottom within a column; the matrix lists its rows and columns in
	// it, so the two takes read in the same order.
	order: string[];
}

// layoutModel places the nodes in layers left to right, sources before
// what they reach, with crossings reduced within each layer. Unmanaged
// peers only ever send, so they land in the first layer, as the design
// draws them.
export function layoutModel(model: MapModel): Layout {
	const g = new dagre.graphlib.Graph({ multigraph: false });
	g.setGraph({
		rankdir: "LR",
		nodesep: 28,
		ranksep: 150,
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
	const positions = wrapLayers(ranked, ids.length);
	const order = [...ids].sort((a, b) => {
		const pa = positions.get(a) ?? { x: 0, y: 0 };
		const pb = positions.get(b) ?? { x: 0, y: 0 };
		return pa.x - pb.x || pa.y - pb.y || a.localeCompare(b);
	});
	return { positions, order };
}

const columnGap = 40;
const rowGap = 28;
const rankGap = 150;

// wrapLayers places each layer as a column, and a layer taller than the
// map is wide into several columns side by side, in its own order, so a
// group that reaches hundreds of others (one layer of hundreds of nodes)
// still fits a canvas at a legible zoom rather than as one sliver. The
// limit grows with the map, keeping the whole roughly as wide as tall.
export function wrapLayers(
	ranked: Map<number, { id: string; y: number }[]>,
	total: number,
): Map<string, { x: number; y: number }> {
	const cap = Math.max(10, Math.ceil(Math.sqrt(total) * 1.2));
	const step = nodeHeight + rowGap;
	const layers = [...ranked.keys()]
		.sort((a, b) => a - b)
		.map((rank) => (ranked.get(rank) ?? []).sort((a, b) => a.y - b.y));
	const positions = new Map<string, { x: number; y: number }>();
	const wraps = layers.some((l) => l.length > cap);
	// Once a layer wraps, the layers beside it are restacked in their own
	// order and centred on its height: their places from the ranking were
	// spread along the unwrapped column and would sit far above or below.
	const height = Math.max(...layers.map((l) => Math.min(l.length, cap))) * step;
	let x = 0;
	for (const layer of layers) {
		if (layer.length <= cap) {
			const top = (height - layer.length * step) / 2;
			layer.forEach((n, i) => {
				positions.set(n.id, {
					x,
					y: wraps
						? Math.round(top + i * step)
						: Math.round(n.y - nodeHeight / 2),
				});
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
