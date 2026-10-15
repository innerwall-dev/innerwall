import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { SyncState } from "@/api/schema";
import { freshInstall, mockSurface, operator, renderApp } from "@/test/harness";
import { storageKey } from "@/theme/ThemeProvider";

const signedIn = () => mockSurface(freshInstall);

describe("shell", () => {
	it("shows the sections, the site label, and the screen's breadcrumb", async () => {
		signedIn();
		renderApp("/workloads");
		expect(
			await screen.findByRole("heading", { name: "No workloads enrolled" }),
		).toBeInTheDocument();
		const nav = screen.getByRole("navigation", { name: "Sections" });
		for (const label of [
			"Simulation review",
			"Flow map",
			"Workloads",
			"Policy",
		]) {
			expect(
				within(nav).getByRole("link", { name: new RegExp(label) }),
			).toBeInTheDocument();
		}
		expect(screen.getByTestId("site-label")).toHaveTextContent("iad1");
		const crumbs = screen.getByRole("navigation", { name: "Breadcrumb" });
		expect(crumbs).toHaveTextContent("Workloads/Fleet");
	});

	it("routes the root to simulation review", async () => {
		signedIn();
		renderApp("/");
		expect(
			await screen.findByRole("heading", { name: "Nothing to review yet" }),
		).toBeInTheDocument();
	});

	it.each(["/no/such/screen", "/flows"])(
		"says %s is not found, with the way back to the fleet",
		async (path) => {
			signedIn();
			renderApp(path);
			expect(
				await screen.findByRole("heading", { name: "No such page" }),
			).toBeInTheDocument();
			expect(screen.getByText(path)).toBeInTheDocument();
			expect(
				screen.getByRole("link", { name: "Back to the fleet" }),
			).toHaveAttribute("href", "/workloads");
			expect(
				screen.getByRole("navigation", { name: "Breadcrumb" }),
			).toHaveTextContent("Not found");
		},
	);

	// W5-a: chrome spans and its contents align to the page container;
	// the Flow map runs full-bleed, its strip with it.
	it.each([
		["/simulation", true],
		["/workloads", true],
		["/policy", true],
		["/no/such/screen", true],
		["/map", false],
	])("aligns %s's strip to the page container: %s", async (path, contained) => {
		signedIn();
		renderApp(path);
		const strip = (await screen.findByRole("banner")).firstElementChild;
		expect(screen.getByRole("banner")).not.toHaveClass("max-w-page");
		if (contained) expect(strip).toHaveClass("max-w-page", "mx-auto");
		else expect(strip).not.toHaveClass("max-w-page");
	});

	it("spans a tab row's hairline and contains its tabs", async () => {
		signedIn();
		renderApp("/workloads");
		const tabs = (await screen.findAllByRole("tablist"))[0];
		const contents = tabs.parentElement;
		expect(contents).toHaveClass("max-w-page", "mx-auto", "px-5");
		expect(contents?.parentElement).toHaveClass("border-b");
		expect(contents?.parentElement).not.toHaveClass("max-w-page");
	});

	it("renders every fresh-install state", async () => {
		signedIn();
		const user = userEvent.setup();
		renderApp("/simulation");
		expect(
			await screen.findByRole("heading", { name: "Nothing to review yet" }),
		).toBeInTheDocument();
		expect(screen.getByText(/Enroll workloads/)).toBeInTheDocument();
		await user.click(screen.getByRole("link", { name: /Flow map/ }));
		expect(
			await screen.findByRole("heading", { name: "No flows observed yet" }),
		).toBeInTheDocument();
		await user.click(screen.getByRole("link", { name: /Policy/ }));
		expect(
			await screen.findByRole("heading", { name: "Create your first ruleset" }),
		).toBeInTheDocument();
		expect(screen.getByText("No rulesets yet.")).toBeInTheDocument();
		await user.click(screen.getByRole("link", { name: /Workloads/ }));
		expect(
			await screen.findByRole("heading", { name: "No workloads enrolled" }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("tab", { name: /Provisioning tokens/ }),
		).toBeInTheDocument();
		await user.click(
			screen.getByRole("link", { name: "Mint a provisioning token" }),
		);
		expect(
			await screen.findByText(
				"No tokens yet. Mint one to enroll your first workload.",
			),
		).toBeInTheDocument();
		expect(
			screen.getByRole("navigation", { name: "Breadcrumb" }),
		).toHaveTextContent("Workloads/Provisioning tokens");
	});
});

describe("sidebar", () => {
	it("collapses to the icon rail and back, keeping every entry's name", async () => {
		signedIn();
		const user = userEvent.setup();
		renderApp("/simulation");
		const collapse = await screen.findByRole("button", {
			name: "Collapse sidebar",
		});
		const rail = document.querySelector('[data-slot="sidebar"]');
		expect(rail).toHaveAttribute("data-state", "expanded");
		await user.click(collapse);
		expect(rail).toHaveAttribute("data-state", "collapsed");
		expect(rail).toHaveAttribute("data-collapsible", "icon");
		expect(document.cookie).toContain("sidebar_state=false");
		const nav = screen.getByRole("navigation", { name: "Sections" });
		for (const label of [
			"Simulation review",
			"Flow map",
			"Workloads",
			"Policy",
		]) {
			expect(
				within(nav).getByRole("link", { name: new RegExp(label) }),
			).toBeInTheDocument();
		}
		await user.click(screen.getByRole("button", { name: "Expand sidebar" }));
		expect(rail).toHaveAttribute("data-state", "expanded");
		expect(document.cookie).toContain("sidebar_state=true");
	});
});

describe("workloads badge", () => {
	const withCounts = (by: Partial<Record<SyncState, number>>, removed = 0) => {
		const by_sync_state = {
			synced: 0,
			pending: 0,
			degraded: 0,
			offline: 0,
			...by,
		};
		const total =
			Object.values(by_sync_state).reduce((a, b) => a + b, 0) + removed;
		return mockSurface([
			{
				method: "GET",
				path: "/api/v1/workloads/counts",
				reply: { status: 200, json: { total, by_sync_state, removed } },
			},
			...freshInstall,
		]);
	};

	it("counts the workloads needing attention and says what it counts", async () => {
		withCounts({ synced: 9, pending: 1, degraded: 2, offline: 1 });
		const user = userEvent.setup();
		renderApp("/policy");
		const badge = await screen.findByTestId("count-Workloads");
		expect(badge).toHaveTextContent("4");
		const hint =
			"4 workloads need attention: 1 pending, 2 degraded, 1 offline. Counts workloads whose sync state is pending, degraded, or offline, and those whose enforcement was removed on the host.";
		expect(badge).toHaveAttribute("aria-label", hint);
		await user.hover(badge);
		expect(await screen.findByRole("tooltip")).toHaveTextContent(hint);
		// The fleet sync block is the fresh install's alone.
		expect(screen.queryByText("no workloads")).not.toBeInTheDocument();
	});

	it("counts a workload whose enforcement was removed", async () => {
		// Issue #96: the control plane counts a kill-switched host in
		// removed and not in synced, so the badge adds it once.
		withCounts({ synced: 11, degraded: 1 }, 2);
		renderApp("/policy");
		const badge = await screen.findByTestId("count-Workloads");
		expect(badge).toHaveTextContent("3");
		expect(badge).toHaveAttribute(
			"aria-label",
			"3 workloads need attention: 1 degraded, 2 enforcement removed. Counts workloads whose sync state is pending, degraded, or offline, and those whose enforcement was removed on the host.",
		);
	});

	it("is hidden when no workload needs attention", async () => {
		withCounts({ synced: 12 });
		renderApp("/policy");
		expect(await screen.findByTestId("count-Policy")).toHaveTextContent("0");
		await waitFor(() =>
			expect(screen.queryByText("no workloads")).not.toBeInTheDocument(),
		);
		expect(screen.queryByTestId("count-Workloads")).not.toBeInTheDocument();
	});

	it("keeps the fresh install's zero, the cue to enroll", async () => {
		signedIn();
		renderApp("/policy");
		const badge = await screen.findByTestId("count-Workloads");
		expect(badge).toHaveTextContent("0");
		expect(badge).toHaveAttribute("aria-label", "No workloads enrolled");
		expect(screen.getByText("no workloads")).toBeInTheDocument();
	});
});

describe("account popover", () => {
	it("opens with the operator's name, identity line, and theme control", async () => {
		signedIn();
		const user = userEvent.setup();
		renderApp("/simulation");
		const trigger = await screen.findByRole("button", { name: "Account" });
		expect(trigger).toHaveTextContent("A. Rao");
		expect(trigger).toHaveTextContent("iad1");
		await user.click(trigger);
		const popover = await screen.findByRole("dialog", { name: "Account" });
		expect(within(popover).getByText("A. Rao")).toBeInTheDocument();
		expect(within(popover).getByTestId("identity-line")).toHaveTextContent(
			"operator · iad1",
		);
		expect(within(popover).getByTestId("version-line")).toHaveTextContent(
			"control plane v0.1.0+6007581f652b",
		);
		expect(
			within(popover).getByRole("radio", { name: "Dark" }),
		).toBeInTheDocument();
		expect(
			within(popover).getByRole("radio", { name: "Light" }),
		).toBeInTheDocument();
		expect(
			within(popover).getByRole("button", { name: /Sign out/ }),
		).toBeInTheDocument();
		expect(
			within(popover).queryByText(/Console settings/),
		).not.toBeInTheDocument();
	});

	it("falls back to a generic name when the display name is null", async () => {
		mockSurface([
			{
				method: "GET",
				path: "/api/v1/me",
				reply: { status: 200, json: { display_name: null, site: "" } },
			},
		]);
		const user = userEvent.setup();
		renderApp("/simulation");
		const trigger = await screen.findByRole("button", { name: "Account" });
		expect(trigger).toHaveTextContent("Operator");
		await user.click(trigger);
		const popover = await screen.findByRole("dialog", { name: "Account" });
		expect(within(popover).getByText("Operator")).toBeInTheDocument();
		expect(within(popover).getByTestId("identity-line")).toHaveTextContent(
			/^operator$/,
		);
		expect(screen.queryByTestId("site-label")).not.toBeInTheDocument();
		expect(screen.queryByTestId("version-line")).not.toBeInTheDocument();
	});

	it("signs out through the session endpoint and lands on login", async () => {
		const { calls } = mockSurface([
			{
				method: "GET",
				path: "/api/v1/me",
				reply: { status: 200, json: operator },
			},
			{
				method: "DELETE",
				path: "/api/v1/session",
				reply: { status: 200, json: {} },
			},
		]);
		const user = userEvent.setup();
		renderApp("/simulation");
		await user.click(await screen.findByRole("button", { name: "Account" }));
		await user.click(await screen.findByRole("button", { name: /Sign out/ }));
		expect(
			await screen.findByRole("button", { name: "Sign in" }),
		).toBeInTheDocument();
		expect(
			calls.some(
				(c) => c.method === "DELETE" && c.path.endsWith("/api/v1/session"),
			),
		).toBe(true);
	});
});

describe("theme", () => {
	it("persists the choice and sets the root's theme", async () => {
		signedIn();
		const user = userEvent.setup();
		renderApp("/simulation");
		await user.click(await screen.findByRole("button", { name: "Account" }));
		await user.click(await screen.findByRole("radio", { name: "Dark" }));
		await waitFor(() => expect(document.documentElement).toHaveClass("dark"));
		expect(document.documentElement.dataset.theme).toBe("dark");
		expect(localStorage.getItem(storageKey)).toBe("dark");
		await user.click(screen.getByRole("radio", { name: "Light" }));
		await waitFor(() =>
			expect(document.documentElement).not.toHaveClass("dark"),
		);
		expect(document.documentElement.dataset.theme).toBe("light");
		expect(localStorage.getItem(storageKey)).toBe("light");
	});

	it("starts from the stored preference", async () => {
		localStorage.setItem(storageKey, "dark");
		signedIn();
		renderApp("/simulation");
		await screen.findByRole("button", { name: "Account" });
		expect(document.documentElement).toHaveClass("dark");
	});
});
