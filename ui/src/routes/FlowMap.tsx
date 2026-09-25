import { type ReactNode, useCallback, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { Centered, EmptyState } from "@/components/EmptyState";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { Button } from "@/components/ui/button";
import { count, headline } from "@/lib/format";
import { useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { parseRequirement } from "./fleet/WorkloadList";
import { Drawer } from "./map/Drawer";
import {
	defaultRange,
	isRange,
	loadMap,
	type MapData,
	type RangeKey,
	ranges,
	rollupLimit,
} from "./map/data";
import { Graph } from "./map/Graph";
import { layoutModel } from "./map/layout";
import { Matrix } from "./map/Matrix";
import {
	buildModel,
	defaultKey,
	formatSelection,
	groupingKeys,
	type MapModel,
	parseSelection,
	resolveSelection,
	type Selection,
} from "./map/model";

type Take = "graph" | "matrix";

// FlowMap is the live dependency map: inbound traffic between label
// groups over a time range, as a graph or as a matrix. What the operator
// chose (the take, the grouping key, the scope, the range, and the
// selection) lives in the address, so switching takes keeps the
// selection and a view can be linked to.
export function FlowMap() {
	const [params, setParams] = useSearchParams();
	const take: Take = params.get("take") === "matrix" ? "matrix" : "graph";
	const scope = useMemo(() => params.getAll("label"), [params]);
	const scopeKey = scope.join("&");
	const rangeParam = params.get("range");
	const range: RangeKey = isRange(rangeParam) ? rangeParam : defaultRange;

	const update = useCallback(
		(change: (p: URLSearchParams) => void) => {
			setParams(
				(prev) => {
					const next = new URLSearchParams(prev);
					change(next);
					return next;
				},
				{ replace: true },
			);
		},
		[setParams],
	);

	// The read answers to the scope's requirements, named by scopeKey: the
	// array itself is rebuilt whenever the address changes.
	const { resource, reload } = useResource(
		() => loadMap(scope, range),
		[scopeKey, range],
	);

	if (resource.status === "loading") {
		return (
			<div className="px-6">
				<LoadingRow what="the flow map" />
			</div>
		);
	}
	if (resource.status === "error") {
		return (
			<div className="px-6">
				<ProblemNotice
					what="the flow map"
					error={resource.error}
					onRetry={reload}
				/>
			</div>
		);
	}
	const data = resource.data;
	const empty =
		data.workloads.length === 0 &&
		Object.values(data.rollups).every((r) => (r?.totals.flow_count ?? 0) === 0);
	if (empty && scope.length === 0) return <FreshMap />;
	return (
		<MapView
			data={data}
			take={take}
			scope={scope}
			range={range}
			params={params}
			update={update}
		/>
	);
}

function MapView({
	data,
	take,
	scope,
	range,
	params,
	update,
}: {
	data: MapData;
	take: Take;
	scope: string[];
	range: RangeKey;
	params: URLSearchParams;
	update: (change: (p: URLSearchParams) => void) => void;
}) {
	const keys = useMemo(
		() => groupingKeys(data.rollups, data.workloads),
		[data],
	);
	const groupKey = params.get("group_by") || defaultKey(data.workloads, keys);
	const model = useMemo(
		() =>
			buildModel(data.rollups, data.workloads, data.addressGroups, groupKey),
		[data, groupKey],
	);
	const layout = useMemo(() => layoutModel(model), [model]);
	const selection = resolveSelection(model, parseSelection(params.get("sel")));
	const rangeControl = (
		<RangeControl model={model} range={range} update={update} />
	);
	const select = useCallback(
		(s: Selection) =>
			update((p) => {
				const v = formatSelection(s);
				if (v) p.set("sel", v);
				else p.delete("sel");
			}),
		[update],
	);

	return (
		<div className="flex min-h-0 flex-1 flex-col overflow-hidden">
			<Toolbar
				model={model}
				take={take}
				groupKey={groupKey}
				keys={keys}
				scope={scope}
				update={update}
			/>
			<div className="flex min-h-0 flex-1 overflow-hidden">
				<div className="relative flex min-w-0 flex-1 flex-col overflow-auto">
					{model.edges.length === 0 ? (
						<NoFlows scope={scope} range={range} control={rangeControl} />
					) : take === "graph" ? (
						<Graph
							model={model}
							layout={layout}
							selection={selection}
							onSelect={select}
							range={rangeControl}
						/>
					) : (
						<Matrix
							model={model}
							layout={layout}
							selection={selection}
							onSelect={select}
							range={rangeControl}
						/>
					)}
				</div>
				{selection ? (
					<Drawer
						model={model}
						workloads={data.workloads}
						selection={selection}
						groupKey={groupKey}
						scope={scope}
						from={data.from}
						to={data.to}
						onSelect={select}
					/>
				) : null}
			</div>
		</div>
	);
}

const chip =
	"rounded-chip border border-input px-[9px] py-1 font-mono text-[12px] text-foreground-secondary";

function Toolbar({
	model,
	take,
	groupKey,
	keys,
	scope,
	update,
}: {
	model: MapModel;
	take: Take;
	groupKey: string;
	keys: string[];
	scope: string[];
	update: (change: (p: URLSearchParams) => void) => void;
}) {
	const [adding, setAdding] = useState(false);
	const [draft, setDraft] = useState("");
	const [invalid, setInvalid] = useState(false);
	const options = keys.includes(groupKey) ? keys : [groupKey, ...keys];

	function addRequirement() {
		const req = parseRequirement(draft);
		if (!req) {
			setInvalid(true);
			return;
		}
		update((p) => {
			if (!p.getAll("label").includes(req)) p.append("label", req);
		});
		setDraft("");
		setInvalid(false);
		setAdding(false);
	}

	return (
		<div className="flex shrink-0 flex-wrap items-center gap-2.5 border-b border-border px-6 pt-3.5 pb-3">
			<div className="flex flex-wrap items-center gap-1.5">
				<label className={cn(chip, "flex items-center gap-1")}>
					<span>group by:</span>
					<select
						aria-label="Group by"
						value={groupKey}
						onChange={(ev) =>
							update((p) => {
								p.set("group_by", ev.target.value);
								p.delete("sel");
							})
						}
						className="cursor-pointer appearance-none bg-transparent font-mono text-[12px] text-foreground-secondary focus:outline-none"
					>
						{options.map((k) => (
							<option key={k} value={k}>
								{k}
							</option>
						))}
					</select>
				</label>
				{scope.map((req) => {
					const i = req.indexOf("=");
					return (
						<span key={req} className={cn(chip, "flex items-center gap-1.5")}>
							{req.slice(0, i)} = {req.slice(i + 1)}
							<button
								type="button"
								aria-label={`Remove ${req}`}
								onClick={() =>
									update((p) => {
										const rest = p.getAll("label").filter((l) => l !== req);
										p.delete("label");
										for (const l of rest) p.append("label", l);
									})
								}
								className="cursor-pointer text-muted-foreground hover:text-foreground"
							>
								×
							</button>
						</span>
					);
				})}
				{adding ? (
					<form
						onSubmit={(ev) => {
							ev.preventDefault();
							addRequirement();
						}}
						className="flex items-center gap-1.5"
					>
						<input
							aria-label="Label requirement"
							placeholder="key=value"
							value={draft}
							ref={(el) => el?.focus()}
							onChange={(ev) => {
								setDraft(ev.target.value);
								setInvalid(false);
							}}
							onKeyDown={(ev) => {
								if (ev.key === "Escape") setAdding(false);
							}}
							className={cn(
								chip,
								"w-[140px] bg-transparent focus:border-ring focus:outline-none",
								invalid && "border-destructive",
							)}
						/>
						{invalid ? (
							<span className="text-[11px] text-destructive">
								A label is written key=value.
							</span>
						) : null}
					</form>
				) : (
					<button
						type="button"
						onClick={() => setAdding(true)}
						className="cursor-pointer rounded-chip border border-dashed border-input-strong px-[9px] py-1 text-[12px] text-muted-foreground hover:text-foreground"
					>
						+ filter
					</button>
				)}
			</div>
			<fieldset className="m-0 ml-2 flex overflow-hidden rounded border border-input p-0">
				<legend className="sr-only">Take</legend>
				{(["graph", "matrix"] as const).map((t) => (
					<button
						key={t}
						type="button"
						aria-pressed={take === t}
						onClick={() =>
							update((p) => {
								if (t === "graph") p.delete("take");
								else p.set("take", t);
							})
						}
						className={cn(
							"cursor-pointer px-3.5 py-[5px] text-[12.5px]",
							take === t
								? "bg-secondary text-foreground"
								: "text-foreground-tertiary hover:text-foreground",
						)}
					>
						{t === "graph" ? "Graph" : "Matrix"}
					</button>
				))}
			</fieldset>
			<Totals model={model} />
		</div>
	);
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

// RangeControl picks the range and says what it covers. The rollup
// counts whole stored windows inside the range, so the extent shown is
// the first and last window actually counted, never the range asked.
function RangeControl({
	model,
	range,
	update,
}: {
	model: MapModel;
	range: RangeKey;
	update: (change: (p: URLSearchParams) => void) => void;
}) {
	return (
		<div className="flex items-center gap-2">
			<label className={cn(chip, "flex items-center gap-1")}>
				<span className="font-sans text-foreground-tertiary">last</span>
				<select
					aria-label="Time range"
					value={range}
					onChange={(ev) =>
						update((p) => {
							if (ev.target.value === defaultRange) p.delete("range");
							else p.set("range", ev.target.value);
						})
					}
					className="cursor-pointer appearance-none bg-transparent font-mono text-[12px] text-foreground-secondary focus:outline-none"
				>
					{Object.keys(ranges).map((r) => (
						<option key={r} value={r}>
							{r}
						</option>
					))}
				</select>
			</label>
			<span
				className="font-mono text-[11px] text-muted-foreground"
				data-testid="map-extent"
				title="Flows are stored in reporting windows; the map counts the windows that lie wholly inside the range."
			>
				{model.effectiveFrom && model.effectiveTo
					? extent(model.effectiveFrom, model.effectiveTo)
					: "no windows in range"}
			</span>
		</div>
	);
}

function Totals({ model }: { model: MapModel }) {
	const dropped = model.dropped.length;
	return (
		<div className="ml-auto flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px] text-foreground-tertiary">
			<span>
				<span className="font-mono text-foreground">
					{headline(model.connections)}
				</span>{" "}
				connections
			</span>
			<span>
				<span className="font-mono text-foreground">
					{count(model.reporting)}
					{model.truncated ? "+" : ""}
				</span>{" "}
				{model.reporting === 1 ? "workload" : "workloads"} reporting
			</span>
			{dropped > 0 ? (
				<span
					role="status"
					className="text-status-degraded"
					title={model.dropped.map((w) => w.hostname).join(", ")}
				>
					▲ {count(dropped)} {dropped === 1 ? "workload" : "workloads"} dropped
					flow records — map may be incomplete
				</span>
			) : null}
			{model.truncated ? (
				<span role="status" className="text-status-degraded">
					▲ showing the busiest {count(rollupLimit)} pairs per decision — map is
					incomplete
				</span>
			) : null}
		</div>
	);
}

function NoFlows({
	scope,
	range,
	control,
}: {
	scope: string[];
	range: RangeKey;
	control: ReactNode;
}) {
	return (
		<Centered>
			<EmptyState
				title="No flows in this range"
				width={480}
				actions={<div className="self-start">{control}</div>}
			>
				{scope.length > 0
					? `No inbound flows into workloads matching ${scope.join(" and ")} were stored in the last ${range}. `
					: `No inbound flows were stored in the last ${range}. `}
				Widen the range, or remove a filter.
			</EmptyState>
		</Centered>
	);
}

// Fresh-install state of the flow map (design screen 18).
function FreshMap() {
	return (
		<Centered>
			<EmptyState
				title="No flows observed yet"
				width={480}
				actions={
					<Button asChild className="self-start">
						<Link to="/workloads">Open enrollment</Link>
					</Button>
				}
			>
				The map draws inbound traffic between label groups as agents report it.
				Enroll workloads and give them a few minutes in visibility mode.
			</EmptyState>
		</Centered>
	);
}
