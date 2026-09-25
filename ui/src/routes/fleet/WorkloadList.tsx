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
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { Button } from "@/components/ui/button";
import { labelPairs } from "@/lib/format";
import { asProblem, useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { useShell } from "@/shell/Shell";
import {
	credential,
	credentialTone,
	lastSeen,
	syncNote,
	version,
} from "./describe";
import { ModeChangeDialog } from "./ModeChangeDialog";

const states: SyncState[] = ["synced", "pending", "degraded", "offline"];

// parseRequirement accepts one label requirement in the domain's
// selector form, `key=value`; the surface is the authority on anything
// finer and answers a malformed one as a problem.
export function parseRequirement(text: string): string | null {
	const t = text.trim();
	const i = t.indexOf("=");
	if (i <= 0 || i === t.length - 1) return null;
	return `${t.slice(0, i).trim()}=${t.slice(i + 1).trim()}`;
}

// WorkloadList is the fleet tab: the workloads in fleet order (the ones
// needing attention first, most recently seen first within a state),
// filtered by sync state and label requirements, a page at a time.
// Selected rows feed a mode change; after one the list is read again
// and shows convergence as each row's applied version against latest.
export function WorkloadList({
	onFresh,
}: {
	onFresh: (fresh: boolean) => void;
}) {
	const shell = useShell();
	const navigate = useNavigate();
	const [syncState, setSyncState] = useState<SyncState | null>(null);
	const [labels, setLabels] = useState<string[]>([]);
	const [pages, setPages] = useState<WorkloadsPage[]>([]);
	const [more, setMore] = useState<{ loading: boolean; error?: string }>({
		loading: false,
	});
	const [selected, setSelected] = useState<Set<string>>(new Set());
	const [changing, setChanging] = useState(false);

	const filter = useMemo<WorkloadFilter>(
		() => ({ sync_state: syncState ?? undefined, label: labels }),
		[syncState, labels],
	);
	const filtered = syncState !== null || labels.length > 0;
	const { resource, reload } = useResource(
		() => listWorkloads(filter),
		[filter],
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
			<div className="flex shrink-0 items-center gap-6 border-b border-border px-6 pt-3.5 pb-3">
				<fieldset className="m-0 flex min-w-0 gap-4 border-0 p-0 text-[12px]">
					<legend className="sr-only">Sync state</legend>
					<FilterChip
						on={syncState === null}
						label="all"
						onClick={() => setSyncState(null)}
					/>
					{states.map((s) => (
						<FilterChip
							key={s}
							on={syncState === s}
							glyph={syncStates[s].glyph}
							glyphClass={syncStates[s].cls}
							label={s}
							onClick={() => setSyncState(syncState === s ? null : s)}
						/>
					))}
				</fieldset>
				<div className="ml-auto flex items-center gap-1.5">
					<LabelFilter labels={labels} onChange={setLabels} />
					<Button
						variant="secondary"
						size="sm"
						className="rounded-chip"
						disabled={selection.length === 0}
						onClick={() => setChanging(true)}
					>
						Change mode for selected…
					</Button>
				</div>
			</div>
			<div className="min-h-0 flex-1 overflow-auto px-6 pb-6">
				{resource.status === "error" ? (
					<ProblemNotice
						what="workloads"
						error={resource.error}
						onRetry={reload}
					/>
				) : (
					<table className="w-full border-collapse text-[12.5px]">
						<thead>
							<tr className="sticky top-0 bg-background text-left text-[11px] uppercase tracking-[0.05em] text-muted-foreground">
								<th className="w-6 pt-2.5 pr-2 pb-2 font-medium">
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
								<th className="px-2 pt-2.5 pb-2 font-medium">Hostname</th>
								<th className="px-2 pt-2.5 pb-2 font-medium">Labels</th>
								<th className="px-2 pt-2.5 pb-2 font-medium">Mode</th>
								<th className="px-2 pt-2.5 pb-2 font-medium">Sync</th>
								<th className="px-2 pt-2.5 pb-2 text-right font-medium">
									Applied
								</th>
								<th className="px-2 pt-2.5 pb-2 font-medium">Credential</th>
								<th className="pt-2.5 pb-2 pl-2 text-right font-medium">
									Last seen
								</th>
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
				{resource.status === "loading" ? <LoadingRow what="workloads" /> : null}
				{resource.status === "ready" && rows.length === 0 ? (
					<div className="flex flex-col items-start gap-2 py-6 text-[12px] text-muted-foreground">
						<span>No workloads match these filters.</span>
						<button
							type="button"
							className="cursor-pointer text-link hover:text-link-hover"
							onClick={() => {
								setSyncState(null);
								setLabels([]);
							}}
						>
							Clear filters
						</button>
					</div>
				) : null}
				{resource.status === "ready" && rows.length > 0 ? (
					<div className="flex items-center gap-3 py-3 text-[12px] text-muted-foreground">
						<span>
							Showing {rows.length} · sorted by sync state, then last seen
						</span>
						{cursor ? (
							<Button
								variant="secondary"
								size="sm"
								className="rounded-chip"
								disabled={more.loading}
								onClick={loadMore}
							>
								{more.loading ? "Loading…" : "Load more"}
							</Button>
						) : null}
						{more.error ? (
							<span role="alert" className="text-destructive">
								{more.error}
							</span>
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
	const cell = "border-t border-border p-2";
	return (
		// The whole row opens the workload for a pointer; the hostname link
		// is the keyboard's way in, and the checkbox selects without opening.
		<tr
			onClick={(e) => {
				if ((e.target as HTMLElement).closest("input, a")) return;
				onOpen();
			}}
			className={cn(
				"cursor-pointer hover:bg-surface-row-selected",
				selected && "bg-surface-row-selected",
			)}
		>
			<td className={cn(cell, "pl-0")}>
				<Checkbox
					label={`Select ${w.hostname}`}
					checked={selected}
					onChange={onToggle}
				/>
			</td>
			<td className={cn(cell, "font-mono text-foreground")}>
				<Link to={`/workloads/${w.id}`} className="hover:underline">
					{w.hostname}
				</Link>
			</td>
			<td className={cell}>
				<div className="flex flex-wrap gap-1">
					{labels.length === 0 ? (
						<span className="text-[11px] text-status-degraded">
							▲ no labels — matches no scope
						</span>
					) : (
						labels.map(([k, v]) => <LabelChip key={k} k={k} v={v} />)
					)}
				</div>
			</td>
			<td className={cell}>
				<ModePill mode={w.mode} />
			</td>
			<td className={cn(cell, "whitespace-nowrap")}>
				<SyncLabel state={w.sync.state} />
				{note ? (
					<span className="ml-1.5 text-[11px] text-muted-foreground">
						{note}
					</span>
				) : null}
			</td>
			<td className={cn(cell, "text-right font-mono")}>
				{version(w.sync.applied_version)}
			</td>
			<td
				className={cn(cell, "font-mono text-[12px]", credentialTone[cred.tone])}
			>
				{cred.text}
			</td>
			<td
				className={cn(
					cell,
					"pr-0 text-right font-mono text-foreground-tertiary",
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
			className="cursor-pointer align-middle"
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
	const [invalid, setInvalid] = useState(false);

	function add(e?: FormEvent) {
		e?.preventDefault();
		const req = parseRequirement(text);
		if (!req) {
			setInvalid(text.trim() !== "");
			if (text.trim() === "") setEditing(false);
			return;
		}
		if (!labels.includes(req)) onChange([...labels, req]);
		setText("");
		setInvalid(false);
		setEditing(false);
	}

	function key(e: KeyboardEvent<HTMLInputElement>) {
		if (e.key === "Escape") {
			setText("");
			setInvalid(false);
			setEditing(false);
		}
	}

	return (
		<fieldset className="m-0 flex min-w-0 items-center gap-1.5 border-0 p-0">
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
						className="inline-flex cursor-pointer items-center gap-1"
					>
						<LabelChip k={k ?? ""} v={v.join("=")} />
						<span className="font-mono text-[11px] text-muted-foreground">
							×
						</span>
					</button>
				);
			})}
			{editing ? (
				<form onSubmit={add}>
					<input
						// biome-ignore lint/a11y/noAutofocus: the field opens on request
						autoFocus
						aria-label="Label requirement"
						aria-invalid={invalid || undefined}
						placeholder="key=value"
						value={text}
						onChange={(e) => {
							setText(e.target.value);
							setInvalid(false);
						}}
						onKeyDown={key}
						onBlur={() => add()}
						className="w-[150px] rounded-chip border border-dashed border-input-strong bg-transparent px-[9px] py-1 font-mono text-[12px] text-foreground placeholder:text-muted-foreground focus:outline-none aria-invalid:border-destructive"
					/>
				</form>
			) : (
				<button
					type="button"
					onClick={() => setEditing(true)}
					className="cursor-pointer rounded-chip border border-dashed border-input-strong px-[9px] py-1 text-[12px] text-muted-foreground hover:text-foreground"
				>
					+ label filter
				</button>
			)}
		</fieldset>
	);
}
