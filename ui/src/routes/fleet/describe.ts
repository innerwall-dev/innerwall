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

export function syncNote(w: Workload, now = Date.now()): string {
	const s = w.sync;
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
	ok: "text-cred-ok",
	warn: "text-cred-renewal-failed",
	bad: "text-cred-expired",
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

// credential is the column's text and tone. A renewing credential shows
// how long until its renewal is due; the rail's longer form reads "renews
// in 9h" where the table's reads "renews 9h".
export function credential(
	w: Workload,
	now = Date.now(),
	form: "table" | "rail" = "table",
): { text: string; tone: CredentialTone } {
	const c = w.health.credential;
	switch (c.state) {
		case "expired":
			return { text: `expired ${ago(c.expires_at, now)}`, tone: "bad" };
		case "renewal-failed":
			return { text: "renewal failed · retrying", tone: "warn" };
		default: {
			const due = renewsAt(w) - now;
			if (due <= 0) return { text: "renewal due", tone: "ok" };
			return {
				text: `renews ${form === "rail" ? "in " : ""}${span(due)}`,
				tone: "ok",
			};
		}
	}
}

// lastSeen is the table's last-seen cell.
export function lastSeen(w: Workload, now = Date.now()): string {
	return w.health.last_seen_at ? since(w.health.last_seen_at, now) : "never";
}
