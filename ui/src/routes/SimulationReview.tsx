import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";
import type { Ruleset, Workload } from "@/api/schema";
import { Centered, EmptyState } from "@/components/EmptyState";
import { FilterChip, verdicts } from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import {
	defaultRange,
	isRange,
	RangeControl,
	type RangeKey,
} from "@/components/RangeControl";
import { Button } from "@/components/ui/button";
import { count, short } from "@/lib/format";
import { useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { editorPath } from "./policy/link";
import { ReviewDrawer } from "./review/Drawer";
import { loadReview, type ReviewData, rowLimit } from "./review/data";
import {
	buildRows,
	chipCounts,
	composeVerdict,
	earliestEffective,
	type Filter,
	filterRows,
	latestEffective,
	type ReviewVerdictResult,
	resolveSelection,
	scopeText,
	syncIssue,
} from "./review/model";
import { PromoteDialog } from "./review/PromoteDialog";
import { Grouped, MatrixTake, Recency } from "./review/Takes";

export type Take = "grouped" | "matrix" | "recency";
const takes: { key: Take; label: string }[] = [
	{ key: "grouped", label: "Grouped by peer" },
	{ key: "matrix", label: "Peer × service matrix" },
	{ key: "recency", label: "By recency" },
];

function isTake(s: string | null): s is Take {
	return s === "matrix" || s === "recency";
}
function isFilter(s: string | null): s is Filter {
	return s === "would_block" || s === "allowed";
}

// SimulationReview says, for one ruleset's scope, whether enforcing it
// now would drop traffic its workloads have seen: the peer and service
// pairs under each decision in three takes, a verdict composed from them
// and from the workloads' own state, and the promotion to enforced. What
// the operator chose (the ruleset, the take, the decision filter, the
// range, and the selection) lives in the address, so switching takes
// keeps the selection and a view can be linked to.
export function SimulationReview() {
	const [params, setParams] = useSearchParams();
	const urlName = params.get("ruleset");
	const rangeParam = params.get("range");
	const range: RangeKey = isRange(rangeParam) ? rangeParam : defaultRange;

	// The ruleset the read asks for. Opening the review with none named
	// reads the default and then names it in the address, which must not
	// read it a second time.
	const [asked, setAsked] = useState(urlName);
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
	const { resource, reload } = useResource(
		() => loadReview(asked, range),
		[asked, range],
	);
	const ready = resource.status === "ready" ? resource.data : null;
	const loaded = ready?.ruleset?.name ?? null;
	const answered = ready !== null && ready.requested === asked;
	useEffect(() => {
		if (urlName !== null && urlName !== loaded && urlName !== asked) {
			// The address moved on its own (back or forward): read what it
			// names.
			setAsked(urlName);
		} else if (answered && loaded !== null && urlName !== loaded) {
			// The review opened on its default, or on a name no enabled
			// ruleset has: the address names what is shown.
			update((p) => {
				p.set("ruleset", loaded);
				p.delete("sel");
			});
		}
	}, [urlName, loaded, asked, answered, update]);

	if (resource.status === "loading") {
		return (
			<div className="px-6">
				<LoadingRow what="the simulation review" />
			</div>
		);
	}
	if (resource.status === "error") {
		return (
			<div className="px-6">
				<ProblemNotice
					what="the simulation review"
					error={resource.error}
					onRetry={reload}
				/>
			</div>
		);
	}
	const data = resource.data;
	if (data.total === 0) return <FreshReview />;
	if (!data.ruleset || !data.rollups) return <NoEnabledRuleset />;
	return (
		<Review
			data={data}
			range={range}
			params={params}
			update={update}
			reload={reload}
			pick={(name) => {
				setAsked(name);
				update((p) => {
					p.set("ruleset", name);
					p.delete("sel");
				});
			}}
		/>
	);
}

function Review({
	data,
	range,
	params,
	update,
	reload,
	pick,
}: {
	data: ReviewData;
	range: RangeKey;
	params: URLSearchParams;
	update: (change: (p: URLSearchParams) => void) => void;
	reload: () => void;
	pick: (name: string) => void;
}) {
	const ruleset = data.ruleset as Ruleset;
	const rollups = data.rollups as NonNullable<ReviewData["rollups"]>;
	const takeParam = params.get("take");
	const take: Take = isTake(takeParam) ? takeParam : "grouped";
	const filterParam = params.get("verdict");
	const filter: Filter = isFilter(filterParam) ? filterParam : "all";
	const [promoting, setPromoting] = useState(false);

	const rows = useMemo(
		() => buildRows(rollups, data.addressGroups, data.workloads.length),
		[rollups, data.addressGroups, data.workloads.length],
	);
	const verdict = useMemo(
		() =>
			composeVerdict({
				rows,
				workloads: data.workloads,
				rollups,
				gaps: data.gaps?.gaps ?? [],
				gapsTruncated: data.gaps?.truncated ?? false,
				range: { from: data.from, to: data.to },
				now: Date.now(),
				rowLimit,
			}),
		[rows, data.workloads, rollups, data.gaps, data.from, data.to],
	);
	const shown = filterRows(rows, filter);
	const counts = chipCounts(rows);
	const selected = resolveSelection(rows, params.get("sel"));
	const select = useCallback(
		(id: string | null) =>
			update((p) => {
				if (id && p.get("sel") !== id) p.set("sel", id);
				else p.delete("sel");
			}),
		[update],
	);
	const from = earliestEffective(rollups);
	const to = latestEffective(rollups);

	return (
		<div className="flex min-h-0 flex-1 flex-col overflow-hidden">
			<div className="flex shrink-0 flex-col gap-3.5 px-6 pt-[18px]">
				<div className="flex items-start gap-4">
					<div className="flex min-w-0 flex-col gap-1.5">
						<RulesetTabs
							rulesets={data.rulesets}
							counts={data.counts}
							current={ruleset}
							onPick={pick}
						/>
						<ScopeLine ruleset={ruleset} workloads={data.workloads} />
					</div>
					<div className="ml-auto flex shrink-0 items-center gap-2">
						<Button variant="secondary" asChild>
							<Link to={editorPath(ruleset.name)}>Edit ruleset</Link>
						</Button>
						<Button
							variant={verdict.safe ? "primary" : "secondary"}
							onClick={() => setPromoting(true)}
						>
							Promote to enforced…
						</Button>
					</div>
				</div>
				<VerdictBanner verdict={verdict} />
				<div className="flex flex-wrap items-center gap-2">
					<fieldset className="m-0 flex overflow-hidden rounded-md border border-strong p-0">
						<legend className="sr-only">Take</legend>
						{takes.map((t) => (
							<button
								key={t.key}
								type="button"
								aria-pressed={take === t.key}
								onClick={() =>
									update((p) => {
										if (t.key === "grouped") p.delete("take");
										else p.set("take", t.key);
									})
								}
								className={cn(
									"cursor-pointer px-3 py-[5px] text-[12px]",
									take === t.key
										? "bg-active text-primary"
										: "text-secondary hover:text-primary",
								)}
							>
								{t.label}
							</button>
						))}
					</fieldset>
					<div className="ml-2 flex gap-1.5">
						{(["all", "would_block", "allowed"] as const).map((f) => (
							<FilterChip
								key={f}
								on={filter === f}
								glyph={f === "all" ? undefined : verdicts[f].glyph}
								glyphClass={f === "all" ? undefined : verdicts[f].text}
								label={f === "all" ? "all" : verdicts[f].label}
								count={counts[f]}
								onClick={() =>
									update((p) => {
										if (f === "all") p.delete("verdict");
										else p.set("verdict", f);
									})
								}
							/>
						))}
					</div>
					<div className="ml-auto">
						<RangeControl
							range={range}
							effectiveFrom={from}
							effectiveTo={to}
							testId="review-extent"
							what="the review's counts"
							onChange={(r) =>
								update((p) => {
									if (r === defaultRange) p.delete("range");
									else p.set("range", r);
								})
							}
						/>
					</div>
				</div>
			</div>
			<div className="mt-3 flex min-h-0 flex-1 overflow-hidden border-t border-default">
				<div className="min-w-0 flex-1 overflow-auto px-6 pb-6">
					{take === "grouped" ? (
						<Grouped
							rows={shown}
							all={rows}
							filter={filter}
							simulating={verdict.kpis.simulating}
							range={range}
							selected={selected?.id ?? null}
							onSelect={select}
						/>
					) : take === "matrix" ? (
						<MatrixTake
							rows={shown}
							range={range}
							selected={selected?.id ?? null}
							onSelect={select}
						/>
					) : (
						<Recency
							rows={shown}
							range={range}
							selected={selected?.id ?? null}
							onSelect={select}
						/>
					)}
				</div>
				{selected ? (
					<ReviewDrawer
						key={selected.id}
						row={selected}
						ruleset={ruleset}
						rulesets={data.rulesets}
						addressGroups={data.addressGroups}
						from={data.from}
						to={data.to}
						onClose={() => select(null)}
						onChanged={reload}
					/>
				) : null}
			</div>
			<PromoteDialog
				open={promoting}
				onOpenChange={setPromoting}
				ruleset={ruleset}
				verdict={verdict}
				onPromoted={() => {
					setPromoting(false);
					reload();
				}}
			/>
		</div>
	);
}

// RulesetTabs lists the enabled rulesets with each one's would-block
// peer/service pairs over the range.
function RulesetTabs({
	rulesets,
	counts,
	current,
	onPick,
}: {
	rulesets: Ruleset[];
	counts: ReviewData["counts"];
	current: Ruleset;
	onPick: (name: string) => void;
}) {
	return (
		<div className="flex flex-wrap items-center gap-2.5">
			<span className="text-[11px] uppercase tracking-[0.06em] text-tertiary">
				Ruleset
			</span>
			<div className="flex flex-wrap gap-1" role="tablist">
				{rulesets.map((rs) => {
					const on = rs.name === current.name;
					const c = counts[rs.id ?? rs.name];
					return (
						<button
							key={rs.name}
							type="button"
							role="tab"
							aria-selected={on}
							onClick={() => onPick(rs.name)}
							className={cn(
								"inline-flex cursor-pointer items-center gap-1.5 rounded-sm border px-[9px] py-[3px] font-mono text-[12px]",
								on
									? "border-strong bg-active text-primary"
									: "border-transparent text-secondary hover:text-primary",
							)}
						>
							<span>{rs.name}</span>
							{c ? (
								<span
									className={cn(
										"text-[11px]",
										c.pairs > 0 ? "text-flow-would-block" : "text-tertiary",
									)}
									title={`${c.pairs}${c.truncated ? "+" : ""} would-block peer/service pairs in range`}
								>
									<span aria-hidden="true">◆</span> {c.pairs}
									{c.truncated ? "+" : ""}
									<span className="sr-only"> would block</span>
								</span>
							) : null}
						</button>
					);
				})}
			</div>
		</div>
	);
}

// ScopeLine is the ruleset's scope, how many workloads it holds now, and
// how they divide by mode and by what keeps any from its latest policy.
function ScopeLine({
	ruleset,
	workloads,
}: {
	ruleset: Ruleset;
	workloads: Workload[];
}) {
	const n = (f: (w: Workload) => boolean) => workloads.filter(f).length;
	const mix = [
		[n((w) => w.mode === "simulation"), "simulation"],
		[n((w) => w.mode === "visibility"), "visibility"],
		[n((w) => w.mode === "enforced"), "enforced"],
		[n((w) => syncIssue(w) === "degraded"), "degraded"],
		[n((w) => syncIssue(w) === "offline"), "offline"],
		[n((w) => syncIssue(w) === "pending"), "pending"],
	] as const;
	return (
		<div
			className="flex flex-wrap items-center gap-2 text-[12px] text-secondary"
			data-testid="scope-line"
		>
			<span className="font-mono text-secondary">
				{scopeText(ruleset.scope)}
			</span>
			<span className="text-disabled" aria-hidden="true">
				·
			</span>
			<span>
				{count(workloads.length)}{" "}
				{workloads.length === 1 ? "workload" : "workloads"} in scope
			</span>
			{workloads.length > 0 ? (
				<>
					<span className="text-disabled" aria-hidden="true">
						·
					</span>
					<span>
						{mix
							.filter(([k]) => k > 0)
							.map(([k, label]) => `${count(k)} ${label}`)
							.join(" · ")}
					</span>
				</>
			) : null}
		</div>
	);
}

// VerdictBanner is the composed verdict: the headline, what it rests on,
// the four numbers, and a line for every condition the scope fails and
// every qualification. Its tint is the one place the soft status
// alphas are used.
function VerdictBanner({ verdict }: { verdict: ReviewVerdictResult }) {
	const k = verdict.kpis;
	const safe = verdict.safe;
	const glyph = safe ? "✓" : k.pairs > 0 ? "◆" : "▲";
	return (
		<section
			aria-label="Verdict"
			className={cn(
				"rounded-lg border px-[18px] py-3.5",
				safe
					? "border-status-ok-border bg-status-ok-bg"
					: "border-status-warn-border bg-status-critical-bg",
			)}
		>
			<div className="flex items-center gap-3.5">
				<div
					aria-hidden="true"
					className={cn(
						"flex size-[38px] shrink-0 items-center justify-center rounded-lg font-mono text-[20px]",
						safe
							? "bg-status-ok-bg text-flow-allowed"
							: "bg-status-warn-bg text-flow-would-block",
					)}
				>
					{glyph}
				</div>
				<div className="flex min-w-0 flex-col gap-0.5">
					<h1 className="text-[17px] font-semibold">{verdict.headline}</h1>
					<p className="text-[12px] text-secondary">{verdict.sub}</p>
				</div>
				<dl className="ml-auto grid shrink-0 grid-cols-[repeat(4,auto)] gap-x-7 text-right">
					<Kpi
						value={count(k.pairs)}
						valueClass="text-flow-would-block"
						label="◆ would block · peer/service"
					/>
					<Kpi value={short(k.connections)} label="connections in range" />
					<Kpi
						value={`${count(k.affected)}${k.affectedLowerBound ? "+" : ""}`}
						label={`of ${count(k.simulating)} simulating ${k.simulating === 1 ? "workload" : "workloads"} affected`}
					/>
					<Kpi
						value={count(k.allowedPairs)}
						valueClass="text-flow-allowed"
						label="✓ allowed · matched a rule"
					/>
				</dl>
			</div>
			{verdict.caveats.length > 0 ? (
				<ul
					className="mt-2.5 flex flex-wrap gap-x-[18px] gap-y-1.5 border-t border-[var(--border-subtle)] pt-2.5 text-[12px] text-secondary"
					aria-label="Caveats"
				>
					{verdict.caveats.map((c) => (
						<li key={c} className="flex gap-1.5">
							<span className="text-status-warn-fg" aria-hidden="true">
								▲
							</span>
							<span>{c}</span>
						</li>
					))}
				</ul>
			) : null}
		</section>
	);
}

function Kpi({
	value,
	label,
	valueClass,
}: {
	value: string;
	label: string;
	valueClass?: string;
}) {
	return (
		<div className="flex max-w-[150px] flex-col-reverse">
			<dt className="text-[11px] text-tertiary">{label}</dt>
			<dd className={cn("m-0 font-mono text-[20px]", valueClass)}>{value}</dd>
		</div>
	);
}

// Fresh-install state of simulation review (design screen 17).
function FreshReview() {
	return (
		<Centered>
			<EmptyState
				title="Nothing to review yet"
				width={520}
				gap={14}
				steps={[
					{
						lead: "Enroll workloads",
						rest: "mint a token in Enrollment, run the installer.",
					},
					{
						lead: "Let flows accumulate",
						rest: "workloads start in visibility; the Flow map fills in.",
					},
					{
						lead: "Author a ruleset and switch its scope to simulation",
						rest: "then this screen tells you if it is safe to enforce.",
					},
				]}
				actions={
					<div className="mt-1 flex gap-2">
						<Button asChild>
							<Link to="/workloads">Open enrollment</Link>
						</Button>
						<Button variant="secondary" asChild>
							<Link to="/policy">Create a ruleset</Link>
						</Button>
					</div>
				}
			>
				Simulation review compares observed traffic against a ruleset's rendered
				policy and shows what enforcement would break. It needs workloads,
				flows, and at least one ruleset.
			</EmptyState>
		</Centered>
	);
}

// Every ruleset is disabled: nothing is rendered from any of them, so
// there is nothing to review until one is enabled.
function NoEnabledRuleset() {
	return (
		<Centered>
			<EmptyState
				title="No enabled ruleset"
				width={480}
				actions={
					<Button variant="secondary" asChild className="self-start">
						<Link to="/policy">Open policy</Link>
					</Button>
				}
			>
				Every ruleset is disabled, so none of them renders policy and no
				workload is evaluated against them. Enable a ruleset to review it.
			</EmptyState>
		</Centered>
	);
}
