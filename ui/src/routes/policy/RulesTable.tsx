import type { ReactNode } from "react";
import type { Rule } from "@/api/schema";
import { Eyebrow, verdicts } from "@/components/fleet/status";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { count } from "@/lib/format";
import type { Resource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { AddInput, ChipView, FindingLines } from "./Chips";
import type { Traffic } from "./data";
import {
	type Attached,
	draftInput,
	entryChip,
	type Names,
	nextKey,
	outstanding,
	parsePeer,
	parseService,
	peerChip,
	type RuleDraft,
	recency,
	serviceRefChip,
	servicesCell,
	shortRuleId,
} from "./model";

// RowState is the one row being edited: its draft, the findings of the
// last refused write attached to what they name, and the draft as it was
// refused, so saving the identical draft again is not offered.
export interface RowState {
	draft: RuleDraft;
	findings: Attached;
	refused: string | null;
}

// rowBlocked says whether saving is pointless as the row stands: the
// draft is the one just refused, unchanged.
export function rowBlocked(row: RowState): boolean {
	return (
		row.refused !== null &&
		row.refused === JSON.stringify(draftInput(row.draft))
	);
}

// A cell's padding: the table's outer columns carry the card's inset.
const cell = "px-2 py-3 align-top first:pl-4 last:pr-4";
const head = "px-2 py-2 font-normal first:pl-4 last:pr-4";

// RulesCard is the ruleset's inbound rules: who may connect to the
// scoped workloads, on what, whether each rule is on, and what each
// admitted over the last day.
export function RulesCard({
	rules,
	names,
	traffic,
	row,
	busy,
	notice,
	inScope,
	saving,
	onToggle,
	onEdit,
	onAdd,
	onDraft,
	onDiscard,
	onSave,
	onDryRun,
}: {
	rules: Rule[];
	names: Names;
	traffic: Resource<Traffic>;
	row: RowState | null;
	busy: boolean;
	notice: ReactNode;
	inScope: number | null;
	saving: boolean;
	onToggle: (rule: Rule, on: boolean) => void;
	onEdit: (rule: Rule) => void;
	onAdd: () => void;
	onDraft: (d: RuleDraft) => void;
	onDiscard: () => void;
	onSave: () => void;
	onDryRun: () => void;
}) {
	const editingId = row ? (row.draft.base?.id ?? null) : undefined;
	const locked = row !== null;
	return (
		<section
			aria-label="Inbound rules"
			className="flex flex-col rounded border border-border bg-surface-sidebar"
		>
			<div className="flex items-center gap-3 px-4 py-3">
				<Eyebrow>
					Inbound rules — who may connect to scoped workloads, on what
				</Eyebrow>
				<Button
					variant="secondary"
					size="sm"
					className="ml-auto"
					disabled={locked}
					title={
						locked ? "Save or discard the row being edited first" : undefined
					}
					onClick={onAdd}
				>
					+ Add rule
				</Button>
			</div>
			{notice}
			<table
				aria-label="Rules"
				className="w-full table-fixed border-collapse border-t border-border text-left"
			>
				<colgroup>
					<col className="w-[64px]" />
					<col className="w-[30%]" />
					<col className="w-[21%]" />
					<col />
					<col className="w-[190px]" />
				</colgroup>
				<thead>
					<tr className="border-b border-border text-[11px] uppercase tracking-[0.05em] text-muted-foreground">
						<th className={head}>On</th>
						<th className={head}>Peers (may connect)</th>
						<th className={head}>Services (on)</th>
						<th className={head}>Description</th>
						<th className={cn(head, "text-right")}>Simulation · last 24h</th>
					</tr>
				</thead>
				<tbody>
					{rules.length === 0 && !row ? (
						<tr>
							<td
								colSpan={5}
								className="px-4 py-5 text-[12px] text-muted-foreground"
							>
								No rules yet: nothing in this ruleset admits traffic to its
								workloads.
							</td>
						</tr>
					) : null}
					{rules.map((r) =>
						row && editingId === r.id ? (
							<EditRow key={r.id} row={row} names={names} onDraft={onDraft} />
						) : (
							<RuleRow
								key={r.id}
								rule={r}
								names={names}
								traffic={traffic}
								locked={locked || busy}
								onToggle={(on) => onToggle(r, on)}
								onEdit={() => onEdit(r)}
							/>
						),
					)}
					{row && editingId === null ? (
						<EditRow row={row} names={names} onDraft={onDraft} />
					) : null}
				</tbody>
			</table>
			{row ? (
				<Footer
					row={row}
					inScope={inScope}
					saving={saving}
					onDiscard={onDiscard}
					onSave={onSave}
					onDryRun={onDryRun}
				/>
			) : null}
			{traffic.status === "ready" && traffic.data.truncated ? (
				<p className="border-t border-border px-4 py-2 text-[12px] text-muted-foreground">
					The day's rule rollup was truncated: a rule without a figure may still
					have admitted traffic.
				</p>
			) : null}
		</section>
	);
}

function RuleRow({
	rule: r,
	names,
	traffic,
	locked,
	onToggle,
	onEdit,
}: {
	rule: Rule;
	names: Names;
	traffic: Resource<Traffic>;
	locked: boolean;
	onToggle: (on: boolean) => void;
	onEdit: () => void;
}) {
	const id = r.id ?? "";
	const label = r.description || shortRuleId(id);
	const cue = recency(r);
	return (
		<tr aria-label={label} className="border-b border-border last:border-b-0">
			<td className={cn(cell, "pt-[15px]")}>
				<Switch
					on={r.enabled !== false}
					label={`Rule ${label} enabled`}
					disabled={locked}
					onChange={onToggle}
				/>
			</td>
			<td className={cell}>
				<div className="flex flex-wrap gap-1.5">
					{r.peers.map((p, i) => (
						// Peers have no identity of their own; their order is theirs.
						// biome-ignore lint/suspicious/noArrayIndexKey: see above
						<ChipView key={i} chip={peerChip(p, names)} />
					))}
				</div>
			</td>
			<td className={cell}>
				<div className="flex flex-wrap gap-1.5">
					{servicesCell(r, names).map((c, i) => (
						// biome-ignore lint/suspicious/noArrayIndexKey: entries have no identity of their own
						<ChipView key={i} chip={c} />
					))}
				</div>
			</td>
			<td className={cell}>
				<div className="flex flex-col gap-0.5">
					<span className="text-[13px] text-foreground">
						{r.description || <span className="text-muted-foreground">—</span>}
					</span>
					<span className="flex flex-wrap items-baseline gap-x-2 font-mono text-[12px] text-muted-foreground">
						<span>{shortRuleId(id)}</span>
						{cue ? (
							<span data-testid="recency">
								<span aria-hidden="true" className="text-status-pending">
									●
								</span>{" "}
								{cue}
							</span>
						) : null}
						<button
							type="button"
							onClick={onEdit}
							disabled={locked}
							className="cursor-pointer font-sans text-[11px] text-link hover:text-link-hover disabled:cursor-default disabled:opacity-50"
							aria-label={`Edit ${label}`}
						>
							Edit
						</button>
					</span>
				</div>
			</td>
			<td className={cn(cell, "text-right")} data-testid="simulation">
				<SimulationCell rule={r} traffic={traffic} />
			</td>
		</tr>
	);
}

// SimulationCell is what a rule admitted over the last day. A disabled
// rule admits nothing while off, and the console does not say what it
// would admit: that is the policy's own evaluation, which only a render
// performs.
function SimulationCell({
	rule: r,
	traffic,
}: {
	rule: Rule;
	traffic: Resource<Traffic>;
}) {
	if (r.enabled === false) {
		return (
			<span className="text-[12px] text-muted-foreground">
				disabled — admits nothing while off
			</span>
		);
	}
	if (traffic.status === "loading") {
		return (
			<span className="font-mono text-[12px] text-muted-foreground">…</span>
		);
	}
	if (traffic.status === "error") {
		return (
			<span
				className="text-[12px] text-muted-foreground"
				title={traffic.error.problem.detail ?? traffic.error.problem.title}
			>
				—
			</span>
		);
	}
	const t = traffic.data.byRule.get(r.id ?? "");
	if (!t) {
		return (
			<span className="text-[12px] text-muted-foreground">
				{traffic.data.truncated ? "—" : "no matched traffic"}
			</span>
		);
	}
	return (
		<span className="flex flex-col items-end gap-0.5">
			<span className={cn("font-mono text-[13px]", verdicts.allowed.text)}>
				<span aria-hidden="true">{verdicts.allowed.glyph}</span>{" "}
				{count(t.connections)} allowed
			</span>
			<span className="text-[12px] text-muted-foreground">
				matched on {count(t.workloads)}
				{t.atLeast ? "+" : ""}{" "}
				{t.workloads === 1 && !t.atLeast ? "workload" : "workloads"}
			</span>
		</span>
	);
}

// EditRow is the row being edited: its elements as chips the operator
// removes, a field to add each kind, and the findings of the last
// refused write on the elements they name.
function EditRow({
	row,
	names,
	onDraft,
}: {
	row: RowState;
	names: Names;
	onDraft: (d: RuleDraft) => void;
}) {
	const d = row.draft;
	const f = row.findings;
	const id = d.base?.id ?? null;
	return (
		<>
			<tr
				aria-label={
					id ? `Editing ${d.base?.description || shortRuleId(id)}` : "New rule"
				}
				className="bg-surface-row-editing"
			>
				<td className={cn(cell, "pt-[15px]")}>
					<Switch
						on={d.enabled}
						label="This rule enabled"
						onChange={(on) => onDraft({ ...d, enabled: on })}
					/>
				</td>
				<td className={cell}>
					<div className="flex flex-col gap-1.5">
						<span className="flex flex-wrap gap-1.5">
							{d.peers.map((p) => (
								<ChipView
									key={p.key}
									chip={peerChip(p.value, names)}
									findings={f.elements[p.key]}
									onRemove={() =>
										onDraft({
											...d,
											peers: d.peers.filter((x) => x.key !== p.key),
										})
									}
								/>
							))}
						</span>
						{d.peers.map((p) => (
							<FindingLines key={p.key} findings={f.elements[p.key]} />
						))}
						<FindingLines findings={f.columns.peers} />
						<AddInput
							label="Add a peer"
							placeholder="app=web · 10.0.0.0/8 · group"
							className="w-full"
							onAdd={(text) =>
								onDraft({
									...d,
									peers: [
										...d.peers,
										{ key: nextKey(), value: parsePeer(text) },
									],
								})
							}
						/>
					</div>
				</td>
				<td className={cell}>
					<div className="flex flex-col gap-1.5">
						<span className="flex flex-wrap gap-1.5">
							{d.refs.map((s) => (
								<ChipView
									key={s.key}
									chip={serviceRefChip(s.value, names)}
									findings={f.elements[s.key]}
									onRemove={() =>
										onDraft({
											...d,
											refs: d.refs.filter((x) => x.key !== s.key),
										})
									}
								/>
							))}
							{d.entries.map((e) => (
								<ChipView
									key={e.key}
									chip={entryChip(e.value)}
									findings={f.elements[e.key]}
									onRemove={() =>
										onDraft({
											...d,
											entries: d.entries.filter((x) => x.key !== e.key),
										})
									}
								/>
							))}
						</span>
						{[...d.refs, ...d.entries].map((s) => (
							<FindingLines key={s.key} findings={f.elements[s.key]} />
						))}
						<FindingLines findings={f.columns.services} />
						<AddInput
							label="Add a service"
							placeholder="tcp/443 · service name"
							className="w-full"
							onAdd={(text) => {
								const s = parseService(text);
								onDraft(
									"ref" in s
										? {
												...d,
												refs: [...d.refs, { key: nextKey(), value: s.ref }],
											}
										: {
												...d,
												entries: [
													...d.entries,
													{ key: nextKey(), value: s.entry },
												],
											},
								);
							}}
						/>
					</div>
				</td>
				<td className={cell}>
					<div className="flex flex-col gap-1">
						<input
							aria-label="Description"
							placeholder="What this rule is for"
							value={d.description}
							onChange={(ev) => onDraft({ ...d, description: ev.target.value })}
							className="rounded-[3px] border border-input-strong bg-card px-2 py-1 text-[13px] text-foreground placeholder:text-muted-foreground focus:outline-none focus-visible:border-ring"
						/>
						<FindingLines findings={f.columns.description} />
						<span className="font-mono text-[12px] text-muted-foreground">
							{id ? shortRuleId(id) : "new rule"}
						</span>
					</div>
				</td>
				<td
					className={cn(
						cell,
						"text-right font-mono text-[12px] text-muted-foreground",
					)}
					data-testid="simulation"
				>
					<div>—</div>
					<div>{id ? "edited, unsaved" : "unsaved"}</div>
				</td>
			</tr>
			{row.findings.row.length > 0 ? (
				<tr className="bg-surface-row-editing">
					<td colSpan={5} className="px-4 pb-3">
						<FindingLines findings={row.findings.row} />
					</td>
				</tr>
			) : null}
		</>
	);
}

// Footer is the edited row's actions: discard it, dry-run it, or save it
// now. Saving writes immediately; the button says to how many
// workloads. Nothing here gates a save on a dry run: the control plane
// admits or refuses the write.
function Footer({
	row,
	inScope,
	saving,
	onDiscard,
	onSave,
	onDryRun,
}: {
	row: RowState;
	inScope: number | null;
	saving: boolean;
	onDiscard: () => void;
	onSave: () => void;
	onDryRun: () => void;
}) {
	const errors = outstanding(row.draft, row.findings);
	const blocked = rowBlocked(row);
	return (
		<div className="flex flex-wrap items-center gap-3 border-t border-border px-4 py-3">
			<span className="text-[12.5px] text-destructive" data-testid="row-errors">
				{errors > 0 ? (
					<>
						<span aria-hidden="true">✕</span> {errors}{" "}
						{errors === 1 ? "error blocks" : "errors block"} saving
					</>
				) : null}
			</span>
			<div className="ml-auto flex items-center gap-2">
				<Button variant="secondary" onClick={onDryRun} disabled={saving}>
					Dry run
				</Button>
				<Button variant="secondary" onClick={onDiscard} disabled={saving}>
					Discard row
				</Button>
				<Button
					variant={blocked ? "secondary" : "primary"}
					onClick={onSave}
					disabled={saving || blocked}
					className={blocked ? "text-muted-foreground" : undefined}
				>
					{inScope === null
						? "Save — applies now"
						: `Save — applies to ${count(inScope)} ${inScope === 1 ? "workload" : "workloads"} now`}
				</Button>
			</div>
		</div>
	);
}
