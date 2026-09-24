// Instants and counts as the screens print them: compact spans in mono
// (20s, 4m, 3h, 2d), relative to now, and counts with digit grouping.

const second = 1000;
const minute = 60 * second;
const hour = 60 * minute;
const day = 24 * hour;

// span is a duration in its largest whole unit.
export function span(ms: number): string {
	const abs = Math.abs(ms);
	if (abs < minute) return `${Math.max(0, Math.floor(abs / second))}s`;
	if (abs < hour) return `${Math.floor(abs / minute)}m`;
	if (abs < day) return `${Math.floor(abs / hour)}h`;
	return `${Math.floor(abs / day)}d`;
}

// since is how long ago an instant was, as a bare span.
export function since(iso: string, now = Date.now()): string {
	return span(now - Date.parse(iso));
}

// ago is an instant in the past, as "4m ago".
export function ago(iso: string, now = Date.now()): string {
	return `${since(iso, now)} ago`;
}

// relative is an instant on either side of now: "in 19d" or "9d ago".
export function relative(iso: string, now = Date.now()): string {
	const d = Date.parse(iso) - now;
	return d >= 0 ? `in ${span(d)}` : `${span(d)} ago`;
}

// isPast says whether an instant has passed.
export function isPast(iso: string, now = Date.now()): boolean {
	return Date.parse(iso) <= now;
}

const grouping = new Intl.NumberFormat("en-US");

// count is an integer with digit grouping: 22,910.
export function count(n: number): string {
	return grouping.format(n);
}

// compact is a large count shortened past five digits: 1,190 or 435k.
export function compact(n: number): string {
	if (n < 10_000) return count(n);
	if (n < 1_000_000) return `${Math.round(n / 1000)}k`;
	return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, "")}M`;
}

// labelPairs is a label map as its `key=value` pairs, in key order.
export function labelPairs(labels: Record<string, string>): [string, string][] {
	return Object.entries(labels).sort(([a], [b]) => a.localeCompare(b));
}

// shortId is the first group of a UUID, enough to tell rules apart in a
// table: 3f2a91c0.
export function shortId(id: string): string {
	return id.split("-")[0] ?? id;
}
