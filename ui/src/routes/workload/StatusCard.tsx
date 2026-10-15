import { useState } from "react";
import type { ProblemError } from "@/api/client";
import { resendSnapshot } from "@/api/fleet";
import { type EvidenceGaps, ProblemType, type Workload } from "@/api/schema";
import { syncStates } from "@/components/fleet/status";
import { SeverityNote, StatusGlyph, tone } from "@/components/StatusGlyph";
import { Button } from "@/components/ui/button";
import { ago, count, since } from "@/lib/format";
import { type Resource, useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import {
	credential,
	credentialTone,
	enforcementRemoved,
	lastContact,
	reconnecting,
	version,
} from "../fleet/describe";
import type { ScreenRange } from "./range";
import { gapsShown } from "./states";

type Outcome =
	| { kind: "idle" }
	| { kind: "sent"; before: string | null }
	| { kind: "offline"; lastSeen: string | null }
	| { kind: "failed"; message: string };

// StatusCard is the Overview's Status card: the workload's convergence
// with its rendered policy (the applied version against the latest with
// the instants the surface records, and the apply error when there is
// one), the agent's channels and credential, the loss counters, the
// evidence-gap summary over the screen's range, and the snapshot action.
// It leads with the health line; what an exceptional state means is said
// by its alert card above it.
export function StatusCard({
	w,
	gaps,
	range,
	onReread,
}: {
	w: Workload;
	gaps: Resource<EvidenceGaps>;
	range: ScreenRange;
	onReread: () => void;
}) {
	const s = w.sync;
	const lag = s.latest_version > s.applied_version;
	// A removed table outranks every sync state: whatever version the
	// agent acknowledged, the host holds none of it (issue #64).
	const removed = enforcementRemoved(w);
	const cred = credential(w);

	return (
		<section
			aria-labelledby="status-card-title"
			className="flex flex-col gap-3 rounded-panel border border-hairline bg-card p-4"
		>
			<h2 id="status-card-title" className="type-card-title text-primary">
				Status
			</h2>
			<div className="flex flex-col gap-3" data-testid="status-card">
				<div
					className={cn(
						"flex items-center gap-2 type-dense-strong",
						removed ? tone.error : syncStates[s.state].cls,
					)}
				>
					<StatusGlyph
						status={removed ? "error" : syncStates[s.state].status}
						size="md"
					/>
					<span>{headline(w)}</span>
				</div>
				<dl className="grid grid-cols-[auto_1fr] items-baseline gap-x-4 gap-y-2">
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
					<dt className={field}>flow report</dt>
					<dd className={value} data-testid="last-flow-report">
						{w.health.last_flow_report_at
							? ago(w.health.last_flow_report_at)
							: "none received"}
					</dd>
					<dt className={field}>enrolled</dt>
					<dd className={value}>{ago(w.enrolled_at)}</dd>
					<dt className={field}>credential</dt>
					<dd className={value}>
						{cred.tone === "ok" ? (
							<span className={credentialTone.ok}>{cred.text}</span>
						) : (
							<SeverityNote
								level={cred.tone === "bad" ? "error" : "alert"}
								size="sm"
								className={dense}
							>
								{cred.text}
							</SeverityNote>
						)}
						{w.health.credential.last_error ? (
							<div className={note}>{w.health.credential.last_error}</div>
						) : null}
					</dd>
					<dt className={field}>dropped flows</dt>
					<dd className={value}>
						<Counted n={w.health.dropped_flow_records} />
						{w.health.dropped_flow_records > 0 ? (
							<div className={note}>
								Records the agent could not deliver; the flows shown are
								incomplete.
							</div>
						) : null}
					</dd>
					<dt className={field}>overruns</dt>
					<dd className={value}>
						<Counted n={w.health.source_overruns} />
						{w.health.source_overruns > 0 ? (
							<div className={note}>
								Times the kernel dropped events because a flow source fell
								behind, since the agent started.
							</div>
						) : null}
					</dd>
					<dt className={field}>evidence gaps</dt>
					<dd className={value}>
						<GapSummary gaps={gaps} range={range} />
					</dd>
				</dl>
				{s.error ? (
					<div className="rounded-control border border-hairline bg-field-sunk px-2.5 py-2 type-mono-xs break-words whitespace-pre-wrap text-primary">
						{s.error}
					</div>
				) : null}
				<Resend w={w} onReread={onReread} />
			</div>
		</section>
	);
}

// GapSummary is the evidence-gap fact: how many intervals in the
// screen's range the agent knows it lost evidence in, the Overview's
// history listing them.
function GapSummary({
	gaps,
	range,
}: {
	gaps: Resource<EvidenceGaps>;
	range: ScreenRange;
}) {
	if (gaps.status === "loading") {
		return <span className="text-tertiary">…</span>;
	}
	if (gaps.status === "error") {
		return (
			<span className="type-caption text-tertiary">could not be read</span>
		);
	}
	const n = gaps.data.gaps.length;
	const more = gaps.data.truncated || n > gapsShown;
	return (
		<span>
			{n === 0 ? "none" : `${count(n)}${more ? "+" : ""}`}{" "}
			<span className="type-caption text-tertiary">in {range.text}</span>
		</span>
	);
}

// Counted is a loss counter: plain at zero, an alert past it.
function Counted({ n }: { n: number }) {
	if (n === 0) return <span>{count(n)}</span>;
	return (
		<SeverityNote level="alert" size="sm" className={dense}>
			{count(n)}
		</SeverityNote>
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

// The card's field rows: the meta label beside a mono value, and the
// caption that explains a value under it.
const field = "type-meta-label text-tertiary";
const value = "min-w-0 type-mono-sm break-words text-primary";
const note = "mt-0.5 type-caption text-tertiary";
// A severity note in the card's 16px lines centres its glyph on them.
const dense = "type-mono-sm";

function headline(w: Workload): string {
	const s = w.sync;
	if (enforcementRemoved(w)) return "Enforcement removed on host";
	switch (s.state) {
		case "degraded":
			return "Degraded — last apply failed";
		case "pending":
			return `Pending — ${version(s.latest_version)} not yet applied`;
		case "offline": {
			if (reconnecting(w)) {
				return "Reconnecting — control channel down, flow reports arriving";
			}
			const contact = lastContact(w);
			return contact
				? `Offline — no contact for ${since(contact)}`
				: "Offline — never connected";
		}
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
		<div className="flex flex-col gap-2 border-t border-hairline pt-3">
			<div className="flex items-center gap-2">
				<Button variant="secondary" size="sm" disabled={sending} onClick={send}>
					{sending ? "Sending…" : "Resend snapshot"}
				</Button>
				{outcome.kind === "sent" && !moved ? (
					<button
						type="button"
						onClick={onReread}
						className="cursor-pointer type-caption link"
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
					prose
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
					prose
					className="type-caption"
				>
					{outcome.message}
				</SeverityNote>
			) : null}
		</div>
	);
}
