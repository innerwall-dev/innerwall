import { type Finding, ProblemType, type Selector } from "@/api/schema";
import { useDebounced } from "@/lib/debounce";
import { type Resource, useResource } from "@/lib/resource";
import { loadScope } from "./data";
import type { ScopeMatch } from "./model";

// The pause after typing before a scope is matched again.
export const liveDelayMs = 300;

const canonical = (sel: Selector) =>
	JSON.stringify(
		Object.keys(sel)
			.sort()
			.map((k) => [k, sel[k]]),
	);

// useScopeMatch is what a scope matches, read live: again whenever the
// scope changes and has held still, never once per keystroke. A scope
// the control plane refuses to resolve (empty, or a key without values)
// answers with its findings. A null scope matches nothing and reads
// nothing.
export function useScopeMatch(
	scope: Selector | null,
	delayMs = liveDelayMs,
): { match: Resource<ScopeMatch> | null; findings: Finding[] } {
	const key = scope ? canonical(scope) : null;
	const held = useDebounced(key, delayMs);
	const { resource } = useResource(
		async () =>
			held === null
				? null
				: {
						key: held,
						match: await loadScope(
							Object.fromEntries(JSON.parse(held) as [string, string[]][]),
						),
					},
		[held],
	);
	if (key === null) return { match: null, findings: [] };
	// A read keeps its last answer while the next is in flight; an answer
	// for another scope is not this one's.
	if (
		held !== key ||
		resource.status === "loading" ||
		(resource.status === "ready" && resource.data?.key !== key)
	) {
		return { match: { status: "loading" }, findings: [] };
	}
	if (resource.status === "error") {
		const p = resource.error.problem;
		return {
			match: resource,
			findings: p.type === ProblemType.validation ? (p.errors ?? []) : [],
		};
	}
	return resource.data === null
		? { match: null, findings: [] }
		: { match: { status: "ready", data: resource.data.match }, findings: [] };
}
