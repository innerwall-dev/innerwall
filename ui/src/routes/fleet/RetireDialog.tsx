import { useEffect, useId, useRef, useState } from "react";
import type { ProblemError } from "@/api/client";
import { retireWorkload } from "@/api/fleet";
import { ProblemType, type RetirementAck, type Workload } from "@/api/schema";
import { SeverityNote } from "@/components/StatusGlyph";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogBody,
	DialogClose,
	DialogContent,
	DialogFooter,
	DialogHeader,
} from "@/components/ui/dialog";
import { ago, count, exact } from "@/lib/format";
import { useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";

// Outcome is what one retirement came to: committed, refused because no
// workload is registered under the id, refused because the workload was
// already retired (with the instant it was), or refused otherwise.
export type Outcome =
	| { kind: "retired"; ack: RetirementAck }
	| { kind: "gone" }
	| { kind: "already"; at: string | null }
	| { kind: "failed"; message: string };

const shown = 8;

// RetireDialog retires workloads, one way (ADR-0023). It says what
// retiring does before anything is sent and asks for an explicit
// acknowledgment that it cannot be undone, as the promote dialog does
// for a scope that is not safe to enforce. The surface retires one
// workload per request, so a selection is retired one request at a time,
// and each workload's outcome is its own: a refusal says what it was and
// that nothing changed for that workload.
export function RetireDialog({
	open,
	workloads,
	onOpenChange,
	onRetired,
}: {
	open: boolean;
	workloads: readonly Workload[];
	onOpenChange: (open: boolean) => void;
	// onRetired is called once every request has settled and at least
	// one workload was retired, so the screen reads the fleet again; the
	// dialog closes itself when every request succeeded.
	onRetired: () => void;
}) {
	const write = useWrite();
	const [acknowledged, setAcknowledged] = useState(false);
	const [nudged, setNudged] = useState(false);
	const [submitting, setSubmitting] = useState(false);
	const [outcomes, setOutcomes] = useState<Map<string, Outcome> | null>(null);
	const ackRef = useRef<HTMLInputElement>(null);
	const ackHintId = useId();

	useEffect(() => {
		if (open) {
			setAcknowledged(false);
			setNudged(false);
			setOutcomes(null);
		}
	}, [open]);

	const n = workloads.length;
	const one = n === 1 ? workloads[0] : undefined;
	const settled = outcomes !== null;

	async function retire() {
		// The confirm is never a disabled control: a click it cannot act
		// on stays here, changes nothing, and points at the
		// acknowledgment.
		if (!acknowledged) {
			setNudged(true);
			ackRef.current?.focus();
			return;
		}
		if (submitting || settled || n === 0) return;
		setSubmitting(true);
		const results = new Map<string, Outcome>();
		for (const w of workloads) {
			try {
				const ack = await write(() => retireWorkload(w.id));
				results.set(w.id, { kind: "retired", ack });
			} catch (err) {
				results.set(w.id, refusal(err as ProblemError));
			}
		}
		setSubmitting(false);
		const done = [...results.values()].filter((o) => o.kind === "retired");
		if (done.length > 0) onRetired();
		if (done.length === n) onOpenChange(false);
		else setOutcomes(results);
	}

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent width={560}>
				<DialogHeader
					title={
						one ? `Retire ${one.hostname}?` : `Retire ${count(n)} workloads?`
					}
				>
					<p className="type-dense text-secondary">
						Retiring ends {one ? "this workload's" : "each workload's"} identity
						for good. It is for a host that is gone, one that was enrolled again
						as a new workload, or one whose key may have been copied.
					</p>
				</DialogHeader>
				<DialogBody className="gap-4">
					{one ? null : (
						<div
							className="flex flex-col gap-1 type-dense"
							data-testid="retiring"
						>
							<span className="text-secondary">
								<span className="type-mono-data text-primary">{count(n)}</span>{" "}
								workloads
							</span>
							<span className="type-mono-sm text-tertiary">
								{workloads
									.slice(0, shown)
									.map((w) => w.hostname)
									.join(" ")}
								{n > shown ? ` +${n - shown} more` : ""}
							</span>
						</div>
					)}
					<ul className="flex list-disc flex-col gap-1.5 pl-5 type-dense text-primary">
						<li>
							Its addresses leave every peer set at once; each workload whose
							policy changes advances a version.
						</li>
						<li>
							Its credential is refused from now on, at renewal and on every
							agent call, and a live sync stream ends.
						</li>
						<li>
							Its labels and mode are frozen. Its flows and evidence gaps stay
							readable under its id.
						</li>
						<li>
							It cannot be undone. A host that should come back enrolls again
							with a provisioning token, as a new workload with a new id and no
							history.
						</li>
					</ul>
					<p className="type-caption text-tertiary">
						Retiring does not revoke the provisioning token that enrolled it;
						revoke the token too if nothing should enroll with it again.
					</p>
					{settled ? (
						<Outcomes workloads={workloads} outcomes={outcomes} />
					) : (
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
									I understand retiring cannot be undone, and that{" "}
									{one ? "the host returns" : "a host returns"} only by
									enrolling again, as a new identity
								</span>
							</label>
							{nudged && !acknowledged ? (
								<div id={ackHintId} role="status">
									<SeverityNote level="alert" prose className="type-caption">
										Nothing was retired. Check the acknowledgment above to
										retire.
									</SeverityNote>
								</div>
							) : null}
						</div>
					)}
				</DialogBody>
				<DialogFooter>
					<DialogClose asChild>
						<Button variant="secondary">{settled ? "Close" : "Cancel"}</Button>
					</DialogClose>
					{settled ? null : (
						// The irreversible confirm is the destructive plate; held
						// back until the acknowledgment, it is the plate's disabled
						// state, kept focusable so its reason can be reached.
						<Button
							variant="destructive"
							aria-disabled={!acknowledged || submitting || undefined}
							onClick={retire}
						>
							{submitting
								? "Retiring…"
								: one
									? "Retire workload"
									: `Retire ${count(n)} workloads`}
						</Button>
					)}
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// refusal reads a refused retirement: an id nothing is registered under,
// a workload already retired (the problem names when), or another
// problem in its own words.
export function refusal(p: ProblemError): Outcome {
	if (p.type === ProblemType.notFound) return { kind: "gone" };
	if (p.type === ProblemType.workloadRetired)
		return { kind: "already", at: p.problem.retired_at ?? null };
	return { kind: "failed", message: p.problem.detail ?? p.problem.title };
}

// Outcomes lists each workload's outcome once the requests settled with
// at least one refused: the retired ones as done, each refusal as what
// it was, saying nothing changed for that workload.
function Outcomes({
	workloads,
	outcomes,
}: {
	workloads: readonly Workload[];
	outcomes: Map<string, Outcome>;
}) {
	return (
		<ul
			className="flex flex-col overflow-hidden rounded-panel border border-hairline bg-canvas type-dense"
			data-testid="retire-outcomes"
		>
			{workloads.map((w) => {
				const o = outcomes.get(w.id);
				if (!o) return null;
				return (
					<li
						key={w.id}
						className="flex min-h-row-pitch flex-col justify-center gap-0.5 border-t border-hairline px-3 py-2 first:border-t-0"
					>
						<span className="type-mono-data text-primary">{w.hostname}</span>
						<OutcomeText o={o} />
					</li>
				);
			})}
		</ul>
	);
}

export function OutcomeText({ o }: { o: Outcome }) {
	switch (o.kind) {
		case "retired":
			return (
				<span className="text-secondary">
					Retired{" "}
					<time dateTime={o.ack.retired_at} className="type-mono-sm">
						{exact(o.ack.retired_at)}
					</time>
					; {count(o.ack.versions_advanced)}{" "}
					{o.ack.versions_advanced === 1 ? "workload's" : "workloads'"} policy
					advanced.
				</span>
			);
		case "gone":
			return (
				<SeverityNote level="error" role="alert" prose size="sm">
					No workload is registered under this id; nothing was retired.
				</SeverityNote>
			);
		case "already":
			return (
				<SeverityNote level="alert" role="alert" prose size="sm">
					Already retired
					{o.at ? (
						<>
							{" "}
							<time dateTime={o.at} className="type-mono-sm">
								{ago(o.at)}
							</time>{" "}
							(<span className="type-mono-sm">{exact(o.at)}</span>)
						</>
					) : null}
					; nothing changed.
				</SeverityNote>
			);
		default:
			return (
				<SeverityNote level="error" role="alert" prose size="sm">
					{o.message} Nothing was retired.
				</SeverityNote>
			);
	}
}
