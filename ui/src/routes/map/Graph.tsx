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
import { short } from "@/lib/format";
import { cn } from "@/lib/utils";
import { type EdgeShape, shapeEdges } from "./geometry";
import { type Layout, nodeHeight, nodeWidth } from "./layout";
import {
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
	width: number;
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
		const volumes = model.edges.map((e) => e.connections);
		const min = Math.min(...volumes);
		const max = Math.max(...volumes);
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
					width: strokeWidth(e, min, max),
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
			<div className="absolute top-3 left-4 rounded border border-input bg-surface-translucent px-2 py-1.5">
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
// own token.
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
						markerWidth="7"
						markerHeight="7"
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

function frame(n: MapNode): { fill: string; stroke: string; dash?: string } {
	if (unmanagedKinds.has(n.kind)) {
		return {
			fill: "var(--viz-node-unmanaged-fill)",
			stroke: "var(--viz-node-unmanaged-stroke)",
			dash: "5 4",
		};
	}
	if (n.kind === "unlabeled") {
		return {
			fill: "var(--viz-node-managed-fill)",
			stroke: "var(--viz-node-unlabeled-stroke)",
			dash: "2 3",
		};
	}
	return {
		fill: "var(--viz-node-managed-fill)",
		stroke: "var(--viz-node-managed-stroke)",
	};
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
			<span className="text-[var(--viz-node-unlabeled-text)]">
				▲ no labels — matches no scope
			</span>
		);
	}
	if (n.kind === "address-group") {
		return (
			<span className="text-[var(--viz-node-unmanaged-text)]">
				unmanaged · address group
			</span>
		);
	}
	if (n.kind === "unknown") {
		return (
			<span className="text-[var(--viz-node-unmanaged-text)]">
				unmanaged · no address group
			</span>
		);
	}
	const m = modeSummary(n);
	if (!m) {
		return (
			<span className="text-[var(--viz-node-subtitle)]">outside scope</span>
		);
	}
	if ("mode" in m) {
		const d = modes[m.mode];
		return (
			<span className={d.cls.split(" ")[0]}>
				<span aria-hidden="true">{d.glyph}</span> {d.label.toLowerCase()}
			</span>
		);
	}
	return (
		<span className="text-[var(--viz-node-subtitle)]">
			{m.mixed.map(([mode, c], i) => (
				<span key={mode}>
					{i > 0 ? " " : ""}
					<span className={modes[mode].cls.split(" ")[0]}>
						<span aria-hidden="true">{modes[mode].glyph}</span>
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
	const f = frame(n);
	return (
		<button
			type="button"
			aria-label={`${n.title}, ${subtitle(n)}`}
			aria-pressed={selected}
			onClick={(ev) => {
				ev.stopPropagation();
				onSelect(selected ? null : { kind: "node", id: n.id });
			}}
			className="nodrag nopan relative block cursor-pointer text-left"
			style={{
				width: nodeWidth,
				height: nodeHeight,
				opacity: dimmed ? "var(--viz-edge-dim-opacity)" : 1,
			}}
		>
			<svg
				aria-hidden="true"
				className="absolute inset-0"
				width={nodeWidth}
				height={nodeHeight}
			>
				<rect
					x={0.5}
					y={0.5}
					width={nodeWidth - 1}
					height={nodeHeight - 1}
					rx={6}
					style={{
						fill: f.fill,
						stroke: selected ? "var(--ring)" : f.stroke,
						strokeWidth: selected ? 1.5 : 1,
						strokeDasharray: f.dash,
					}}
				/>
			</svg>
			<span className="relative flex flex-col gap-[2px] px-2.5 pt-[7px] leading-[14px]">
				<span className="truncate text-[12.5px] font-semibold text-[var(--viz-node-title)]">
					{n.title}
				</span>
				<span className="truncate font-mono text-[10px] text-[var(--viz-node-subtitle)]">
					{subtitle(n)}
				</span>
				<span className="truncate font-mono text-[10px]">
					<ModeLine n={n} />
				</span>
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
	const opacity = dimmed ? "var(--viz-edge-dim-opacity)" : 1;
	const v = verdicts[e.decision];
	const shown = e.byDecision[e.decision]?.connections ?? e.connections;
	const select = () => onSelect(selected ? null : { kind: "edge", id: e.id });
	return (
		<>
			<BaseEdge
				path={path}
				markerEnd={`url(#iw-arrow-${e.decision})`}
				interactionWidth={14}
				style={{
					stroke: color,
					strokeWidth: selected ? "var(--viz-edge-selected-width)" : width,
					strokeDasharray: edgeDash[e.decision],
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
							"nodrag nopan pointer-events-auto absolute flex h-4 cursor-pointer items-center gap-1 rounded-[3px] border px-1 font-mono text-[10px] leading-none",
						)}
						style={{
							transform: `translate(-50%, -50%) translate(${lx}px, ${ly}px)`,
							color,
							borderColor: color,
							background: "var(--viz-edge-label-bg)",
							opacity,
							zIndex: selected ? 1 : 0,
						}}
					>
						<span aria-hidden="true">{v.glyph}</span>
						{short(shown)}
					</button>
				</EdgeLabelRenderer>
			) : null}
		</>
	);
}

// Legend is the floating key on the translucent surface: each decision
// with its line sample in the tokens' own geometry, and the node frames.
function Legend() {
	const sample = (d: keyof typeof edgeDash, w: number) => (
		<svg width="26" height="8" aria-hidden="true">
			<line
				x1="0"
				y1="4"
				x2="26"
				y2="4"
				style={{ stroke: edgeColor[d], strokeWidth: w }}
				strokeDasharray={edgeDash[d]}
			/>
		</svg>
	);
	return (
		<div
			className="pointer-events-none absolute bottom-3.5 left-4 flex flex-col gap-1.5 rounded border border-input bg-surface-translucent px-3 py-2.5 text-[11px]"
			data-testid="map-legend"
		>
			<div className="flex flex-wrap gap-x-3.5 gap-y-1 text-foreground-tertiary">
				<span className="flex items-center gap-1.5">
					{sample("observed", 1.5)}○ observed · no policy evaluated
				</span>
				<span className="flex items-center gap-1.5">
					{sample("allowed", 1.5)}✓ allowed
				</span>
				<span className="flex items-center gap-1.5">
					{sample("would_block", 1.5)}◆ would block · simulation
				</span>
				<span className="flex items-center gap-1.5">
					{sample("blocked", 2)}✕ blocked · dropped
				</span>
			</div>
			<div className="flex flex-wrap gap-x-3.5 gap-y-1 text-muted-foreground">
				<span className="flex items-center gap-[5px]">
					<svg width="12" height="8" aria-hidden="true">
						<rect
							x="0.5"
							y="0.5"
							width="11"
							height="7"
							rx="2"
							style={{ fill: "none", stroke: "var(--foreground-tertiary)" }}
						/>
					</svg>
					managed label group
				</span>
				<span className="flex items-center gap-[5px]">
					<svg width="12" height="8" aria-hidden="true">
						<rect
							x="0.5"
							y="0.5"
							width="11"
							height="7"
							style={{ fill: "none", stroke: "var(--foreground-tertiary)" }}
							strokeDasharray="5 4"
						/>
					</svg>
					unmanaged peer (address group / unknown) — appears only as a source;
					no agent observes traffic into it
				</span>
				<span>line weight grows with connections</span>
			</div>
		</div>
	);
}
