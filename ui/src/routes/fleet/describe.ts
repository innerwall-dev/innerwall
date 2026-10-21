import type { Workload } from "@/api/schema";
import { ago, relative, since, span, stamp } from "@/lib/format";

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

// retired says the operator retired the workload (ADR-0023): a state of
// its own, outside every sync state and the fleet's attention ordering.
// Offline is something to fix; retired is something the operator
// decided, and it is one way. Its sync state and health are what was
// last recorded before it, and describe nothing live.
export function retired(w: Workload): boolean {
	return w.lifecycle === "retired";
}

// reconnecting says the sync stream is down while the agent's flow
// reports are still arriving: the agent is running and reaching the
// control plane on its telemetry connection, and only the control channel
// is waiting out its reconnect backoff. The two travel on separate
// connections, so either can be down alone (issue #61).
export function reconnecting(w: Workload): boolean {
	return w.sync.state === "offline" && w.health.telemetry_live;
}

// lastContact is the latest instant the control plane heard from the
// agent on either channel: the sync stream's last contact or the last
// flow report received. Null when it has heard nothing on either.
export function lastContact(w: Workload): string | null {
	const { last_seen_at: seen, last_flow_report_at: report } = w.health;
	if (seen === null) return report;
	if (report === null) return seen;
	return Date.parse(report) > Date.parse(seen) ? report : seen;
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
		case "offline": {
			if (reconnecting(w))
				return "control channel down · flow reports arriving";
			// Measured from the latest contact on either channel, so it is
			// what it says: nothing has been heard from the agent since.
			const contact = lastContact(w);
			return contact
				? `no contact for ${since(contact, now)}`
				: "never connected";
		}
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

// A credential's tone: quiet is a retired workload's, whose credential
// is refused whatever it says and is nothing to act on.
export type CredentialTone = "ok" | "warn" | "bad" | "quiet";

export const credentialTone: Record<CredentialTone, string> = {
	quiet: "text-tertiary",
	ok: "text-secondary",
	warn: "text-status-warn-fg",
	bad: "text-status-critical-fg",
};

// renewsAt is when the agent renews its credential by, at the latest, by
// the renewal rule it follows (ADR-0016, decision 4, as corrected on
// 2026-10-20): its first attempt falls by the two-thirds mark of the
// lifetime, and jitter moves it earlier, never later, so at least a third
// of the lifetime remains for retries. The credential was issued at its
// last renewal, or at enrollment before any. The jitter is not modelled,
// so this is the latest instant the first attempt falls at, not the
// instant it will happen.
export function renewsAt(w: Workload): number {
	const c = w.health.credential;
	const issued = Date.parse(c.last_renewed_at ?? w.enrolled_at);
	const expires = Date.parse(c.expires_at);
	return issued + ((expires - issued) * 2) / 3;
}

// renewedLate is how long after its expiry the current credential's
// predecessor was renewed, when the current credential came from a late
// renewal (one the control plane accepted within its renewal grace, after
// an outage spanned the renewal window); null otherwise. A later renewal
// on time makes the credential current again and this null, though the
// workload keeps the record of its latest late renewal.
export function renewedLate(w: Workload): number | null {
	const c = w.health.credential;
	const late = c.last_late_renewal;
	if (late === null || c.last_renewed_at === null) return null;
	if (Date.parse(late.renewed_at) !== Date.parse(c.last_renewed_at))
		return null;
	return Date.parse(late.renewed_at) - Date.parse(late.expired_at);
}

// credential is the column's text and tone. A renewing credential counts
// down to the point its renewal is due, in one phrasing throughout:
// "renewal due in 9h", then "renewal due now" once the point has passed.
// The point is the latest instant the agent's first attempt falls at
// (renewsAt). A credential that came from a late renewal says so first,
// as a warning: the workload went without a valid credential for that
// long. An expired one says which side of the renewal grace it is on, as
// the control plane judges it (`lifecycle`): within it the workload
// renews by itself the next time its agent reaches the control plane;
// beyond it only re-enrollment brings the host back. A retired
// workload's credential is refused whatever it says. The column keeps to
// the width its longest phrase already takes; title says the rest.
export function credential(
	w: Workload,
	now = Date.now(),
): { text: string; tone: CredentialTone; title?: string } {
	const c = w.health.credential;
	if (retired(w)) return { text: "refused · retired", tone: "quiet" };
	if (c.lifecycle === "in-grace")
		return {
			text: `expired ${ago(c.expires_at, now)} · in grace`,
			tone: "bad",
			title: `Renews by itself the next time its agent reaches the control plane, until ${stamp(c.renewable_until)}.`,
		};
	if (c.lifecycle === "past-grace")
		return {
			text: `expired ${ago(c.expires_at, now)} · re-enroll`,
			tone: "bad",
			title:
				"Beyond the renewal grace: the host returns only by enrolling again with a new provisioning token.",
		};
	switch (c.state) {
		case "expired":
			return { text: `expired ${ago(c.expires_at, now)}`, tone: "bad" };
		case "renewal-failed":
			return { text: "renewal failed · retrying", tone: "warn" };
		default: {
			const due = renewsAt(w) - now;
			const countdown = `renewal due ${due <= 0 ? "now" : `in ${span(due)}`}`;
			const late = renewedLate(w);
			if (late !== null)
				return {
					text: `renewed ${span(late)} late · ${countdown}`,
					tone: "warn",
				};
			return { text: countdown, tone: "ok" };
		}
	}
}

// Standing is what the credential card says of the credential the
// control plane last issued: the row's value and tone, the sentence under
// it, and whether the way back is re-enrollment, so the card names the
// provisioning-token path.
export interface Standing {
	text: string;
	tone: CredentialTone;
	note: string;
	reenroll: boolean;
}

// standing derives the card's wording from the control plane's judgment
// of the credential (`lifecycle`, against the renewal grace it is
// configured with, which no client can know) and the instant that grace
// ends (`renewable_until`); a valid one renews by the two-thirds mark
// (renewsAt).
export function standing(w: Workload, now = Date.now()): Standing {
	const c = w.health.credential;
	if (retired(w))
		return {
			text: "refused · workload retired",
			tone: "quiet",
			note: "A retired workload's credential is refused at renewal and on every agent call, whatever its expiry.",
			reenroll: false,
		};
	switch (c.lifecycle) {
		case "in-grace":
			return {
				text: `expired ${ago(c.expires_at, now)} · will recover by renewal`,
				tone: "bad",
				note: `It may still renew until ${stamp(c.renewable_until)} (${relative(c.renewable_until, now)}), and do nothing else. The agent keeps retrying and enforcing its last policy; once a renewal succeeds the workload carries on with the same identity.`,
				reenroll: false,
			};
		case "past-grace":
			return {
				text: `expired ${ago(c.expires_at, now)} · needs re-enrollment`,
				tone: "bad",
				note: `Its renewal grace ended ${ago(c.renewable_until, now)}, so nothing accepts it. The agent keeps enforcing its last policy; the host returns only by enrolling again with a new provisioning token, as a new workload.`,
				reenroll: true,
			};
		default:
			break;
	}
	if (c.state === "expired")
		// Past its expiry on one clock, not yet on the other: the
		// control plane's judgment of the grace is the next read's.
		return {
			text: `expired ${ago(c.expires_at, now)}`,
			tone: "bad",
			note: "It may still renew within the control plane's renewal grace; beyond it the host must be enrolled again.",
			reenroll: false,
		};
	const valid = `Valid until ${stamp(c.expires_at)} (${relative(c.expires_at, now)}).`;
	if (c.state === "renewal-failed")
		return {
			text: "renewal failed · retrying",
			tone: "warn",
			note: `${valid} The agent retries with backoff until it renews.`,
			reenroll: false,
		};
	const by = renewsAt(w);
	const late = renewedLate(w);
	return {
		text: `valid · renews by ${stamp(new Date(by).toISOString())}`,
		tone: late === null ? "ok" : "warn",
		note: `${late === null ? "" : `It came from a renewal ${span(late)} after its predecessor had expired. `}${valid} Renewal begins by two thirds of its lifetime (${relative(new Date(by).toISOString(), now)}), never later.`,
		reenroll: false,
	};
}

// Presented is the credential the workload's latest sync stream opened
// on, as the card shows it: in use while the stream is up, otherwise only
// what was presented last; diverged when it is not the credential last
// issued. Null when no stream has opened since the control plane began
// recording it, so nothing is guessed.
export interface Presented {
	serial: string;
	expires_at: string;
	inUse: boolean;
	diverged: boolean;
}

export function presented(w: Workload): Presented | null {
	const c = w.health.credential;
	const p = c.last_presented;
	if (p === null) return null;
	const inUse = !retired(w) && w.sync.state !== "offline";
	return {
		serial: p.serial,
		expires_at: p.expires_at,
		inUse,
		diverged: p.serial !== c.serial,
	};
}

// lastSeen is the table's last-seen cell.
export function lastSeen(w: Workload, now = Date.now()): string {
	return w.health.last_seen_at ? since(w.health.last_seen_at, now) : "never";
}
