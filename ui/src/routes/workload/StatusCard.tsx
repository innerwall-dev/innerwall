import { useState } from "react";
import type { ProblemError } from "@/api/client";
import { resendSnapshot } from "@/api/fleet";
import { ProblemType, type Workload } from "@/api/schema";
import { syncStates } from "@/components/fleet/status";
import { Button } from "@/components/ui/button";
import { ago, since } from "@/lib/format";
import { useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { version } from "../fleet/describe";

type Outcome =
	| { kind: "idle" }
	| { kind: "sent"; before: string | null }
	| { kind: "offline"; lastSeen: string | null }
	| { kind: "failed"; message: string };

// StatusCard is the workload's convergence with its rendered policy: the
// applied version against the latest with the instants the surface
// records, the apply error when there is one, and the snapshot action.
// The degraded form is the design's; the others are drawn from it.
export function StatusCard({
	w,
	onReread,
}: {
	w: Workload;
	onReread: () => void;
}) {
	const s = w.sync;
	const lag = s.latest_version > s.applied_version;
	const tone = {
		degraded: "border-status-would-block-border bg-status-would-block-surface",
		pending: "border-accent-border",
		synced: "border-border",
		offline: "border-border",
	}[s.state];

	return (
		<div
			className={cn("flex flex-col gap-1.5 rounded border px-3 py-2.5", tone)}
			data-testid="status-card"
		>
			<div
				className={cn(
					"flex items-center gap-2 font-semibold",
					syncStates[s.state].cls,
				)}
			>
				<span aria-hidden="true">{syncStates[s.state].glyph}</span>
				<span>{headline(w)}</span>
			</div>
			<dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-[3px] text-[12px] text-foreground-secondary">
				<dt className="text-muted-foreground">applied</dt>
				<dd className="font-mono">{version(s.applied_version)}</dd>
				<dt className="text-muted-foreground">rendered</dt>
				<dd className="font-mono">
					{version(s.latest_version)}
					{lag ? (
						<span className="text-muted-foreground"> (not applied)</span>
					) : null}
				</dd>
				{s.latest_rendered_at ? (
					<>
						<dt className="text-muted-foreground">rendered at</dt>
						<dd className="font-mono">
							<time dateTime={s.latest_rendered_at}>
								{ago(s.latest_rendered_at)}
							</time>
						</dd>
					</>
				) : null}
				<dt className="text-muted-foreground">last snapshot</dt>
				<dd className="font-mono" data-testid="snapshot-instant">
					{s.last_snapshot_sent_at ? (
						<time dateTime={s.last_snapshot_sent_at}>
							{ago(s.last_snapshot_sent_at)}
						</time>
					) : (
						<span className="text-muted-foreground">none recorded</span>
					)}
				</dd>
			</dl>
			{s.error ? (
				<div className="rounded-pill bg-background p-2 font-mono text-[11px] break-words whitespace-pre-wrap text-status-degraded">
					{s.error}
				</div>
			) : null}
			{s.state === "degraded" ? (
				<p className="text-[11px] text-foreground-tertiary">
					{s.applied_version > 0
						? `Host stays on ${version(s.applied_version)} (its last good policy).`
						: "Host keeps the policy it had before; it has applied none from this control plane."}{" "}
					The control plane resends a full snapshot on the next attempt.
				</p>
			) : null}
			<Resend w={w} onReread={onReread} />
		</div>
	);
}

function headline(w: Workload): string {
	const s = w.sync;
	switch (s.state) {
		case "degraded":
			return "Degraded — last apply failed";
		case "pending":
			return `Pending — ${version(s.latest_version)} not yet applied`;
		case "offline":
			return w.health.last_seen_at
				? `Offline — no stream for ${since(w.health.last_seen_at)}`
				: "Offline — never connected";
		default:
			return `Synced — ${version(s.applied_version)} applied`;
	}
}

// Resend directs the workload's agent to reconnect, so its new stream
// begins with a snapshot. The acknowledgement says the directive was
// fired, never that it arrived; the outcome is the snapshot instant
// moving on a later read. An agent the control plane last recorded as
// offline is refused with the instant it was last heard from.
function Resend({ w, onReread }: { w: Workload; onReread: () => void }) {
	const write = useWrite();
	const [sending, setSending] = useState(false);
	const [outcome, setOutcome] = useState<Outcome>({ kind: "idle" });
	const current = w.sync.last_snapshot_sent_at;
	const moved =
		outcome.kind === "sent" &&
		current !== null &&
		(outcome.before === null ||
			Date.parse(current) > Date.parse(outcome.before));

	async function send() {
		setSending(true);
		try {
			const ack = await write(() => resendSnapshot(w.id));
			setOutcome({ kind: "sent", before: ack.last_snapshot_sent_at });
			onReread();
		} catch (err) {
			const p = err as ProblemError;
			if (p.type === ProblemType.agentOffline) {
				setOutcome({
					kind: "offline",
					lastSeen: p.problem.last_seen_at ?? null,
				});
			} else {
				setOutcome({
					kind: "failed",
					message: p.problem.detail ?? p.problem.title,
				});
			}
		} finally {
			setSending(false);
		}
	}

	return (
		<div className="mt-1 flex flex-col gap-1.5 border-t border-[var(--hairline-soft)] pt-2">
			<div className="flex items-center gap-2">
				<Button
					variant="secondary"
					size="sm"
					className="rounded-chip bg-card"
					disabled={sending}
					onClick={send}
				>
					{sending ? "Sending…" : "Resend snapshot"}
				</Button>
				{outcome.kind === "sent" && !moved ? (
					<button
						type="button"
						onClick={onReread}
						className="cursor-pointer text-[11px] text-link hover:text-link-hover"
					>
						Read again
					</button>
				) : null}
			</div>
			{outcome.kind === "sent" ? (
				<p role="status" className="text-[11px] text-foreground-tertiary">
					{moved
						? "The agent reconnected and was sent a fresh snapshot."
						: "Reconnect requested. The snapshot instant above moves once the agent has reconnected."}
				</p>
			) : null}
			{outcome.kind === "offline" ? (
				<p role="alert" className="text-[11px] text-status-degraded">
					▲ The agent is offline, so nothing was sent.{" "}
					{outcome.lastSeen ? (
						<>
							Last heard from{" "}
							<time dateTime={outcome.lastSeen} className="font-mono">
								{ago(outcome.lastSeen)}
							</time>{" "}
							({new Date(outcome.lastSeen).toISOString().replace(".000Z", "Z")}
							).
						</>
					) : (
						"It has never been heard from."
					)}
				</p>
			) : null}
			{outcome.kind === "failed" ? (
				<p role="alert" className="text-[11px] text-destructive">
					✕ {outcome.message}
				</p>
			) : null}
		</div>
	);
}
