import { listRulesets, previewSelector } from "@/api/policy";
import type { Ruleset } from "@/api/schema";

// A ruleset whose scope selects some of a set of workloads, with how many
// workloads its scope selects now: where an "open in policy editor"
// that starts from traffic into those workloads lands.
export interface EditorTarget {
	ruleset: Ruleset;
	inScope: number;
}

// rulesetsSelecting resolves, on the control plane, which rulesets'
// scopes select any of the given workloads: each scope is previewed
// through the renderer's own match, and the console only joins the
// matched ids with its own. Enabled rulesets come first, then by name.
export async function rulesetsSelecting(
	workloadIds: readonly string[],
): Promise<EditorTarget[]> {
	const ids = new Set(workloadIds);
	const { rulesets } = await listRulesets();
	const previews = await Promise.all(
		rulesets.map((rs) =>
			Object.keys(rs.scope).length === 0
				? Promise.resolve(null)
				: previewSelector(rs.scope),
		),
	);
	const out: EditorTarget[] = [];
	rulesets.forEach((rs, i) => {
		const p = previews[i];
		if (p?.matched.some((w) => ids.has(w.id))) {
			out.push({ ruleset: rs, inScope: p.count });
		}
	});
	return out.sort(
		(a, b) =>
			Number(b.ruleset.enabled !== false) -
				Number(a.ruleset.enabled !== false) ||
			a.ruleset.name.localeCompare(b.ruleset.name),
	);
}
