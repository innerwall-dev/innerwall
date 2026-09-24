import type { Workload } from "@/api/schema";
import { ago, since } from "@/lib/format";

// The fleet table's words for a workload's sync and credential, from the
// workload object alone. Each note states only what a field holds: the
// surface records when a version was rendered, not when it reached the
// agent, and keeps an apply's error but not its instant.

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
			return "apply failed";
		case "pending":
			return s.latest_rendered_at
				? `v${s.latest_version} rendered ${ago(s.latest_rendered_at, now)}`
				: `v${s.latest_version} pending`;
		case "offline":
			return w.health.last_seen_at
				? `no stream for ${since(w.health.last_seen_at, now)}`
				: "never connected";
		default:
			return "";
	}
}

export type CredentialTone = "ok" | "warn" | "bad";

export const credentialTone: Record<CredentialTone, string> = {
	ok: "text-cred-ok",
	warn: "text-cred-renewal-failed",
	bad: "text-cred-expired",
};

// credential is the column's text and tone. A renewing credential shows
// how long it has before expiry: the surface holds the expiry, not when
// the agent will next renew.
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
		default:
			return {
				text: `renews · expires ${since(c.expires_at, now)}`,
				tone: "ok",
			};
	}
}

// lastSeen is the table's last-seen cell.
export function lastSeen(w: Workload, now = Date.now()): string {
	return w.health.last_seen_at ? since(w.health.last_seen_at, now) : "never";
}
