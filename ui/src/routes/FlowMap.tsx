import { type ReactNode, useCallback, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { Centered, EmptyState } from "@/components/EmptyState";
import { Icon } from "@/components/Icon";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { RangeControl } from "@/components/RangeControl";
import { SeverityNote } from "@/components/StatusGlyph";
import { Button } from "@/components/ui/button";
import { count, headline } from "@/lib/format";
import { between, kindText } from "@/lib/gaps";
import { labelFormHint, shownLabelText } from "@/lib/labels";
import { useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { labelRequirements } from "./fleet/WorkloadList";
import { Drawer } from "./map/Drawer";
import {
	defaultRange,
	isRange,
	loadMap,
	type MapData,
	type RangeKey,
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
			buildModel(
				data.rollups,
				data.workloads,
				data.addressGroups,
				groupKey,
				data.gaps.gaps,
			),
		[data, groupKey],
	);
	const layout = useMemo(() => layoutModel(model), [model]);
	const selection = resolveSelection(model, parseSelection(params.get("sel")));
	const rangeControl = <MapRange model={model} range={range} update={update} />;
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

// The toolbar's controls, control-sm tall: the grouping picker and the
// scope's requirements on the default hairline, the draft requirement
// on an input's stronger one.
const control =
	"relative flex h-control-sm items-center gap-1.5 rounded-md border border-default bg-app px-2 type-caption text-secondary hover:bg-hover has-[:focus-visible]:focus-ring";
const requirement =
	"flex h-control-sm items-center gap-1 rounded-md border border-default bg-subtle pr-1 pl-2 type-mono-sm text-primary";

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
	const [invalid, setInvalid] = useState<string | null>(null);
	const options = keys.includes(groupKey) ? keys : [groupKey, ...keys];

	function addRequirement() {
		const parsed = labelRequirements(draft);
		if (!parsed.ok || parsed.requirements.length === 0) {
			setInvalid(parsed.ok ? labelFormHint : parsed.error);
			return;
		}
		update((p) => {
			for (const req of parsed.requirements) {
				if (!p.getAll("label").includes(req)) p.append("label", req);
			}
		});
		setDraft("");
		setInvalid(null);
		setAdding(false);
	}

	return (
		<div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-default px-6 py-3">
			<div className="flex flex-wrap items-center gap-2">
				<label className={control}>
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
						className="cursor-pointer appearance-none bg-transparent pr-5 type-mono-sm text-primary focus:outline-none"
					>
						{options.map((k) => (
							<option key={k} value={k}>
								{k}
							</option>
						))}
					</select>
					<Icon
						name="chevron-down"
						className="pointer-events-none absolute right-2"
					/>
				</label>
				{scope.map((req) => {
					const i = req.indexOf("=");
					return (
						<span key={req} className={requirement}>
							<span className="text-tertiary">
								{shownLabelText(req.slice(0, i), "key")} =
							</span>{" "}
							{shownLabelText(req.slice(i + 1), "value")}
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
								className="flex size-5 cursor-pointer items-center justify-center rounded-sm hover:bg-hover"
							>
								<Icon name="x" className="size-3.5" />
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
						className="flex items-center gap-2"
					>
						<input
							aria-label="Label requirement"
							placeholder="key=value"
							value={draft}
							ref={(el) => el?.focus()}
							onChange={(ev) => {
								setDraft(ev.target.value);
								setInvalid(null);
							}}
							onKeyDown={(ev) => {
								if (ev.key === "Escape") setAdding(false);
							}}
							className={cn(
								"h-control-sm w-[160px] rounded-md border bg-app px-2 type-mono-sm text-primary placeholder:text-tertiary",
								invalid ? "border-status-critical-fg" : "border-strong",
							)}
						/>
						{invalid ? (
							<SeverityNote level="error" className="type-caption">
								{invalid}
							</SeverityNote>
						) : null}
					</form>
				) : (
					<Button variant="ghost" size="sm" onClick={() => setAdding(true)}>
						<Icon name="plus" />
						filter
					</Button>
				)}
			</div>
			<fieldset className="m-0 ml-2 flex h-control-sm items-center gap-0.5 rounded-md border border-default bg-subtle p-0.5">
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
							"flex h-full cursor-pointer items-center rounded-sm px-2.5 type-caption",
							take === t
								? "bg-active font-medium text-primary"
								: "text-secondary hover:bg-hover hover:text-primary",
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

// MapRange is the shared range control over the map's own extent.
function MapRange({
	model,
	range,
	update,
}: {
	model: MapModel;
	range: RangeKey;
	update: (change: (p: URLSearchParams) => void) => void;
}) {
	return (
		<RangeControl
			range={range}
			effectiveFrom={model.effectiveFrom}
			effectiveTo={model.effectiveTo}
			testId="map-extent"
			what="the map's counts"
			onChange={(r) =>
				update((p) => {
					if (r === defaultRange) p.delete("range");
					else p.set("range", r);
				})
			}
		/>
	);
}

function Totals({ model }: { model: MapModel }) {
	const dropped = model.dropped.length;
	const gapped = model.gapped.length;
	return (
		<div className="ml-auto flex flex-wrap items-center gap-x-4 gap-y-1 type-caption text-secondary">
			<span>
				<span className="type-mono-sm text-primary">
					{headline(model.connections)}
				</span>{" "}
				connections
			</span>
			<span>
				<span className="type-mono-sm text-primary">
					{count(model.reporting)}
					{model.truncated ? "+" : ""}
				</span>{" "}
				{model.reporting === 1 ? "workload" : "workloads"} reporting
			</span>
			{gapped > 0 ? (
				<SeverityNote level="alert" role="status">
					<span
						title={model.gaps
							.map(
								(g) =>
									`${g.workload.hostname}: ${kindText(g.kind)} between ${between(g.from, g.to)}`,
							)
							.join("\n")}
					>
						{count(gapped)} {gapped === 1 ? "workload" : "workloads"} lost
						evidence in this range — map is incomplete
					</span>
				</SeverityNote>
			) : null}
			{dropped > 0 ? (
				<SeverityNote level="alert" role="status">
					<span title={model.dropped.map((w) => w.hostname).join(", ")}>
						{count(dropped)} {dropped === 1 ? "workload" : "workloads"} dropped
						flow records — map may be incomplete
					</span>
				</SeverityNote>
			) : null}
			{model.truncated ? (
				<SeverityNote level="alert" role="status">
					showing the busiest {count(rollupLimit)} pairs per decision — map is
					incomplete
				</SeverityNote>
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
					? `No flows to or from workloads matching ${scope.join(" and ")} were stored in the last ${range}. `
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
