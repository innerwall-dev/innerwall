import { describe, expect, it } from "vitest";
import type { EvidenceGap } from "@/api/schema";
import { kindsOf, lost, stretches } from "./gaps";

const from = "2026-10-04T18:00:00Z";
const to = "2026-10-04T20:00:00Z";

function g(
	a: string,
	b: string,
	kind: EvidenceGap["kind"] = "agent_silent",
	workload = "w-1",
): EvidenceGap {
	return {
		workload: { id: workload, hostname: workload, labels: {} },
		kind,
		source: null,
		from: `2026-10-04T${a}Z`,
		to: `2026-10-04T${b}Z`,
		count: null,
	};
}

const iso = (t: string) => new Date(`2026-10-04T${t}Z`).toISOString();

describe("stretches", () => {
	it("is nothing for no gaps", () => {
		expect(stretches([], from, to)).toEqual([]);
		expect(lost([])).toBe(0);
	});

	it("keeps disjoint gaps apart, earliest first", () => {
		const runs = stretches(
			[g("19:00:00", "19:02:00"), g("18:00:00", "18:02:00")],
			from,
			to,
		);
		expect(runs).toEqual([
			{ from: iso("18:00:00"), to: iso("18:02:00") },
			{ from: iso("19:00:00"), to: iso("19:02:00") },
		]);
		expect(lost(runs)).toBe(4 * 60_000);
	});

	it("merges overlapping, nested, and touching gaps across kinds and workloads", () => {
		const runs = stretches(
			[
				g("18:10:00", "18:20:00"),
				g("18:15:00", "18:25:00", "table_removed"),
				g("18:12:00", "18:14:00", "source_restart", "w-2"),
				g("18:25:00", "18:30:00", "agent_silent", "w-3"),
				g("18:40:00", "18:41:00"),
			],
			from,
			to,
		);
		expect(runs).toEqual([
			{ from: iso("18:10:00"), to: iso("18:30:00") },
			{ from: iso("18:40:00"), to: iso("18:41:00") },
		]);
		expect(lost(runs)).toBe(21 * 60_000);
	});

	it("clamps to the range, so the total never exceeds it", () => {
		const runs = stretches(
			[g("17:00:00", "18:05:00"), g("19:58:00", "21:00:00")],
			from,
			to,
		);
		expect(runs).toEqual([
			{ from: iso("18:00:00"), to: iso("18:05:00") },
			{ from: iso("19:58:00"), to: iso("20:00:00") },
		]);
		expect(lost(runs)).toBe(7 * 60_000);
		const whole = stretches([g("17:00:00", "21:00:00")], from, to);
		expect(lost(whole)).toBe(Date.parse(to) - Date.parse(from));
	});

	it("keeps an instantaneous gap as an instant", () => {
		const runs = stretches([g("18:30:10", "18:30:10")], from, to);
		expect(runs).toEqual([{ from: iso("18:30:10"), to: iso("18:30:10") }]);
		expect(lost(runs)).toBe(0);
	});
});

describe("kindsOf", () => {
	it("is each kind once, in the order it first occurs", () => {
		expect(
			kindsOf([
				g("19:00:00", "19:01:00", "buffer_overflow"),
				g("18:30:00", "18:31:00"),
				g("18:10:00", "18:11:00", "source_restart"),
				g("18:50:00", "18:51:00", "source_restart"),
			]),
		).toEqual(["source_restart", "agent_silent", "buffer_overflow"]);
	});
});
