import type { LabelMap } from "@/api/schema";
import { labelPairs } from "@/lib/format";
import { labelOutsideGrammar } from "@/lib/labels";

// The label editor's model: the set being edited against the set as
// read, and what saving it changes. Admission is the control plane's
// (ADR-0022); this only says what differs and what the console's own
// parser already refused.

// LabelChange is one key's change from the set as read to the edit.
export interface LabelChange {
	kind: "added" | "removed" | "changed";
	key: string;
	before?: string;
	after?: string;
}

// labelChanges lists what the edit changes, in key order.
export function labelChanges(before: LabelMap, after: LabelMap): LabelChange[] {
	const keys = [...new Set([...Object.keys(before), ...Object.keys(after)])];
	keys.sort((a, b) => a.localeCompare(b));
	const out: LabelChange[] = [];
	for (const key of keys) {
		const b = before[key];
		const a = after[key];
		if (b === undefined && a !== undefined) {
			out.push({ kind: "added", key, after: a });
		} else if (b !== undefined && a === undefined) {
			out.push({ kind: "removed", key, before: b });
		} else if (b !== a) {
			out.push({ kind: "changed", key, before: b, after: a });
		}
	}
	return out;
}

// withLabels sets each `key=value` requirement on the set: a workload
// carries one value per key, so a new value replaces the old.
export function withLabels(
	set: LabelMap,
	requirements: readonly string[],
): LabelMap {
	const out = { ...set };
	for (const r of requirements) {
		const at = r.indexOf("=");
		out[r.slice(0, at)] = r.slice(at + 1);
	}
	return out;
}

export function withoutLabel(set: LabelMap, key: string): LabelMap {
	const { [key]: _, ...rest } = set;
	return rest;
}

// outsideGrammar lists the labels in a set the grammar refuses, which
// only labels stored before it can be: the control plane refuses a set
// that still holds one, so the edit must remove or replace each.
export function outsideGrammar(set: LabelMap): [string, string][] {
	return labelPairs(set).filter(([k, v]) => labelOutsideGrammar(k, v));
}
