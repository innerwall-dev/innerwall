import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { mockSurface, operator, renderApp } from "@/test/harness";
import { storageKey } from "@/theme/ThemeProvider";
import { initials } from "./AccountPopover";

const signedIn = () =>
	mockSurface([
		{
			method: "GET",
			path: "/api/v1/me",
			reply: { status: 200, json: operator },
		},
	]);

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
	});
});

describe("account popover", () => {
	it("opens with the operator's initials, identity line, and theme control", async () => {
		signedIn();
		const user = userEvent.setup();
		renderApp("/simulation");
		const trigger = await screen.findByRole("button", { name: "Account" });
		expect(trigger).toHaveTextContent("AR");
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

	it("falls back to a generic mark when the display name is null", async () => {
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
		expect(trigger).toHaveTextContent("OP");
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
	it("persists the choice and toggles the root class", async () => {
		signedIn();
		const user = userEvent.setup();
		renderApp("/simulation");
		await user.click(await screen.findByRole("button", { name: "Account" }));
		await user.click(await screen.findByRole("radio", { name: "Dark" }));
		await waitFor(() => expect(document.documentElement).toHaveClass("dark"));
		expect(localStorage.getItem(storageKey)).toBe("dark");
		await user.click(screen.getByRole("radio", { name: "Light" }));
		await waitFor(() =>
			expect(document.documentElement).not.toHaveClass("dark"),
		);
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

describe("initials", () => {
	it("takes the first and last parts of a name and falls back generically", () => {
		expect(initials("A. Rao")).toBe("AR");
		expect(initials("Avinash Papineni")).toBe("AP");
		expect(initials("ops")).toBe("OP");
		expect(initials("  ")).toBe("OP");
		expect(initials(null)).toBe("OP");
	});
});
