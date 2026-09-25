import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { ProvisioningToken } from "@/api/schema";
import { minutesAgo, token } from "@/test/fixtures";
import { mockSurface, type Route, renderApp, signedIn } from "@/test/harness";

const secret = "iw_Qm8xZk3vT2pLw9sYh4RaN7cJe1Bd6FgUiVoXt0KqM5";

function surface(tokens: () => ProvisioningToken[], extra: Route[] = []) {
	return mockSurface([
		signedIn,
		{
			method: "GET",
			path: "/api/v1/workloads",
			reply: { status: 200, json: { workloads: [], next_cursor: null } },
		},
		{
			method: "GET",
			path: "/api/v1/rulesets",
			reply: { status: 200, json: { rulesets: [], state_version: "0" } },
		},
		{
			method: "GET",
			path: "/api/v1/provisioning-tokens",
			reply: () => ({ status: 200, json: { tokens: tokens() } }),
		},
		...extra,
	]);
}

function rowOf(name: string): HTMLElement {
	const row = screen.getByText(name).closest("tr");
	if (!row) throw new Error(`no row for ${name}`);
	return row;
}

describe("provisioning tokens", () => {
	it("lists tokens by prefix and metadata, and a missing prefix as none", async () => {
		surface(() => [
			token({ name: "checkout-prod-image", prefix: "iw_Qm8x" }),
			token({
				name: "legacy-import",
				prefix: null,
				labels: {},
				state: "revoked",
				revoked_at: minutesAgo(60),
				use_count: 30,
			}),
			token({
				name: "staging-trial",
				prefix: "iw_h4Ra",
				labels: { env: "staging" },
				state: "expired",
				expires_at: minutesAgo(9 * 24 * 60),
				use_count: 0,
				last_used_at: null,
			}),
		]);
		renderApp("/workloads/tokens");
		await screen.findByText("checkout-prod-image");
		expect(
			screen.getByRole("tab", { name: /Provisioning tokens/ }),
		).toHaveTextContent("Provisioning tokens3");

		const active = within(rowOf("checkout-prod-image"));
		expect(active.getByText("iw_Qm8x…")).toBeInTheDocument();
		expect(active.getByText("active")).toBeInTheDocument();
		expect(active.getByText("42")).toBeInTheDocument();
		expect(active.getByText("3d ago")).toBeInTheDocument();
		expect(active.getByText("in 19d")).toBeInTheDocument();
		expect(active.getByRole("button", { name: /Revoke/ })).toBeInTheDocument();

		// Nothing is fabricated for a token that predates the prefix.
		const legacy = rowOf("legacy-import");
		expect(legacy.querySelector("td")?.textContent).toBe("legacy-import");
		expect(within(legacy).getByText("revoked")).toBeInTheDocument();
		expect(within(legacy).getByText("—")).toBeInTheDocument();
		expect(within(legacy).queryByRole("button")).not.toBeInTheDocument();

		const expired = within(rowOf("staging-trial"));
		expect(expired.getByText("expired")).toBeInTheDocument();
		expect(expired.getByText("never")).toBeInTheDocument();
		expect(expired.getByText("9d ago")).toBeInTheDocument();
	});

	it("shows the empty listing on a fresh install", async () => {
		surface(() => []);
		renderApp("/workloads/tokens");
		expect(
			await screen.findByText(
				"No tokens yet. Mint one to enroll your first workload.",
			),
		).toBeInTheDocument();
	});

	it("revokes only after confirmation", async () => {
		let revoked = false;
		const t = token({ name: "ci-bootstrap" });
		const { calls } = surface(
			() => [
				revoked ? { ...t, state: "revoked", revoked_at: minutesAgo(0) } : t,
			],
			[
				{
					method: "DELETE",
					path: `/api/v1/provisioning-tokens/${t.id}`,
					reply: () => {
						revoked = true;
						return { status: 204 };
					},
				},
			],
		);
		const user = userEvent.setup();
		renderApp("/workloads/tokens");
		await user.click(
			await screen.findByRole("button", { name: "Revoke ci-bootstrap" }),
		);
		const dialog = await screen.findByRole("dialog", {
			name: "Revoke this token?",
		});
		expect(dialog).toHaveTextContent(
			"Workloads it already enrolled keep their credentials and identity.",
		);
		await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
		expect(calls.some((c) => c.method === "DELETE")).toBe(false);

		await user.click(
			screen.getByRole("button", { name: "Revoke ci-bootstrap" }),
		);
		await user.click(
			within(await screen.findByRole("dialog")).getByRole("button", {
				name: "Revoke token",
			}),
		);
		await waitFor(() =>
			expect(within(rowOf("ci-bootstrap")).getByText("revoked")),
		);
		expect(calls.filter((c) => c.method === "DELETE")).toHaveLength(1);
	});
});

describe("mint dialog", () => {
	it("mints with the form's labels and lifetime and shows the secret exactly once", async () => {
		let minted = false;
		const listed = token({
			name: "search-staging-image",
			prefix: "iw_Qm8x",
			labels: { app: "search", env: "staging" },
			use_count: 0,
			last_used_at: null,
		});
		const { calls } = surface(
			() => (minted ? [listed] : []),
			[
				{
					method: "POST",
					path: "/api/v1/provisioning-tokens",
					reply: (body) => {
						minted = true;
						const b = body as { labels: Record<string, string> };
						return {
							status: 201,
							json: {
								...listed,
								labels: b.labels,
								created_at: new Date().toISOString(),
								expires_at: new Date(
									Date.now() + 7 * 24 * 3_600_000,
								).toISOString(),
								token: secret,
							},
						};
					},
				},
			],
		);
		const user = userEvent.setup();
		renderApp("/workloads/tokens");
		await user.click(await screen.findByRole("button", { name: "Mint token" }));
		const form = await screen.findByRole("dialog", {
			name: "Mint a provisioning token",
		});
		await user.type(
			within(form).getByLabelText("Name"),
			"search-staging-image",
		);
		const label = within(form).getByLabelText("Label");
		await user.type(label, "app=search{Enter}env=staging ");
		await user.type(label, "bogus{Enter}");
		expect(
			within(form).getByText("A label is written key=value."),
		).toBeInTheDocument();
		await user.clear(label);
		await user.click(within(form).getByRole("radio", { name: "7 d" }));
		await user.click(within(form).getByRole("button", { name: "Mint token" }));

		const done = await screen.findByRole("dialog", {
			name: "Token minted — copy it now",
		});
		expect(calls.find((c) => c.method === "POST")?.body).toEqual({
			name: "search-staging-image",
			labels: { app: "search", env: "staging" },
			ttl_seconds: 7 * 24 * 3600,
		});
		expect(within(done).getByTestId("minted-secret")).toHaveTextContent(secret);
		expect(done).toHaveTextContent(
			"This is the only time the plaintext is shown.",
		);
		expect(done).toHaveTextContent("--token iw_Qm8x…qM5");
		expect(done).toHaveTextContent(
			"Expires in 7d · assigns app=search env=staging",
		);

		await user.click(within(done).getByRole("button", { name: "Done" }));
		await waitFor(() =>
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
		);
		// The listing that follows carries the prefix and never the secret,
		// and reopening the dialog starts a new form.
		expect(await screen.findByText("search-staging-image")).toBeInTheDocument();
		expect(document.body).not.toHaveTextContent(secret);
		await user.click(screen.getByRole("button", { name: "Mint token" }));
		expect(
			await screen.findByRole("dialog", { name: "Mint a provisioning token" }),
		).toBeInTheDocument();
		expect(document.body).not.toHaveTextContent(secret);
	});

	it("renders a refused mint's findings in the form", async () => {
		surface(
			() => [],
			[
				{
					method: "POST",
					path: "/api/v1/provisioning-tokens",
					reply: {
						status: 400,
						problem: {
							type: "urn:innerwall:problem:validation",
							title: "Validation failed",
							status: 400,
							errors: [
								{
									path: "labels",
									rule: "label-key",
									message: "label key Env is not a valid key",
								},
							],
						},
					},
				},
			],
		);
		const user = userEvent.setup();
		renderApp("/workloads/tokens");
		await user.click(await screen.findByRole("button", { name: "Mint token" }));
		const form = await screen.findByRole("dialog");
		await user.type(within(form).getByLabelText("Label"), "Env=prod{Enter}");
		await user.click(within(form).getByRole("button", { name: "Mint token" }));
		expect(await within(form).findByRole("alert")).toHaveTextContent(
			"label key Env is not a valid key",
		);
	});
});
