import { type Problem, ProblemType } from "./schema";

// The surface is mounted under one prefix on the console's own origin
// (ADR-0021): every request is same-origin, carries the session cookie,
// and passes the listener's origin guard without a token scheme.
export const apiPrefix = "/api/v1";

// ProblemError is a failed request as the console handles it: the problem
// document the surface returned, and the retry interval a throttled login
// was told to wait.
export class ProblemError extends Error {
	readonly problem: Problem;
	readonly retryAfterSeconds: number | undefined;

	constructor(problem: Problem, retryAfterSeconds?: number) {
		super(problem.detail ?? problem.title);
		this.name = "ProblemError";
		this.problem = problem;
		this.retryAfterSeconds = retryAfterSeconds;
	}

	get type(): string {
		return this.problem.type;
	}
}

// A response that is not a problem document (a proxy's error page, a
// dropped connection) is reported as an internal problem so the screens
// have one shape to render.
function synthesize(status: number, detail: string): Problem {
	return {
		type: ProblemType.internal,
		title: "Request failed",
		status,
		detail,
	};
}

async function toError(res: Response): Promise<ProblemError> {
	const contentType = res.headers.get("content-type") ?? "";
	const retryAfter = res.headers.get("retry-after");
	const retryAfterSeconds = retryAfter
		? Number.parseInt(retryAfter, 10) || undefined
		: undefined;
	if (contentType.startsWith("application/problem+json")) {
		try {
			const problem = (await res.json()) as Problem;
			if (typeof problem.type === "string") {
				return new ProblemError(problem, retryAfterSeconds);
			}
		} catch {
			// fall through to the synthesized problem
		}
	}
	return new ProblemError(
		synthesize(res.status, `${res.status} ${res.statusText}`.trim()),
	);
}

// Query is a read's parameters; a repeated parameter (a label filter's
// several requirements) is an array, and an absent value is left out.
export type Query = Record<
	string,
	string | number | readonly string[] | null | undefined
>;

// withQuery appends the parameters to a path in the order given.
export function withQuery(path: string, query?: Query): string {
	const params = new URLSearchParams();
	for (const [key, value] of Object.entries(query ?? {})) {
		if (value === undefined || value === null || value === "") continue;
		if (Array.isArray(value)) {
			for (const v of value) params.append(key, v);
		} else {
			params.append(key, String(value));
		}
	}
	const qs = params.toString();
	return qs ? `${path}?${qs}` : path;
}

// request performs one call against the surface and returns its JSON
// body (nothing for a 204), or throws a ProblemError.
export async function request<T>(
	method: string,
	path: string,
	body?: unknown,
): Promise<T> {
	const headers: Record<string, string> = { Accept: "application/json" };
	if (body !== undefined) {
		headers["Content-Type"] = "application/json";
	}
	let res: Response;
	try {
		res = await fetch(apiPrefix + path, {
			method,
			headers,
			body: body === undefined ? undefined : JSON.stringify(body),
			credentials: "same-origin",
			cache: "no-store",
		});
	} catch (err) {
		throw new ProblemError(
			synthesize(
				0,
				err instanceof Error
					? err.message
					: "the control plane could not be reached",
			),
		);
	}
	if (!res.ok) {
		throw await toError(res);
	}
	if (res.status === 204) {
		return undefined as T;
	}
	return (await res.json()) as T;
}
