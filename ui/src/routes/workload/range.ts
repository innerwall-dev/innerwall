// A workload's screen reads every flow, gap, and pairing over one range,
// fixed when it is chosen, so that a page and its cursor, the tab counts,
// the state chips, and the listening services' pairing all describe the
// same windows. The filter panel's time range resets it for the whole
// screen; 14 days is where a screen opens.

export const screenRanges = {
	"1h": { ms: 3_600_000, text: "1 hour" },
	"6h": { ms: 6 * 3_600_000, text: "6 hours" },
	"24h": { ms: 24 * 3_600_000, text: "24 hours" },
	"7d": { ms: 7 * 86_400_000, text: "7 days" },
	"14d": { ms: 14 * 86_400_000, text: "14 days" },
	"30d": { ms: 30 * 86_400_000, text: "30 days" },
} as const;

export type ScreenRangeKey = keyof typeof screenRanges;

export const defaultScreenRange: ScreenRangeKey = "14d";

// ScreenRange is the range a screen reads: its key, the instant it
// starts (fixed when chosen), and its length in words ("14 days") and
// short ("14d").
export interface ScreenRange {
	key: ScreenRangeKey;
	from: string;
	text: string;
	short: string;
}

export function screenRange(
	key: ScreenRangeKey,
	now = Date.now(),
): ScreenRange {
	const r = screenRanges[key];
	return {
		key,
		from: new Date(now - r.ms).toISOString(),
		text: r.text,
		short: key,
	};
}

export function isScreenRange(s: string): s is ScreenRangeKey {
	return Object.hasOwn(screenRanges, s);
}
