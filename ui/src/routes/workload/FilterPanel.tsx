import { type ReactNode, useId, useState } from "react";
import { Icon } from "@/components/Icon";
import { Button } from "@/components/ui/button";
import { count } from "@/lib/format";
import { cn } from "@/lib/utils";
import {
	isScreenRange,
	type ScreenRange,
	type ScreenRangeKey,
	screenRanges,
} from "./range";

// A filter group's values, each with what it matches among the rows the
// screen holds: an optional glyph (decisions), the label, an optional
// annotation ("not managed"), whether the label is set in mono (ports,
// labels, rules), and the count.
export interface FilterValue {
	key: string;
	label: string;
	glyph?: ReactNode;
	annotation?: string;
	mono?: boolean;
	count: number;
}

export interface FilterGroup {
	key: string;
	title: string;
	values: FilterValue[];
}

// Filters are the panel's choices: per group, the values checked. A row
// matches when, in every group with a value checked, it holds one of
// them.
export type Filters = Record<string, ReadonlySet<string>>;

export function toggled(
	filters: Filters,
	group: string,
	value: string,
): Filters {
	const next = new Set(filters[group] ?? []);
	if (next.has(value)) next.delete(value);
	else next.add(value);
	return { ...filters, [group]: next };
}

export function active(filters: Filters): number {
	return Object.values(filters).reduce((n, s) => n + s.size, 0);
}

// FilterPanel is the list pages' filter column: 280px on the canvas
// behind a right hairline, scrolling on its own. Its header resets every
// choice; the search narrows the rows by their text; the time range
// resets the screen's one range; and each group is a collapsible titled
// box of checkbox rows with right-aligned mono counts.
export function FilterPanel({
	label,
	search,
	onSearch,
	placeholder,
	range,
	onRange,
	groups,
	filters,
	onFilters,
}: {
	label: string;
	search: string;
	onSearch: (q: string) => void;
	placeholder: string;
	range: ScreenRange;
	onRange: (key: ScreenRangeKey) => void;
	groups: FilterGroup[];
	filters: Filters;
	onFilters: (f: Filters) => void;
}) {
	const searchId = useId();
	return (
		<aside
			aria-label={label}
			className="mb-6 flex max-h-[calc(100%-24px)] w-[232px] shrink-0 flex-col self-start overflow-auto rounded-panel border border-overlay bg-card"
		>
			<div className="flex flex-col gap-2.5 px-4 pt-3.5 pb-4">
				<div className="flex items-center justify-between">
					<span className="type-dense-strong text-primary">Filters</span>
					<Button
						variant="ghost"
						size="sm"
						disabled={active(filters) === 0 && search === ""}
						onClick={() => {
							onFilters({});
							onSearch("");
						}}
					>
						<Icon name="x" />
						Reset
					</Button>
				</div>
				<label
					htmlFor={searchId}
					className="flex h-control items-center gap-2 rounded-control border border-strong bg-field-sunk px-2.5 text-tertiary hover:border-control-hover has-[:focus-visible]:focus-ring"
				>
					<Icon name="search" />
					<input
						id={searchId}
						type="search"
						aria-label={placeholder}
						placeholder={placeholder}
						value={search}
						onChange={(e) => onSearch(e.target.value)}
						className="min-w-0 flex-1 bg-transparent type-dense text-primary outline-none placeholder:text-disabled"
					/>
				</label>
				<label className="relative flex h-control items-center gap-1.5 rounded-control border border-strong bg-field-sunk pr-2 pl-2.5 type-control text-secondary hover:border-control-hover has-[:focus-visible]:focus-ring">
					<Icon name="clock" />
					<span>last</span>
					<select
						aria-label="Time range"
						value={range.key}
						onChange={(e) => {
							if (isScreenRange(e.target.value)) onRange(e.target.value);
						}}
						className="min-w-0 flex-1 cursor-pointer appearance-none bg-transparent pr-5 type-mono-sm text-primary focus:outline-none"
					>
						{Object.keys(screenRanges).map((r) => (
							<option key={r} value={r}>
								{r}
							</option>
						))}
					</select>
					<Icon
						name="chevron-down"
						className="pointer-events-none absolute right-2"
					/>
				</label>
			</div>
			{groups.map((g) => (
				<Group
					key={g.key}
					group={g}
					chosen={filters[g.key] ?? new Set()}
					onToggle={(v) => onFilters(toggled(filters, g.key, v))}
				/>
			))}
		</aside>
	);
}

// Group is one titled section on the subtle rule: a 28px title row (the
// mono eyebrow) with the active count and the chevron (turned while
// collapsed), then its checkbox rows.
function Group({
	group,
	chosen,
	onToggle,
}: {
	group: FilterGroup;
	chosen: ReadonlySet<string>;
	onToggle: (value: string) => void;
}) {
	const [open, setOpen] = useState(true);
	const listId = useId();
	return (
		<fieldset className="m-0 min-w-0 border-0 border-t border-hairline px-2 pt-2 pb-2.5">
			<legend className="sr-only">{group.title}</legend>
			<button
				type="button"
				aria-expanded={open}
				aria-controls={listId}
				onClick={() => setOpen(!open)}
				className="flex h-7 w-full cursor-pointer items-center gap-2 rounded-control px-2 text-left type-meta-label text-tertiary hover:bg-wash-hover hover:text-primary focus-visible:focus-ring"
			>
				<span className="min-w-0 flex-1 truncate">{group.title}</span>
				{chosen.size > 0 ? (
					<span className="inline-flex h-[18px] items-center rounded-full border border-overlay px-1.5 type-mono-xs tracking-normal text-secondary normal-case">
						× {chosen.size}
					</span>
				) : null}
				<Icon
					name="chevron-down"
					className={cn("transition-transform", !open && "-rotate-90")}
				/>
			</button>
			<div id={listId} hidden={!open} className="flex flex-col">
				{group.values.map((v) => (
					<label
						key={v.key}
						className={cn(
							"flex h-7 cursor-pointer items-center gap-2 rounded-control px-2 hover:bg-wash-hover",
							v.count === 0 ? "text-tertiary" : "text-primary",
						)}
					>
						<input
							type="checkbox"
							checked={chosen.has(v.key)}
							onChange={() => onToggle(v.key)}
						/>
						{v.glyph}
						<span
							className={cn(
								"min-w-0 truncate",
								v.mono ? "type-mono-sm" : "type-dense",
							)}
							title={v.label}
						>
							{v.label}
						</span>
						{v.annotation ? (
							<span className="shrink-0 type-caption text-tertiary">
								{v.annotation}
							</span>
						) : null}
						<span className="ml-auto shrink-0 pl-1 type-caption text-tertiary tabular-nums">
							{count(v.count)}
						</span>
					</label>
				))}
			</div>
		</fieldset>
	);
}
