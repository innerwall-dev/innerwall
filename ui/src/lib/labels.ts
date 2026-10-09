// The label grammar (ADR-0022), mirrored from the control plane so a
// label input refuses what admission would refuse before anything is
// sent. The control plane stays the authority and refuses the same
// text. A key is 1 to 63 letters, digits, ".", "_", "-", or "/"; a value
// 1 to 63 letters, digits, ".", "_", or "-"; each begins and ends with a
// letter or digit. Nothing in either can be whitespace, "=", or "|", so
// the `key=value` form every surface speaks round-trips.

const maxLength = 63;
const keyPattern = /^[A-Za-z0-9](?:[A-Za-z0-9._/-]*[A-Za-z0-9])?$/;
const valuePattern = /^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$/;

export function validLabelKey(key: string): boolean {
	return key.length <= maxLength && keyPattern.test(key);
}

export function validLabelValue(value: string): boolean {
	return value.length <= maxLength && valuePattern.test(value);
}

// The hint every label input shows when what was typed is not even
// `key=value`.
export const labelFormHint = "A label is written key=value.";

// Requirement is one key with the values it accepts: one for a label, any
// number (ORed) for a selector's requirement.
export interface Requirement {
	key: string;
	values: string[];
}

export type Parsed =
	| { ok: true; requirements: Requirement[] }
	| { ok: false; error: string };

// parseRequirements reads label requirements from typed or pasted text:
// `key=value` requirements separated by whitespace or commas, spaces
// around "=" and "|" tolerated, and, where alternatives are allowed, a
// key's values separated by "|". `app=web env=lab` is two requirements,
// never one label whose value holds a space. Text it cannot read wholly
// as requirements in the grammar is refused with the reason, and nothing
// from it is kept; empty text is no requirements.
export function parseRequirements(
	text: string,
	opts: { alternatives?: boolean } = {},
): Parsed {
	const t = text.trim().replace(/\s*([=|])\s*/g, "$1");
	const requirements: Requirement[] = [];
	for (const token of t.split(/[\s,]+/)) {
		if (token === "") continue;
		const at = token.indexOf("=");
		if (at < 0) return { ok: false, error: labelFormHint };
		const key = token.slice(0, at);
		if (!validLabelKey(key)) {
			return {
				ok: false,
				error: `${shown(key)} is not a label key: letters, digits, ".", "_", "-", or "/", beginning and ending with a letter or digit.`,
			};
		}
		const rest = token.slice(at + 1);
		const values = opts.alternatives ? rest.split("|") : [rest];
		for (const v of values) {
			if (!validLabelValue(v)) {
				return {
					ok: false,
					error:
						v === ""
							? `${key} has no value.`
							: `${shown(v)} is not a label value: letters, digits, ".", "_", or "-", beginning and ending with a letter or digit.`,
				};
			}
		}
		requirements.push({ key, values });
	}
	return { ok: true, requirements };
}

// shownLabelText is a key or value as a chip draws it: as it is when it
// is in the grammar, and quoted otherwise, so a value stored before the
// grammar that holds a space or an "=" can never read as two labels.
export function shownLabelText(text: string, kind: "key" | "value"): string {
	return (kind === "key" ? validLabelKey(text) : validLabelValue(text))
		? text
		: shown(text);
}

// labelText is a label as text, `key=value`, each side as shownLabelText
// draws it.
export function labelText(key: string, value: string): string {
	return `${shownLabelText(key, "key")}=${shownLabelText(value, "value")}`;
}

// labelOutsideGrammar says whether a stored label is one the grammar
// would refuse, which only a label stored before it can be.
export function labelOutsideGrammar(key: string, value: string): boolean {
	return !validLabelKey(key) || !validLabelValue(value);
}

function shown(text: string): string {
	return JSON.stringify(text);
}
