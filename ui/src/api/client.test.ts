import { describe, expect, it, vi } from "vitest";
import { ProblemError, request } from "./client";
import { ProblemType } from "./types";

describe("request", () => {
	it("throws the surface's problem document with its retry interval", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn(
				async () =>
					new Response(
						JSON.stringify({
							type: ProblemType.tooManyAttempts,
							title: "Too many",
							status: 429,
						}),
						{
							status: 429,
							headers: {
								"Content-Type": "application/problem+json",
								"Retry-After": "12",
							},
						},
					),
			),
		);
		const err = await request("POST", "/session", { password: "x" }).catch(
			(e: unknown) => e,
		);
		expect(err).toBeInstanceOf(ProblemError);
		expect((err as ProblemError).type).toBe(ProblemType.tooManyAttempts);
		expect((err as ProblemError).retryAfterSeconds).toBe(12);
	});

	it("synthesizes an internal problem for anything that is not a problem document", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn(
				async () =>
					new Response("<html>bad gateway</html>", {
						status: 502,
						statusText: "Bad Gateway",
					}),
			),
		);
		const err = (await request("GET", "/me").catch(
			(e: unknown) => e,
		)) as ProblemError;
		expect(err.type).toBe(ProblemType.internal);
		expect(err.problem.status).toBe(502);
	});

	it("reports a dropped connection the same way", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn(async () => {
				throw new TypeError("Failed to fetch");
			}),
		);
		const err = (await request("GET", "/me").catch(
			(e: unknown) => e,
		)) as ProblemError;
		expect(err.type).toBe(ProblemType.internal);
		expect(err.message).toContain("Failed to fetch");
	});

	it("sends JSON same-origin under the prefix", async () => {
		const fetchMock = vi.fn(
			async () =>
				new Response("{}", {
					status: 200,
					headers: { "Content-Type": "application/json" },
				}),
		);
		vi.stubGlobal("fetch", fetchMock);
		await request("DELETE", "/session");
		expect(fetchMock).toHaveBeenCalledWith(
			"/api/v1/session",
			expect.objectContaining({ method: "DELETE", credentials: "same-origin" }),
		);
	});
});
