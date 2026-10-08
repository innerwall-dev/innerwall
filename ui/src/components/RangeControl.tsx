import { cn } from "@/lib/utils";

// The time ranges the rollup screens offer. Stored windows are what a
// rollup counts, so a range covers the windows that lie inside it and its
// honest extent is the rollup's effective bounds, not the range asked.
export const ranges = {
	"1h": 3_600_000,
	"6h": 6 * 3_600_000,
	"24h": 24 * 3_600_000,
	"7d": 7 * 24 * 3_600_000,
	"30d": 30 * 24 * 3_600_000,
} as const;
export type RangeKey = keyof typeof ranges;
export const defaultRange: RangeKey = "24h";

export function isRange(s: string | null): s is RangeKey {
	return s !== null && Object.hasOwn(ranges, s);
}

const utc = new Intl.DateTimeFormat("en-GB", {
	month: "2-digit",
	day: "2-digit",
	hour: "2-digit",
	minute: "2-digit",
	hourCycle: "h23",
	timeZone: "UTC",
});

function parts(iso: string): Record<string, string> {
	return Object.fromEntries(
		utc.formatToParts(new Date(iso)).map((p) => [p.type, p.value]),
	);
}

// extent is the span of windows a rollup counted, in UTC: "18:20 → 19:25
// UTC" within one day, with the dates when it spans more than one.
export function extent(from: string, to: string): string {
	const a = parts(from);
	const b = parts(to);
	const sameDay = a.month === b.month && a.day === b.day;
	const at = (p: Record<string, string>) =>
		sameDay
			? `${p.hour}:${p.minute}`
			: `${p.month}-${p.day} ${p.hour}:${p.minute}`;
	return `windows ${at(a)} → ${at(b)} UTC`;
}

const chip =
	"rounded-sm border border-strong px-[9px] py-1 font-mono text-[12px] text-secondary";

// RangeControl picks the range and says what it covers. A rollup counts
// whole stored windows inside the range, so the extent shown is the
// first and last window actually counted, never the range asked.
export function RangeControl({
	range,
	effectiveFrom,
	effectiveTo,
	onChange,
	testId,
	what,
}: {
	range: RangeKey;
	effectiveFrom: string | null;
	effectiveTo: string | null;
	onChange: (r: RangeKey) => void;
	testId: string;
	// what the counts are of, for the explanation on hover.
	what: string;
}) {
	return (
		<div className="flex items-center gap-2">
			<label className={cn(chip, "flex items-center gap-1")}>
				<span className="font-sans text-secondary">last</span>
				<select
					aria-label="Time range"
					value={range}
					onChange={(ev) => onChange(ev.target.value as RangeKey)}
					className="cursor-pointer appearance-none bg-transparent font-mono text-[12px] text-secondary focus:outline-none"
				>
					{Object.keys(ranges).map((r) => (
						<option key={r} value={r}>
							{r}
						</option>
					))}
				</select>
			</label>
			<span
				className="font-mono text-[11px] text-tertiary"
				data-testid={testId}
				title={`Flows are stored in reporting windows; ${what} count the windows that lie wholly inside the range.`}
			>
				{effectiveFrom && effectiveTo
					? extent(effectiveFrom, effectiveTo)
					: "no windows in range"}
			</span>
		</div>
	);
}
