import { Link } from "react-router";
import type { DryRunResult, Finding } from "@/api/schema";
import { Eyebrow, modes } from "@/components/fleet/status";
import { Button } from "@/components/ui/button";
import { count } from "@/lib/format";
import { cn } from "@/lib/utils";
import { FindingLines } from "./Chips";
import { type DeltaLine, deltaLines, type Freshness } from "./model";

// DryRunState is the pane's one run: in flight, refused by admission
// (its findings are also placed on the fields they name), failed, or a
// result with how fresh it is.
export type DryRunState =
	| { status: "running" }
	| { status: "refused"; findings: Finding[] }
	| { status: "failed"; message: string }
	| {
			status: "done";
			result: DryRunResult;
			freshness: Freshness;
			label: (resolvedId: string) => string;
	  };

const kinds: Record<
	DeltaLine["kind"],
	{ glyph: string; word: string; cls: string }
> = {
	added: { glyph: "+", word: "gains", cls: "text-status-allowed" },
	removed: { glyph: "−", word: "loses", cls: "text-destructive" },
	changed: { glyph: "~", word: "changes", cls: "text-status-pending" },
};

// DryRunCard is the render-dryrun endpoint's reading: what saving the
// editor's unsaved changes would change on each workload, computed by the
// control plane against the persisted state and discarded. Running it
// writes nothing and is never required before saving.
export function DryRunCard({
	state,
	onRun,
	onReloadAndRun,
}: {
	state: DryRunState | null;
	onRun: () => void;
	onReloadAndRun: () => void;
}) {
	return (
		<section
			aria-label="Dry run"
			className="flex flex-col gap-3 rounded border border-border bg-surface-sidebar px-4 py-3.5"
		>
			<div className="flex items-center gap-3">
				<Eyebrow>Dry run — what saving these changes would change</Eyebrow>
				{state?.status === "done" ? (
					<Button
						variant="secondary"
						size="sm"
						className="ml-auto"
						onClick={state.freshness === "stale" ? onReloadAndRun : onRun}
					>
						{state.freshness === "stale" ? "Reload and re-run" : "Re-run"}
					</Button>
				) : null}
			</div>
			{state === null ? (
				<p className="text-[12px] text-muted-foreground">
					Render the current set with your unsaved changes and see each
					workload's added, removed, and changed rules. It reads; it never
					saves, and saving never waits for it.
				</p>
			) : state.status === "running" ? (
				<p
					className="font-mono text-[12px] text-muted-foreground"
					aria-busy="true"
				>
					Rendering…
				</p>
			) : state.status === "refused" ? (
				<div className="flex flex-col gap-1.5" role="alert">
					<p className="text-[12px] text-destructive">
						The control plane refused this set as a write would: the findings
						are shown at the fields they name.
					</p>
					<FindingLines findings={state.findings} />
				</div>
			) : state.status === "failed" ? (
				<p role="alert" className="text-[12px] text-destructive">
					The dry run failed: {state.message}
				</p>
			) : (
				<Result state={state} />
			)}
		</section>
	);
}

function Result({
	state,
}: {
	state: Extract<DryRunState, { status: "done" }>;
}) {
	const { result, freshness, label } = state;
	return (
		<div className="flex flex-col gap-3">
			{freshness !== "current" ? (
				<div
					role="status"
					data-testid="dryrun-freshness"
					className="rounded border border-status-would-block-border bg-status-would-block-surface px-3 py-2 text-[12px]"
				>
					<span className="font-semibold text-status-degraded">
						<span aria-hidden="true">▲</span>{" "}
						{freshness === "stale"
							? "The state moved before this ran."
							: "The state has moved since this ran."}
					</span>{" "}
					<span className="text-foreground-tertiary">
						{freshness === "stale"
							? "It was computed against the current state, but your changes were made on an older read. Reload and re-run to see them against what is stored now."
							: "A write has landed since. Re-run to see what saving would change now."}
					</span>
				</div>
			) : null}
			{result.workloads.length === 0 ? (
				<p
					className="text-[12px] text-foreground-tertiary"
					data-testid="dryrun-summary"
				>
					Nothing would change: no workload's rendered policy differs from what
					it has now.
				</p>
			) : (
				<>
					<p
						className="text-[12px] text-foreground-tertiary"
						data-testid="dryrun-summary"
					>
						Saving would change the rendered policy of{" "}
						<span className="font-mono text-foreground">
							{count(result.workloads.length)}
						</span>{" "}
						{result.workloads.length === 1 ? "workload" : "workloads"}.
					</p>
					<ul className="flex flex-col divide-y divide-border">
						{result.workloads.map((w) => (
							<li
								key={w.workload.id}
								className="flex flex-col gap-1 py-2"
								data-testid="dryrun-workload"
							>
								<div className="flex flex-wrap items-baseline gap-2 text-[12.5px]">
									<Link
										to={`/workloads/${encodeURIComponent(w.workload.id)}`}
										className="font-mono text-foreground hover:text-link"
									>
										{w.workload.hostname || w.workload.id}
									</Link>
									<span className="font-mono text-[11px] text-muted-foreground">
										{w.version > 0
											? `against v${w.version}`
											: "nothing rendered yet"}
									</span>
									{w.mode ? (
										<span className="text-[12px] text-foreground-tertiary">
											mode {modes[w.mode.from].label.toLowerCase()} →{" "}
											{modes[w.mode.to].label.toLowerCase()}
										</span>
									) : null}
								</div>
								<ul className="flex flex-col gap-0.5">
									{deltaLines(w, label).map((l, i) => (
										<li
											// biome-ignore lint/suspicious/noArrayIndexKey: lines are positional within one workload
											key={i}
											className="flex flex-wrap gap-x-2 font-mono text-[12px]"
										>
											<span
												className={cn("w-3", kinds[l.kind].cls)}
												aria-hidden="true"
											>
												{kinds[l.kind].glyph}
											</span>
											<span className="sr-only">{kinds[l.kind].word}</span>
											<span className="text-foreground">{l.service}</span>
											<span className="text-muted-foreground">
												from {l.peers}
											</span>
											<span className="font-sans text-foreground-tertiary">
												· {l.rule}
											</span>
											{l.detail ? (
												<span className="text-foreground-tertiary">
													({l.detail})
												</span>
											) : null}
										</li>
									))}
								</ul>
							</li>
						))}
					</ul>
				</>
			)}
		</div>
	);
}
