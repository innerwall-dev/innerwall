import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Ruleset } from "@/api/schema";
import { workload } from "@/test/fixtures";
import { freshInstall, mockSurface, renderApp } from "@/test/harness";
import { search } from "./search";

const web = workload({
	hostname: "iw-web",
	labels: { app: "web", env: "lab" },
});
const db = workload({ hostname: "iw-db", labels: { app: "db", env: "lab" } });
const ops = workload({ hostname: "iw-ops", labels: { role: "ops" } });

const ruleset = (name: string): Ruleset => ({
	id: `5f000000-0000-4000-8000-${name.length.toString().padStart(12, "0")}`,
	name,
	description: "",
	enabled: true,
	scope: { app: ["db"] },
	rules: [],
	version: "1",
	created_at: new Date().toISOString(),
	updated_at: new Date().toISOString(),
});

const rulesets = [ruleset("db-ingress"), ruleset("web-ingress")];

describe("search", () => {
	it("matches hostnames, labels as key=value, and ruleset names by substring", () => {
		const path = (n: string) => `/policy?ruleset=${n}`;
		expect(search("", [web, db], rulesets, path)).toEqual({
			workloads: [],
			rulesets: [],
		});
		const byName = search("WEB", [web, db, ops], rulesets, path);
		expect(byName.workloads.map((h) => h.title)).toEqual(["iw-web"]);
		expect(byName.rulesets.map((h) => h.title)).toEqual(["web-ingress"]);
		const byLabel = search("env=lab", [web, db, ops], rulesets, path);
		expect(byLabel.workloads.map((h) => [h.title, h.detail])).toEqual([
			["iw-web", "env=lab"],
			["iw-db", "env=lab"],
		]);
		expect(byLabel.rulesets).toEqual([]);
		expect(search("ingress", [], rulesets, path).rulesets[1]?.to).toBe(
			"/policy?ruleset=web-ingress",
		);
	});
});

describe("search palette", () => {
	const populated = () =>
		mockSurface([
			{
				method: "GET",
				path: "/api/v1/workloads",
				reply: {
					status: 200,
					json: { workloads: [web, db, ops], next_cursor: null },
				},
			},
			{
				method: "GET",
				path: `/api/v1/workloads/${ops.id}`,
				reply: { status: 200, json: ops },
			},
			{
				method: "GET",
				path: "/api/v1/rulesets",
				reply: { status: 200, json: { rulesets, state_version: "3" } },
			},
			...freshInstall,
		]);

	it("opens from the header box, finds workloads and rulesets, and opens the chosen one", async () => {
		populated();
		const user = userEvent.setup();
		renderApp("/workloads/tokens");
		await user.click(
			await screen.findByRole("button", {
				name: /Search workloads and rulesets/,
			}),
		);
		const box = await screen.findByRole("combobox", {
			name: "Search workloads and rulesets",
		});
		await user.type(box, "db");
		const results = await screen.findByRole("listbox", { name: "Results" });
		expect(
			within(results)
				.getAllByRole("option")
				.map((o) => o.textContent),
		).toEqual(["iw-db", "db-ingress"]);
		await user.keyboard("{ArrowDown}{Enter}");
		expect(
			await screen.findByRole("heading", { name: "db-ingress" }),
		).toBeInTheDocument();
		expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
	});

	it("opens with ⌘K or Ctrl+K, says when nothing matches, and closes on Escape", async () => {
		populated();
		const user = userEvent.setup();
		renderApp("/workloads/tokens");
		await screen.findByRole("button", {
			name: /Search workloads and rulesets/,
		});
		fireEvent.keyDown(window, { key: "k", metaKey: true });
		const box = await screen.findByRole("combobox");
		await user.type(box, "nothing-here");
		expect(await screen.findByText(/Nothing matches/)).toBeInTheDocument();
		await user.keyboard("{Escape}");
		await waitFor(() =>
			expect(screen.queryByRole("combobox")).not.toBeInTheDocument(),
		);
		fireEvent.keyDown(window, { key: "K", ctrlKey: true });
		await user.type(await screen.findByRole("combobox"), "role=ops{Enter}");
		await waitFor(() =>
			expect(
				screen.getByRole("navigation", { name: "Breadcrumb" }),
			).toHaveTextContent("iw-ops"),
		);
	});
});
