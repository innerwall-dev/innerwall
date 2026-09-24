import { useEffect, useState } from "react";
import type { ProblemError } from "@/api/client";
import { createModeChange } from "@/api/fleet";
import { type Mode, ProblemType, type Workload } from "@/api/schema";
import { ChoiceChips, modes } from "@/components/fleet/status";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogBody,
	DialogClose,
	DialogContent,
	DialogFooter,
	DialogHeader,
} from "@/components/ui/dialog";
import { useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";

const order: Mode[] = ["visibility", "simulation", "enforced"];
const shown = 8;

// ModeChangeDialog sets the enforcement mode of the workloads the
// operator selected, by id. The request states how many workloads it
// expects to change, so a selection the control plane resolves
// differently (a workload gone since the list was read) is refused with
// both numbers rather than applied to a set nobody reviewed. Success is
// an acknowledgement of the recorded intent, not of progress: the list
// shows convergence as each workload's applied version meets latest.
export function ModeChangeDialog({
	open,
	workloads,
	onOpenChange,
	onChanged,
	onReload,
}: {
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

	useEffect(() => {
		if (open) {
			setTarget(null);
			setProblem(null);
		}
	}, [open]);

	const already = target
		? workloads.filter((w) => w.mode === target).length
		: 0;

	async function submit() {
		if (!target) return;
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
				<DialogHeader title="Change mode for selected">
					<p className="text-[12px] text-foreground-tertiary">
						Sets the enforcement mode of{" "}
						{workloads.length === 1
							? "this workload"
							: `these ${workloads.length} workloads`}
						. Each agent takes it with the next policy version it applies; the
						sync column shows when it has.
					</p>
				</DialogHeader>
				<DialogBody>
					<div className="flex flex-col gap-[5px]">
						<span className="text-[12px] text-foreground-tertiary">
							Workloads
						</span>
						<ul className="flex flex-wrap gap-x-3 gap-y-1 font-mono text-[12px]">
							{workloads.slice(0, shown).map((w) => (
								<li key={w.id} className="flex items-center gap-1.5">
									<span>{w.hostname}</span>
									<span
										className={cn("text-[11px]", modes[w.mode].cls)}
										title={modes[w.mode].label}
									>
										{modes[w.mode].glyph}
									</span>
								</li>
							))}
							{workloads.length > shown ? (
								<li className="text-muted-foreground">
									+{workloads.length - shown} more
								</li>
							) : null}
						</ul>
					</div>
					<div className="flex flex-col gap-[5px]">
						<span className="text-[12px] text-foreground-tertiary">
							Target mode
						</span>
						<ChoiceChips
							legend="Target mode"
							name="target-mode"
							value={target}
							onChange={setTarget}
							className="text-[12.5px]"
							options={order.map((m) => ({
								value: m,
								label: (
									<>
										<span aria-hidden="true" className={modes[m].cls}>
											{modes[m].glyph}
										</span>
										{modes[m].label}
									</>
								),
							}))}
						/>
						{target && already > 0 ? (
							<span className="text-[11px] text-muted-foreground">
								{already === workloads.length
									? `All ${already} are already in ${target}; nothing changes.`
									: `${already} already in ${target}; the other ${workloads.length - already} change.`}
							</span>
						) : null}
					</div>
					{mismatch ? (
						<div
							role="alert"
							className="flex flex-col gap-1 rounded border border-status-would-block-border bg-status-would-block-surface px-3 py-2.5 text-[12px]"
						>
							<span className="font-semibold text-status-degraded">
								▲ The selection no longer matches
							</span>
							<span className="text-foreground-secondary">
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
								className="mt-1 self-start rounded-chip"
								onClick={onReload}
							>
								Reload the list
							</Button>
						</div>
					) : problem ? (
						<p
							role="alert"
							className="flex items-start gap-2 text-[12px] text-destructive"
						>
							<span className="font-mono" aria-hidden="true">
								✕
							</span>
							<span>
								{problem.problem.errors?.map((f) => f.message).join(" ") ||
									problem.problem.detail ||
									problem.problem.title}
							</span>
						</p>
					) : null}
				</DialogBody>
				<DialogFooter>
					<DialogClose asChild>
						<Button variant="secondary">Cancel</Button>
					</DialogClose>
					<Button
						disabled={!target || submitting || mismatch !== null}
						onClick={submit}
					>
						{submitting
							? "Changing…"
							: target
								? `Change to ${target}`
								: "Change mode"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
