import { useEffect, useId, useRef, useState } from "react";
import { Link } from "react-router";
import type { ProblemError } from "@/api/client";
import { createModeChange } from "@/api/fleet";
import { type Mode, ProblemType, type Workload } from "@/api/schema";
import { ChoiceChips, modes } from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { defaultRange } from "@/components/RangeControl";
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
import { count } from "@/lib/format";
import { useResource, useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { loadWorkloadEvidence } from "../review/data";
import { type EnforceEvidence, enforceEvidence } from "../review/model";

const order: Mode[] = ["visibility", "simulation", "enforced"];
const shown = 8;

// ModeChangeDialog sets the enforcement mode of the workloads the
// operator selected, by id. The request states how many workloads it
// expects to change, so a selection the control plane resolves
// differently (a workload gone since the list was read) is refused with
// both numbers rather than applied to a set nobody reviewed. Success is
// an acknowledgement of the recorded intent, not of progress: the list
// shows convergence as each workload's applied version meets latest.
//
// A change to enforced from here does not pass through the simulation
// review, so it brings the review's evidence with it: each workload it
// would move is given the verdict the review composes for it, and when
// any of them is not safe, or never simulated, the confirm waits on the
// same explicit acknowledgment a promotion past an unsafe verdict takes
// (ADR-0001). It stays the deliberate way to enforce what the review
// would not offer, and says so. A change to visibility or simulation
// only loosens, and stays one step.
export function ModeChangeDialog({
	title = "Change mode for selected",
	open,
	workloads,
	onOpenChange,
	onChanged,
	onReload,
}: {
	title?: string;
	open: boolean;
	workloads: Workload[];
	onOpenChange: (open: boolean) => void;
	onChanged: () => void;
	onReload: () => void;
}) {
	const write = useWrite();
	const [target, setTarget] = useState<Mode | null>(null);
	const [submitting, setSubmitting] = useState(false);
	const [problem, setProblem] = useState<ProblemError | null>(null);
	const [acknowledged, setAcknowledged] = useState(false);
	// The operator tried to confirm before acknowledging: the
	// acknowledgment says what it is waiting for and takes the focus.
	const [nudged, setNudged] = useState(false);
	const ackRef = useRef<HTMLInputElement>(null);
	const ackHintId = useId();

	useEffect(() => {
		if (open) {
			setTarget(null);
			setProblem(null);
		}
	}, [open]);
	// biome-ignore lint/correctness/useExhaustiveDependencies: a new target or a reopened dialog asks again
	useEffect(() => {
		setAcknowledged(false);
		setNudged(false);
	}, [open, target]);

	const enforcing = open && target === "enforced";
	const moving = workloads.filter((w) => w.mode !== "enforced");
	const { resource: evidence, reload: reloadEvidence } = useResource(
		async () =>
			enforcing
				? enforceEvidence(workloads, await loadWorkloadEvidence(moving))
				: null,
		[enforcing, workloads.map((w) => `${w.id}:${w.mode}`).join(",")],
	);
	const judged =
		enforcing && evidence.status === "ready" ? evidence.data : null;
	const unsupported = judged
		? judged.unsimulated.length + judged.notSafe.length
		: 0;
	// The evidence for a change to enforced is still being read or could
	// not be: nothing is submitted without it.
	const gateHeld = enforcing && moving.length > 0 && judged === null;
	const awaitingAck = unsupported > 0 && !acknowledged;

	const already = target
		? workloads.filter((w) => w.mode === target).length
		: 0;

	async function submit() {
		if (!target) return;
		// The confirm is never a disabled control while it waits on the
		// evidence or the acknowledgment: a click it cannot act on changes
		// nothing and keeps the dialog open.
		if (gateHeld) return;
		if (awaitingAck) {
			setNudged(true);
			ackRef.current?.focus();
			return;
		}
		setSubmitting(true);
		setProblem(null);
		try {
			await write(() =>
				createModeChange({
					workload_ids: workloads.map((w) => w.id),
					target_mode: target,
					expected_match_count: workloads.length,
				}),
			);
			onChanged();
		} catch (err) {
			setProblem(err as ProblemError);
		} finally {
			setSubmitting(false);
		}
	}

	const mismatch =
		problem?.type === ProblemType.matchCountMismatch ? problem.problem : null;

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent width={560}>
				<DialogHeader title={title}>
					<p className="type-dense text-secondary">
						Sets the enforcement mode of{" "}
						{workloads.length === 1
							? "this workload"
							: `these ${workloads.length} workloads`}
						. Each agent takes it with the next policy version it applies; the
						sync column shows when it has.
					</p>
				</DialogHeader>
				<DialogBody>
					<div className="flex flex-col gap-1.5">
						<span className="type-control text-secondary">Workloads</span>
						<ul className="flex flex-wrap gap-x-3 gap-y-1 type-mono-sm text-primary">
							{workloads.slice(0, shown).map((w) => (
								<li key={w.id} className="flex items-center gap-1.5">
									<span>{w.hostname}</span>
									<span className="inline-flex" title={modes[w.mode].label}>
										{modes[w.mode].glyph}
									</span>
								</li>
							))}
							{workloads.length > shown ? (
								<li className="text-tertiary">
									+{workloads.length - shown} more
								</li>
							) : null}
						</ul>
					</div>
					<div className="flex flex-col gap-1.5">
						<span className="type-control text-secondary">Target mode</span>
						<ChoiceChips
							legend="Target mode"
							name="target-mode"
							value={target}
							onChange={setTarget}
							options={order.map((m) => ({
								value: m,
								label: (
									<>
										{modes[m].glyph}
										{modes[m].label}
									</>
								),
							}))}
						/>
						{target && already > 0 ? (
							<span className="type-caption text-tertiary">
								{already === workloads.length
									? `All ${already} are already in ${target}; nothing changes.`
									: `${already} already in ${target}; the other ${workloads.length - already} change.`}
							</span>
						) : null}
					</div>
					{enforcing && moving.length > 0 ? (
						evidence.status === "error" ? (
							<ProblemNotice
								what="each workload's evidence"
								error={evidence.error}
								onRetry={reloadEvidence}
							/>
						) : judged ? (
							<Evidence
								judged={judged}
								selected={workloads.length}
								unsupported={unsupported}
							/>
						) : (
							<LoadingRow what="each workload's evidence" />
						)
					) : null}
					{judged && unsupported > 0 ? (
						<div className="flex flex-col gap-1.5">
							<label
								className={cn(
									"flex cursor-pointer items-start gap-2.5 rounded-panel border bg-card px-3 py-2.5 type-dense text-primary",
									nudged && !acknowledged
										? "border-status-warn-border"
										: "border-hairline",
								)}
							>
								<input
									ref={ackRef}
									type="checkbox"
									checked={acknowledged}
									onChange={(ev) => {
										setAcknowledged(ev.target.checked);
										setNudged(false);
									}}
									aria-describedby={nudged ? ackHintId : undefined}
									className="mt-[3px] size-3.5 shrink-0"
								/>
								<span>
									{workloads.length === 1
										? "I understand this workload is not safe to enforce yet"
										: `I understand ${count(unsupported)} of these ${count(workloads.length)} workloads ${unsupported === 1 ? "is" : "are"} not safe to enforce yet`}
								</span>
							</label>
							{nudged && !acknowledged ? (
								<div id={ackHintId} role="status">
									<SeverityNote level="alert" prose className="type-caption">
										Nothing was enforced. Check the acknowledgment above to
										enforce anyway.
									</SeverityNote>
								</div>
							) : null}
						</div>
					) : null}
					{mismatch ? (
						<div
							role="alert"
							className="flex flex-col gap-1 rounded-panel border border-status-warn-border bg-status-warn-bg px-4 py-3"
						>
							<span className="flex items-center gap-2 type-dense-strong text-status-warn-fg">
								<StatusGlyph status="alert" size="lg" />
								The selection no longer matches
							</span>
							<span className="type-dense text-secondary">
								You selected{" "}
								<span className="font-mono">{mismatch.expected}</span>{" "}
								workloads; the control plane resolved{" "}
								<span className="font-mono">{mismatch.matched}</span>. Nothing
								was changed. The fleet moved since this list was read; reload it
								and select again.
							</span>
							<Button
								variant="secondary"
								size="sm"
								className="mt-1 self-start"
								onClick={onReload}
							>
								Reload the list
							</Button>
						</div>
					) : problem ? (
						<SeverityNote level="error" role="alert" className="type-dense">
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
					{/* A confirm held back by the evidence or the acknowledgment
					    is the plate's disabled state, kept focusable so its
					    reason can be reached. */}
					<Button
						disabled={!target || submitting || mismatch !== null}
						aria-disabled={gateHeld || awaitingAck || undefined}
						onClick={submit}
					>
						{submitting
							? "Changing…"
							: target === "enforced" && unsupported > 0
								? `Enforce anyway on ${count(moving.length)} ${moving.length === 1 ? "workload" : "workloads"}`
								: target
									? `Change to ${target}`
									: "Change mode"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// Evidence is the verdict block of a change to enforced: the review's
// own headline for the set, how many of the selection no verdict stands
// behind, and the workloads behind each count.
function Evidence({
	judged,
	selected,
	unsupported,
}: {
	judged: EnforceEvidence;
	selected: number;
	unsupported: number;
}) {
	const safe = unsupported === 0;
	const u = judged.unsimulated.length;
	const k = judged.notSafe.length;
	const counts = [
		u > 0
			? `No simulation evidence for ${count(u)} of ${count(selected)} selected`
			: null,
		k > 0
			? `Not safe to enforce for ${count(k)} of ${count(selected)} selected`
			: null,
	].filter((x): x is string => x !== null);
	const n = (x: number) => `${count(x)} ${x === 1 ? "workload" : "workloads"}`;
	const row =
		"grid min-h-row-pitch grid-cols-[1fr_auto] items-center gap-x-4 border-t border-hairline px-3 py-2 first:border-t-0";
	const num = "type-mono-sm text-secondary";
	const host = (w: Workload) => (
		<Link key={w.id} to={`/workloads/${w.id}`} className="type-mono-data link">
			{w.hostname}
		</Link>
	);
	return (
		<div className="flex flex-col gap-3" data-testid="evidence">
			<div
				className={cn(
					"flex items-start gap-2.5 rounded-panel border px-4 py-3",
					safe
						? "border-status-ok-border bg-status-ok-bg"
						: "border-status-critical-border bg-status-critical-bg",
				)}
			>
				<StatusGlyph
					status={safe ? "allowed" : "error"}
					size="lg"
					className="mt-0.5"
				/>
				<div className="flex min-w-0 flex-col gap-0.5">
					<div
						className={cn(
							"type-dense-strong",
							safe ? "text-status-ok-fg" : "text-status-critical-fg",
						)}
					>
						{safe ? "Safe to enforce" : "Not safe to enforce yet"}
					</div>
					<div className="type-dense text-secondary">
						{safe
							? "No observed traffic would be dropped."
							: `${counts.join("; ")}.`}
					</div>
				</div>
			</div>
			{!safe ? (
				<div className="overflow-hidden rounded-panel border border-overlay bg-canvas type-dense">
					{u > 0 ? (
						<div className={row}>
							<span className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
								<span>
									Never simulated{" "}
									<span className="type-caption text-tertiary">no verdict</span>
								</span>
								{judged.unsimulated.slice(0, shown).map(host)}
								{u > shown ? (
									<span className="type-caption text-tertiary">
										+{u - shown} more
									</span>
								) : null}
							</span>
							<span className={num}>{n(u)}</span>
						</div>
					) : null}
					{judged.notSafe.slice(0, shown).map(({ workload: w, verdict }) => (
						<div key={w.id} className={row}>
							<span className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
								{host(w)}
								<SeverityNote level="alert" className="type-caption">
									{verdict.failing[0]?.text ?? verdict.headline}
								</SeverityNote>
							</span>
							<span className={num}>{n(1)}</span>
						</div>
					))}
					{k > shown ? (
						<div className={row}>
							<span className="type-caption text-tertiary">
								+{k - shown} more not safe to enforce
							</span>
							<span className={num}>{n(k - shown)}</span>
						</div>
					) : null}
					{judged.safe.length > 0 ? (
						<div className={row}>
							<span>Safe to enforce</span>
							<span className={num}>{n(judged.safe.length)}</span>
						</div>
					) : null}
					{judged.enforced.length > 0 ? (
						<div className={row}>
							<span>Already enforced</span>
							<span className={num}>{n(judged.enforced.length)}</span>
						</div>
					) : null}
				</div>
			) : null}
			<p className="type-caption text-tertiary">
				Each verdict is the one the{" "}
				<Link to="/simulation" className="link">
					simulation review
				</Link>{" "}
				composes for the workload over the last {defaultRange}.
				{safe
					? ""
					: " This enforces the selection as chosen; the review promotes only the workloads that earned it."}
			</p>
		</div>
	);
}
