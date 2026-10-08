import { useState } from "react";
import type { ProblemError } from "@/api/client";
import { resendSnapshot } from "@/api/fleet";
import { ProblemType, type Workload } from "@/api/schema";
import { syncStates } from "@/components/fleet/status";
import { SeverityNote, StatusGlyph } from "@/components/StatusGlyph";
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
		degraded: "border-status-warn-border bg-status-warn-bg",
		pending: "border-strong bg-app",
		synced: "border-default bg-app",
		offline: "border-default bg-app",
	}[s.state];

	return (
		<div
			className={cn("flex flex-col gap-2.5 rounded-lg border px-3 py-3", tone)}
			data-testid="status-card"
		>
			<div
				className={cn(
					"flex items-center gap-2 type-ui-strong",
					syncStates[s.state].cls,
				)}
			>
				<StatusGlyph status={syncStates[s.state].status} size="md" />
				<span>{headline(w)}</span>
			</div>
			<dl className="grid grid-cols-[auto_1fr] items-baseline gap-x-4 gap-y-1">
				<dt className={field}>applied</dt>
				<dd className={value}>{version(s.applied_version)}</dd>
				<dt className={field}>rendered</dt>
				<dd className={value}>
					{version(s.latest_version)}
					{lag ? <span className="text-tertiary"> (not applied)</span> : null}
				</dd>
				<dt className={field}>last ack</dt>
				<dd className={value} data-testid="ack-instant">
					<LastAck s={s} />
				</dd>
				{s.latest_rendered_at ? (
					<>
						<dt className={field}>rendered at</dt>
						<dd className={value}>
							<time dateTime={s.latest_rendered_at}>
								{ago(s.latest_rendered_at)}
							</time>
						</dd>
					</>
				) : null}
				<dt className={field}>last snapshot</dt>
				<dd className={value} data-testid="snapshot-instant">
					{s.last_snapshot_sent_at ? (
						<time dateTime={s.last_snapshot_sent_at}>
							{ago(s.last_snapshot_sent_at)}
						</time>
					) : (
						<span className="text-tertiary">none recorded</span>
					)}
				</dd>
			</dl>
			{s.error ? (
				<div className="rounded-md border border-default bg-app px-2.5 py-2 type-mono-xs break-words whitespace-pre-wrap text-status-warn-fg">
					{s.error}
				</div>
			) : null}
			{s.state === "degraded" ? (
				<p className="type-caption text-secondary">
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

// LastAck is the agent's most recent answer to a version, as the design
// words it: "4m ago · FAILED" when the last thing it reported was a
// failed apply, "· applied" when it was an acknowledgement. The two
// instants are kept separately; whichever is later is the last answer.
function LastAck({ s }: { s: Workload["sync"] }) {
	const acked = s.last_acked_at ? Date.parse(s.last_acked_at) : null;
	const failed = s.last_apply_failed_at
		? Date.parse(s.last_apply_failed_at)
		: null;
	if (acked === null && failed === null) {
		return <span className="text-tertiary">none recorded</span>;
	}
	const lastFailed = failed !== null && (acked === null || failed > acked);
	const at = (lastFailed ? s.last_apply_failed_at : s.last_acked_at) as string;
	return (
		<>
			<time dateTime={at}>{ago(at)}</time>
			{lastFailed ? (
				<span className="text-status-warn-fg"> · FAILED</span>
			) : (
				<span className="text-tertiary"> · applied</span>
			)}
		</>
	);
}

// The card's field rows: a sentence-case label beside a mono value.
const field = "type-label text-tertiary";
const value = "min-w-0 type-mono-sm text-primary";

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
		<div className="flex flex-col gap-2 border-t border-subtle pt-3">
			<div className="flex items-center gap-2">
				<Button variant="secondary" size="sm" disabled={sending} onClick={send}>
					{sending ? "Sending…" : "Resend snapshot"}
				</Button>
				{outcome.kind === "sent" && !moved ? (
					<button
						type="button"
						onClick={onReread}
						className="cursor-pointer type-caption text-link hover:underline"
					>
						Read again
					</button>
				) : null}
			</div>
			{outcome.kind === "sent" ? (
				<p role="status" className="type-caption text-secondary">
					{moved
						? "The agent reconnected and was sent a fresh snapshot."
						: "Reconnect requested. The snapshot instant above moves once the agent has reconnected."}
				</p>
			) : null}
			{outcome.kind === "offline" ? (
				<SeverityNote
					level="alert"
					role="alert"
					size="sm"
					className="type-caption"
				>
					The agent is offline, so nothing was sent.{" "}
					{outcome.lastSeen ? (
						<>
							Last heard from{" "}
							<time dateTime={outcome.lastSeen} className="type-mono-sm">
								{ago(outcome.lastSeen)}
							</time>{" "}
							({new Date(outcome.lastSeen).toISOString().replace(".000Z", "Z")}
							).
						</>
					) : (
						"It has never been heard from."
					)}
				</SeverityNote>
			) : null}
			{outcome.kind === "failed" ? (
				<SeverityNote
					level="error"
					role="alert"
					size="sm"
					className="type-caption"
				>
					{outcome.message}
				</SeverityNote>
			) : null}
		</div>
	);
}
