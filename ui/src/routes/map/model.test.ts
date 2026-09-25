import { describe, expect, it } from "vitest";
import { workload } from "@/test/fixtures";
import { addressGroup, peer, rollupOf, row, wref } from "@/test/map";
import { peerSelector } from "./Drawer";
import { shapeEdges } from "./geometry";
import { layoutModel } from "./layout";
import {
	baseWidth,
	blockedWidth,
	buildModel,
	defaultKey,
	edgeEmphasis,
	groupingKeys,
	type MapModel,
	nodeDimmed,
	resolveSelection,
	strokeWidth,
} from "./model";

const checkout1 = wref("w-checkout-1", "checkout-prod-01", {
	app: "checkout",
	env: "prod",
});
const checkout2 = wref("w-checkout-2", "checkout-prod-02", {
	app: "checkout",
	env: "prod",
});
const auth1 = wref("w-auth-1", "auth-prod-01", { app: "auth", env: "prod" });
const api1 = wref("w-api-1", "storefront-api-01", {
	app: "storefront-api",
	env: "prod",
});
const api2 = wref("w-api-2", "storefront-api-02", {
	app: "storefront-api",
	env: "prod",
});
const legacy = wref("w-legacy", "legacy-vm-0117", {});
const keyless = wref("w-db", "db-1", { role: "db", env: "prod" });
const office = addressGroup("ag-office", "office", [
	"172.16.0.0/12",
	"192.0.2.0/24",
]);

function node(m: MapModel, id: string) {
	const n = m.nodes.find((x) => x.id === id);
	if (!n) throw new Error(`no node ${id}`);
	return n;
}

function edge(m: MapModel, id: string) {
	const e = m.edges.find((x) => x.id === id);
	if (!e) throw new Error(`no edge ${id}`);
	return e;
}

describe("rollup to graph model", () => {
	const rollups = {
		allowed: rollupOf([
			row(peer.workload(api1), checkout1, 400_000),
			row(peer.workload(api2), checkout2, 12_880),
			row(peer.workload(checkout1), auth1, 91_000),
		]),
		would_block: rollupOf([
			row(peer.workload(api1), checkout2, 30),
			row(peer.group(office.id, "office"), checkout1, 41),
			row(peer.address("198.51.100.7"), checkout1, 2),
		]),
		blocked: rollupOf([
			row(peer.address("198.51.100.7"), auth1, 7),
			row(peer.unknown("stored-key-9"), auth1, 2),
		]),
		observed: rollupOf([
			row(peer.workload(legacy), keyless, 1_900),
			row(peer.workload(keyless), keyless, 12),
		]),
	};
	const listed = [
		workload({
			id: checkout1.id,
			labels: checkout1.labels,
			mode: "simulation",
		}),
		workload({
			id: checkout2.id,
			labels: checkout2.labels,
			mode: "simulation",
		}),
		workload({
			id: "w-checkout-3",
			labels: checkout1.labels,
			mode: "simulation",
			health: { dropped_flow_records: 212 },
		}),
		workload({ id: auth1.id, labels: auth1.labels, mode: "enforced" }),
		workload({ id: api1.id, labels: api1.labels, mode: "visibility" }),
		workload({ id: api2.id, labels: api2.labels, mode: "enforced" }),
		workload({ id: keyless.id, labels: keyless.labels, mode: "visibility" }),
	];
	const m = buildModel(rollups, listed, [office], "app");

	it("groups workloads into label groups by the key", () => {
		expect(m.nodes.map((n) => n.id).sort()).toEqual([
			"ag:ag-office",
			"g:auth",
			"g:checkout",
			"g:storefront-api",
			"keyless",
			"unknown",
			"unlabeled",
		]);
		const c = node(m, "g:checkout");
		expect(c.kind).toBe("managed");
		expect(c.title).toBe("checkout");
		expect(c.selector).toBe("app=checkout");
		// Every listed member counts, including one that reported nothing.
		expect(c.workloadIds).toEqual([
			"w-checkout-1",
			"w-checkout-2",
			"w-checkout-3",
		]);
		expect(c.modes).toEqual({ simulation: 3 });
		expect(node(m, "g:storefront-api").modes).toEqual({
			visibility: 1,
			enforced: 1,
		});
	});

	it("keeps workloads with no labels apart from those without the key", () => {
		const u = node(m, "unlabeled");
		expect(u.kind).toBe("unlabeled");
		// Seen only as a source, outside the listed scope: counted, mode unread.
		expect(u.workloadIds).toEqual(["w-legacy"]);
		expect(u.unlisted).toBe(1);
		expect(u.modes).toEqual({});
		const k = node(m, "keyless");
		expect(k.kind).toBe("keyless");
		expect(k.title).toBe("no app label");
	});

	it("keeps unmanaged peers as ingestion resolved them", () => {
		const ag = node(m, "ag:ag-office");
		expect(ag.kind).toBe("address-group");
		expect(ag.title).toBe("office");
		expect(ag.cidrs).toEqual(["172.16.0.0/12", "192.0.2.0/24"]);
		const u = node(m, "unknown");
		expect(u.kind).toBe("unknown");
		expect(u.title).toBe("unknown peers");
		// An address no group holds, once however many edges it is on; a
		// stored kind this console does not know is never shown as one.
		expect(u.addresses).toEqual(["198.51.100.7"]);
		expect(u.unrecognized).toEqual(["stored-key-9"]);
		expect(m.nodes.some((n) => n.title === "198.51.100.7")).toBe(false);
	});

	it("draws one edge per group pair, by the decision that takes precedence", () => {
		const e = edge(m, "g:storefront-api>g:checkout");
		// 412,880 allowed and 30 would block: would block is drawn.
		expect(e.decision).toBe("would_block");
		expect(e.byDecision.allowed?.connections).toBe(412_880);
		expect(e.byDecision.would_block?.connections).toBe(30);
		expect(e.connections).toBe(412_910);
		// Each pair is a rollup row, busiest first.
		expect(e.pairs.map((p) => [p.dst.id, p.peerKey, p.verdict])).toEqual([
			["w-checkout-1", "w-api-1", "allowed"],
			["w-checkout-2", "w-api-2", "allowed"],
			["w-checkout-2", "w-api-1", "would_block"],
		]);

		// Blocked outranks everything; the unknown peers' two kinds share a
		// node and so an edge.
		const u = edge(m, "unknown>g:auth");
		expect(u.decision).toBe("blocked");
		expect(u.connections).toBe(9);
		expect(u.pairs.map((p) => p.src.kind).sort()).toEqual([
			"address",
			"unknown",
		]);
		expect(edge(m, "unknown>g:checkout").decision).toBe("would_block");
		expect(edge(m, "g:checkout>g:auth").decision).toBe("allowed");
		expect(edge(m, "unlabeled>keyless").decision).toBe("observed");
		// A group reaching itself is an edge like any other.
		expect(edge(m, "keyless>keyless").connections).toBe(12);
	});

	it("totals the range and names what may be missing", () => {
		expect(m.connections).toBe(
			400_000 + 12_880 + 91_000 + 30 + 41 + 2 + 7 + 2 + 1_900 + 12,
		);
		expect(m.reporting).toBe(4);
		expect(m.truncated).toBe(false);
		expect(m.dropped.map((w) => w.id)).toEqual(["w-checkout-3"]);
		expect(m.effectiveFrom).not.toBeNull();
	});

	it("says the map is incomplete when a rollup was truncated", () => {
		const t = buildModel(
			{
				observed: rollupOf([row(peer.workload(api1), auth1, 3)], {
					truncated: true,
				}),
			},
			[],
			[],
			"app",
		);
		expect(t.truncated).toBe(true);
		expect(t.dropped).toEqual([]);
	});

	it("takes the widest effective extent across the rollups", () => {
		const a = rollupOf([row(peer.workload(api1), auth1, 3)], {
			effective: ["2026-09-25T10:00:00Z", "2026-09-25T11:00:00Z"],
		});
		const b = rollupOf([row(peer.workload(api1), checkout1, 3)], {
			effective: ["2026-09-25T09:00:00Z", "2026-09-25T10:30:00Z"],
		});
		const e = buildModel({ allowed: a, observed: b }, [], [], "app");
		expect(e.effectiveFrom).toBe("2026-09-25T09:00:00Z");
		expect(e.effectiveTo).toBe("2026-09-25T11:00:00Z");
		expect(buildModel({}, [], [], "app").effectiveFrom).toBeNull();
	});

	it("regroups under another key", () => {
		const byEnv = buildModel(rollups, listed, [office], "env");
		expect(node(byEnv, "g:prod").workloadIds).toContain("w-db");
		expect(byEnv.edges.some((e) => e.id === "g:prod>g:prod")).toBe(true);
	});

	it("offers every key the workloads carry, and opens on app", () => {
		const keys = groupingKeys(rollups, listed);
		expect(keys).toEqual(["app", "env", "role"]);
		expect(defaultKey(listed, keys)).toBe("app");
		// Without app: the key most workloads carry that splits them.
		const noApp = [
			workload({ labels: { env: "prod", role: "db" } }),
			workload({ labels: { env: "prod", role: "web" } }),
			workload({ labels: { env: "prod" } }),
		];
		expect(defaultKey(noApp, ["env", "role"])).toBe("role");
	});
});

describe("volume scaling", () => {
	it("widens by volume within the tokens' band, blocked fixed at its width", () => {
		expect(
			strokeWidth({ decision: "allowed", connections: 10 }, 10, 100_000),
		).toBe(baseWidth);
		expect(
			strokeWidth({ decision: "observed", connections: 100_000 }, 10, 100_000),
		).toBe(blockedWidth);
		const mid = strokeWidth(
			{ decision: "would_block", connections: 1000 },
			10,
			100_000,
		);
		expect(mid).toBeGreaterThan(baseWidth);
		expect(mid).toBeLessThan(blockedWidth);
		expect(strokeWidth({ decision: "blocked", connections: 1 }, 1, 1e6)).toBe(
			blockedWidth,
		);
		// One edge, or all of one volume: the base width.
		expect(strokeWidth({ decision: "allowed", connections: 5 }, 5, 5)).toBe(
			baseWidth,
		);
	});
});

describe("selection", () => {
	const a = wref("a1", "a-1", { app: "a" });
	const b = wref("b1", "b-1", { app: "b" });
	const c = wref("c1", "c-1", { app: "c" });
	const m = buildModel(
		{
			allowed: rollupOf([row(peer.workload(a), b, 10)]),
			observed: rollupOf([
				row(peer.workload(b), c, 10),
				row(peer.workload(a), c, 10),
			]),
			blocked: rollupOf([row(peer.address("203.0.113.9"), c, 1)]),
		},
		[],
		[],
		"app",
	);

	it("dims the other observed edges around a selected edge", () => {
		const s = { kind: "edge", id: "g:a>g:b" } as const;
		expect(edgeEmphasis(edge(m, "g:a>g:b"), s)).toEqual({
			selected: true,
			dimmed: false,
		});
		expect(edgeEmphasis(edge(m, "g:b>g:c"), s).dimmed).toBe(true);
		// Only observed edges dim; a blocked one keeps its weight.
		expect(edgeEmphasis(edge(m, "unknown>g:c"), s).dimmed).toBe(false);
		expect(edgeEmphasis(edge(m, "g:b>g:c"), null)).toEqual({
			selected: false,
			dimmed: false,
		});
	});

	it("scopes to a selected node's own edges and neighbours", () => {
		const s = { kind: "node", id: "g:b" } as const;
		expect(edgeEmphasis(edge(m, "g:a>g:b"), s).dimmed).toBe(false);
		expect(edgeEmphasis(edge(m, "g:b>g:c"), s).dimmed).toBe(false);
		expect(edgeEmphasis(edge(m, "g:a>g:c"), s).dimmed).toBe(true);
		expect(nodeDimmed(m, "g:a", s)).toBe(false);
		expect(nodeDimmed(m, "unknown", s)).toBe(true);
		expect(nodeDimmed(m, "g:b", s)).toBe(false);
	});

	it("drops a selection the model no longer holds", () => {
		expect(resolveSelection(m, { kind: "edge", id: "g:a>g:b" })).not.toBeNull();
		expect(resolveSelection(m, { kind: "edge", id: "g:z>g:b" })).toBeNull();
		expect(resolveSelection(m, { kind: "node", id: "unknown" })).not.toBeNull();
		expect(resolveSelection(m, { kind: "node", id: "g:z" })).toBeNull();
	});
});

describe("rule from an edge", () => {
	it("names the source by its group, and by the scope when every source carries it", () => {
		const m = buildModel(
			{ would_block: rollupOf([row(peer.workload(api1), checkout1, 3)]) },
			[],
			[],
			"app",
		);
		const e = edge(m, "g:storefront-api>g:checkout");
		const from = node(m, "g:storefront-api");
		expect(peerSelector(e, from, "app", ["env=prod"])).toBe(
			"app=storefront-api AND env=prod",
		);
		expect(peerSelector(e, from, "app", ["env=staging"])).toBe(
			"app=storefront-api",
		);
	});

	it("names unmanaged sources by group or address", () => {
		const m = buildModel(
			{
				would_block: rollupOf([
					row(peer.group(office.id, "office"), checkout1, 3),
					row(peer.address("198.51.100.7"), checkout1, 3),
					row(peer.address("2001:db8::7"), checkout1, 3),
				]),
			},
			[],
			[office],
			"app",
		);
		expect(
			peerSelector(
				edge(m, "ag:ag-office>g:checkout"),
				node(m, "ag:ag-office"),
				"app",
				[],
			),
		).toBe("address group office");
		expect(
			peerSelector(
				edge(m, "unknown>g:checkout"),
				node(m, "unknown"),
				"app",
				[],
			),
		).toMatch(/^cidr (198\.51\.100\.7\/32|2001:db8::7\/128) \(\+1\)$/);
	});
});

describe("edge labels", () => {
	it("never overlap one another", () => {
		const dsts = ["b", "c", "d", "e", "f"].map((x) =>
			wref(`${x}1`, `${x}-1`, { app: x }),
		);
		const src = wref("a1", "a-1", { app: "a" });
		const m = buildModel(
			{
				observed: rollupOf(
					dsts.map((d, i) => row(peer.workload(src), d, 10 ** (i + 1))),
				),
			},
			[],
			[],
			"app",
		);
		const shapes = [...shapeEdges(m, layoutModel(m)).values()];
		for (let i = 0; i < shapes.length; i++) {
			for (let j = i + 1; j < shapes.length; j++) {
				const a = shapes[i];
				const b = shapes[j];
				const apart =
					Math.abs(a.ly - b.ly) >= 16 || Math.abs(a.lx - b.lx) >= 60;
				expect(apart).toBe(true);
			}
		}
	});
});

describe("layout at estate scale", () => {
	it("wraps a layer of hundreds of groups into columns", () => {
		const src = wref("hub", "hub-1", { app: "hub" });
		const targets = Array.from({ length: 300 }, (_, i) =>
			wref(`t${i}`, `svc-${i}`, { app: `svc-${String(i).padStart(3, "0")}` }),
		);
		const m = buildModel(
			{ observed: rollupOf(targets.map((t) => row(peer.workload(src), t, 5))) },
			[],
			[],
			"app",
		);
		const { positions, order } = layoutModel(m);
		const placed = targets.map((t) => positions.get(`g:${t.labels.app}`));
		const xs = new Set(placed.map((p) => p?.x));
		const ys = placed.map((p) => p?.y ?? 0);
		expect(xs.size).toBeGreaterThan(10);
		expect(Math.max(...ys)).toBeLessThan(25 * 92);
		// No two groups share a place, and reading order starts at the hub.
		const keys = new Set([...positions.values()].map((p) => `${p.x},${p.y}`));
		expect(keys.size).toBe(positions.size);
		expect(order[0]).toBe("g:hub");
	});
});
