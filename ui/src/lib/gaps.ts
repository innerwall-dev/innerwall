import type { EvidenceGap } from "@/api/schema";

// Evidence gaps are intervals [from, to) in which a workload's agent knows
// its flow evidence is incomplete: the kernel dropped events, a source was
// down, the agent's buffer dropped windows, or the connection table was cut
// at its bound. What was lost cannot be recovered; the interval bounds it.
// These helpers are the one reading of a gap the screens share.

// intersects is the read's own test: a gap meets [from, to) when it starts
// before the range ends and ends after it starts; an instantaneous gap
// when its instant lies in the range.
export function intersects(g: EvidenceGap, from: string, to: string): boolean {
	const gf = Date.parse(g.from);
	const gt = Date.parse(g.to);
	const f = Date.parse(from);
	const t = Date.parse(to);
	return gf < t && (gt > f || (gf === gt && gf >= f));
}

export function gapsIn(
	gaps: readonly EvidenceGap[],
	from: string,
	to: string,
): EvidenceGap[] {
	return gaps.filter((g) => intersects(g, from, to));
}

// span is the earliest start and latest end of gaps, clamped to the range
// they were read for: what a verdict over that range is missing.
export function span(
	gaps: readonly EvidenceGap[],
	from: string,
	to: string,
): { from: string; to: string } | null {
	if (gaps.length === 0) return null;
	const f = Date.parse(from);
	const t = Date.parse(to);
	let a = Number.POSITIVE_INFINITY;
	let b = Number.NEGATIVE_INFINITY;
	for (const g of gaps) {
		a = Math.min(a, Math.max(Date.parse(g.from), f));
		b = Math.max(b, Math.min(Date.parse(g.to), t));
	}
	return {
		from: new Date(a).toISOString(),
		to: new Date(Math.max(a, b)).toISOString(),
	};
}

const utc = new Intl.DateTimeFormat("en-GB", {
	month: "2-digit",
	day: "2-digit",
	hour: "2-digit",
	minute: "2-digit",
	second: "2-digit",
	hourCycle: "h23",
	timeZone: "UTC",
});

function parts(iso: string): Record<string, string> {
	return Object.fromEntries(
		utc.formatToParts(new Date(iso)).map((p) => [p.type, p.value]),
	);
}

// between names an interval in UTC to the minute, "13:02 and 13:04 UTC"
// within a day and with dates across days; to the second when the two
// would otherwise read the same.
export function between(from: string, to: string): string {
	const a = parts(from);
	const b = parts(to);
	const sameDay = a.month === b.month && a.day === b.day;
	const sameMinute = sameDay && a.hour === b.hour && a.minute === b.minute;
	const at = (p: Record<string, string>) => {
		const time = sameMinute
			? `${p.hour}:${p.minute}:${p.second}`
			: `${p.hour}:${p.minute}`;
		return sameDay ? time : `${p.month}-${p.day} ${time}`;
	};
	return `${at(a)} and ${at(b)} UTC`;
}

// kindText says what happened, in the operator's words.
export function kindText(kind: EvidenceGap["kind"]): string {
	switch (kind) {
		case "source_overrun":
			return "the kernel dropped events";
		case "source_restart":
			return "a flow source was down";
		case "buffer_overflow":
			return "the agent dropped buffered windows";
		case "dump_truncated":
			return "the connection table was cut at its bound";
		case "window_overflow":
			return "the agent's flow window was full";
	}
}

// workloadsWith is the distinct workloads gaps name, in first-seen order.
export function workloadsWith(
	gaps: readonly EvidenceGap[],
): EvidenceGap["workload"][] {
	const seen = new Map<string, EvidenceGap["workload"]>();
	for (const g of gaps) {
		if (!seen.has(g.workload.id)) seen.set(g.workload.id, g.workload);
	}
	return [...seen.values()];
}
