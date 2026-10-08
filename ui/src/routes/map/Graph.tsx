import "@xyflow/react/dist/base.css";
import {
	BaseEdge,
	type Edge,
	EdgeLabelRenderer,
	type EdgeProps,
	getViewportForBounds,
	Handle,
	type Node,
	type NodeProps,
	Position,
	ReactFlow,
	useReactFlow,
	useStore,
} from "@xyflow/react";
import { memo, type ReactNode, useEffect, useMemo } from "react";
import { modes, verdicts } from "@/components/fleet/status";
import { Icon } from "@/components/Icon";
import { SeverityNote, StatusGlyph } from "@/components/StatusGlyph";
import { short } from "@/lib/format";
import { cn } from "@/lib/utils";
import { type EdgeShape, shapeEdges } from "./geometry";
import { type Layout, nodeHeight, nodeWidth } from "./layout";
import {
	edgeCap,
	edgeColor,
	edgeDash,
	edgeEmphasis,
	type MapEdge,
	type MapModel,
	type MapNode,
	modeSummary,
	nodeDimmed,
	precedence,
	type Selection,
	strokeWidth,
} from "./model";

// The graph take: label groups as nodes, laid out in layers, and one
// edge per source and destination group, drawn by the decision that
// takes precedence among its pairs. Rendering is the graph library's
// (pan, zoom, and only the elements in view are mounted); the look is
// the tokens' throughout.

type GroupData = {
	node: MapNode;
	dimmed: boolean;
	selected: boolean;
	onSelect: (s: Selection) => void;
};
type FlowData = {
	edge: MapEdge;
	// The groups' titles, as the edge's accessible name reads them.
	from: string;
	to: string;
	shape: EdgeShape;
	// The stroke width, a calc() over the edge-width tokens.
	width: string;
	selected: boolean;
	dimmed: boolean;
	onSelect: (s: Selection) => void;
};
type GroupNode = Node<GroupData, "group">;
type FlowEdge = Edge<FlowData, "flow">;

const nodeTypes = { group: memo(GroupNodeView) };
const edgeTypes = { flow: memo(FlowEdgeView) };

export function Graph({
	model,
	layout,
	selection,
	onSelect,
	range,
}: {
	model: MapModel;
	layout: Layout;
	selection: Selection;
	onSelect: (s: Selection) => void;
	// range is the time-range control, floated over the canvas.
	range: ReactNode;
}) {
	const nodes = useMemo<GroupNode[]>(
		() =>
			model.nodes.map((n) => ({
				id: n.id,
				type: "group",
				position: layout.positions.get(n.id) ?? { x: 0, y: 0 },
				width: nodeWidth,
				height: nodeHeight,
				// Nodes are neither dragged nor selected by the library, which
				// would leave them deaf to the pointer; each is a button that
				// selects itself.
				style: { pointerEvents: "all" },
				// The handles' places are known, so edges draw without first
				// measuring the nodes.
				handles: [
					{
						type: "target",
						position: Position.Left,
						x: 0,
						y: nodeHeight / 2,
						width: 1,
						height: 1,
					},
					{
						type: "source",
						position: Position.Right,
						x: nodeWidth - 1,
						y: nodeHeight / 2,
						width: 1,
						height: 1,
					},
				],
				data: {
					node: n,
					dimmed: nodeDimmed(model, n.id, selection),
					selected: selection?.kind === "node" && selection.id === n.id,
					onSelect,
				},
			})),
		[model, layout, selection, onSelect],
	);
	const edges = useMemo<FlowEdge[]>(() => {
		const max = Math.max(0, ...model.edges.map((e) => e.connections));
		const shapes = shapeEdges(model, layout);
		const titles = new Map(model.nodes.map((n) => [n.id, n.title]));
		return model.edges.map((e) => {
			const { selected, dimmed } = edgeEmphasis(e, selection);
			return {
				id: e.id,
				type: "flow",
				source: e.source,
				target: e.target,
				// The selected edge draws over the others.
				zIndex: selected ? 1 : 0,
				data: {
					edge: e,
					from: titles.get(e.source) ?? e.source,
					to: titles.get(e.target) ?? e.target,
					shape: shapes.get(e.id) as EdgeShape,
					width: strokeWidth(e, max),
					selected,
					dimmed,
					onSelect,
				},
			};
		});
	}, [model, layout, selection, onSelect]);

	return (
		<div className="absolute inset-0" data-testid="flow-graph">
			<Markers />
			<ReactFlow
				nodes={nodes}
				edges={edges}
				nodeTypes={nodeTypes}
				edgeTypes={edgeTypes}
				minZoom={minZoom}
				maxZoom={2.5}
				onlyRenderVisibleElements
				nodesDraggable={false}
				nodesConnectable={false}
				elementsSelectable={false}
				edgesFocusable={false}
				nodesFocusable={false}
				onPaneClick={() => onSelect(null)}
				proOptions={{ hideAttribution: true }}
			>
				<Refit layout={layout} />
			</ReactFlow>
			<div className="absolute top-3 left-4 rounded-lg border border-default bg-raised p-1.5 pr-2.5">
				{range}
			</div>
			<Legend />
		</div>
	);
}

// The view fits the whole map, clear of the range panel above it and the
// legend below it.
const minZoom = 0.1;
const labelZoom = 0.5;
const loopRise = 40;
const fitOptions = {
	padding: { top: "60px", bottom: "96px", left: "24px", right: "24px" },
	maxZoom: 1.25,
} as const;

// Refit fits the view again when the map is regrouped or rescoped, and
// when the canvas changes size (the drawer opening or closing, the
// window resizing); panning and zooming in between are the operator's.
// The bounds come from the layout, which knows every node's box, so the
// fit never waits on measuring nodes that are not mounted.
function Refit({ layout }: { layout: Layout }) {
	const { setViewport } = useReactFlow();
	const width = useStore((s) => s.width);
	const height = useStore((s) => s.height);
	useEffect(() => {
		if (width <= 0 || height <= 0 || layout.positions.size === 0) return;
		const xs = [...layout.positions.values()];
		const minX = Math.min(...xs.map((p) => p.x));
		const minY = Math.min(...xs.map((p) => p.y));
		const maxX = Math.max(...xs.map((p) => p.x + nodeWidth));
		// A loop rises above its node; leave room for one on the top row.
		const maxY = Math.max(...xs.map((p) => p.y + nodeHeight));
		const bounds = {
			x: minX,
			y: minY - loopRise,
			width: maxX - minX,
			height: maxY - minY + loopRise,
		};
		void setViewport(
			getViewportForBounds(
				bounds,
				width,
				height,
				minZoom,
				fitOptions.maxZoom,
				fitOptions.padding,
			),
		);
	}, [layout, width, height, setViewport]);
	return null;
}

// Markers are the arrowheads, one per decision, filled with the edge's
// own token. They are sized in the canvas's units, not the stroke's, so
// an arrowhead stays the same size whatever volume widens its edge.
function Markers() {
	return (
		<svg aria-hidden="true" className="absolute size-0">
			<defs>
				{precedence.map((d) => (
					<marker
						key={d}
						id={`iw-arrow-${d}`}
						viewBox="0 0 10 10"
						refX="9"
						refY="5"
						markerUnits="userSpaceOnUse"
						markerWidth="9"
						markerHeight="9"
						orient="auto-start-reverse"
					>
						<path d="M0 0L10 5L0 10z" style={{ fill: edgeColor[d] }} />
					</marker>
				))}
			</defs>
		</svg>
	);
}

const unmanagedKinds = new Set(["address-group", "unknown"]);

// frame is a node's fill and outline: a managed group on the raised
// surface, an unmanaged peer on the canvas behind the strong hairline,
// the unlabeled workloads outlined in the warning tone; selection is
// blue in every kind.
function frame(n: MapNode, selected: boolean): string {
	if (selected) return "bg-selection-bg border-selection-fg";
	if (unmanagedKinds.has(n.kind)) return "bg-app border-strong";
	if (n.kind === "unlabeled") return "bg-raised border-status-warn-border";
	return "bg-raised border-default";
}

// subtitle is a node's second line: how many workloads, or what the
// unmanaged peer is.
export function subtitle(n: MapNode): string {
	switch (n.kind) {
		case "address-group": {
			const cidrs = n.cidrs ?? [];
			if (cidrs.length === 0) return "no CIDRs";
			return cidrs.length > 1 ? `${cidrs[0]} (+${cidrs.length - 1})` : cidrs[0];
		}
		case "unknown": {
			const parts = [];
			if (n.addresses.length > 0)
				parts.push(
					`${n.addresses.length} ${n.addresses.length === 1 ? "address" : "addresses"}`,
				);
			if (n.unrecognized.length > 0)
				parts.push(`${n.unrecognized.length} unrecognized`);
			return parts.join(" · ");
		}
		default: {
			const c = n.workloadIds.length;
			return `${c} ${c === 1 ? "workload" : "workloads"}`;
		}
	}
}

// ModeLine is a node's third line: the managed group's mode, the
// unlabeled warning, or what kind of unmanaged peer it is.
export function ModeLine({ n }: { n: MapNode }) {
	if (n.kind === "unlabeled") {
		return (
			<SeverityNote
				level="alert"
				size="sm"
				className="max-w-full [&>span]:truncate"
			>
				no labels — matches no scope
			</SeverityNote>
		);
	}
	if (n.kind === "address-group") {
		return <span className="text-tertiary">unmanaged · address group</span>;
	}
	if (n.kind === "unknown") {
		return <span className="text-tertiary">unmanaged · no address group</span>;
	}
	const m = modeSummary(n);
	if (!m) {
		return <span className="text-tertiary">outside scope</span>;
	}
	if ("mode" in m) {
		const d = modes[m.mode];
		return (
			<span className="inline-flex items-center gap-1 text-secondary">
				<span aria-hidden="true" className="inline-flex text-icon-default">
					{d.glyph}
				</span>{" "}
				{d.label.toLowerCase()}
			</span>
		);
	}
	return (
		<span className="inline-flex items-center gap-1 text-tertiary">
			{m.mixed.map(([mode, c], i) => (
				<span key={mode} className="inline-flex items-center gap-0.5">
					{i > 0 ? " " : ""}
					<span aria-hidden="true" className="inline-flex text-icon-default">
						{modes[mode].glyph}
					</span>
					{c}
				</span>
			))}{" "}
			mixed
		</span>
	);
}

function GroupNodeView({ data }: NodeProps<GroupNode>) {
	const { node: n, dimmed, selected, onSelect } = data;
	return (
		<button
			type="button"
			aria-label={`${n.title}, ${subtitle(n)}`}
			aria-pressed={selected}
			onClick={(ev) => {
				ev.stopPropagation();
				onSelect(selected ? null : { kind: "node", id: n.id });
			}}
			className={cn(
				"nodrag nopan relative flex cursor-pointer flex-col justify-center rounded-md border px-2.5 text-left",
				frame(n, selected),
			)}
			style={{
				width: nodeWidth,
				height: nodeHeight,
				opacity: dimmed ? "var(--opacity-dimmed)" : 1,
			}}
		>
			<span className="flex min-w-0 items-center gap-1.5">
				<Icon
					name={unmanagedKinds.has(n.kind) ? "globe" : "boxes"}
					className="size-3.5"
				/>
				<span className="truncate type-ui-strong text-primary">{n.title}</span>
			</span>
			<span className="truncate type-mono-xs text-tertiary">{subtitle(n)}</span>
			<span className="flex min-w-0 truncate type-mono-xs">
				<ModeLine n={n} />
			</span>
			<Handle
				type="target"
				position={Position.Left}
				isConnectable={false}
				className="!min-h-0 !min-w-0 !size-px !border-0 !opacity-0"
			/>
			<Handle
				type="source"
				position={Position.Right}
				isConnectable={false}
				className="!min-h-0 !min-w-0 !size-px !border-0 !opacity-0"
			/>
		</button>
	);
}

// FlowEdgeView draws an edge from its precomputed shape: the path and
// label place follow from the layout (geometry.ts), which puts the ends
// exactly where the nodes' handles are.
function FlowEdgeView({ data }: EdgeProps<FlowEdge>) {
	// Below half size a chip's count cannot be read, and hundreds of them
	// cost every frame of a pan; the edge stays, drawn and selectable,
	// and the chips return as the view zooms in. The selector answers
	// only when the threshold is crossed, so zooming redraws no edge.
	const legible = useStore((s) => s.transform[2] >= labelZoom);
	if (!data) return null;
	const { edge: e, width, selected, dimmed, onSelect } = data;
	const { path, lx, ly } = data.shape;
	const color = edgeColor[e.decision];
	const opacity = dimmed ? "var(--opacity-dimmed)" : 1;
	const v = verdicts[e.decision];
	const shown = e.byDecision[e.decision]?.connections ?? e.connections;
	const select = () => onSelect(selected ? null : { kind: "edge", id: e.id });
	return (
		<>
			{selected ? (
				// The selected edge's halo, in the selection hue, under the
				// edge itself.
				<path
					d={path}
					style={{
						pointerEvents: "none",
						fill: "none",
						stroke: "var(--selection-border)",
						strokeWidth: `calc(${width} + 5px)`,
						strokeLinecap: "round",
					}}
				/>
			) : null}
			<BaseEdge
				path={path}
				markerEnd={`url(#iw-arrow-${e.decision})`}
				interactionWidth={14}
				style={{
					stroke: color,
					strokeWidth: width,
					strokeDasharray: edgeDash[e.decision],
					strokeLinecap: edgeCap[e.decision],
					opacity,
					cursor: "pointer",
				}}
				onClick={select}
			/>
			{legible || selected ? (
				<EdgeLabelRenderer>
					<button
						type="button"
						aria-pressed={selected}
						aria-label={`${data.from} to ${data.to}: ${v.label} ${shown} connections`}
						data-edge={e.id}
						onClick={select}
						className={cn(
							"nodrag nopan pointer-events-auto absolute flex h-[18px] cursor-pointer items-center gap-1 rounded-sm border px-1 type-mono-xs text-primary",
							selected ? "border-selection-fg bg-selection-bg" : "bg-app",
						)}
						style={{
							transform: `translate(-50%, -50%) translate(${lx}px, ${ly}px)`,
							borderColor: selected ? undefined : color,
							opacity,
							zIndex: selected ? 1 : 0,
						}}
					>
						<StatusGlyph status={v.status} size="sm" />
						{short(shown)}
					</button>
				</EdgeLabelRenderer>
			) : null}
		</>
	);
}

// Legend is the floating key on the raised surface: each decision with
// its glyph and a line sample in the tokens' own color and dash, and the
// node kinds by their icons.
function Legend() {
	const sample = (d: keyof typeof edgeDash) => (
		<svg width="32" height="8" aria-hidden="true" className="shrink-0">
			<line
				x1="2"
				y1="4"
				x2="30"
				y2="4"
				style={{
					stroke: edgeColor[d],
					strokeWidth: 2,
					strokeDasharray: edgeDash[d],
					strokeLinecap: edgeCap[d],
				}}
			/>
		</svg>
	);
	const entry = (d: keyof typeof edgeDash, words: string) => (
		<span className="flex items-center gap-1.5">
			{sample(d)}
			<StatusGlyph status={verdicts[d].status} size="lg" />
			{words}
		</span>
	);
	return (
		<div
			className="pointer-events-none absolute bottom-3.5 left-4 flex flex-col gap-2 rounded-lg border border-default bg-raised px-3 py-2.5 type-caption"
			data-testid="map-legend"
		>
			<div className="flex flex-wrap gap-x-4 gap-y-1.5 text-secondary">
				{entry("observed", "observed · no policy evaluated")}
				{entry("allowed", "allowed")}
				{entry("would_block", "would block · simulation")}
				{entry("blocked", "blocked · dropped")}
			</div>
			<div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-tertiary">
				<span className="flex items-center gap-1.5">
					<Icon name="boxes" className="size-3.5" />
					managed label group
				</span>
				<span className="flex items-center gap-1.5">
					<Icon name="globe" className="size-3.5" />
					unmanaged peer (address group / unknown) — appears only as a source;
					no agent observes traffic into it
				</span>
				<span>line weight grows with connections</span>
			</div>
		</div>
	);
}
