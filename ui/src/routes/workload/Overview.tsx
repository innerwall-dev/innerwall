import type { EvidenceGaps, Workload } from "@/api/schema";
import { LabelChip } from "@/components/fleet/status";
import { Icon } from "@/components/Icon";
import { SeverityNote, StatusGlyph } from "@/components/StatusGlyph";
import { Button } from "@/components/ui/button";
import { ago, count, exact, labelPairs } from "@/lib/format";
import { between, kindText } from "@/lib/gaps";
import type { Resource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { retired } from "../fleet/describe";
import type { ScreenRange } from "./range";
import { StatusCard } from "./StatusCard";
import { AlertCards, type ExceptionalState, gapsShown } from "./states";

// Overview is where a workload's page lands: a centered reading column
// (content-max), with the active exceptional states as alert cards, then
// three cards (Status, Labels, Host facts) that wrap to fewer columns on
// a narrow screen, then the history of evidence gaps over the screen's
// range. Mode and last seen are in the header strip. A retired
// workload's page leads with its retirement, and its labels and identity
// are frozen, so neither offers an edit.
export function Overview({
	w,
	gaps,
	range,
	states,
	onReread,
	onEditLabels,
	onRetire,
}: {
	w: Workload;
	gaps: Resource<EvidenceGaps>;
	range: ScreenRange;
	states: ExceptionalState[];
	onReread: () => void;
	onEditLabels: () => void;
	onRetire: () => void;
}) {
	return (
		<div className="min-h-0 flex-1 overflow-auto">
			{/* The design's layout: the notices and the status cards in the
			    main column, the labels and the host's facts as one 320px card
			    beside it. Below 1024px the side card drops under the column. */}
			<div className="flex gap-7 px-8 pt-5 pb-8 max-lg:flex-col">
				<div className="flex min-w-0 flex-1 flex-col gap-4">
					{retired(w) ? <RetiredCard w={w} /> : null}
					<AlertCards states={states} />
					<StatusCard
						w={w}
						gaps={gaps}
						range={range}
						alerts={states.map((s) => s.key)}
						onReread={onReread}
					/>
					<GapHistory gaps={gaps} range={range} />
				</div>
				<aside
					aria-label="Workload facts"
					className="flex w-[320px] shrink-0 flex-col self-start rounded-panel border border-overlay bg-card max-lg:w-full"
				>
					<LabelsCard w={w} onEdit={onEditLabels} />
					<HostFacts w={w} onRetire={onRetire} />
				</aside>
			</div>
		</div>
	);
}

const card =
	"flex flex-col gap-3 rounded-panel border border-overlay bg-card px-5 pt-[18px] pb-5";

// RetiredCard says the workload was retired, when, and what that means
// for reading the page: a state the operator decided, drawn as chrome on
// the card surface, not in a status tint, since nothing here is wrong
// and nothing is to be fixed.
function RetiredCard({ w }: { w: Workload }) {
	return (
		<section
			aria-label="Retired"
			data-testid="retired"
			className="flex items-start gap-3 rounded-panel border border-overlay bg-card px-5 py-4 type-dense"
		>
			<div className="flex min-w-0 flex-1 flex-col gap-0.5">
				<span className="inline-flex items-center gap-1.5 font-medium text-status-neutral-fg">
					<Icon name="circle-minus" />
					<span>
						Retired
						{w.retired_at ? (
							<>
								{" "}
								<time dateTime={w.retired_at}>{ago(w.retired_at)}</time>
							</>
						) : null}
					</span>
				</span>
				<div className="text-primary">
					{w.retired_at ? (
						<>
							Since <span className="type-mono-sm">
								{exact(w.retired_at)}
							</span>{" "}
						</>
					) : null}
					{w.retired_at ? "its" : "Its"} credential is refused, its addresses
					are in no peer set, and its labels and mode are frozen. Its flows and
					evidence gaps stay readable under this id. There is no un-retire: the
					host returns by enrolling again, as a new workload.
				</div>
			</div>
		</section>
	);
}
const title = "type-card-title text-primary";
// A section of the side card: 18/20px padding, its title the mono
// eyebrow, the sections apart by the subtle rule.
const section =
	"flex flex-col gap-3 px-5 py-[18px] [&+&]:border-t [&+&]:border-hairline";
const eyebrow = "type-meta-label font-normal text-tertiary";
const rows =
	"grid grid-cols-[96px_minmax(0,1fr)] items-baseline gap-x-0 gap-y-2.5";
const field = "type-dense text-tertiary first-letter:uppercase";
// Values in the sans with tabular figures, as the design sets them;
// addresses, identities and serials stay mono.
const value = "min-w-0 type-dense break-words text-primary tabular-nums";

function LabelsCard({ w, onEdit }: { w: Workload; onEdit: () => void }) {
	const labels = labelPairs(w.labels);
	return (
		<section aria-labelledby="labels-card-title" className={section}>
			<div className="flex items-center">
				<h2 id="labels-card-title" className={eyebrow}>
					Labels
				</h2>
				{retired(w) ? (
					<span className="ml-auto type-caption text-tertiary">frozen</span>
				) : (
					<button
						type="button"
						onClick={onEdit}
						aria-label="Edit labels"
						className="ml-auto type-caption link"
					>
						edit
					</button>
				)}
			</div>
			<div className="flex flex-wrap gap-1">
				{labels.length === 0 ? (
					<SeverityNote level="alert" size="sm" className="type-caption">
						no labels — matches no scope
					</SeverityNote>
				) : (
					labels.map(([k, v]) => <LabelChip key={k} k={k} v={v} />)
				)}
			</div>
			<div className="type-caption text-tertiary">
				Assigned by the provisioning token at enrollment; an operator may edit
				them. The agent cannot.
			</div>
		</section>
	);
}

function HostFacts({ w, onRetire }: { w: Workload; onRetire: () => void }) {
	const os = w.os;
	return (
		<section aria-labelledby="host-facts-title" className={section}>
			<h2 id="host-facts-title" className={eyebrow}>
				Host facts
			</h2>
			<dl className={rows}>
				<dt className={field}>hostname</dt>
				<dd className={value}>{w.hostname}</dd>
				<dt className={field}>addresses</dt>
				<dd className={value}>
					{w.addresses.length === 0
						? "none reported"
						: w.addresses.map((a) => (
								<div key={a} className="type-mono-sm">
									{a}
								</div>
							))}
				</dd>
				<dt className={field}>agent</dt>
				<dd className={value}>{w.agent.version || "not reported"}</dd>
				<dt className={cn(field, "uppercase")}>os</dt>
				<dd className={value}>
					{os
						? [
								[os.name, os.version].filter(Boolean).join(" "),
								[os.family, os.kernel_version].filter(Boolean).join(" "),
								os.architecture,
							]
								.filter(Boolean)
								.join(" · ")
						: "not reported"}
				</dd>
				<dt className={field}>enrolled</dt>
				<dd className={value}>{ago(w.enrolled_at)}</dd>
				<dt className={cn(field, "uppercase")}>id</dt>
				<dd className={value}>
					<div className="break-all type-mono-sm">{w.id}</div>
					<div className="mt-0.5 type-caption text-tertiary">
						Identity is assigned at enrollment and cannot be edited.
					</div>
				</dd>
			</dl>
			{retired(w) ? null : (
				// Retiring ends the identity above, so its action sits with it.
				<div className="flex flex-col items-start gap-2 border-t border-hairline pt-3">
					<Button variant="secondary" size="sm" onClick={onRetire}>
						Retire…
					</Button>
					<span className="type-caption text-tertiary">
						Ends this identity for good: its credential is refused and its
						addresses leave every peer set.
					</span>
				</div>
			)}
		</section>
	);
}

// GapHistory lists the newest intervals over the screen's range in
// which the agent knows it lost evidence, a removed table among them:
// flows in them are incomplete.
function GapHistory({
	gaps,
	range,
}: {
	gaps: Resource<EvidenceGaps>;
	range: ScreenRange;
}) {
	if (gaps.status !== "ready" || gaps.data.gaps.length === 0) return null;
	const shown = gaps.data.gaps.slice(0, gapsShown);
	const more = gaps.data.gaps.length > gapsShown || gaps.data.truncated;
	return (
		<section
			aria-labelledby="gap-history-title"
			className={card}
			data-testid="evidence-gaps"
		>
			<div className="flex items-baseline gap-2">
				<h2 id="gap-history-title" className={title}>
					Evidence gaps
				</h2>
				<span className="type-mono-sm text-primary">
					{count(gaps.data.gaps.length)}
					{more ? "+" : ""}{" "}
					<span className="type-caption text-tertiary">in {range.text}</span>
				</span>
			</div>
			<ul className="flex flex-col">
				{shown.map((g) => (
					<li
						key={`${g.kind}|${g.source}|${g.from}|${g.to}`}
						className="flex min-h-row-pitch items-center gap-2 border-t border-hairline py-2 type-dense text-primary"
					>
						<StatusGlyph status="alert" size="md" className="shrink-0" />
						<span className="min-w-0">
							{kindText(g.kind)} between {between(g.from, g.to)}
							{g.count !== null ? ` · ${count(g.count)} lost` : ""}
						</span>
					</li>
				))}
			</ul>
			<div className="type-caption text-tertiary">
				Intervals the agent knows it lost evidence in; the flows shown in them
				are incomplete.
			</div>
		</section>
	);
}
