import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, describe, expect, it, vi } from "vitest";
import type { EvidenceGap, RollupGroup, Workload } from "@/api/schema";
import { hoursFromNow, minutesAgo, workload } from "@/test/fixtures";
import {
	chip,
	emptyRollup,
	gapsOf,
	mockSurface,
	problem,
	type Route,
	renderApp,
	signedIn,
} from "@/test/harness";
import { peer } from "@/test/map";
import { gap, peerService, ps } from "@/test/review";

// The clock is fixed for this file: the fixtures below are instants
// relative to now, built when the module loads, and the assertions read
// the spans the screen prints from them ("20s", "42s ago"). With a moving
// clock the two disagree whenever a second boundary passes between
// building a fixture and rendering it. Only Date is fixed; timers stay
// real, so waiting for the screen still works.
vi.useFakeTimers({ now: new Date("2026-10-05T12:00:00Z"), toFake: ["Date"] });
afterAll(() => {
	vi.useRealTimers();
});

const frame: Route[] = [
	signedIn,
	{
		method: "GET",
		path: "/api/v1/provisioning-tokens",
		reply: { status: 200, json: { tokens: [] } },
	},
	{
		method: "GET",
		path: "/api/v1/rulesets",
		reply: { status: 200, json: { rulesets: [], state_version: "0" } },
	},
];

function fleet(
	pages: (q: URLSearchParams) => {
		workloads: Workload[];
		next_cursor: string | null;
	},
): Route {
	return {
		method: "GET",
		path: "/api/v1/workloads",
		reply: (_body, q) => ({ status: 200, json: pages(q) }),
	};
}

const checkout = workload({
	hostname: "checkout-prod-07",
	labels: { app: "checkout", env: "prod", tier: "api" },
	mode: "simulation",
	sync: {
		state: "degraded",
		applied_version: 41,
		latest_version: 42,
		error: "apply refused: set element exceeds the table's size",
	},
	// A 24-hour credential renewed 7h ago: renewal falls due at two thirds
	// of its lifetime, 9h from now.
	health: {
		last_seen_at: minutesAgo(0.34),
		credential: {
			last_renewed_at: minutesAgo(7 * 60),
			expires_at: hoursFromNow(17),
		},
	},
});
const auth = workload({
	hostname: "auth-prod-11",
	mode: "enforced",
	sync: {
		state: "pending",
		applied_version: 88,
		latest_version: 89,
		latest_rendered_at: minutesAgo(0.7),
	},
});
const legacy = workload({
	hostname: "legacy-vm-0117",
	labels: {},
	agent: { version: "", capabilities: [] },
	sync: { state: "offline", applied_version: 1, latest_version: 1 },
	health: {
		last_seen_at: minutesAgo(3 * 24 * 60),
		last_flow_report_at: null,
		telemetry_live: false,
		credential: { state: "expired", expires_at: minutesAgo(2 * 24 * 60) },
	},
});
const bastion = workload({
	hostname: "bastion-01",
	health: {
		credential: {
			state: "renewal-failed",
			last_error: "renewal refused: authority unreachable",
		},
	},
});

function rowOf(hostname: string): HTMLElement {
	const link = screen.getByRole("link", { name: hostname });
	const row = link.closest("tr");
	if (!row) throw new Error(`no row for ${hostname}`);
	return row;
}

describe("fleet workloads", () => {
	it("renders each workload's columns in the design's words", async () => {
		mockSurface([
			...frame,
			fleet(() => ({
				workloads: [checkout, auth, legacy, bastion],
				next_cursor: null,
			})),
		]);
		renderApp("/workloads");
		await screen.findByRole("link", { name: "checkout-prod-07" });

		const c = within(rowOf("checkout-prod-07"));
		for (const l of ["app=checkout", "env=prod", "tier=api"]) {
			expect(c.getByText(chip(l))).toBeInTheDocument();
		}
		expect(c.getByText("Simulation")).toBeInTheDocument();
		expect(c.getByText("Degraded")).toBeInTheDocument();
		expect(c.getByText("v41 · v42 failed")).toBeInTheDocument();
		expect(c.getByText("v41")).toBeInTheDocument();
		expect(c.getByText("renewal due in 9h")).toBeInTheDocument();
		// The agent column is the version the agent reports.
		expect(c.getByText("0.3.0")).toBeInTheDocument();
		expect(c.getByText("20s")).toBeInTheDocument();

		const a = within(rowOf("auth-prod-11"));
		expect(a.getByText("Pending")).toBeInTheDocument();
		expect(a.getByText("v89 rendered 42s ago")).toBeInTheDocument();

		const l = within(rowOf("legacy-vm-0117"));
		expect(l.getByText("no labels — matches no scope")).toBeInTheDocument();
		expect(l.getByText("no contact for 3d")).toBeInTheDocument();
		expect(l.getByText("expired 2d ago")).toBeInTheDocument();
		// An agent that reports no version is a dash, named on hover.
		expect(l.getByTitle("not reported")).toHaveTextContent("—");

		expect(
			within(rowOf("bastion-01")).getByText("renewal failed · retrying"),
		).toBeInTheDocument();

		// No fleet total is known, so none is claimed.
		expect(
			screen.getByText("Showing 4 · sorted by sync state, then last seen"),
		).toBeInTheDocument();
		expect(screen.getByRole("tab", { name: "Workloads" })).toHaveTextContent(
			/^Workloads$/,
		);
	});

	it("says enforcement was removed instead of the sync state it still holds", async () => {
		// Issue #64: the kill switch on an enforced host left the row saying
		// Synced on its version. The agent now reports the table gone.
		const killed = workload({
			hostname: "pay-prod-02",
			mode: "enforced",
			sync: { state: "synced", applied_version: 12, latest_version: 12 },
			health: {
				owned_table: { state: "absent", absent_since: minutesAgo(4) },
			},
		});
		mockSurface([
			...frame,
			fleet(() => ({ workloads: [killed, bastion], next_cursor: null })),
		]);
		renderApp("/workloads");
		await screen.findByRole("link", { name: "pay-prod-02" });
		const k = within(rowOf("pay-prod-02"));
		expect(k.getByTestId("enforcement-removed")).toHaveTextContent(
			"Enforcement removed on host",
		);
		expect(k.queryByText("Synced")).not.toBeInTheDocument();
		expect(k.getByText("v12 not installed · 4m ago")).toBeInTheDocument();
		// A healthy row is untouched.
		const b = within(rowOf("bastion-01"));
		expect(b.getByText("Synced")).toBeInTheDocument();
		expect(b.queryByTestId("enforcement-removed")).not.toBeInTheDocument();
	});

	it("names an offline stream whose flow reports still land as reconnecting", async () => {
		// Issue #61: during a control-plane restart the sync stream waits out
		// its backoff while the flow reports keep landing.
		const restarting = workload({
			hostname: "api-prod-03",
			sync: { state: "offline", applied_version: 9, latest_version: 9 },
			health: {
				last_seen_at: minutesAgo(3.5),
				last_flow_report_at: minutesAgo(0.5),
				telemetry_live: true,
			},
		});
		mockSurface([
			...frame,
			fleet(() => ({ workloads: [restarting, legacy], next_cursor: null })),
		]);
		renderApp("/workloads");
		await screen.findByRole("link", { name: "api-prod-03" });
		const r = within(rowOf("api-prod-03"));
		expect(r.getByText("Reconnecting")).toBeInTheDocument();
		expect(r.queryByText("Offline")).not.toBeInTheDocument();
		expect(
			r.getByText("control channel down · flow reports arriving"),
		).toBeInTheDocument();
		// No contact on either channel stays Offline.
		expect(
			within(rowOf("legacy-vm-0117")).getByText("Offline"),
		).toBeInTheDocument();
	});

	it("filters by sync state and label requirements through the query", async () => {
		const { calls } = mockSurface([
			...frame,
			fleet((q) => ({
				workloads:
					q.get("sync_state") === "degraded" ? [checkout] : [checkout, auth],
				next_cursor: null,
			})),
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "auth-prod-11" });

		await user.click(screen.getByRole("button", { name: /degraded/ }));
		await waitFor(() =>
			expect(
				screen.queryByRole("link", { name: "auth-prod-11" }),
			).not.toBeInTheDocument(),
		);
		expect(screen.getByRole("button", { name: /degraded/ })).toHaveAttribute(
			"aria-pressed",
			"true",
		);

		await user.click(screen.getByRole("button", { name: "label filter" }));
		await user.type(
			screen.getByLabelText("Label requirement"),
			"app=checkout{Enter}",
		);
		await user.click(screen.getByRole("button", { name: "label filter" }));
		await user.type(screen.getByLabelText("Label requirement"), "tier{Enter}");
		expect(screen.getByLabelText("Label requirement")).toHaveAttribute(
			"aria-invalid",
			"true",
		);
		// A value outside the label grammar is refused with the reason.
		await user.clear(screen.getByLabelText("Label requirement"));
		await user.type(
			screen.getByLabelText("Label requirement"),
			"tier=a=b{Enter}",
		);
		expect(screen.getByLabelText("Label requirement")).toHaveAttribute(
			"title",
			expect.stringMatching(/^"a=b" is not a label value/),
		);
		await user.keyboard("{Escape}");

		await waitFor(() => {
			const last = calls.filter((c) => c.path.includes("/workloads")).at(-1);
			const q = new URL(last?.path ?? "", "http://x").searchParams;
			expect(q.get("sync_state")).toBe("degraded");
			expect(q.getAll("label")).toEqual(["app=checkout"]);
		});

		await user.click(
			screen.getByRole("button", { name: "Remove app=checkout" }),
		);
		await waitFor(() => {
			const last = calls.filter((c) => c.path.includes("/workloads")).at(-1);
			expect(
				new URL(last?.path ?? "", "http://x").searchParams.getAll("label"),
			).toEqual([]);
		});
	});

	it("adds a label requirement left pending when the field loses focus, and keeps one it cannot read", async () => {
		const { calls } = mockSurface([
			...frame,
			fleet(() => ({ workloads: [checkout, auth], next_cursor: null })),
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "auth-prod-11" });
		await user.click(screen.getByRole("button", { name: "label filter" }));
		const input = screen.getByLabelText("Label requirement");
		await user.type(input, "tier=");
		await user.click(screen.getByRole("button", { name: /degraded/ }));
		expect(input).toHaveValue("tier=");
		expect(input).toHaveAttribute("aria-invalid", "true");
		expect(input).toHaveAttribute(
			"title",
			"tier has no value. The text in the field was not added.",
		);
		await user.clear(input);
		await user.type(input, "app=checkout");
		await user.click(screen.getByRole("button", { name: /degraded/ }));
		await waitFor(() => {
			const last = calls.filter((c) => c.path.includes("/workloads")).at(-1);
			const q = new URL(last?.path ?? "", "http://x").searchParams;
			expect(q.getAll("label")).toEqual(["app=checkout"]);
		});
		expect(
			screen.getByRole("button", { name: "Remove app=checkout" }),
		).toBeInTheDocument();
	});

	it("filters to the workloads whose enforcement was removed", async () => {
		// Issue #96: removed is not a sync state, so no query selects it.
		// The fleet order ranks it with degraded at the top, and the chip
		// pages through that leading run and stops at the first row past it.
		const killed = (hostname: string, state: Workload["sync"]["state"]) =>
			workload({
				hostname,
				sync: { state, applied_version: 7, latest_version: 7 },
				health: {
					owned_table: { state: "absent", absent_since: minutesAgo(2) },
				},
			});
		const pay = killed("pay-prod-02", "synced");
		const edge = killed("edge-prod-05", "offline");
		// The server's order: removed and degraded first, then the rest.
		const order = [pay, checkout, edge, legacy, auth, bastion];
		const { calls } = mockSurface([
			...frame,
			fleet((q) => {
				// Two rows a page, so the run spans pages and the read must
				// follow the cursor through it.
				const from = Number(q.get("cursor") ?? 0);
				const to = from + 2;
				return {
					workloads: order.slice(from, to),
					next_cursor: to < order.length ? String(to) : null,
				};
			}),
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "checkout-prod-07" });

		const chipButton = screen.getByRole("button", { name: /removed/ });
		expect(
			chipButton.querySelector('[data-slot="status-glyph"]'),
		).toHaveAttribute("data-status", "error");
		await user.click(chipButton);
		await waitFor(() =>
			expect(
				screen.queryByRole("link", { name: "checkout-prod-07" }),
			).not.toBeInTheDocument(),
		);
		expect(chipButton).toHaveAttribute("aria-pressed", "true");
		expect(
			screen.getByRole("link", { name: "pay-prod-02" }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: "edge-prod-05" }),
		).toBeInTheDocument();
		for (const h of ["legacy-vm-0117", "auth-prod-11", "bastion-01"]) {
			expect(screen.queryByRole("link", { name: h })).not.toBeInTheDocument();
		}
		// Both removed rows say so; the whole result is one page.
		expect(screen.getAllByTestId("enforcement-removed")).toHaveLength(2);
		expect(
			screen.queryByRole("button", { name: "Load more" }),
		).not.toBeInTheDocument();

		// The chip's reads ask for no sync state, follow the cursor through
		// the leading run, and stop at legacy-vm-0117, the first row past
		// it: the fourth row, on the second page, so no third page is read.
		const reads = calls
			.filter((c) => /\/workloads(\?|$)/.test(c.path))
			.map((c) => new URL(c.path, "http://x").searchParams);
		const chipReads = reads.filter((q) => q.get("limit") === "500");
		expect(chipReads.map((q) => q.get("cursor"))).toEqual([null, "2"]);
		for (const q of chipReads) expect(q.get("sync_state")).toBeNull();

		// Pressed again, it clears back to the whole fleet.
		await user.click(chipButton);
		expect(
			await screen.findByRole("link", { name: "checkout-prod-07" }),
		).toBeInTheDocument();
		expect(chipButton).toHaveAttribute("aria-pressed", "false");
	});

	it("says so when filters match nothing, and clears them", async () => {
		mockSurface([
			...frame,
			fleet((q) => ({
				workloads: q.get("sync_state") ? [] : [checkout],
				next_cursor: null,
			})),
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "checkout-prod-07" });
		await user.click(screen.getByRole("button", { name: /offline/ }));
		expect(
			await screen.findByText("No workloads match these filters."),
		).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Clear filters" }));
		expect(
			await screen.findByRole("link", { name: "checkout-prod-07" }),
		).toBeInTheDocument();
	});

	it("pages by cursor", async () => {
		const { calls } = mockSurface([
			...frame,
			fleet((q) =>
				q.get("cursor") === "c2"
					? { workloads: [legacy], next_cursor: null }
					: { workloads: [checkout, auth], next_cursor: "c2" },
			),
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "auth-prod-11" });
		await user.click(screen.getByRole("button", { name: "Load more" }));
		expect(
			await screen.findByRole("link", { name: "legacy-vm-0117" }),
		).toBeInTheDocument();
		expect(screen.getByText(/^Showing 3 ·/)).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Load more" }),
		).not.toBeInTheDocument();
		expect(calls.some((c) => c.path.includes("cursor=c2"))).toBe(true);
	});

	it("shows the fresh state for an empty fleet, and a problem with a retry", async () => {
		let fail = true;
		mockSurface([
			...frame,
			{
				method: "GET",
				path: "/api/v1/workloads",
				reply: () =>
					fail
						? {
								status: 500,
								problem: problem("internal", 500, "the store is unreachable"),
							}
						: { status: 200, json: { workloads: [], next_cursor: null } },
			},
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Could not load workloads",
		);
		expect(screen.getByRole("alert")).toHaveTextContent(
			"the store is unreachable",
		);
		fail = false;
		await user.click(screen.getByRole("button", { name: "Retry" }));
		expect(
			await screen.findByRole("heading", { name: "No workloads enrolled" }),
		).toBeInTheDocument();
		expect(screen.getByRole("tab", { name: /Workloads/ })).toHaveTextContent(
			"Workloads0",
		);
	});
});

describe("mode change for selected", () => {
	it("posts the selection by id with its count, then reads the list again", async () => {
		let changed = false;
		const { calls } = mockSurface([
			...frame,
			...evidence({}),
			fleet(() => ({
				workloads: changed
					? [
							{
								...checkout,
								mode: "enforced",
								sync: { ...checkout.sync, state: "pending" },
							},
							{ ...auth },
						]
					: [checkout, auth, legacy],
				next_cursor: null,
			})),
			{
				method: "POST",
				path: "/api/v1/mode-changes",
				reply: () => {
					changed = true;
					return {
						status: 200,
						json: {
							mode_change_id: "5e0c1c7e-0000-4000-8000-000000000001",
							matched: 2,
							desired_updated: 1,
						},
					};
				},
			},
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "checkout-prod-07" });

		const change = screen.getByRole("button", {
			name: "Change mode for selected…",
		});
		expect(change).toBeDisabled();
		await user.click(screen.getByLabelText("Select checkout-prod-07"));
		await user.click(screen.getByLabelText("Select auth-prod-11"));
		expect(change).toBeEnabled();
		await user.click(change);

		const dialog = await screen.findByRole("dialog", {
			name: "Change mode for selected",
		});
		expect(within(dialog).getByText(/these 2 workloads/)).toBeInTheDocument();
		await user.click(within(dialog).getByRole("radio", { name: /Enforced/ }));
		expect(
			within(dialog).getByText("1 already in enforced; the other 1 change."),
		).toBeInTheDocument();
		// checkout-prod-07 is degraded: the review would not call it safe,
		// so the change waits on the acknowledgment.
		expect(
			await within(dialog).findByText(
				"Not safe to enforce for 1 of 2 selected.",
			),
		).toBeInTheDocument();
		await user.click(
			within(dialog).getByLabelText(
				"I understand 1 of these 2 workloads is not safe to enforce yet",
			),
		);
		await user.click(
			within(dialog).getByRole("button", {
				name: "Enforce anyway on 1 workload",
			}),
		);

		await waitFor(() =>
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
		);
		const post = calls.find((c) => c.method === "POST");
		expect(post?.body).toEqual({
			workload_ids: [checkout.id, auth.id],
			target_mode: "enforced",
			expected_match_count: 2,
		});
		// The screen reflects the list it renders: the next read shows the
		// new mode and the workload converging, with no progress of its own.
		await waitFor(() =>
			expect(
				within(rowOf("checkout-prod-07")).getByText("Enforced"),
			).toBeInTheDocument(),
		);
		expect(within(rowOf("checkout-prod-07")).getByText("Pending"));
		expect(screen.getByLabelText("Select checkout-prod-07")).not.toBeChecked();
	});

	it("renders a count mismatch with both numbers and changes nothing", async () => {
		const { calls } = mockSurface([
			...frame,
			fleet(() => ({ workloads: [checkout, auth], next_cursor: null })),
			{
				method: "POST",
				path: "/api/v1/mode-changes",
				reply: {
					status: 409,
					problem: {
						...problem("match-count-mismatch", 409),
						expected: 2,
						matched: 1,
					},
				},
			},
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "checkout-prod-07" });
		await user.click(screen.getByLabelText("Select all shown"));
		await user.click(
			screen.getByRole("button", { name: "Change mode for selected…" }),
		);
		const dialog = await screen.findByRole("dialog");
		await user.click(within(dialog).getByRole("radio", { name: /Simulation/ }));
		await user.click(
			within(dialog).getByRole("button", { name: "Change to simulation" }),
		);
		const alert = await within(dialog).findByRole("alert");
		expect(alert).toHaveTextContent("The selection no longer matches");
		expect(alert).toHaveTextContent(
			"You selected 2 workloads; the control plane resolved 1.",
		);
		expect(
			within(dialog).getByRole("button", { name: "Change to simulation" }),
		).toBeDisabled();

		const reads = calls.filter((c) => c.path.includes("/workloads")).length;
		await user.click(
			within(alert).getByRole("button", { name: "Reload the list" }),
		);
		await waitFor(() =>
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
		);
		await waitFor(() =>
			expect(
				calls.filter((c) => c.path.includes("/workloads")).length,
			).toBeGreaterThan(reads),
		);
	});
});

// evidence answers the reads a change to enforced makes for each workload
// it would move: the peer-and-service groups under each decision and the
// evidence gaps, by the workload the read names.
function evidence(
	groups: Record<
		string,
		Partial<Record<"would_block" | "allowed", RollupGroup[]>>
	>,
	gaps: Record<string, EvidenceGap[]> = {},
): Route[] {
	return [
		{
			method: "GET",
			path: "/api/v1/address-groups",
			reply: { status: 200, json: { address_groups: [] } },
		},
		{
			method: "GET",
			path: "/api/v1/flows/rollup",
			reply: (_b, q) => {
				const v = q.get("verdict") as "would_block" | "allowed";
				const g = groups[q.get("workload") ?? ""]?.[v] ?? [];
				return {
					status: 200,
					json:
						g.length > 0 && q.get("group_by") === "peer,service"
							? peerService(g)
							: emptyRollup(q),
				};
			},
		},
		{
			method: "GET",
			path: "/api/v1/flows/gaps",
			reply: (_b, q) => ({
				status: 200,
				json: gapsOf(gaps[q.get("workload") ?? ""] ?? [], q),
			}),
		},
	];
}

function evidenceReads(
	calls: { method: string; path: string }[],
	what: "rollup" | "gaps",
) {
	return calls
		.filter((c) => c.path.startsWith(`/api/v1/flows/${what}`))
		.map((c) => new URL(c.path, "http://console.test").searchParams);
}

const okPost: Route = {
	method: "POST",
	path: "/api/v1/mode-changes",
	reply: {
		status: 200,
		json: {
			mode_change_id: "5e0c1c7e-0000-4000-8000-000000000003",
			matched: 1,
			desired_updated: 1,
		},
	},
};

// A workload enrolled minutes ago: visibility, no flows, never simulated.
const fresh = workload({
	hostname: "web-new-01",
	labels: { app: "web", env: "prod" },
	mode: "visibility",
	enrolled_at: minutesAgo(4),
});
// Simulating on its latest policy, every observed connection admitted.
const earned = workload({
	hostname: "web-prod-02",
	labels: { app: "web", env: "prod" },
	mode: "simulation",
});
// Simulating on its latest policy, with traffic enforcing would drop.
const dropping = workload({
	hostname: "web-prod-03",
	labels: { app: "web", env: "prod" },
	mode: "simulation",
});
const office = peer.address("192.0.2.40");

async function openEnforce(user: ReturnType<typeof userEvent.setup>) {
	await user.click(
		screen.getByRole("button", { name: "Change mode for selected…" }),
	);
	const dialog = await screen.findByRole("dialog", {
		name: "Change mode for selected",
	});
	await user.click(within(dialog).getByRole("radio", { name: /Enforced/ }));
	return dialog;
}

describe("a change to enforced from the fleet", () => {
	it("holds a workload with no simulation evidence for the acknowledgment and writes nothing without it", async () => {
		const { calls } = mockSurface([
			...frame,
			fleet(() => ({ workloads: [fresh], next_cursor: null })),
			...evidence({}),
			okPost,
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "web-new-01" });
		await user.click(screen.getByLabelText("Select web-new-01"));
		const dialog = await openEnforce(user);

		const block = await within(dialog).findByTestId("evidence");
		expect(block).toHaveTextContent("Not safe to enforce yet");
		expect(block).toHaveTextContent(
			"No simulation evidence for 1 of 1 selected.",
		);
		expect(block).toHaveTextContent("Never simulated");
		expect(
			within(block).getByRole("link", { name: "web-new-01" }),
		).toHaveAttribute("href", `/workloads/${fresh.id}`);
		expect(block).toHaveTextContent(
			"This enforces the selection as chosen; the review promotes only the workloads that earned it.",
		);

		const confirm = within(dialog).getByRole("button", {
			name: "Enforce anyway on 1 workload",
		});
		expect(confirm).toHaveAttribute("aria-disabled", "true");
		await user.click(confirm);
		expect(
			within(dialog).getByText(
				"Nothing was enforced. Check the acknowledgment above to enforce anyway.",
			),
		).toBeInTheDocument();
		const ack = within(dialog).getByLabelText(
			"I understand this workload is not safe to enforce yet",
		);
		expect(ack).toHaveFocus();
		expect(calls.some((c) => c.method === "POST")).toBe(false);
		expect(screen.getByRole("dialog")).toBeInTheDocument();

		await user.click(ack);
		expect(confirm).not.toHaveAttribute("aria-disabled");
		await user.click(confirm);
		await waitFor(() =>
			expect(calls.find((c) => c.method === "POST")?.body).toEqual({
				workload_ids: [fresh.id],
				target_mode: "enforced",
				expected_match_count: 1,
			}),
		);
	});

	it("reads each workload's evidence over the review's default range and enforces safe evidence in one step, as a promotion does", async () => {
		const { calls } = mockSurface([
			...frame,
			fleet(() => ({ workloads: [earned], next_cursor: null })),
			...evidence({
				[earned.id]: { allowed: [ps(office, "tcp/443", 120, 1)] },
			}),
			okPost,
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "web-prod-02" });
		await user.click(screen.getByLabelText("Select web-prod-02"));
		const dialog = await openEnforce(user);

		const block = await within(dialog).findByTestId("evidence");
		expect(block).toHaveTextContent("Safe to enforce");
		expect(block).toHaveTextContent("No observed traffic would be dropped.");
		expect(within(dialog).queryByRole("checkbox")).not.toBeInTheDocument();

		// The same four rollups and the gaps the review reads, for this
		// one workload, over the last 24 hours.
		const rollups = evidenceReads(calls, "rollup");
		expect(
			rollups.map((q) => `${q.get("group_by")} ${q.get("verdict")}`).sort(),
		).toEqual([
			"peer,service allowed",
			"peer,service would_block",
			"src,dst allowed",
			"src,dst would_block",
		]);
		const gaps = evidenceReads(calls, "gaps");
		expect(gaps).toHaveLength(1);
		for (const q of [...rollups, ...gaps]) {
			expect(q.get("workload")).toBe(earned.id);
			expect(
				Date.parse(q.get("to") ?? "") - Date.parse(q.get("from") ?? ""),
			).toBe(24 * 3_600_000);
		}

		await user.click(
			within(dialog).getByRole("button", { name: "Change to enforced" }),
		);
		await waitFor(() =>
			expect(calls.find((c) => c.method === "POST")?.body).toEqual({
				workload_ids: [earned.id],
				target_mode: "enforced",
				expected_match_count: 1,
			}),
		);
	});

	it("counts a mixed selection by what stands behind each workload and asks once for the batch", async () => {
		const enforcedAlready = workload({
			hostname: "web-prod-04",
			labels: { app: "web", env: "prod" },
			mode: "enforced",
		});
		const gapped = workload({
			hostname: "web-prod-05",
			labels: { app: "web", env: "prod" },
			mode: "simulation",
		});
		const { calls } = mockSurface([
			...frame,
			fleet(() => ({
				workloads: [fresh, earned, dropping, gapped, enforcedAlready],
				next_cursor: null,
			})),
			...evidence(
				{
					[earned.id]: { allowed: [ps(office, "tcp/443", 120, 1)] },
					[dropping.id]: {
						would_block: [ps(office, "tcp/22", 4, 1)],
						allowed: [ps(office, "tcp/443", 80, 1)],
					},
					[gapped.id]: { allowed: [ps(office, "tcp/443", 60, 1)] },
				},
				{ [gapped.id]: [gap(gapped, 90, 80)] },
			),
			okPost,
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "web-new-01" });
		await user.click(screen.getByLabelText("Select all shown"));
		const dialog = await openEnforce(user);

		const block = await within(dialog).findByTestId("evidence");
		expect(block).toHaveTextContent(
			"No simulation evidence for 1 of 5 selected; Not safe to enforce for 2 of 5 selected.",
		);
		expect(
			within(block).getByRole("link", { name: "web-new-01" }),
		).toBeInTheDocument();
		const droppingRow = within(block)
			.getByRole("link", { name: "web-prod-03" })
			.closest("div");
		expect(droppingRow).toHaveTextContent(
			"1 peer/service pair would be dropped",
		);
		const gappedRow = within(block)
			.getByRole("link", { name: "web-prod-05" })
			.closest("div");
		expect(gappedRow).toHaveTextContent("Evidence incomplete for web-prod-05");
		expect(block).toHaveTextContent("Safe to enforce1 workload");
		expect(block).toHaveTextContent("Already enforced1 workload");
		expect(
			within(block).queryByRole("link", { name: "web-prod-02" }),
		).not.toBeInTheDocument();
		// The enforced workload is not moved, so its evidence is not read.
		expect(
			evidenceReads(calls, "rollup").some(
				(q) => q.get("workload") === enforcedAlready.id,
			),
		).toBe(false);

		const confirm = within(dialog).getByRole("button", {
			name: "Enforce anyway on 4 workloads",
		});
		await user.click(confirm);
		expect(calls.some((c) => c.method === "POST")).toBe(false);
		await user.click(
			within(dialog).getByLabelText(
				"I understand 3 of these 5 workloads are not safe to enforce yet",
			),
		);
		expect(within(dialog).getAllByRole("checkbox")).toHaveLength(1);
		await user.click(confirm);
		await waitFor(() =>
			expect(calls.find((c) => c.method === "POST")?.body).toEqual({
				workload_ids: [
					fresh.id,
					earned.id,
					dropping.id,
					gapped.id,
					enforcedAlready.id,
				],
				target_mode: "enforced",
				expected_match_count: 5,
			}),
		);
	});

	it("writes nothing while the evidence cannot be read", async () => {
		const { calls } = mockSurface([
			...frame,
			fleet(() => ({ workloads: [earned], next_cursor: null })),
			{
				method: "GET",
				path: "/api/v1/flows/rollup",
				reply: { status: 500, problem: problem("internal", 500) },
			},
			...evidence({}),
			okPost,
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "web-prod-02" });
		await user.click(screen.getByLabelText("Select web-prod-02"));
		const dialog = await openEnforce(user);
		expect(
			await within(dialog).findByText(/each workload's evidence/),
		).toBeInTheDocument();
		const confirm = within(dialog).getByRole("button", {
			name: "Change to enforced",
		});
		expect(confirm).toHaveAttribute("aria-disabled", "true");
		await user.click(confirm);
		expect(calls.some((c) => c.method === "POST")).toBe(false);
	});

	it.each([
		["simulation", "Simulation"],
		["visibility", "Visibility"],
	] as const)(
		"keeps a change to %s one step, with no evidence read",
		async (mode, label) => {
			const { calls } = mockSurface([
				...frame,
				fleet(() => ({
					workloads: [{ ...dropping, mode: "enforced" }, fresh],
					next_cursor: null,
				})),
				...evidence({}),
				okPost,
			]);
			const user = userEvent.setup();
			renderApp("/workloads");
			await screen.findByRole("link", { name: "web-new-01" });
			await user.click(screen.getByLabelText("Select all shown"));
			await user.click(
				screen.getByRole("button", { name: "Change mode for selected…" }),
			);
			const dialog = await screen.findByRole("dialog");
			await user.click(
				within(dialog).getByRole("radio", { name: new RegExp(label) }),
			);
			expect(within(dialog).queryByTestId("evidence")).not.toBeInTheDocument();
			expect(within(dialog).queryByRole("checkbox")).not.toBeInTheDocument();
			await user.click(
				within(dialog).getByRole("button", { name: `Change to ${mode}` }),
			);
			await waitFor(() =>
				expect(calls.find((c) => c.method === "POST")?.body).toEqual({
					workload_ids: [dropping.id, fresh.id],
					target_mode: mode,
					expected_match_count: 2,
				}),
			);
			expect(evidenceReads(calls, "rollup")).toHaveLength(0);
			expect(evidenceReads(calls, "gaps")).toHaveLength(0);
		},
	);
});

const decommissioned = workload({
	hostname: "batch-prod-01",
	labels: { app: "batch", env: "prod" },
	lifecycle: "retired",
	retired_at: minutesAgo(26 * 60),
	sync: { state: "offline", applied_version: 7, latest_version: 7 },
	health: {
		last_seen_at: minutesAgo(30 * 60),
		telemetry_live: false,
		credential: {
			state: "expired",
			lifecycle: "in-grace",
			expires_at: minutesAgo(20 * 60),
		},
	},
});

function counts(retired: number): Route {
	return {
		method: "GET",
		path: "/api/v1/workloads/counts",
		reply: {
			status: 200,
			json: {
				total: 4,
				by_sync_state: { synced: 1, pending: 1, degraded: 1, offline: 1 },
				removed: 0,
				retired,
			},
		},
	};
}

describe("retired workloads", () => {
	it("lists them apart, counted, frozen, and unlike an offline row", async () => {
		const { calls } = mockSurface([
			...frame,
			counts(1),
			fleet((q) => ({
				workloads:
					q.get("lifecycle") === "retired"
						? [decommissioned]
						: [checkout, auth, legacy, bastion],
				next_cursor: null,
			})),
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "checkout-prod-07" });
		// The active fleet never carries a retired row; the chip says how
		// many there are, from the control plane's counts.
		expect(
			screen.queryByRole("link", { name: "batch-prod-01" }),
		).not.toBeInTheDocument();
		const lifecycle = screen.getByRole("group", { name: "Lifecycle" });
		const toggle = within(lifecycle).getByRole("button", { name: /retired/ });
		await waitFor(() => expect(toggle).toHaveTextContent("retired1"));

		await user.click(toggle);
		await screen.findByRole("link", { name: "batch-prod-01" });
		const listed = calls.filter((c) => c.path.includes("lifecycle=retired"));
		expect(listed).toHaveLength(1);
		expect(listed[0]?.path).not.toContain("sync_state");
		expect(
			screen.queryByRole("link", { name: "checkout-prod-07" }),
		).not.toBeInTheDocument();

		const r = within(rowOf("batch-prod-01"));
		expect(r.getByTestId("retired")).toHaveTextContent("Retired");
		expect(r.getByText("retired 1d ago")).toBeInTheDocument();
		expect(r.getByText("refused · retired")).toBeInTheDocument();
		// Not offline: no health glyph, no offline word, no contact note.
		expect(r.queryByText("Offline")).not.toBeInTheDocument();
		expect(r.queryByText(/no contact/)).not.toBeInTheDocument();
		expect(
			rowOf("batch-prod-01").querySelector("[data-slot=status-glyph]"),
		).toBeNull();
		// Frozen: nothing to select for a mode change or a retirement.
		expect(r.queryByRole("checkbox")).not.toBeInTheDocument();
		expect(
			screen.queryByRole("checkbox", { name: "Select all shown" }),
		).not.toBeInTheDocument();
		expect(
			screen.getByText("Showing 1 · most recently retired first"),
		).toBeInTheDocument();
	});

	it("does not call a fleet of retired workloads empty", async () => {
		mockSurface([
			...frame,
			{
				method: "GET",
				path: "/api/v1/workloads/counts",
				reply: {
					status: 200,
					json: {
						total: 0,
						by_sync_state: { synced: 0, pending: 0, degraded: 0, offline: 0 },
						removed: 0,
						retired: 2,
					},
				},
			},
			fleet((q) => ({
				workloads: q.get("lifecycle") === "retired" ? [decommissioned] : [],
				next_cursor: null,
			})),
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		expect(
			await screen.findByText("No active workloads; 2 workloads are retired."),
		).toBeInTheDocument();
		expect(screen.queryByText("No workloads enrolled")).not.toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Show retired" }));
		await screen.findByRole("link", { name: "batch-prod-01" });
	});
});

describe("retire selected", () => {
	it("asks for the acknowledgment, retires each, and reports every outcome", async () => {
		let retiredIds: string[] = [];
		const { calls } = mockSurface([
			...frame,
			counts(0),
			fleet(() => ({
				workloads: [checkout, auth, legacy, bastion].filter(
					(w) => !retiredIds.includes(w.id),
				),
				next_cursor: null,
			})),
			{
				method: "POST",
				path: `/api/v1/workloads/${checkout.id}/retire`,
				reply: () => {
					retiredIds = [...retiredIds, checkout.id];
					return {
						status: 200,
						json: {
							workload_id: checkout.id,
							retired_at: "2026-10-05T11:59:58.123456Z",
							versions_advanced: 3,
							silence_closed: null,
						},
					};
				},
			},
			{
				method: "POST",
				path: `/api/v1/workloads/${auth.id}/retire`,
				reply: {
					status: 409,
					problem: {
						...problem("workload-retired", 409),
						retired_at: "2026-10-05T10:00:00Z",
					},
				},
			},
			{
				method: "POST",
				path: `/api/v1/workloads/${legacy.id}/retire`,
				reply: { status: 404, problem: problem("not-found", 404) },
			},
		]);
		const user = userEvent.setup();
		renderApp("/workloads");
		await screen.findByRole("link", { name: "checkout-prod-07" });
		const retire = screen.getByRole("button", { name: "Retire selected…" });
		expect(retire).toBeDisabled();
		for (const h of ["checkout-prod-07", "auth-prod-11", "legacy-vm-0117"]) {
			await user.click(screen.getByLabelText(`Select ${h}`));
		}
		await user.click(retire);

		const dialog = await screen.findByRole("dialog", {
			name: "Retire 3 workloads?",
		});
		for (const said of [
			/leave every peer set/,
			/a live sync stream ends/,
			/^It cannot be undone/,
		]) {
			expect(within(dialog).getByText(said)).toBeInTheDocument();
		}
		// Held back until acknowledged: a click sends nothing and says why.
		const confirm = within(dialog).getByRole("button", {
			name: "Retire 3 workloads",
		});
		expect(confirm).toHaveAttribute("aria-disabled", "true");
		await user.click(confirm);
		expect(
			within(dialog).getByText(/Nothing was retired. Check the acknowledgment/),
		).toBeInTheDocument();
		expect(calls.some((c) => c.method === "POST")).toBe(false);

		await user.click(
			within(dialog).getByRole("checkbox", { name: /cannot be undone/ }),
		);
		await user.click(confirm);

		const outcomes = await within(dialog).findByTestId("retire-outcomes");
		const posts = calls.filter((c) => c.method === "POST").map((c) => c.path);
		expect(posts).toEqual([
			`/api/v1/workloads/${checkout.id}/retire`,
			`/api/v1/workloads/${auth.id}/retire`,
			`/api/v1/workloads/${legacy.id}/retire`,
		]);
		expect(outcomes).toHaveTextContent(
			"checkout-prod-07Retired 2026-10-05T11:59:58Z; 3 workloads' policy advanced.",
		);
		// The 409 names the instant it was already retired.
		expect(outcomes).toHaveTextContent(
			"auth-prod-11Already retired 2h ago (2026-10-05T10:00:00Z); nothing changed.",
		);
		expect(outcomes).toHaveTextContent(
			"legacy-vm-0117No workload is registered under this id; nothing was retired.",
		);
		// The one retired leaves the active list read again under it.
		await waitFor(() =>
			expect(
				screen.queryByRole("link", { name: "checkout-prod-07" }),
			).not.toBeInTheDocument(),
		);
		await user.click(within(dialog).getByRole("button", { name: "Close" }));
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});
});
