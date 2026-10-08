import { useEffect, useState } from "react";
import { Link } from "react-router";
import type { ProblemError } from "@/api/client";
import { createModeChange } from "@/api/fleet";
import { previewSelector } from "@/api/policy";
import { ProblemType, type Ruleset } from "@/api/schema";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { SeverityNote, StatusGlyph } from "@/components/StatusGlyph";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogBody,
	DialogClose,
	DialogContent,
	DialogFooter,
	DialogHeader,
} from "@/components/ui/dialog";
import { count, short } from "@/lib/format";
import { useResource, useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { walkWorkloads } from "./data";
import {
	type Partition,
	partition,
	promotionIds,
	type ReviewVerdictResult,
	scopeRequirements,
	scopeText,
} from "./model";

const shown = 8;

const issueText = {
	degraded: (v: number) => `would enforce stale v${v}`,
	pending: (v: number) => `has applied only v${v}`,
	offline: () => "takes the change when it reconnects",
} as const;

const issueLabel = {
	degraded: "Degraded",
	pending: "Not yet applied",
	offline: "Offline",
} as const;

// PromoteDialog switches a ruleset's scope to enforced. It previews what
// the scope matches now, divides it by what each workload has earned,
// and submits the chosen workloads by id with the number it expects, so
// a set that no longer resolves as previewed is refused with both
// numbers rather than changed. The change is one recorded intent; the
// review shows convergence as each workload's applied version meets its
// latest, and there is nothing here to wait on.
export function PromoteDialog({
	open,
	onOpenChange,
	ruleset,
	verdict,
	onPromoted,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	ruleset: Ruleset;
	verdict: ReviewVerdictResult;
	onPromoted: () => void;
}) {
	const [generation, setGeneration] = useState(0);
	const { resource, reload } = useResource(async () => {
		if (!open) return null;
		const [preview, workloads] = await Promise.all([
			previewSelector(ruleset.scope),
			walkWorkloads(scopeRequirements(ruleset.scope)),
		]);
		return { preview, parts: partition(preview.matched, workloads) };
	}, [open, generation, ruleset.name]);
	const write = useWrite();
	const [optedIn, setOptedIn] = useState<ReadonlySet<string>>(new Set());
	const [acknowledged, setAcknowledged] = useState(false);
	const [submitting, setSubmitting] = useState(false);
	const [problem, setProblem] = useState<ProblemError | null>(null);
	// A fresh preview is in flight: the set on screen is the old one, and
	// nothing is submitted from it.
	const [previewing, setPreviewing] = useState(false);
	// biome-ignore lint/correctness/useExhaustiveDependencies: a new read settles the preview
	useEffect(() => setPreviewing(false), [resource]);

	useEffect(() => {
		if (open) {
			setOptedIn(new Set());
			setAcknowledged(false);
			setProblem(null);
		}
	}, [open]);

	function preview() {
		setPreviewing(true);
		setProblem(null);
		setOptedIn(new Set());
		setGeneration((g) => g + 1);
	}

	const ready = resource.status === "ready" ? resource.data : null;
	const ids = ready ? promotionIds(ready.parts, optedIn) : [];
	const k = verdict.kpis;
	// Known evidence loss is overridden by the same acknowledgment; when
	// dropped pairs lead it, the line names the loss too, since the
	// banner above shows the pairs alone.
	const incomplete = verdict.failing.some((c) => c.id === "evidence-gaps");

	async function submit() {
		setSubmitting(true);
		setProblem(null);
		try {
			await write(() =>
				createModeChange({
					workload_ids: ids,
					target_mode: "enforced",
					expected_match_count: ids.length,
				}),
			);
			onPromoted();
		} catch (err) {
			setProblem(err as ProblemError);
		} finally {
			setSubmitting(false);
		}
	}

	const mismatch =
		problem?.type === ProblemType.matchCountMismatch ? problem.problem : null;
	// A workload deleted between the preview and the submit is refused as
	// an id nothing is registered under; the remedy is the same.
	const gone =
		problem?.type === ProblemType.validation &&
		(problem.problem.errors ?? []).some((f) =>
			f.path.startsWith("workload_ids"),
		)
			? problem.problem
			: null;
	const blocked = mismatch !== null || gone !== null;
	const canSubmit =
		ready !== null &&
		!previewing &&
		ids.length > 0 &&
		!submitting &&
		!blocked &&
		(verdict.safe || acknowledged);

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent width={600}>
				<DialogHeader title="Switch scope to enforced">
					<p className="type-body text-secondary">
						Denied inbound traffic on these workloads will be dropped, starting
						with the next policy version each agent applies.
					</p>
				</DialogHeader>
				<DialogBody className="gap-4">
					<div
						className={cn(
							"flex items-start gap-2.5 rounded-lg border px-4 py-3",
							verdict.safe
								? "border-status-ok-border bg-status-ok-bg"
								: "border-status-critical-border bg-status-critical-bg",
						)}
					>
						<StatusGlyph
							status={verdict.safe ? "allowed" : "error"}
							size="lg"
							className="mt-0.5"
						/>
						<div className="flex min-w-0 flex-col gap-0.5">
							<div
								className={cn(
									"type-body-strong",
									verdict.safe
										? "text-status-ok-fg"
										: "text-status-critical-fg",
								)}
							>
								{verdict.headline}
							</div>
							<div className="type-ui text-secondary">
								{verdict.safe
									? "No observed traffic would be dropped."
									: k.pairs > 0
										? `${count(k.pairs)} peer/service ${k.pairs === 1 ? "pair" : "pairs"} (${short(k.connections)} connections) would start being dropped; ${k.recent} of them ${k.recent === 1 ? "was" : "were"} seen in the last hour.`
										: (verdict.caveats[0] ?? verdict.sub)}
							</div>
						</div>
					</div>
					{resource.status === "loading" ? (
						<LoadingRow what="the scope's workloads" />
					) : resource.status === "error" ? (
						<ProblemNotice
							what="the scope's workloads"
							error={resource.error}
							onRetry={reload}
						/>
					) : ready ? (
						<>
							<Matched
								scope={scopeText(ruleset.scope)}
								count={ready.preview.count}
								hostnames={ready.preview.matched.map((w) => w.hostname)}
							/>
							<PartitionTable
								parts={ready.parts}
								optedIn={optedIn}
								onToggle={(id) =>
									setOptedIn((cur) => {
										const next = new Set(cur);
										if (next.has(id)) next.delete(id);
										else next.add(id);
										return next;
									})
								}
								onPreview={preview}
							/>
						</>
					) : null}
					<p className="type-caption text-tertiary">
						Mode is per workload. This records one mode change covering these
						workloads; each workload's sync state shows when its agent has
						applied the new version. Rollback is the same action in reverse.
					</p>
					{!verdict.safe ? (
						<label className="flex cursor-pointer items-start gap-2.5 rounded-lg border border-default bg-subtle px-3 py-2.5 type-ui text-primary">
							<input
								type="checkbox"
								checked={acknowledged}
								onChange={(ev) => setAcknowledged(ev.target.checked)}
								className="mt-[3px] size-3.5 shrink-0"
							/>
							<span>
								{k.pairs > 0
									? `I understand ${count(k.pairs)} peer/service ${k.pairs === 1 ? "pair" : "pairs"} (${count(k.connections)} connections) will be dropped${incomplete ? ", and that the evidence for this range is incomplete" : ""}`
									: "I understand this scope is not safe to enforce yet"}
							</span>
						</label>
					) : null}
					{mismatch ? (
						<StaleSet
							title="The set no longer resolves as previewed"
							onPreview={preview}
						>
							You submitted{" "}
							<span className="type-mono-ui">{mismatch.expected}</span>{" "}
							workloads; the control plane resolved{" "}
							<span className="type-mono-ui">{mismatch.matched}</span>. Nothing
							was changed.
						</StaleSet>
					) : gone ? (
						<StaleSet
							title="A workload in this set is no longer registered"
							onPreview={preview}
						>
							{gone.errors?.map((f) => f.message).join(" ")} Nothing was
							changed.
						</StaleSet>
					) : problem ? (
						<SeverityNote level="error" role="alert" className="type-ui">
							{problem.problem.errors?.map((f) => f.message).join(" ") ||
								problem.problem.detail ||
								problem.problem.title}
						</SeverityNote>
					) : null}
				</DialogBody>
				<DialogFooter>
					<DialogClose asChild>
						<Button variant="secondary">Cancel</Button>
					</DialogClose>
					<Button variant="primary" disabled={!canSubmit} onClick={submit}>
						{submitting
							? "Enforcing…"
							: `${verdict.safe ? "Enforce" : "Enforce anyway"} on ${count(ids.length)} ${ids.length === 1 ? "workload" : "workloads"}`}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// Matched is what the scope's selector resolves to now: the same
// resolution a mode change by selector would use.
function Matched({
	scope,
	count: n,
	hostnames,
}: {
	scope: string;
	count: number;
	hostnames: string[];
}) {
	return (
		<div className="flex flex-col gap-1 type-ui" data-testid="preview">
			<span className="text-secondary">
				<span className="type-mono-sm text-secondary">{scope}</span> matches{" "}
				<span className="type-mono-ui text-primary">{count(n)}</span>{" "}
				{n === 1 ? "workload" : "workloads"} now
			</span>
			<span className="type-mono-sm text-tertiary">
				{hostnames.slice(0, shown).join(" ")}
				{hostnames.length > shown ? ` +${hostnames.length - shown} more` : ""}
			</span>
		</div>
	);
}

function PartitionTable({
	parts,
	optedIn,
	onToggle,
	onPreview,
}: {
	parts: Partition;
	optedIn: ReadonlySet<string>;
	onToggle: (id: string) => void;
	onPreview: () => void;
}) {
	const row =
		"grid min-h-row-dense grid-cols-[1fr_auto_auto] items-center gap-x-4 border-t border-subtle px-3 py-2 first:border-t-0";
	const n = (x: number) => `${count(x)} ${x === 1 ? "workload" : "workloads"}`;
	const num = "type-mono-sm text-secondary";
	const included = "type-ui-strong text-primary";
	const skipped = "text-tertiary";
	return (
		<div
			className="overflow-hidden rounded-lg border border-default bg-app type-ui"
			data-testid="partition"
		>
			<div className={row}>
				<span>Simulation → Enforced</span>
				<span className={num}>{n(parts.included.length)}</span>
				<span className={included}>included</span>
			</div>
			{parts.visibility.length > 0 ? (
				<div className={row}>
					<span>
						Visibility → Enforced{" "}
						<span className="type-caption text-tertiary">never simulated</span>
					</span>
					<span className={num}>{n(parts.visibility.length)}</span>
					<span className={skipped}>skipped</span>
				</div>
			) : null}
			{parts.unsynced.map(({ workload: w, issue }) => {
				const on = optedIn.has(w.id);
				return (
					<div key={w.id} className={row}>
						<label className="flex cursor-pointer items-start gap-2">
							<input
								type="checkbox"
								checked={on}
								onChange={() => onToggle(w.id)}
								className="mt-[3px] size-3.5 shrink-0"
							/>
							<span className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
								<span>
									{issueLabel[issue]} ·{" "}
									<Link
										to={`/workloads/${w.id}`}
										className="type-mono-ui text-link hover:underline"
									>
										{w.hostname}
									</Link>
								</span>{" "}
								<SeverityNote level="alert" className="type-caption">
									{issueText[issue](w.sync.applied_version)}
								</SeverityNote>
							</span>
						</label>
						<span className={num}>{n(1)}</span>
						<span className={on ? included : skipped}>
							{on ? "included" : "skipped"}
						</span>
					</div>
				);
			})}
			{parts.enforced.length > 0 ? (
				<div className={row}>
					<span>Already enforced</span>
					<span className={num}>{n(parts.enforced.length)}</span>
					<span className={skipped}>unchanged</span>
				</div>
			) : null}
			{parts.unlisted.length > 0 ? (
				<div className={row}>
					<SeverityNote level="alert">
						Matched but not listed: the fleet moved between reads
					</SeverityNote>
					<span className={num}>{n(parts.unlisted.length)}</span>
					<button
						type="button"
						onClick={onPreview}
						className="cursor-pointer text-link hover:underline"
					>
						Preview again
					</button>
				</div>
			) : null}
		</div>
	);
}

function StaleSet({
	title,
	children,
	onPreview,
}: {
	title: string;
	children: React.ReactNode;
	onPreview: () => void;
}) {
	return (
		<div
			role="alert"
			className="flex flex-col gap-1.5 rounded-lg border border-status-warn-border bg-status-warn-bg px-3 py-2.5 type-ui"
		>
			<SeverityNote level="alert" className="font-medium">
				{title}
			</SeverityNote>
			<span className="text-secondary">{children}</span>
			<Button
				variant="secondary"
				size="sm"
				className="mt-1 self-start"
				onClick={onPreview}
			>
				Preview again
			</Button>
		</div>
	);
}
