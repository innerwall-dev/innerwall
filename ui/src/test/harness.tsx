import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router";
import { vi } from "vitest";
import { App } from "@/App";
import type { Me, Problem, ProblemTypeURN } from "@/api/schema";

// A scripted operator surface: each entry answers one request by method
// and path, in order of registration, so a test states exactly what the
// control plane says.
export type Reply = {
	status: number;
	json?: unknown;
	problem?: Problem;
	headers?: Record<string, string>;
};

export type Route = {
	method: string;
	path: string;
	reply: Reply | ((body: unknown) => Reply);
};

export function mockSurface(routes: Route[]) {
	const calls: { method: string; path: string; body: unknown }[] = [];
	const fetchMock = vi.fn(
		async (input: RequestInfo | URL, init?: RequestInit) => {
			const url =
				typeof input === "string"
					? input
					: input instanceof URL
						? input.toString()
						: input.url;
			const method = init?.method ?? "GET";
			const body = init?.body ? JSON.parse(String(init.body)) : undefined;
			calls.push({ method, path: url, body });
			const route = routes.find(
				(r) => r.method === method && url.endsWith(r.path),
			);
			if (!route) {
				return new Response(
					JSON.stringify({
						type: "urn:innerwall:problem:not-found",
						title: "Not Found",
						status: 404,
					}),
					{
						status: 404,
						headers: { "Content-Type": "application/problem+json" },
					},
				);
			}
			const reply =
				typeof route.reply === "function" ? route.reply(body) : route.reply;
			const headers: Record<string, string> = { ...(reply.headers ?? {}) };
			let payload: string;
			if (reply.problem) {
				headers["Content-Type"] = "application/problem+json";
				payload = JSON.stringify(reply.problem);
			} else {
				headers["Content-Type"] = "application/json";
				payload = JSON.stringify(reply.json ?? {});
			}
			return new Response(payload, { status: reply.status, headers });
		},
	);
	vi.stubGlobal("fetch", fetchMock);
	return { calls, fetchMock };
}

export const operator: Me = { display_name: "A. Rao", site: "iad1" };

// A problem type named without its URN prefix, from the description's
// closed set.
type ProblemName = ProblemTypeURN extends `urn:innerwall:problem:${infer N}`
	? N
	: never;

export function problem(
	type: ProblemName,
	status: number,
	detail?: string,
): Problem {
	return {
		type: `urn:innerwall:problem:${type}` as ProblemTypeURN,
		title: type,
		status,
		detail,
	};
}

// renderApp mounts the whole console at a path.
export function renderApp(path = "/"): ReturnType<typeof render> {
	return render(
		<MemoryRouter initialEntries={[path]}>
			<App />
		</MemoryRouter>,
	);
}

export function renderAt(
	path: string,
	element: ReactElement,
): ReturnType<typeof render> {
	return render(<MemoryRouter initialEntries={[path]}>{element}</MemoryRouter>);
}
