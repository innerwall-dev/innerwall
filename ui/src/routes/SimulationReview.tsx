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
import { type GlyphStatus, StatusGlyph } from "@/components/StatusGlyph";
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
			<div className="flex shrink-0 flex-col gap-4 px-6 pt-5">
				<div className="flex flex-wrap items-start gap-x-4 gap-y-3">
					<div className="flex min-w-0 flex-1 flex-col gap-2">
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
					<fieldset className="m-0 flex h-control-sm overflow-hidden rounded-md border border-default p-0">
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
									"cursor-pointer border-l border-default px-3 type-caption first-of-type:border-l-0",
									take === t.key
										? "bg-active font-medium text-primary"
										: "text-secondary hover:bg-hover hover:text-primary",
								)}
							>
								{t.label}
							</button>
						))}
					</fieldset>
					<div className="flex flex-wrap gap-1">
						{(["all", "would_block", "allowed"] as const).map((f) => (
							<FilterChip
								key={f}
								on={filter === f}
								glyph={f === "all" ? undefined : verdicts[f].glyph}
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
			<div className="mt-4 flex min-h-0 flex-1 overflow-hidden border-t border-default">
				<div className="min-w-0 flex-1 overflow-auto px-6 pt-4 pb-6">
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
		<div className="flex flex-wrap items-center gap-2">
			<span className="type-label text-tertiary">Ruleset</span>
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
								"inline-flex h-control-sm cursor-pointer items-center gap-2 rounded-md border px-2.5 type-mono-sm",
								on
									? "border-strong bg-active text-primary"
									: "border-transparent text-secondary hover:bg-hover hover:text-primary",
							)}
						>
							<span>{rs.name}</span>
							{c ? (
								<span
									className={cn(
										"inline-flex items-center gap-1 type-mono-xs",
										on ? "text-secondary" : "text-tertiary",
									)}
									title={`${c.pairs}${c.truncated ? "+" : ""} would-block peer/service pairs in range`}
								>
									<StatusGlyph status="would-block" size="sm" />
									{c.pairs}
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
			className="flex flex-wrap items-center gap-x-2 gap-y-1 type-caption text-secondary"
			data-testid="scope-line"
		>
			<span className="type-mono-sm text-secondary">
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
// every qualification. It is tinted with the verdict's tone; the
// would-block pairs are the screen's one headline metric.
function VerdictBanner({ verdict }: { verdict: ReviewVerdictResult }) {
	const k = verdict.kpis;
	const safe = verdict.safe;
	// The headline's glyph agrees with the banner's tone: allowed when safe,
	// the error severity when not; the reasons carry their own glyphs.
	const glyph: GlyphStatus = safe ? "allowed" : "error";
	return (
		<section
			aria-label="Verdict"
			className={cn(
				"rounded-lg border px-4 py-3",
				safe
					? "border-status-ok-border bg-status-ok-bg"
					: "border-status-critical-border bg-status-critical-bg",
			)}
		>
			<div className="flex flex-wrap items-start gap-x-8 gap-y-3">
				<div className="flex min-w-0 flex-1 basis-80 items-start gap-2.5">
					<StatusGlyph status={glyph} size="lg" className="mt-0.5" />
					<div className="flex min-w-0 flex-col gap-0.5">
						<h1
							className={cn(
								"type-body-strong",
								safe ? "text-status-ok-fg" : "text-status-critical-fg",
							)}
						>
							{verdict.headline}
						</h1>
						<p className="type-body text-secondary">{verdict.sub}</p>
					</div>
				</div>
				<dl className="flex max-w-full flex-wrap items-end gap-x-6 gap-y-3">
					<Kpi
						headline
						value={count(k.pairs)}
						label={
							<>
								<StatusGlyph status="would-block" size="sm" />
								<span>would block · peer/service</span>
							</>
						}
					/>
					<Kpi value={short(k.connections)} label="connections in range" />
					<Kpi
						value={`${count(k.affected)}${k.affectedLowerBound ? "+" : ""}`}
						label={`of ${count(k.simulating)} simulating ${k.simulating === 1 ? "workload" : "workloads"} affected`}
					/>
					<Kpi
						value={count(k.allowedPairs)}
						label={
							<>
								<StatusGlyph status="allowed" size="sm" />
								<span>allowed · matched a rule</span>
							</>
						}
					/>
				</dl>
			</div>
			{verdict.caveats.length > 0 ? (
				<ul
					className={cn(
						"mt-3 flex flex-col gap-1.5 border-t pt-3 type-ui text-primary",
						safe ? "border-status-ok-border" : "border-status-critical-border",
					)}
					aria-label="Caveats"
				>
					{verdict.caveats.map((c) => (
						<li key={c} className="flex items-start gap-2">
							<StatusGlyph status="alert" size="md" className="mt-[3px]" />
							<span className="min-w-0">{c}</span>
						</li>
					))}
				</ul>
			) : null}
		</section>
	);
}

// Kpi is one of the banner's numbers under its term. The term reads
// first; the number is drawn above it.
function Kpi({
	value,
	label,
	headline = false,
}: {
	value: string;
	label: React.ReactNode;
	headline?: boolean;
}) {
	return (
		<div className="flex max-w-[160px] flex-col-reverse gap-0.5">
			<dt className="flex items-start gap-1 type-caption text-secondary [&>svg]:mt-0.5">
				{label}
			</dt>
			<dd
				className={cn(
					"m-0 text-primary",
					headline
						? "type-mono-kpi"
						: "type-title-section font-mono font-medium tabular-nums",
				)}
			>
				{value}
			</dd>
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
