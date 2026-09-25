import { describe, expect, it } from "vitest";
import { hoursFromNow, minutesAgo, workload } from "@/test/fixtures";
import { credential, renewsAt, syncNote, version } from "./describe";

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

	it("says an offline agent that never connected did not", () => {
		const w = workload({
			sync: { state: "offline" },
			health: { last_seen_at: null },
		});
		expect(syncNote(w)).toBe("never connected");
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
		expect(credential(w, now)).toEqual({ text: "renews 9h", tone: "ok" });
		expect(credential(w, now, "rail").text).toBe("renews in 9h");
	});

	it("dates a never-renewed credential from enrollment", () => {
		const w = workload({
			enrolled_at: minutesAgo(60),
			health: {
				credential: { last_renewed_at: null, expires_at: hoursFromNow(23) },
			},
		});
		expect(credential(w).text).toBe("renews 15h");
	});

	it("says renewal is due once the point has passed", () => {
		const w = workload({
			enrolled_at: minutesAgo(14 * 24 * 60),
			health: {
				credential: { last_renewed_at: null, expires_at: hoursFromNow(9) },
			},
		});
		expect(credential(w).text).toBe("renewal due");
	});
});
