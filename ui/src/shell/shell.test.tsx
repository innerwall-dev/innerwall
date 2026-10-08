import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
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

	it("routes the root to simulation review and unknown paths with it", async () => {
		signedIn();
		renderApp("/no/such/screen");
		expect(
			await screen.findByRole("heading", { name: "Nothing to review yet" }),
		).toBeInTheDocument();
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
