import type { Workload } from "@/api/schema";
import { ago, since, span } from "@/lib/format";

// The fleet table's words for a workload's sync and credential, from the
// workload object alone. Each note states only what a field holds: the
// surface records when a version was rendered, not when it reached the
// agent, and when the agent last acknowledged an apply or reported a
// failed one.

// version prints a policy version; zero means none has been applied.
export function version(v: number): string {
	return v === 0 ? "—" : `v${v}`;
}

// enforcementRemoved says the agent's owned firewall table is gone from
// the host's kernel, as its heartbeats report: the local kill switch, or
// another tool, removed it. The version the agent acknowledged still
// stands, so the sync state alone would keep saying the host runs it;
// nothing on the host is enforced or observed until the table is back
// (issue #64).
export function enforcementRemoved(w: Workload): boolean {
	return w.health.owned_table.state === "absent";
}

export function syncNote(w: Workload, now = Date.now()): string {
	const s = w.sync;
	if (enforcementRemoved(w)) {
		const since = w.health.owned_table.absent_since;
		return since
			? `${version(s.applied_version)} not installed · ${ago(since, now)}`
			: `${version(s.applied_version)} not installed`;
	}
	switch (s.state) {
		case "degraded":
			if (s.latest_version > s.applied_version) {
				return s.applied_version === 0
					? `v${s.latest_version} failed`
					: `v${s.applied_version} · v${s.latest_version} failed`;
			}
			return s.last_apply_failed_at
				? `apply failed ${ago(s.last_apply_failed_at, now)}`
				: "apply failed";
		case "offline":
			return w.health.last_seen_at
				? `no stream for ${since(w.health.last_seen_at, now)}`
				: "never connected";
		default:
			// A newer version than the one applied is drift in flight, whatever
			// state the stream last reported: after a mode change the list
			// shows it here until the agent applies it.
			if (s.latest_version > s.applied_version) {
				return s.latest_rendered_at
					? `v${s.latest_version} rendered ${ago(s.latest_rendered_at, now)}`
					: `v${s.latest_version} not yet applied`;
			}
			return "";
	}
}

export type CredentialTone = "ok" | "warn" | "bad";

export const credentialTone: Record<CredentialTone, string> = {
	ok: "text-secondary",
	warn: "text-status-warn-fg",
	bad: "text-status-critical-fg",
};

// renewsAt is when the agent is due to renew its credential, by the
// renewal rule the agent follows (ADR-0016, decision 4): once less than a
// third of the lifetime remains, so at the issue instant plus two thirds
// of the lifetime. The credential was issued at its last renewal, or at
// enrollment before any. The agent renews at a jittered point in that
// last third; the jitter is not modelled, so this is when renewal becomes
// due, not the instant it will happen.
export function renewsAt(w: Workload): number {
	const c = w.health.credential;
	const issued = Date.parse(c.last_renewed_at ?? w.enrolled_at);
	const expires = Date.parse(c.expires_at);
	return issued + ((expires - issued) * 2) / 3;
}

// credential is the column's text and tone. A renewing credential counts
// down to the point its renewal is due, in one phrasing throughout:
// "renewal due in 9h", then "renewal due now" once the point has passed.
// The point is when renewal becomes due, not the jittered instant the
// agent renews (renewsAt), so neither form says the agent "renews" then.
export function credential(
	w: Workload,
	now = Date.now(),
): { text: string; tone: CredentialTone } {
	const c = w.health.credential;
	switch (c.state) {
		case "expired":
			return { text: `expired ${ago(c.expires_at, now)}`, tone: "bad" };
		case "renewal-failed":
			return { text: "renewal failed · retrying", tone: "warn" };
		default: {
			const due = renewsAt(w) - now;
			return {
				text: `renewal due ${due <= 0 ? "now" : `in ${span(due)}`}`,
				tone: "ok",
			};
		}
	}
}

// lastSeen is the table's last-seen cell.
export function lastSeen(w: Workload, now = Date.now()): string {
	return w.health.last_seen_at ? since(w.health.last_seen_at, now) : "never";
}
