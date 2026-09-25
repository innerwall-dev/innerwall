import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Workload } from "@/api/schema";
import { minutesAgo, workload } from "@/test/fixtures";
import {
	mockSurface,
	problem,
	type Route,
	renderApp,
	signedIn,
} from "@/test/harness";

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
	health: { last_seen_at: minutesAgo(0.34) },
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
	sync: { state: "offline", applied_version: 1, latest_version: 1 },
	health: {
		last_seen_at: minutesAgo(3 * 24 * 60),
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
			expect(c.getByText(l)).toBeInTheDocument();
		}
		expect(c.getByText("Simulation")).toBeInTheDocument();
		expect(c.getByText("Degraded")).toBeInTheDocument();
		expect(c.getByText("v41 · v42 failed")).toBeInTheDocument();
		expect(c.getByText("v41")).toBeInTheDocument();
		expect(c.getByText("renews · expires 9h")).toBeInTheDocument();
		expect(c.getByText("20s")).toBeInTheDocument();

		const a = within(rowOf("auth-prod-11"));
		expect(a.getByText("Pending")).toBeInTheDocument();
		expect(a.getByText("v89 rendered 42s ago")).toBeInTheDocument();

		const l = within(rowOf("legacy-vm-0117"));
		expect(l.getByText("▲ no labels — matches no scope")).toBeInTheDocument();
		expect(l.getByText("no stream for 3d")).toBeInTheDocument();
		expect(l.getByText("expired 2d ago")).toBeInTheDocument();

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

		await user.click(screen.getByRole("button", { name: "+ label filter" }));
		await user.type(
			screen.getByLabelText("Label requirement"),
			"app=checkout{Enter}",
		);
		await user.click(screen.getByRole("button", { name: "+ label filter" }));
		await user.type(screen.getByLabelText("Label requirement"), "tier{Enter}");
		expect(screen.getByLabelText("Label requirement")).toHaveAttribute(
			"aria-invalid",
			"true",
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
		await user.click(
			within(dialog).getByRole("button", { name: "Change to enforced" }),
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
