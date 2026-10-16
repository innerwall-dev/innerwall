import { describe, expect, it } from "vitest";
import type { Verdict } from "@/api/schema";
import { type ByVerdict, peer, rollupOf, row, wref } from "@/test/map";
import { type EdgeShape, shapeEdges } from "./geometry";
import {
	chipHeight,
	chipWidth,
	type Layout,
	layoutModel,
	nodeHeight,
	nodeWidth,
} from "./layout";
import { buildModel, type MapModel } from "./model";

// The layout's promises, held on the lab estate's shape and on graphs
// built from a seed: no edge passes under a node, no label overlaps a
// node or another label, every label sits on its own edge, the edges a
// node shares leave it at ports of their own, and the same input lays out
// the same way every time.

function modelOf(byVerdict: ByVerdict): MapModel {
	const rollups = Object.fromEntries(
		Object.entries(byVerdict).map(([v, groups]) => [v, rollupOf(groups ?? [])]),
	);
	return buildModel(rollups, [], [], "app");
}

const web = wref("w-web", "web-01", { app: "web" });
const ops = wref("w-ops", "ops-01", { app: "ops" });
const db = wref("w-db", "db-01", { app: "db" });

// The lab estate: unknown peers reach ops (blocked), web and db (would
// block), and ops reaches web and db, which web reaches too.
const lab = modelOf({
	blocked: [row(peer.address("203.0.113.9"), ops, 412)],
	would_block: [
		row(peer.address("203.0.113.9"), web, 88),
		row(peer.address("198.51.100.4"), db, 31),
	],
	allowed: [
		row(peer.workload(ops), web, 5400),
		row(peer.workload(web), db, 21000),
		row(peer.workload(ops), db, 1900),
	],
});

// seeded draws a graph of eight groups from a seed: every decision, fans
// out of and into one group, edges that skip layers, and cycles.
function seeded(seed: number): MapModel {
	let s = seed;
	const next = () => {
		s = (s * 1103515245 + 12345) % 2147483648;
		return s / 2147483648;
	};
	const groups = Array.from({ length: 8 }, (_, i) =>
		wref(`w${i}`, `h${i}`, { app: `g${i}` }),
	);
	const verdicts: Verdict[] = ["blocked", "would_block", "allowed", "observed"];
	const byVerdict: ByVerdict = {};
	const seen = new Set<string>();
	for (let k = 0; k < 14; k++) {
		const a = Math.floor(next() * 8);
		const b = Math.floor(next() * 8);
		if (a === b || seen.has(`${a}>${b}`)) continue;
		seen.add(`${a}>${b}`);
		const v = verdicts[Math.floor(next() * 4)] ?? "observed";
		const src = groups[a] ?? web;
		const dst = groups[b] ?? web;
		byVerdict[v] = [
			...(byVerdict[v] ?? []),
			row(peer.workload(src), dst, 1 + Math.floor(next() * 50_000)),
		];
	}
	byVerdict.blocked = [
		...(byVerdict.blocked ?? []),
		row(peer.address("203.0.113.9"), groups[0] ?? web, 7),
		row(peer.address("203.0.113.9"), groups[5] ?? web, 3),
	];
	return modelOf(byVerdict);
}

function check(
	model: MapModel,
	layout: Layout,
	shapes: Map<string, EdgeShape>,
) {
	const nodes = [...layout.positions.entries()];
	const chips = model.edges.map((e) => {
		const s = shapes.get(e.id) as EdgeShape;
		return { e, s, w: chipWidth(e) };
	});
	for (const { e, s } of chips) {
		const at = s.at;
		if (!at) continue;
		const samples = Array.from({ length: 801 }, (_, k) => at(k / 800));
		// No edge passes under a node, its own ends included.
		for (const [id, n] of nodes) {
			const under = samples.find(
				(p) =>
					p.x > n.x + 1 &&
					p.x < n.x + nodeWidth - 1 &&
					p.y > n.y + 1 &&
					p.y < n.y + nodeHeight - 1,
			);
			expect(under, `${e.id} under ${id}`).toBeUndefined();
		}
		// The edge never turns back on itself: it runs one way across the
		// map, left to right, or right to left where it runs back.
		const steps = samples.slice(1).map((p, k) => p.x - (samples[k]?.x ?? 0));
		const forward = steps.every((d) => d >= -1e-6);
		const backward = steps.every((d) => d <= 1e-6);
		expect(forward || backward, `${e.id} turns back`).toBe(true);
		// The label sits on its own edge.
		const near = samples.some((p) => Math.hypot(p.x - s.lx, p.y - s.ly) < 1.5);
		expect(near, `${e.id}'s label off its edge`).toBe(true);
	}
	for (const [i, a] of chips.entries()) {
		for (const b of chips.slice(i + 1)) {
			const apart =
				Math.abs(a.s.lx - b.s.lx) >= (a.w + b.w) / 2 ||
				Math.abs(a.s.ly - b.s.ly) >= chipHeight;
			expect(apart, `${a.e.id} and ${b.e.id}'s labels overlap`).toBe(true);
		}
		for (const [id, n] of nodes) {
			const over =
				a.s.lx + a.w / 2 > n.x &&
				a.s.lx - a.w / 2 < n.x + nodeWidth &&
				a.s.ly + chipHeight / 2 > n.y &&
				a.s.ly - chipHeight / 2 < n.y + nodeHeight;
			expect(over, `${a.e.id}'s label over ${id}`).toBe(false);
		}
	}
}

function start(s: EdgeShape): { x: number; y: number } {
	const [x, y] = s.path.slice(1).split(" C")[0]?.split(" ").map(Number) ?? [];
	return { x: x ?? 0, y: y ?? 0 };
}

describe("the map's layout on the lab estate", () => {
	const layout = layoutModel(lab);
	const shapes = shapeEdges(lab, layout);

	it("routes every edge", () => {
		expect(layout.routes?.size).toBe(lab.edges.length);
	});

	it("keeps edges out from under nodes and labels clear", () => {
		check(lab, layout, shapes);
	});

	it("parts the fan out of unknown peers at the node", () => {
		const fan = lab.edges.filter((e) => e.source === "unknown");
		expect(fan).toHaveLength(3);
		const ys = fan.map((e) => start(shapes.get(e.id) as EdgeShape).y);
		expect(new Set(ys).size).toBe(3);
		// Spread evenly down the node's right side.
		const top = layout.positions.get("unknown")?.y ?? 0;
		expect(ys.sort((a, b) => a - b)).toEqual([
			top + nodeHeight / 4,
			top + nodeHeight / 2,
			top + (nodeHeight * 3) / 4,
		]);
	});

	it("lays out the same way every time", () => {
		const again = layoutModel(lab);
		expect([...again.positions]).toEqual([...layout.positions]);
		expect([...shapeEdges(lab, again)].map(([id, s]) => [id, s.path])).toEqual(
			[...shapes].map(([id, s]) => [id, s.path]),
		);
	});
});

describe("the map's layout on graphs drawn from a seed", () => {
	it("draws cycles, so some edges run back", () => {
		const back = Array.from({ length: 40 }, (_, i) =>
			[...(layoutModel(seeded(i + 1)).routes?.values() ?? [])].filter(
				(r) => r.back,
			),
		).flat();
		expect(back.length).toBeGreaterThan(0);
	});

	for (let seed = 1; seed <= 40; seed++) {
		it(`holds for seed ${seed}`, () => {
			const m = seeded(seed);
			const layout = layoutModel(m);
			expect(layout.routes).not.toBeNull();
			check(m, layout, shapeEdges(m, layout));
		});
	}
});
