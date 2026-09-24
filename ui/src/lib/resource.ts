import { type DependencyList, useCallback, useEffect, useState } from "react";
import { ProblemError } from "@/api/client";
import { ProblemType } from "@/api/schema";
import { useSession } from "@/auth/SessionProvider";

// Resource is one read as a screen renders it: loading, the problem the
// surface answered with, or the data.
export type Resource<T> =
	| { status: "loading" }
	| { status: "error"; error: ProblemError }
	| { status: "ready"; data: T };

// asProblem turns anything a request threw into the one shape the
// screens render.
export function asProblem(err: unknown): ProblemError {
	if (err instanceof ProblemError) return err;
	return new ProblemError({
		type: ProblemType.internal,
		title: "Request failed",
		status: 0,
		detail: err instanceof Error ? err.message : String(err),
	});
}

// useResource runs a read when its dependencies change and on reload.
// A refusal that says the session is gone ends it, which returns the
// console to the login screen; every other problem is the screen's to
// render. A read superseded by a newer one is dropped.
export function useResource<T>(
	load: () => Promise<T>,
	deps: DependencyList,
): { resource: Resource<T>; reload: () => void } {
	const { expire } = useSession();
	const [resource, setResource] = useState<Resource<T>>({ status: "loading" });
	const [generation, setGeneration] = useState(0);

	// biome-ignore lint/correctness/useExhaustiveDependencies: the caller's deps name what the load reads
	useEffect(() => {
		let current = true;
		setResource((r) => (r.status === "ready" ? r : { status: "loading" }));
		load().then(
			(data) => {
				if (current) setResource({ status: "ready", data });
			},
			(err) => {
				if (!current) return;
				const problem = asProblem(err);
				if (problem.type === ProblemType.unauthenticated) {
					expire();
					return;
				}
				setResource({ status: "error", error: problem });
			},
		);
		return () => {
			current = false;
		};
	}, [...deps, generation]);

	const reload = useCallback(() => setGeneration((g) => g + 1), []);
	return { resource, reload };
}

// useWrite wraps a write so that a refusal meaning the
// session is gone ends it, like a read's would, and anything else is
// rethrown as a problem for the caller to render.
export function useWrite(): <T>(write: () => Promise<T>) => Promise<T> {
	const { expire } = useSession();
	return useCallback(
		async <T>(write: () => Promise<T>) => {
			try {
				return await write();
			} catch (err) {
				const problem = asProblem(err);
				if (problem.type === ProblemType.unauthenticated) expire();
				throw problem;
			}
		},
		[expire],
	);
}
