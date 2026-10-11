import type { Ruleset, Workload } from "@/api/schema";
import { labelPairs } from "@/lib/format";
import { labelText } from "@/lib/labels";

// The palette's search: a case-insensitive substring of a workload's
// hostname or of one of its labels as `key=value`, or of a ruleset's
// name. Nothing is ranked or fuzzed; what matches is listed in the order
// the reads returned it (the fleet's order, the listing's order).

export interface Hit {
	kind: "workload" | "ruleset";
	key: string;
	title: string;
	// For a workload matched by a label, the label that matched.
	detail?: string;
	to: string;
}

export interface Hits {
	workloads: Hit[];
	rulesets: Hit[];
}

export function search(
	query: string,
	workloads: readonly Workload[],
	rulesets: readonly Ruleset[],
	rulesetPath: (name: string) => string,
): Hits {
	const q = query.trim().toLowerCase();
	if (q === "") return { workloads: [], rulesets: [] };
	const hits: Hits = { workloads: [], rulesets: [] };
	for (const w of workloads) {
		const byName = w.hostname.toLowerCase().includes(q);
		const label = byName
			? undefined
			: labelPairs(w.labels)
					.map(([k, v]) => labelText(k, v))
					.find((l) => l.toLowerCase().includes(q));
		if (byName || label) {
			hits.workloads.push({
				kind: "workload",
				key: `w:${w.id}`,
				title: w.hostname,
				detail: label,
				to: `/workloads/${w.id}`,
			});
		}
	}
	for (const rs of rulesets) {
		if (rs.name.toLowerCase().includes(q)) {
			hits.rulesets.push({
				kind: "ruleset",
				key: `r:${rs.id ?? rs.name}`,
				title: rs.name,
				to: rulesetPath(rs.name),
			});
		}
	}
	return hits;
}
