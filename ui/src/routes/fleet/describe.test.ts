import { describe, expect, it } from "vitest";
import { hoursFromNow, minutesAgo, workload } from "@/test/fixtures";
import {
	credential,
	lastContact,
	presented,
	reconnecting,
	renewsAt,
	standing,
	syncNote,
	version,
} from "./describe";

describe("sync note", () => {
	it("shows drift in flight whatever state the stream last reported", () => {
		// After a mode change the stream may still say synced while a newer
		// version waits; the note is how the list shows convergence.
		const w = workload({
			sync: {
				state: "synced",
				applied_version: 2,
				latest_version: 3,
				latest_rendered_at: minutesAgo(1),
			},
		});
		expect(syncNote(w)).toBe("v3 rendered 1m ago");
		expect(
			syncNote(workload({ sync: { applied_version: 3, latest_version: 3 } })),
		).toBe("");
	});

	it("names a failed version against none applied", () => {
		const w = workload({
			sync: { state: "degraded", applied_version: 0, latest_version: 3 },
		});
		expect(syncNote(w)).toBe("v3 failed");
		expect(version(0)).toBe("—");
		expect(
			syncNote(
				workload({
					sync: { state: "degraded", applied_version: 3, latest_version: 3 },
				}),
			),
		).toBe("apply failed");
	});

	it("dates a failed apply when the agent reported one", () => {
		const w = workload({
			sync: {
				state: "degraded",
				applied_version: 88,
				latest_version: 88,
				last_apply_failed_at: minutesAgo(120),
			},
		});
		expect(syncNote(w)).toBe("apply failed 2h ago");
	});

	it("names the acknowledged version as not installed once the table is gone", () => {
		const w = workload({
			sync: { state: "synced", applied_version: 7, latest_version: 7 },
			health: {
				owned_table: { state: "absent", absent_since: minutesAgo(9) },
			},
		});
		expect(syncNote(w)).toBe("v7 not installed · 9m ago");
		expect(
			syncNote(
				workload({
					sync: { applied_version: 7, latest_version: 7 },
					health: { owned_table: { state: "unknown", absent_since: null } },
				}),
			),
		).toBe("");
	});

	it("says an offline agent that never connected did not", () => {
		const w = workload({
			sync: { state: "offline" },
			health: {
				last_seen_at: null,
				last_flow_report_at: null,
				telemetry_live: false,
			},
		});
		expect(syncNote(w)).toBe("never connected");
	});

	it("tells a reconnecting control channel from no contact at all", () => {
		// Issue #61: the sync stream waits out its backoff while flow reports
		// keep landing; that is not a host gone.
		const now = Date.parse("2026-10-13T12:00:00Z");
		const at = (m: number) => new Date(now - m * 60_000).toISOString();
		const alive = workload({
			sync: { state: "offline" },
			health: {
				last_seen_at: at(3.5),
				last_flow_report_at: at(0.5),
				telemetry_live: true,
			},
		});
		expect(reconnecting(alive)).toBe(true);
		expect(syncNote(alive, now)).toBe(
			"control channel down · flow reports arriving",
		);
		// Once the reports stop too, the counter runs from the latest contact
		// on either channel: the last report, not the stream's end.
		const gone = workload({
			sync: { state: "offline" },
			health: {
				last_seen_at: at(30),
				last_flow_report_at: at(12),
				telemetry_live: false,
			},
		});
		expect(reconnecting(gone)).toBe(false);
		expect(lastContact(gone)).toBe(at(12));
		expect(syncNote(gone, now)).toBe("no contact for 12m");
		// A synced workload is never reconnecting, whatever its reports.
		expect(reconnecting(workload())).toBe(false);
	});
});

describe("credential", () => {
	it("tones each state", () => {
		expect(credential(workload()).tone).toBe("ok");
		expect(
			credential(
				workload({ health: { credential: { state: "renewal-failed" } } }),
			),
		).toEqual({ text: "renewal failed · retrying", tone: "warn" });
		expect(
			credential(
				workload({
					health: {
						credential: { state: "expired", expires_at: minutesAgo(240) },
					},
				}),
			),
		).toEqual({ text: "expired 4h ago", tone: "bad" });
	});

	it("dates renewal at two thirds of the lifetime since the last issue", () => {
		const now = Date.parse("2026-09-25T12:00:00Z");
		const w = workload({
			enrolled_at: "2026-09-01T00:00:00Z",
			health: {
				credential: {
					last_renewed_at: "2026-09-25T05:00:00Z",
					expires_at: "2026-09-26T05:00:00Z",
				},
			},
		});
		// Issued 05:00, 24h lifetime: due 16h later, at 21:00.
		expect(renewsAt(w)).toBe(Date.parse("2026-09-25T21:00:00Z"));
		expect(credential(w, now)).toEqual({
			text: "renewal due in 9h",
			tone: "ok",
		});
	});

	it("dates a never-renewed credential from enrollment", () => {
		const w = workload({
			enrolled_at: minutesAgo(60),
			health: {
				credential: { last_renewed_at: null, expires_at: hoursFromNow(23) },
			},
		});
		expect(credential(w).text).toBe("renewal due in 15h");
	});

	it("says a credential that came from a late renewal renewed late", () => {
		const now = Date.parse("2026-10-14T12:00:00Z");
		const w = workload({
			enrolled_at: "2026-10-01T00:00:00Z",
			health: {
				credential: {
					last_renewed_at: "2026-10-14T05:00:00Z",
					expires_at: "2026-10-15T05:00:00Z",
					last_late_renewal: {
						renewed_at: "2026-10-14T05:00:00Z",
						expired_at: "2026-10-13T15:00:00Z",
					},
				},
			},
		});
		expect(credential(w, now)).toEqual({
			text: "renewed 14h late · renewal due in 9h",
			tone: "warn",
		});
		// A renewal on time since then: the credential is current again.
		const onTime = workload({
			...w,
			health: {
				credential: {
					...w.health.credential,
					last_renewed_at: "2026-10-14T11:00:00Z",
					expires_at: "2026-10-15T11:00:00Z",
				},
			},
		});
		expect(credential(onTime, now)).toEqual({
			text: "renewal due in 15h",
			tone: "ok",
		});
	});

	it("counts down to the renewal point in one phrasing, past it included", () => {
		const w = workload({
			enrolled_at: minutesAgo(14 * 24 * 60),
			health: {
				credential: { last_renewed_at: null, expires_at: hoursFromNow(9) },
			},
		});
		expect(credential(w).text).toBe("renewal due now");
	});
});

describe("credential lifecycle", () => {
	const now = Date.parse("2026-10-21T12:00:00Z");

	it("words an expired credential by the side of the grace it is on", () => {
		// INN-63: within the grace the workload renews by itself the next
		// time its agent reaches the control plane; beyond it only
		// re-enrollment brings the host back.
		const inGrace = workload({
			health: {
				credential: {
					state: "expired",
					lifecycle: "in-grace",
					expires_at: "2026-10-21T07:00:00Z",
					renewable_until: "2026-10-24T07:00:00Z",
				},
			},
		});
		expect(credential(inGrace, now)).toEqual({
			text: "expired 5h ago · in grace",
			tone: "bad",
			title:
				"Renews by itself the next time its agent reaches the control plane, until 10-24 07:00 UTC.",
		});
		expect(standing(inGrace, now)).toEqual({
			text: "expired 5h ago · will recover by renewal",
			tone: "bad",
			note: "It may still renew until 10-24 07:00 UTC (in 2d), and do nothing else. The agent keeps retrying and enforcing its last policy; once a renewal succeeds the workload carries on with the same identity.",
			reenroll: false,
		});

		const pastGrace = workload({
			health: {
				credential: {
					state: "expired",
					lifecycle: "past-grace",
					expires_at: "2026-10-17T08:00:00Z",
					renewable_until: "2026-10-20T08:00:00Z",
				},
			},
		});
		expect(credential(pastGrace, now)).toMatchObject({
			text: "expired 4d ago · re-enroll",
			tone: "bad",
		});
		const s = standing(pastGrace, now);
		expect(s.text).toBe("expired 4d ago · needs re-enrollment");
		expect(s.note).toBe(
			"Its renewal grace ended 1d ago, so nothing accepts it. The agent keeps enforcing its last policy; the host returns only by enrolling again with a new provisioning token, as a new workload.",
		);
		expect(s.reenroll).toBe(true);
	});

	it("says a valid credential renews by the two-thirds mark", () => {
		// Issued 05:00 for 24h: the first attempt falls by 21:00, never
		// later (ADR-0016, decision 4, as corrected).
		const w = workload({
			enrolled_at: "2026-10-01T00:00:00Z",
			health: {
				credential: {
					last_renewed_at: "2026-10-21T05:00:00Z",
					expires_at: "2026-10-22T05:00:00Z",
				},
			},
		});
		const s = standing(w, now);
		expect(s.text).toBe("valid · renews by 10-21 21:00 UTC");
		expect(s.tone).toBe("ok");
		expect(s.note).toBe(
			"Valid until 10-22 05:00 UTC (in 17h). Renewal begins by two thirds of its lifetime (in 9h), never later.",
		);
		const failing = workload({
			...w,
			health: {
				credential: { ...w.health.credential, state: "renewal-failed" },
			},
		});
		expect(standing(failing, now)).toMatchObject({
			text: "renewal failed · retrying",
			tone: "warn",
			note: "Valid until 10-22 05:00 UTC (in 17h). The agent retries with backoff until it renews.",
		});
	});

	it("calls a retired workload's credential refused, whatever it says", () => {
		const w = workload({
			lifecycle: "retired",
			retired_at: "2026-10-20T18:02:11Z",
			health: {
				credential: {
					state: "expired",
					lifecycle: "past-grace",
					expires_at: "2026-10-17T08:00:00Z",
				},
			},
		});
		expect(credential(w, now)).toEqual({
			text: "refused · retired",
			tone: "quiet",
		});
		expect(standing(w, now)).toMatchObject({
			text: "refused · workload retired",
			tone: "quiet",
			reenroll: false,
		});
	});
});

describe("presented credential", () => {
	const issued = { serial: "4e2b", expires_at: "2026-10-22T05:00:00Z" };

	it("is in use only while the stream is up, and marks divergence", () => {
		const same = workload({
			health: {
				credential: {
					...issued,
					last_presented: { ...issued },
				},
			},
		});
		expect(presented(same)).toEqual({
			...issued,
			inUse: true,
			diverged: false,
		});
		// The stream outlived a renewal, or two parties renew one identity:
		// the live stream presents a credential other than the one issued.
		const older = { serial: "3c9a", expires_at: "2026-10-21T13:00:00Z" };
		const diverged = workload({
			health: { credential: { ...issued, last_presented: older } },
		});
		expect(presented(diverged)).toEqual({
			...older,
			inUse: true,
			diverged: true,
		});
		// Offline, it is only what the last stream presented.
		const offline = workload({
			sync: { state: "offline" },
			health: { credential: { ...issued, last_presented: older } },
		});
		expect(presented(offline)?.inUse).toBe(false);
	});

	it("is nothing before a stream has opened since it was recorded", () => {
		expect(presented(workload())).toBeNull();
	});
});
