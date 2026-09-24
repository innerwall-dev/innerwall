import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { mockSurface, operator, problem, renderApp } from "@/test/harness";

const anonymous = {
	method: "GET",
	path: "/api/v1/me",
	reply: { status: 401, problem: problem("unauthenticated", 401) },
};

async function typeAndSubmit(password: string) {
	const user = userEvent.setup();
	await user.type(await screen.findByLabelText("Operator password"), password);
	await user.click(screen.getByRole("button", { name: "Sign in" }));
}

describe("login", () => {
	it("sends an anonymous session to the login screen and back to where it was going", async () => {
		const { calls } = mockSurface([
			anonymous,
			{
				method: "POST",
				path: "/api/v1/session",
				reply: { status: 200, json: operator },
			},
		]);
		renderApp("/policy");
		await typeAndSubmit("correct horse");
		expect(
			await screen.findByRole("heading", { name: "Create your first ruleset" }),
		).toBeInTheDocument();
		const post = calls.find((c) => c.method === "POST");
		expect(post?.body).toEqual({ password: "correct horse" });
	});

	it("renders a wrong password inline and keeps the form", async () => {
		mockSurface([
			anonymous,
			{
				method: "POST",
				path: "/api/v1/session",
				reply: {
					status: 401,
					problem: problem(
						"invalid-credentials",
						401,
						"the password is not correct",
					),
				},
			},
		]);
		renderApp("/login");
		await typeAndSubmit("nope");
		expect(await screen.findByRole("alert")).toHaveTextContent(
			"That password is not correct.",
		);
		expect(screen.getByLabelText("Operator password")).toHaveAttribute(
			"aria-invalid",
			"true",
		);
		expect(screen.getByLabelText("Operator password")).toHaveValue("");
	});

	it("renders the throttle with its retry interval and holds the button", async () => {
		mockSurface([
			anonymous,
			{
				method: "POST",
				path: "/api/v1/session",
				reply: {
					status: 429,
					problem: problem("too-many-attempts", 429),
					headers: { "Retry-After": "37" },
				},
			},
		]);
		renderApp("/login");
		await typeAndSubmit("again");
		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Too many attempts. Try again in 37s.",
		);
		await userEvent.type(screen.getByLabelText("Operator password"), "x");
		expect(screen.getByRole("button", { name: "Sign in" })).toBeDisabled();
	});

	it("renders the fresh-install instruction for the no-password problem, with no setup form", async () => {
		mockSurface([
			anonymous,
			{
				method: "POST",
				path: "/api/v1/session",
				reply: { status: 403, problem: problem("no-password", 403) },
			},
		]);
		renderApp("/login");
		await typeAndSubmit("anything");
		const block = await screen.findByTestId("fresh-install");
		expect(block).toHaveTextContent("No operator password has been set");
		expect(block).toHaveTextContent("innerwall operator set-password");
		expect(
			screen.queryByLabelText("Operator password"),
		).not.toBeInTheDocument();
		expect(screen.queryByLabelText(/new password/i)).not.toBeInTheDocument();
		// The way back is to try again once the command has run.
		await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
		expect(
			await screen.findByLabelText("Operator password"),
		).toBeInTheDocument();
	});

	it("renders a non-problem failure as the surface's own words", async () => {
		mockSurface([
			anonymous,
			{
				method: "POST",
				path: "/api/v1/session",
				reply: { status: 502, json: {} },
			},
		]);
		renderApp("/login");
		await typeAndSubmit("x");
		await waitFor(() =>
			expect(screen.getByRole("alert")).toHaveTextContent("502"),
		);
	});

	it("does not show the login screen to an authenticated session", async () => {
		mockSurface([
			{
				method: "GET",
				path: "/api/v1/me",
				reply: { status: 200, json: operator },
			},
		]);
		renderApp("/login");
		expect(
			await screen.findByRole("heading", { name: "Nothing to review yet" }),
		).toBeInTheDocument();
	});
});
