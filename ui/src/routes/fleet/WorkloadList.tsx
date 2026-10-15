import {
	type FormEvent,
	type KeyboardEvent,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { Link, useNavigate } from "react-router";
import { listWorkloads, type WorkloadFilter } from "@/api/fleet";
import type { SyncState, Workload, WorkloadsPage } from "@/api/schema";
import { Centered, EmptyState } from "@/components/EmptyState";
import {
	FilterChip,
	LabelChip,
	ModePill,
	SyncLabel,
	syncStates,
} from "@/components/fleet/status";
import { Icon } from "@/components/Icon";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import {
	SeverityNote,
	StatusBadge,
	StatusGlyph,
} from "@/components/StatusGlyph";
import { Button } from "@/components/ui/button";
import { labelPairs } from "@/lib/format";
import { parseRequirements } from "@/lib/labels";
import { asProblem, useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { useShell } from "@/shell/Shell";
import {
	credential,
	enforcementRemoved,
	lastSeen,
	reconnecting,
	syncNote,
	version,
} from "./describe";
import { ModeChangeDialog } from "./ModeChangeDialog";

const states: SyncState[] = ["synced", "pending", "degraded", "offline"];

// Chip is one choice of the chip row: a sync state, or removed. Removed
// is not a sync state: the stored state still records the version the
// agent acknowledged. It is the health fact the row already reads
// (enforcementRemoved), derived beside the states (issue #96).
type Chip = SyncState | "removed";

// largestPage is the page size the surface allows at most.
const largestPage = 500;

// listRemoved reads the workloads whose enforcement was removed on the
// host, under the filter's labels, through the fleet read the list
// already makes; no query parameter selects them. The fleet order ranks
// a removed workload with the degraded ones, ahead of every other row, so
// every removed workload lies in the leading run of rows that are removed
// or degraded. The read pages through that run at the largest page and
// stops at the first row past it, or at the fleet's end, so the result is
// complete in one page.
async function listRemoved(filter: WorkloadFilter): Promise<WorkloadsPage> {
	const workloads: Workload[] = [];
	let cursor: string | null = null;
	do {
		const page: WorkloadsPage = await listWorkloads(
			filter,
			cursor ?? undefined,
			largestPage,
		);
		for (const w of page.workloads) {
			if (enforcementRemoved(w)) workloads.push(w);
			else if (w.sync.state !== "degraded") {
				return { workloads, next_cursor: null };
			}
		}
		cursor = page.next_cursor;
	} while (cursor !== null);
	return { workloads, next_cursor: null };
}

// th is the table's header cell: the mono meta label on the canvas,
// held at the top while the rows scroll under it.
const th =
	"h-row-header whitespace-nowrap border-b border-hairline bg-canvas px-3 text-left type-meta-label text-tertiary";

// labelRequirements reads typed or pasted filter text as `key=value`
// requirements, one per label, through the console's one label parser
// (ADR-0022): `app=web env=lab` is two requirements, and text outside the
// grammar is refused with the reason.
export function labelRequirements(
	text: string,
): { ok: true; requirements: string[] } | { ok: false; error: string } {
	const parsed = parseRequirements(text);
	if (!parsed.ok) return parsed;
	return {
		ok: true,
		requirements: parsed.requirements.map((r) => `${r.key}=${r.values[0]}`),
	};
}

// WorkloadList is the fleet tab: the workloads in fleet order (the ones
// needing attention first, most recently seen first within a rank),
// filtered by sync state or enforcement removed, and label requirements,
// a page at a time.
// Selected rows feed a mode change; after one the list is read again
// and shows convergence as each row's applied version against latest.
export function WorkloadList({
	onFresh,
}: {
	onFresh: (fresh: boolean) => void;
}) {
	const shell = useShell();
	const navigate = useNavigate();
	const [chip, setChip] = useState<Chip | null>(null);
	const [labels, setLabels] = useState<string[]>([]);
	const [pages, setPages] = useState<WorkloadsPage[]>([]);
	const [more, setMore] = useState<{ loading: boolean; error?: string }>({
		loading: false,
	});
	const [selected, setSelected] = useState<Set<string>>(new Set());
	const [changing, setChanging] = useState(false);

	const removed = chip === "removed";
	const filter = useMemo<WorkloadFilter>(
		() => ({
			sync_state: chip === null || chip === "removed" ? undefined : chip,
			label: labels,
		}),
		[chip, labels],
	);
	const filtered = chip !== null || labels.length > 0;
	const { resource, reload } = useResource(
		() => (removed ? listRemoved(filter) : listWorkloads(filter)),
		[filter, removed],
	);

	// A new first page replaces whatever was paged in after the last one;
	// a filter change also ends the selection it was made under.
	useEffect(() => {
		if (resource.status === "ready") setPages([resource.data]);
	}, [resource]);
	// biome-ignore lint/correctness/useExhaustiveDependencies: selection belongs to one filter
	useEffect(() => setSelected(new Set()), [filter]);

	const rows = pages.flatMap((p) => p.workloads);
	const cursor = pages.at(-1)?.next_cursor ?? null;
	const fresh =
		resource.status === "ready" &&
		!filtered &&
		resource.data.workloads.length === 0;
	useEffect(() => onFresh(fresh), [fresh, onFresh]);

	async function loadMore() {
		if (!cursor) return;
		setMore({ loading: true });
		try {
			const page = await listWorkloads(filter, cursor);
			setPages((p) => [...p, page]);
			setMore({ loading: false });
		} catch (err) {
			setMore({ loading: false, error: asProblem(err).message });
		}
	}

	function toggle(id: string) {
		setSelected((s) => {
			const next = new Set(s);
			if (next.has(id)) next.delete(id);
			else next.add(id);
			return next;
		});
	}

	const allSelected = rows.length > 0 && rows.every((w) => selected.has(w.id));
	const someSelected = rows.some((w) => selected.has(w.id));
	const selection = rows.filter((w) => selected.has(w.id));

	if (fresh) {
		return (
			<Centered>
				<EmptyState
					title="No workloads enrolled"
					icon="server"
					width={560}
					actions={
						<Button asChild className="self-start">
							<Link to="/workloads/tokens">Mint a provisioning token</Link>
						</Button>
					}
				>
					Workloads appear here when an agent enrolls with a provisioning token.
					The token's label scope becomes the workload's labels.
				</EmptyState>
			</Centered>
		);
	}

	return (
		<div className="flex min-h-0 flex-1 flex-col">
			<div className="flex shrink-0 items-center gap-2 border-b border-hairline px-5 py-3">
				<fieldset className="m-0 flex min-w-0 gap-1 border-0 p-0">
					<legend className="sr-only">Sync state</legend>
					<FilterChip
						on={chip === null}
						label="all"
						onClick={() => setChip(null)}
					/>
					{states.map((s) => (
						<FilterChip
							key={s}
							on={chip === s}
							glyph={syncStates[s].glyph}
							label={s}
							onClick={() => setChip(chip === s ? null : s)}
						/>
					))}
					<FilterChip
						on={removed}
						glyph={<StatusGlyph status="error" size="sm" />}
						label="removed"
						onClick={() => setChip(removed ? null : "removed")}
					/>
				</fieldset>
				<div className="ml-auto flex items-center gap-2">
					<LabelFilter labels={labels} onChange={setLabels} />
					<Button
						variant="secondary"
						size="sm"
						disabled={selection.length === 0}
						onClick={() => setChanging(true)}
					>
						Change mode for selected…
					</Button>
				</div>
			</div>
			<div className="min-h-0 flex-1 overflow-auto pb-6">
				{resource.status === "error" ? (
					<div className="px-5">
						<ProblemNotice
							what="workloads"
							error={resource.error}
							onRetry={reload}
						/>
					</div>
				) : (
					<table className="w-full border-separate border-spacing-0">
						<thead className="sticky top-0 z-10">
							<tr>
								<th className={cn(th, "w-px pr-1 pl-5")}>
									<Checkbox
										label="Select all shown"
										checked={allSelected}
										indeterminate={someSelected && !allSelected}
										onChange={() =>
											setSelected(
												allSelected
													? new Set()
													: new Set(rows.map((w) => w.id)),
											)
										}
									/>
								</th>
								<th className={th}>Hostname</th>
								<th className={th}>Labels</th>
								<th className={th}>Mode</th>
								<th className={th}>Sync</th>
								<th className={cn(th, "text-right")}>Applied</th>
								<th className={th}>Credential</th>
								<th className={th}>Agent</th>
								<th className={cn(th, "pr-5 text-right")}>Last seen</th>
							</tr>
						</thead>
						<tbody>
							{rows.map((w) => (
								<Row
									key={w.id}
									w={w}
									selected={selected.has(w.id)}
									onToggle={() => toggle(w.id)}
									onOpen={() => navigate(`/workloads/${w.id}`)}
								/>
							))}
						</tbody>
					</table>
				)}
				{resource.status === "loading" ? (
					<div className="px-5">
						<LoadingRow what="workloads" />
					</div>
				) : null}
				{resource.status === "ready" && rows.length === 0 ? (
					<div className="flex flex-col items-start gap-2 px-5 py-6 type-dense text-tertiary">
						<span>No workloads match these filters.</span>
						<button
							type="button"
							className="cursor-pointer link"
							onClick={() => {
								setChip(null);
								setLabels([]);
							}}
						>
							Clear filters
						</button>
					</div>
				) : null}
				{resource.status === "ready" && rows.length > 0 ? (
					<div className="flex items-center gap-3 px-5 py-3 type-caption text-tertiary">
						<span>
							Showing {rows.length} · sorted by sync state, then last seen
						</span>
						{cursor ? (
							<Button
								variant="secondary"
								size="sm"
								disabled={more.loading}
								onClick={loadMore}
							>
								{more.loading ? "Loading…" : "Load more"}
							</Button>
						) : null}
						{more.error ? (
							<SeverityNote level="error" role="alert">
								{more.error}
							</SeverityNote>
						) : null}
					</div>
				) : null}
			</div>
			<ModeChangeDialog
				open={changing}
				workloads={selection}
				onOpenChange={setChanging}
				onChanged={() => {
					setChanging(false);
					setSelected(new Set());
					reload();
					shell.fleetChanged();
				}}
				onReload={() => {
					setChanging(false);
					setSelected(new Set());
					reload();
				}}
			/>
		</div>
	);
}

function Row({
	w,
	selected,
	onToggle,
	onOpen,
}: {
	w: Workload;
	selected: boolean;
	onToggle: () => void;
	onOpen: () => void;
}) {
	const labels = labelPairs(w.labels);
	const cred = credential(w);
	const note = syncNote(w);
	// row-pitch is the row pitch including its hairline: the 22px mode
	// pill, 9px above and 8px below it, and the 1px separator make 40.
	const cell =
		"h-row-pitch border-b border-hairline px-3 pt-[9px] pb-[8px] type-dense [&>*]:align-middle";
	return (
		// The whole row opens the workload for a pointer; the hostname link
		// is the keyboard's way in, and the checkbox selects without opening.
		<tr
			onClick={(e) => {
				if ((e.target as HTMLElement).closest("input, a")) return;
				onOpen();
			}}
			className={cn(
				"cursor-pointer",
				selected ? "bg-wash-selected" : "hover:bg-wash-hover",
			)}
		>
			<td className={cn(cell, "pr-1 pl-5")}>
				<Checkbox
					label={`Select ${w.hostname}`}
					checked={selected}
					onChange={onToggle}
				/>
			</td>
			<td className={cn(cell, "whitespace-nowrap")}>
				<Link
					to={`/workloads/${w.id}`}
					className="type-mono-data font-medium text-primary hover:underline"
				>
					{w.hostname}
				</Link>
			</td>
			<td className={cell}>
				<div className="flex flex-wrap gap-1">
					{labels.length === 0 ? (
						<SeverityNote level="alert">
							no labels — matches no scope
						</SeverityNote>
					) : (
						labels.map(([k, v]) => <LabelChip key={k} k={k} v={v} />)
					)}
				</div>
			</td>
			<td className={cell}>
				<ModePill mode={w.mode} />
			</td>
			<td className={cn(cell, "whitespace-nowrap")}>
				{enforcementRemoved(w) ? (
					// The acknowledged version is not what the host runs: its
					// table is gone, so the row says that instead of the
					// sync state.
					<SeverityNote level="error" data-testid="enforcement-removed">
						Enforcement removed on host
					</SeverityNote>
				) : reconnecting(w) ? (
					// Offline by its sync stream, and still reporting flows:
					// the offline glyph, named for what is happening.
					<StatusBadge status="offline">Reconnecting</StatusBadge>
				) : (
					<SyncLabel state={w.sync.state} />
				)}
				{note ? (
					<span className="ml-2 type-caption text-tertiary">{note}</span>
				) : null}
			</td>
			<td className={cn(cell, "text-right type-mono-data")}>
				{version(w.sync.applied_version)}
			</td>
			<td className={cn(cell, "whitespace-nowrap")}>
				{cred.tone === "ok" ? (
					<span className="text-secondary">{cred.text}</span>
				) : (
					<SeverityNote level={cred.tone === "warn" ? "alert" : "error"}>
						{cred.text}
					</SeverityNote>
				)}
			</td>
			<td
				className={cn(cell, "whitespace-nowrap type-mono-sm text-secondary")}
				title={w.agent.version || "not reported"}
			>
				{w.agent.version || "—"}
			</td>
			<td
				className={cn(
					cell,
					"whitespace-nowrap pr-5 text-right type-mono-data text-secondary",
				)}
			>
				{lastSeen(w)}
			</td>
		</tr>
	);
}

function Checkbox({
	label,
	checked,
	indeterminate = false,
	onChange,
}: {
	label: string;
	checked: boolean;
	indeterminate?: boolean;
	onChange: () => void;
}) {
	const ref = useRef<HTMLInputElement>(null);
	useEffect(() => {
		if (ref.current) ref.current.indeterminate = indeterminate;
	}, [indeterminate]);
	return (
		<input
			ref={ref}
			type="checkbox"
			aria-label={label}
			checked={checked}
			onChange={onChange}
			className="size-3.5 cursor-pointer align-middle"
		/>
	);
}

// LabelFilter holds the label requirements: each one a chip that removes
// itself, and the dashed control that takes the next in `key=value`
// form. Requirements on different keys AND; on one key they OR.
function LabelFilter({
	labels,
	onChange,
}: {
	labels: string[];
	onChange: (labels: string[]) => void;
}) {
	const [editing, setEditing] = useState(false);
	const [text, setText] = useState("");
	const [invalid, setInvalid] = useState<string | null>(null);

	function add(e?: FormEvent) {
		e?.preventDefault();
		if (text.trim() === "") {
			setInvalid(null);
			setEditing(false);
			return;
		}
		const parsed = labelRequirements(text);
		if (!parsed.ok) {
			setInvalid(parsed.error);
			return;
		}
		onChange([
			...labels,
			...parsed.requirements.filter(
				(r, i, all) => !labels.includes(r) && all.indexOf(r) === i,
			),
		]);
		setText("");
		setInvalid(null);
		setEditing(false);
	}

	function key(e: KeyboardEvent<HTMLInputElement>) {
		if (e.key === "Escape") {
			setText("");
			setInvalid(null);
			setEditing(false);
		}
	}

	return (
		<fieldset className="m-0 flex min-w-0 items-center gap-1 border-0 p-0">
			<legend className="sr-only">Label filter</legend>
			{labels.map((l) => {
				const [k, ...v] = l.split("=");
				return (
					<button
						key={l}
						type="button"
						title={`Remove ${l}`}
						aria-label={`Remove ${l}`}
						onClick={() => onChange(labels.filter((x) => x !== l))}
						className="inline-flex h-control cursor-pointer items-center gap-1 rounded-control px-1 hover:bg-wash-hover"
					>
						<LabelChip k={k ?? ""} v={v.join("=")} />
						<Icon name="x" className="size-3.5" />
					</button>
				);
			})}
			{editing ? (
				<form onSubmit={add}>
					<input
						// biome-ignore lint/a11y/noAutofocus: the field opens on request
						autoFocus
						aria-label="Label requirement"
						aria-invalid={invalid !== null || undefined}
						title={invalid ?? undefined}
						placeholder="key=value"
						value={text}
						onChange={(e) => {
							setText(e.target.value);
							setInvalid(null);
						}}
						onKeyDown={key}
						onBlur={() => add()}
						className="h-control w-[150px] rounded-control border border-strong bg-field-sunk px-2.5 type-mono-sm text-primary outline-none placeholder:text-tertiary focus-visible:focus-ring aria-invalid:border-status-critical-fg"
					/>
				</form>
			) : (
				<button
					type="button"
					onClick={() => setEditing(true)}
					className="inline-flex h-control cursor-pointer items-center gap-1.5 rounded-control border border-dashed border-strong px-2.5 type-control text-secondary hover:border-control-hover hover:text-primary"
				>
					<Icon name="plus" className="size-3.5" />
					label filter
				</button>
			)}
		</fieldset>
	);
}
