import { describe, expect, it } from "vitest";
import type { EvidenceGap, Workload } from "@/api/schema";
import { minutesAgo, workload } from "@/test/fixtures";
import { addressGroup, peer, row as srcDst, wref } from "@/test/map";
import { gap, ps, rollups } from "@/test/review";
import {
	barWidth,
	bucketOf,
	buildRows,
	cellKey,
	chipCounts,
	composeVerdict,
	duration,
	filterRows,
	matrixOf,
	partition,
	promotionIds,
	type ReviewRollups,
	recencyOf,
	resolveSelection,
	rowId,
	scopeRequirements,
	scopeText,
} from "./model";

const metricsLabels = { app: "metrics-collector", env: "prod", tier: "infra" };
const m1 = wref("w-m1", "metrics-01", metricsLabels);
const m2 = wref("w-m2", "metrics-02", metricsLabels);
const api = wref("w-api", "api-01", { app: "storefront-api", env: "prod" });
const c1 = wref("w-c1", "checkout-01", { app: "checkout", env: "prod" });
const c2 = wref("w-c2", "checkout-02", { app: "checkout", env: "prod" });
const c3 = wref("w-c3", "checkout-03", { app: "checkout", env: "prod" });
const office = addressGroup("ag-office", "office", ["172.16.0.0/12"]);

describe("scope", () => {
	it("passes a selector as label requirements and prints it", () => {
		const sel = { env: ["prod", "staging"], app: ["checkout"] };
		expect(scopeRequirements(sel)).toEqual([
			"app=checkout",
			"env=prod",
			"env=staging",
		]);
		expect(scopeText(sel)).toBe("app=checkout AND (env=prod OR env=staging)");
		expect(scopeText({ app: ["checkout"], env: ["prod"] })).toBe(
			"app=checkout AND env=prod",
		);
	});
});

describe("rows", () => {
	it("folds workload peers by their whole label set and sums them", () => {
		const r = rollups({
			would_block: [
				ps(peer.workload(m1), "tcp/9100", 9000, 2, 1),
				ps(peer.workload(m2), "tcp/9100", 9204, 2, 3),
				ps(peer.group(office.id, "office"), "tcp/22", 41, 3),
			],
			allowed: [ps(peer.workload(api), "tcp/8443", 412_880, 1)],
		});
		const rows = buildRows(r, [office], 40);
		expect(rows.map((x) => [x.verdict, x.peer.title, x.service])).toEqual([
			["would_block", "app=metrics-collector", "tcp/9100"],
			["would_block", "office", "tcp/22"],
			["allowed", "app=storefront-api", "tcp/8443"],
		]);
		const metrics = rows[0];
		expect(metrics.members).toHaveLength(2);
		expect(metrics.connections).toBe(18_204);
		expect(metrics.peer.rest).toBe("env=prod tier=infra");
		expect(metrics.peer.full).toBe("app=metrics-collector env=prod tier=infra");
		expect(metrics.lastSeen).toBe(
			r.peerService.would_block.groups[0].last_seen,
		);
		// An address group keeps its identity and gains its CIDRs.
		expect(rows[1].peer).toMatchObject({
			kind: "group",
			addressGroupId: office.id,
			cidrs: ["172.16.0.0/12"],
		});
		expect(rows[1].id).toBe(rowId("would_block", `ag:${office.id}`, "tcp/22"));
	});

	it("keeps unknown addresses, unrecognized keys, and unlabeled workloads apart", () => {
		const bare = wref("w-bare", "legacy-01", {});
		const rows = buildRows(
			rollups({
				would_block: [
					ps(peer.address("10.7.44.19"), "tcp/8080", 2, 1),
					ps(peer.unknown("mystery"), "tcp/8080", 1, 1),
					ps(peer.workload(bare), "tcp/5432", 9, 1),
				],
			}),
			[],
			40,
		);
		expect(rows.map((x) => [x.peer.kind, x.peer.title])).toEqual([
			["unlabeled", "unlabeled workloads"],
			["address", "10.7.44.19"],
			["unrecognized", "mystery"],
		]);
	});

	it("counts workloads exactly when it can and says when it cannot", () => {
		// One member: the store's own distinct count.
		let rows = buildRows(
			rollups({ would_block: [ps(peer.workload(m1), "tcp/9100", 5, 7)] }),
			[],
			40,
		);
		expect(rows[0]).toMatchObject({ workloads: 7, workloadsExact: true });

		// Two members on one service: their destinations are that service's,
		// from the source-by-destination rollup.
		const onePort = rollups(
			{
				would_block: [
					ps(peer.workload(m1), "tcp/9100", 5, 2),
					ps(peer.workload(m2), "tcp/9100", 5, 2),
				],
			},
			{
				would_block: [
					srcDst(peer.workload(m1), c1, 1),
					srcDst(peer.workload(m1), c2, 1),
					srcDst(peer.workload(m2), c2, 1),
					srcDst(peer.workload(m2), c3, 1),
				],
			},
		);
		rows = buildRows(onePort, [], 40);
		expect(rows[0]).toMatchObject({ workloads: 3, workloadsExact: true });

		// The same group on two services: its destinations cannot be split
		// between them, so the row says at least its busiest member's.
		const twoPorts: ReviewRollups = rollups(
			{
				would_block: [
					ps(peer.workload(m1), "tcp/9100", 5, 2),
					ps(peer.workload(m2), "tcp/9100", 5, 1),
					ps(peer.workload(m1), "tcp/22", 5, 1),
				],
			},
			{
				would_block: [
					srcDst(peer.workload(m1), c1, 1),
					srcDst(peer.workload(m1), c2, 1),
					srcDst(peer.workload(m2), c3, 1),
				],
			},
		);
		rows = buildRows(twoPorts, [], 40);
		const scrape = rows.find((x) => x.service === "tcp/9100");
		expect(scrape).toMatchObject({ workloads: 2, workloadsExact: false });

		// A lower bound that reaches the whole scope is exact.
		rows = buildRows(twoPorts, [], 2);
		expect(rows.find((x) => x.service === "tcp/9100")).toMatchObject({
			workloads: 2,
			workloadsExact: true,
		});

		// A truncated rollup cannot attribute destinations.
		const truncated = rollups(
			{
				would_block: [
					ps(peer.workload(m1), "tcp/9100", 5, 2),
					ps(peer.workload(m2), "tcp/9100", 5, 2),
				],
			},
			{ would_block: [srcDst(peer.workload(m1), c1, 1)] },
			{ truncated: true },
		);
		expect(buildRows(truncated, [], 40)[0].workloadsExact).toBe(false);
	});
});

describe("the takes", () => {
	const rows = buildRows(
		rollups({
			would_block: [
				ps(peer.workload(m1), "tcp/9100", 18_204, 40, 0.5),
				ps(peer.group(office.id, "office"), "tcp/22", 41, 3, 2),
				ps(peer.address("10.7.44.19"), "tcp/8080", 2, 1, 11 * 24 * 60),
			],
			allowed: [
				ps(peer.workload(api), "tcp/8443", 412_880, 40, 0.1),
				ps(peer.group(office.id, "office"), "tcp/22", 7, 1, 90),
			],
		}),
		[office],
		40,
	);

	it("filters by the verdict chips and counts each", () => {
		expect(chipCounts(rows)).toEqual({ all: 5, would_block: 3, allowed: 2 });
		expect(
			filterRows(rows, "would_block").every((r) => r.verdict === "would_block"),
		).toBe(true);
		expect(filterRows(rows, "allowed")).toHaveLength(2);
		expect(filterRows(rows, "all")).toBe(rows);
	});

	it("lays out peers down and services across, drawing what would be dropped", () => {
		const m = matrixOf(rows);
		expect(m.peers.map((p) => p.title)).toEqual([
			"app=metrics-collector",
			"office",
			"10.7.44.19",
			"app=storefront-api",
		]);
		expect(m.services).toEqual(["tcp/22", "tcp/8080", "tcp/8443", "tcp/9100"]);
		const cell = m.cells.get(cellKey(`ag:${office.id}`, "tcp/22"));
		expect(cell?.row.verdict).toBe("would_block");
		expect(cell?.others.map((r) => r.verdict)).toEqual(["allowed"]);
		expect(m.cells.has(cellKey(`ag:${office.id}`, "tcp/9100"))).toBe(false);
	});

	it("buckets rows by when each was last seen, newest first", () => {
		const now = Date.now();
		const b = recencyOf(rows, now);
		expect(b.map((x) => [x.bucket, x.rows.map((r) => r.service)])).toEqual([
			["last minute", ["tcp/8443", "tcp/9100"]],
			["last hour", ["tcp/22"]],
			["last day", ["tcp/22"]],
			["older than a day", ["tcp/8080"]],
		]);
		expect(bucketOf(minutesAgo(0.5), now)).toBe("last minute");
		expect(bucketOf(minutesAgo(26 * 60), now)).toBe("older than a day");
	});

	it("resolves a selection only while its row exists", () => {
		expect(resolveSelection(rows, rows[1].id)).toBe(rows[1]);
		expect(resolveSelection(rows, "would_block|gone|tcp/1")).toBeNull();
		expect(resolveSelection(rows, null)).toBeNull();
	});

	it("scales bars by volume on a log scale", () => {
		expect(barWidth(412_880, 412_880)).toBe(100);
		expect(barWidth(2, 412_880)).toBe(8);
		expect(barWidth(0, 412_880)).toBe(3);
		expect(barWidth(5, 0)).toBe(0);
	});
});

// A scope that passes every condition, to break one at a time.
function ready(n = 3): Workload[] {
	return Array.from({ length: n }, (_, i) =>
		workload({
			hostname: `checkout-0${i + 1}`,
			mode: "simulation",
			sync: { state: "synced", applied_version: 4, latest_version: 4 },
		}),
	);
}

const allowedOnly = rollups(
	{ allowed: [ps(peer.workload(api), "tcp/8443", 400, 3)] },
	{
		allowed: [
			srcDst(peer.workload(api), c1, 200),
			srcDst(peer.workload(api), c2, 200),
		],
	},
);

function verdictOf(
	workloads: Workload[],
	r = allowedOnly,
	gaps: EvidenceGap[] = [],
	gapsTruncated = false,
) {
	return composeVerdict({
		rows: buildRows(r, [], workloads.length),
		workloads,
		rollups: r,
		gaps,
		gapsTruncated,
		range: { from: minutesAgo(24 * 60), to: minutesAgo(0) },
		now: Date.now(),
		rowLimit: 1000,
	});
}

describe("the verdict", () => {
	it("is safe only with no would-block traffic and every workload simulating on its latest policy", () => {
		const v = verdictOf(ready());
		expect(v.safe).toBe(true);
		expect(v.headline).toBe("Safe to enforce");
		expect(v.failing).toEqual([]);
		expect(v.kpis).toMatchObject({
			pairs: 0,
			connections: 0,
			allowedPairs: 1,
			simulating: 3,
		});
		// Its one qualification is how long the traffic was observed for.
		expect(v.caveats).toHaveLength(1);
		expect(v.caveats[0]).toMatch(
			/^Observed for 1h 10m \(windows .* UTC\); a peer/,
		);
	});

	it("says a safe verdict rests on nothing when nothing was stored", () => {
		const v = verdictOf(ready(), rollups({}));
		expect(v.safe).toBe(true);
		expect(v.caveats).toEqual([
			"No flows were stored in this range; no observed traffic stands behind this verdict",
		]);
	});

	it("fails on would-block traffic, stated in the headline", () => {
		const r = rollups(
			{ would_block: [ps(peer.workload(m1), "tcp/9100", 18_204, 3, 2)] },
			{
				would_block: [
					srcDst(peer.workload(m1), c1, 9000),
					srcDst(peer.workload(m1), c2, 9204),
				],
			},
		);
		const v = verdictOf(ready(), r);
		expect(v.safe).toBe(false);
		expect(v.failing.map((c) => c.id)).toEqual(["would-block"]);
		expect(v.headline).toBe("Not safe to enforce yet");
		expect(v.sub).toBe(
			"1 peer/service pair carrying 18,204 connections would be dropped. 1 of them was seen in the last hour.",
		);
		expect(v.kpis).toMatchObject({
			pairs: 1,
			connections: 18_204,
			affected: 2,
		});
		// The would-block traffic is the headline, not a caveat line.
		expect(v.caveats).toEqual([]);
	});

	const broken: [string, (w: Workload[]) => void, string][] = [
		[
			"degraded",
			(w) => {
				w[0].hostname = "checkout-prod-07";
				w[0].sync = {
					...w[0].sync,
					state: "degraded",
					applied_version: 41,
					latest_version: 42,
				};
			},
			"checkout-prod-07 is degraded (applied v41, rendered v42) — its flows were evaluated against stale policy",
		],
		[
			"offline",
			(w) => {
				w[0].sync = { ...w[0].sync, state: "offline" };
				w[0].health = { ...w[0].health, last_seen_at: minutesAgo(180) };
			},
			"checkout-01 is offline (last seen 3h ago) — nothing it saw since reaches this review",
		],
		[
			"pending",
			(w) => {
				w[0].sync = {
					...w[0].sync,
					state: "synced",
					applied_version: 3,
					latest_version: 4,
				};
			},
			"checkout-01 has not applied its latest policy (applied v3, rendered v4)",
		],
		[
			"visibility",
			(w) => {
				w[1].mode = "visibility";
				w[2].mode = "visibility";
			},
			"2 workloads in scope are still in visibility mode and produced no verdict",
		],
	];
	for (const [id, breakIt, line] of broken) {
		it(`fails on its own when a workload is ${id}, with its own line`, () => {
			const w = ready();
			breakIt(w);
			const v = verdictOf(w);
			expect(v.safe).toBe(false);
			expect(v.failing.map((c) => c.id)).toEqual([id]);
			expect(v.caveats[0]).toBe(line);
			expect(v.headline).toBe("Not safe to enforce yet");
			expect(v.sub).toBe(
				"No observed traffic would be dropped, but not every workload in scope is simulating on its latest policy.",
			);
		});
	}

	it("states every failing condition together, each on its line", () => {
		const w = ready(4);
		w[0].sync = {
			...w[0].sync,
			state: "degraded",
			applied_version: 1,
			latest_version: 2,
		};
		w[1].mode = "visibility";
		w[2].sync = { ...w[2].sync, state: "offline" };
		const v = verdictOf(w);
		expect(v.failing.map((c) => c.id)).toEqual([
			"degraded",
			"offline",
			"visibility",
		]);
		expect(v.caveats).toHaveLength(3);
	});

	it("does not call an empty or an already-enforced scope safe", () => {
		const empty = verdictOf([]);
		expect(empty.safe).toBe(false);
		expect(empty.failing.map((c) => c.id)).toEqual(["no-workloads"]);
		expect(empty.headline).toBe("No workload in scope");

		const enforced = ready(2).map((w) => ({ ...w, mode: "enforced" as const }));
		const v = verdictOf(enforced);
		expect(v.safe).toBe(false);
		expect(v.failing.map((c) => c.id)).toEqual(["nothing-simulating"]);
		expect(v.caveats[0]).toBe("All 2 workloads in scope are already enforced");
	});

	it("qualifies without failing: dropped records and a truncated listing", () => {
		const w = ready();
		w[2].hostname = "checkout-prod-31";
		w[2].health = { ...w[2].health, dropped_flow_records: 212 };
		const r = rollups(
			{ allowed: [ps(peer.workload(api), "tcp/8443", 400, 3)] },
			{},
			{ truncated: true },
		);
		const v = verdictOf(w, r);
		expect(v.safe).toBe(true);
		expect(v.caveats.slice(0, 2)).toEqual([
			"checkout-prod-31 dropped 212 flow records — its verdict may be incomplete",
			"Only the busiest 1,000 peer/service pairs per decision are listed; the totals count every pair",
		]);
	});

	describe("evidence gaps", () => {
		// The allowed-only rollups covered windows from 125 to 55 minutes
		// ago; that is the range the evidence is judged over.
		const hhmm = /\d\d:\d\d and \d\d:\d\d UTC/;

		it("fails on their own when evidence is missing from the covered range", () => {
			const w = ready();
			const v = verdictOf(w, allowedOnly, [gap(w[0], 90, 88)]);
			expect(v.safe).toBe(false);
			expect(v.failing.map((c) => c.id)).toEqual(["evidence-gaps"]);
			expect(v.caveats[0]).toMatch(
				/^Evidence incomplete for checkout-01 between \d\d:\d\d and \d\d:\d\d UTC — the kernel dropped events$/,
			);
			expect(v.headline).toBe("Not safe to enforce yet");
			expect(v.sub).toBe(
				"No observed traffic would be dropped, but evidence is missing from part of this range: traffic in it may have gone unseen.",
			);
			// Nothing else changes: the counts are the rollups' own.
			expect(v.kpis).toMatchObject({
				pairs: 0,
				allowedPairs: 1,
				simulating: 3,
			});
		});

		it("clamps the interval it names to the covered range", () => {
			const w = ready();
			const v = verdictOf(w, allowedOnly, [gap(w[0], 200, 100)]);
			const covered = allowedOnly.peerService.allowed.effective_from as string;
			const at = new Date(covered).toISOString().slice(11, 16);
			expect(v.caveats[0]).toContain(`between ${at} and`);
		});

		it("ignores gaps outside the covered range and of workloads out of scope", () => {
			const w = ready();
			const elsewhere = { id: "w-elsewhere", hostname: "batch-01" };
			const v = verdictOf(w, allowedOnly, [
				gap(w[0], 300, 290),
				gap(w[1], 30, 20),
				gap(elsewhere, 90, 80),
			]);
			expect(v.safe).toBe(true);
			expect(v.failing).toEqual([]);
		});

		it("judge the requested range when no windows were stored at all", () => {
			const w = ready();
			const v = verdictOf(w, rollups({}), [
				gap(w[1], 600, 500, {
					kind: "buffer_overflow",
					source: null,
					count: 412,
				}),
			]);
			expect(v.safe).toBe(false);
			expect(v.failing.map((c) => c.id)).toEqual(["evidence-gaps"]);
			expect(v.caveats[0]).toMatch(
				/^Evidence incomplete for checkout-02 between .* — the agent dropped buffered windows$/,
			);
		});

		it("name the count of workloads, and no single reason, when several are missing evidence", () => {
			const w = ready();
			const v = verdictOf(w, allowedOnly, [
				gap(w[0], 100, 99),
				gap(w[2], 70, 60, { kind: "source_restart", source: "conntrack" }),
			]);
			expect(v.caveats[0]).toMatch(
				/^Evidence incomplete for 2 workloads in scope between \d\d:\d\d and \d\d:\d\d UTC$/,
			);
			expect(v.caveats[0]).toMatch(hhmm);
		});

		it("fail beside would-block traffic and beside a sync condition, each stated", () => {
			const w = ready();
			const r = rollups(
				{ would_block: [ps(peer.workload(m1), "tcp/9100", 18_204, 3, 2)] },
				{ would_block: [srcDst(peer.workload(m1), c1, 18_204)] },
			);
			const both = verdictOf(w, r, [gap(w[0], 90, 88)]);
			expect(both.failing.map((c) => c.id)).toEqual([
				"would-block",
				"evidence-gaps",
			]);
			expect(both.sub).toMatch(/^1 peer\/service pair carrying/);
			expect(both.caveats).toHaveLength(1);

			w[1].sync = { ...w[1].sync, state: "offline" };
			const v = verdictOf(w, allowedOnly, [gap(w[0], 90, 88)]);
			expect(v.failing.map((c) => c.id)).toEqual(["evidence-gaps", "offline"]);
			expect(v.sub).toBe(
				"No observed traffic would be dropped, but evidence is missing from part of this range and not every workload in scope is simulating on its latest policy.",
			);
		});

		it("fail when the read was truncated, even with nothing returned in range", () => {
			const v = verdictOf(ready(), allowedOnly, [], true);
			expect(v.failing.map((c) => c.id)).toEqual(["evidence-gaps"]);
		});

		it("take over from the dropped-records note for the workloads they place in range", () => {
			const w = ready();
			w[0].health = { ...w[0].health, dropped_flow_records: 50 };
			w[2].hostname = "checkout-prod-31";
			w[2].health = { ...w[2].health, dropped_flow_records: 212 };
			const v = verdictOf(w, allowedOnly, [
				gap(w[0], 90, 88, { kind: "buffer_overflow", source: null, count: 50 }),
			]);
			expect(v.caveats).toContain(
				"checkout-prod-31 dropped 212 flow records — its verdict may be incomplete",
			);
			expect(v.caveats.some((c) => c.startsWith("checkout-01 dropped"))).toBe(
				false,
			);
		});
	});

	it("prints spans in their two largest units", () => {
		expect(duration(14 * 86_400_000)).toBe("14d");
		expect(duration(65 * 60_000)).toBe("1h 5m");
		expect(duration(12 * 60_000)).toBe("12m");
		expect(duration(26 * 3_600_000)).toBe("1d 2h");
	});
});

describe("promotion", () => {
	const w = ready(3);
	const vis = workload({ hostname: "checkout-41", mode: "visibility" });
	const degraded = workload({
		hostname: "checkout-prod-07",
		mode: "simulation",
		sync: { state: "degraded", applied_version: 41, latest_version: 42 },
	});
	const done = workload({ hostname: "checkout-99", mode: "enforced" });
	const refs = [...w, vis, degraded, done].map((x) => ({
		id: x.id,
		hostname: x.hostname,
		labels: x.labels,
	}));

	it("includes what simulated on its latest policy and never offers visibility", () => {
		const gone = { id: "w-gone", hostname: "gone-01", labels: {} };
		const p = partition([...refs, gone], [...w, vis, degraded, done]);
		expect(p.included.map((x) => x.hostname)).toEqual([
			"checkout-01",
			"checkout-02",
			"checkout-03",
		]);
		expect(p.visibility.map((x) => x.hostname)).toEqual(["checkout-41"]);
		expect(p.unsynced).toEqual([{ workload: degraded, issue: "degraded" }]);
		expect(p.enforced.map((x) => x.hostname)).toEqual(["checkout-99"]);
		expect(p.unlisted).toEqual([gone]);
	});

	it("submits the included workloads and only the opted-in unsynced ones", () => {
		const p = partition(refs, [...w, vis, degraded, done]);
		expect(promotionIds(p, new Set())).toEqual(w.map((x) => x.id));
		expect(promotionIds(p, new Set([degraded.id, vis.id]))).toEqual([
			...w.map((x) => x.id),
			degraded.id,
		]);
	});
});
