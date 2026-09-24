import { describe, expect, it } from "vitest";
import { minutesAgo, workload } from "@/test/fixtures";
import { credential, syncNote, version } from "./describe";

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
});
