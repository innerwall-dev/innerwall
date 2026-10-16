import {
	chipHeight,
	chipWidth,
	type Layout,
	nodeHeight,
	nodeWidth,
	type Route,
} from "./layout";
import type { MapEdge, MapModel } from "./model";

// The graph's edge geometry. Where the layout found every edge a route
// (layout.ts), an edge leaves its source's right side and enters its
// target's left side (the other way round for an edge that runs back),
// at a port of its own along that side, crosses each column it passes
// level, along its lane, and bends only in the free space between
// columns; its label sits in the slot the layout kept for it, on the
// level run through the chips' column. Where a layer was wrapped, edges
// fall back to one curve between mid-height handles.

export interface EdgeShape {
	path: string;
	// The label chip's centre.
	lx: number;
	ly: number;
	// The point on the edge's path at t in [0, 1], and the t the label
	// was first placed at; absent for a loop.
	at?: (t: number) => { x: number; y: number };
	t?: number;
}

const chipGap = 3;

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

// Severity orders a node's ports where their lanes leave no order of
// their own: blocked, would block, observed, allowed.
const severity: MapEdge["decision"][] = [
	"blocked",
	"would_block",
	"observed",
	"allowed",
];

// ports gives every routed edge its two ends. A node's edges attach at
// points spread evenly along the side they use, in the order their lanes
// leave that side, top to bottom, so edges that share a node part at the
// node itself and never cross there.
function ports(
	edges: MapEdge[],
	layout: Layout,
	routes: Map<string, Route>,
): Map<string, { s: { x: number; y: number }; t: { x: number; y: number } }> {
	const at = (id: string) => layout.positions.get(id) ?? { x: 0, y: 0 };
	const sides = new Map<
		string,
		{ e: MapEdge; end: "s" | "t"; x: number; toward: number }[]
	>();
	const attach = (
		node: string,
		right: boolean,
		e: MapEdge,
		end: "s" | "t",
		toward: number,
	) => {
		const p = at(node);
		const key = `${right ? "r" : "l"}\u0000${node}`;
		const x = right ? p.x + nodeWidth : p.x;
		sides.set(key, [...(sides.get(key) ?? []), { e, end, x, toward }]);
	};
	for (const e of edges) {
		const r = routes.get(e.id);
		const first = r?.points[0];
		const last = r?.points[r.points.length - 1];
		if (!r || !first || !last) continue;
		attach(e.source, !r.back, e, "s", first.y);
		attach(e.target, r.back, e, "t", last.y);
	}
	const ends = new Map<
		string,
		{ s: { x: number; y: number }; t: { x: number; y: number } }
	>();
	for (const [key, list] of sides) {
		const node = key.slice(2);
		const top = at(node).y;
		list.sort(
			(a, b) =>
				a.toward - b.toward ||
				severity.indexOf(a.e.decision) - severity.indexOf(b.e.decision) ||
				a.e.id.localeCompare(b.e.id),
		);
		list.forEach((p, i) => {
			const y = top + (nodeHeight * (i + 1)) / (list.length + 1);
			const pair = ends.get(p.e.id) ?? {
				s: { x: 0, y: 0 },
				t: { x: 0, y: 0 },
			};
			pair[p.end] = { x: p.x, y };
			ends.set(p.e.id, pair);
		});
	}
	return ends;
}

// routedPath draws an edge through its route: level across each column,
// and a cubic with level ends through each gap between, so the path is
// smooth, never turns back on itself, and stays in its lane wherever it
// passes a node.
function routedPath(
	s: { x: number; y: number },
	t: { x: number; y: number },
	r: Route,
): EdgeShape {
	const dir = r.back ? -1 : 1;
	const stops = [s];
	for (const p of r.points) {
		if (p.half > 0) {
			stops.push({ x: p.x - dir * p.half, y: p.y });
			stops.push({ x: p.x + dir * p.half, y: p.y });
		} else stops.push({ x: p.x, y: p.y });
	}
	stops.push(t);
	const segments: [number, number, number, number, number, number][] = [];
	let path = `M${s.x} ${s.y}`;
	for (let i = 1; i < stops.length; i++) {
		const a = stops[i - 1] ?? s;
		const b = stops[i] ?? t;
		const mx = (a.x + b.x) / 2;
		segments.push([a.x, a.y, mx, b.y, b.x, b.y]);
		path += ` C${mx} ${a.y} ${mx} ${b.y} ${b.x} ${b.y}`;
	}
	const at = (u: number) => {
		const k = Math.min(segments.length - 1, Math.floor(u * segments.length));
		const [ax, ay, mx, , bx, by] = segments[k] ?? [
			s.x,
			s.y,
			s.x,
			s.y,
			s.x,
			s.y,
		];
		const v = u * segments.length - k;
		return { x: bezier(v, ax, mx, mx, bx), y: bezier(v, ay, ay, by, by) };
	};
	return { path, lx: r.label.x, ly: r.label.y, at };
}

// Where a label may slide along its edge from its first place, nearest
// first: either side of it, out to the ends' margins.
const slides = [0.06, -0.06, 0.12, -0.12, 0.18, -0.18, 0.26, -0.26, 0.34];

// shapeEdges computes every edge's path and label place from the layout.
// A routed edge's label sits in the slot the layout kept for it, which no
// node or other label can reach. Every other label (a loop's, and every
// edge's where a layer was wrapped) is kept clear of the others and of
// the nodes, busiest edge first: a chip that would collide slides along
// its own edge to the nearest free place, so it always sits on its
// curve; only when the curve has no free place is it moved off it,
// alternately below and above.
export function shapeEdges(
	model: MapModel,
	layout: Layout,
): Map<string, EdgeShape> {
	const at = (id: string) => layout.positions.get(id) ?? { x: 0, y: 0 };
	const shapes = new Map<string, EdgeShape>();
	const routes = layout.routes;
	const ends = routes ? ports(model.edges, layout, routes) : new Map();
	const routed = new Set<string>();
	const bySource = new Map<string, MapEdge[]>();
	for (const e of model.edges) {
		const r = routes?.get(e.id);
		const end = ends.get(e.id);
		if (r && end) {
			shapes.set(e.id, routedPath(end.s, end.t, r));
			routed.add(e.id);
			continue;
		}
		bySource.set(e.source, [...(bySource.get(e.source) ?? []), e]);
	}
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
	for (const e of model.edges) {
		const shape = shapes.get(e.id);
		if (shape && routed.has(e.id))
			placed.push({ x: shape.lx, y: shape.ly, w: chipWidth(e) });
	}
	const nodes = [...layout.positions.values()];
	const order = [...model.edges]
		.filter((e) => !routed.has(e.id))
		.sort((a, b) => b.connections - a.connections || a.id.localeCompare(b.id));
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
