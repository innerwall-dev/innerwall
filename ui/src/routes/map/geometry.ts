import { short } from "@/lib/format";
import { type Layout, nodeHeight, nodeWidth } from "./layout";
import type { MapEdge, MapModel } from "./model";

// The graph's edge geometry: every edge leaves its source's right side
// and enters its target's left side at mid-height, where the nodes'
// handles are, so paths and label places follow from the layout alone.

export interface EdgeShape {
	path: string;
	// The label chip's centre.
	lx: number;
	ly: number;
	// The point on the edge's curve at t in [0, 1], and the t the label
	// was first placed at; absent for a loop.
	at?: (t: number) => { x: number; y: number };
	t?: number;
}

const chipHeight = 18;
const chipGap = 3;

// chipWidth is a label chip's width in mono-xs (11px, about 6.6px a
// figure): the 12px glyph, a 4px gap, and the count, with the chip's
// padding and border.
export function chipWidth(e: MapEdge): number {
	const n = e.byDecision[e.decision]?.connections ?? e.connections;
	return Math.ceil(12 + 4 + short(n).length * 6.6 + 10);
}

function bezier(t: number, a: number, b: number, c: number, d: number) {
	const u = 1 - t;
	return u * u * u * a + 3 * u * u * t * b + 3 * u * t * t * c + t * t * t * d;
}

// loopPath is an edge from a group to itself: an arc over the node's
// top, from its right side back into its left.
export function loopPath(
	sx: number,
	sy: number,
	tx: number,
	ty: number,
): EdgeShape {
	const lift = nodeHeight + 26;
	return {
		path: `M${sx} ${sy} C${sx + 60} ${sy - lift} ${tx - 60} ${ty - lift} ${tx} ${ty}`,
		lx: (sx + tx) / 2,
		ly: sy - lift * 0.75,
	};
}

// flowPath is an edge from one node's right side into another's left,
// as the design draws it: a cubic whose ends leave and arrive level. A
// forward edge bends at its midpoint; an edge that runs back (a cycle)
// swings out past both ends. The label sits on the curve a little way
// out from the source, further for each edge the source sends (fan), so
// labels sit where edges part rather than where they converge.
export function flowPath(
	sx: number,
	sy: number,
	tx: number,
	ty: number,
	fan: number,
): EdgeShape {
	const forward = tx > sx + 20;
	const dx = forward ? (tx - sx) / 2 : Math.max(80, (sx - tx) / 3);
	const c1x = sx + dx;
	const c2x = tx - dx;
	let t = 0.5;
	if (forward) {
		const target = sx + 40 + fan * 30;
		let lo = 0;
		let hi = 0.5;
		for (let i = 0; i < 24; i++) {
			const mid = (lo + hi) / 2;
			if (bezier(mid, sx, c1x, c2x, tx) < target) lo = mid;
			else hi = mid;
		}
		t = lo;
	}
	const at = (u: number) => ({
		x: bezier(u, sx, c1x, c2x, tx),
		y: bezier(u, sy, sy, ty, ty),
	});
	const p = at(t);
	return {
		path: `M${sx} ${sy} C${c1x} ${sy} ${c2x} ${ty} ${tx} ${ty}`,
		lx: p.x,
		ly: p.y,
		at,
		t,
	};
}

// Where a label may slide along its edge from its first place, nearest
// first: either side of it, out to the ends' margins.
const slides = [0.06, -0.06, 0.12, -0.12, 0.18, -0.18, 0.26, -0.26, 0.34];

// shapeEdges computes every edge's path and label place from the layout,
// then keeps every chip clear of the others and of the nodes, busiest
// edge first. A chip that would collide slides along its own edge to the
// nearest free place, so it always sits on its curve; only when the curve
// has no free place is it moved off it, alternately below and above.
export function shapeEdges(
	model: MapModel,
	layout: Layout,
): Map<string, EdgeShape> {
	const at = (id: string) => layout.positions.get(id) ?? { x: 0, y: 0 };
	const bySource = new Map<string, MapEdge[]>();
	for (const e of model.edges) {
		bySource.set(e.source, [...(bySource.get(e.source) ?? []), e]);
	}
	const shapes = new Map<string, EdgeShape>();
	for (const list of bySource.values()) {
		list.sort(
			(a, b) => at(a.target).y - at(b.target).y || a.id.localeCompare(b.id),
		);
		list.forEach((e, fan) => {
			const s = at(e.source);
			const t = at(e.target);
			const sx = s.x + nodeWidth;
			const sy = s.y + nodeHeight / 2;
			const tx = t.x;
			const ty = t.y + nodeHeight / 2;
			shapes.set(
				e.id,
				e.source === e.target
					? loopPath(sx, sy, tx, ty)
					: flowPath(sx, sy, tx, ty, fan),
			);
		});
	}

	const placed: { x: number; y: number; w: number }[] = [];
	const nodes = [...layout.positions.values()];
	const order = [...model.edges].sort(
		(a, b) => b.connections - a.connections || a.id.localeCompare(b.id),
	);
	for (const e of order) {
		const shape = shapes.get(e.id);
		if (!shape) continue;
		const w = chipWidth(e);
		const free = (x: number, y: number) =>
			!placed.some(
				(p) =>
					Math.abs(p.x - x) < (p.w + w) / 2 + chipGap &&
					Math.abs(p.y - y) < chipHeight + chipGap,
			) &&
			!nodes.some(
				(n) =>
					x + w / 2 > n.x - chipGap &&
					x - w / 2 < n.x + nodeWidth + chipGap &&
					y + chipHeight / 2 > n.y - chipGap &&
					y - chipHeight / 2 < n.y + nodeHeight + chipGap,
			);
		let x = shape.lx;
		let y = shape.ly;
		if (!free(x, y) && shape.at && shape.t !== undefined) {
			for (const d of slides) {
				const u = shape.t + d;
				if (u < 0.08 || u > 0.92) continue;
				const p = shape.at(u);
				if (free(p.x, p.y)) {
					x = p.x;
					y = p.y;
					break;
				}
			}
		}
		// No free place on the curve: off it, alternately below and above.
		for (let step = 1; !free(x, y) && step < 12; step++) {
			const d = Math.ceil(step / 2) * (chipHeight + chipGap);
			y = shape.ly + (step % 2 === 1 ? d : -d);
		}
		shape.lx = x;
		shape.ly = y;
		placed.push({ x, y, w });
	}
	return shapes;
}
