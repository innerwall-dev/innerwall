import { describe, expect, it } from "vitest";
import {
	labelFormHint,
	labelOutsideGrammar,
	parseRequirements,
	shownLabelText,
	validLabelKey,
	validLabelValue,
} from "./labels";

describe("label grammar", () => {
	it("admits what the control plane admits, and nothing else", () => {
		for (const ok of [
			"web",
			"storefront-api",
			"v1.2_3",
			"A9",
			"x",
			"a".repeat(63),
		]) {
			expect(validLabelKey(ok)).toBe(true);
			expect(validLabelValue(ok)).toBe(true);
		}
		expect(validLabelKey("team/owner")).toBe(true);
		expect(validLabelValue("team/owner")).toBe(false);
		for (const bad of [
			"",
			"a".repeat(64),
			"web env=lab",
			"web env",
			"a=b",
			"a|b",
			" web",
			"web ",
			"-web",
			"web.",
			"wéb",
			'"web"',
		]) {
			expect(validLabelKey(bad)).toBe(false);
			expect(validLabelValue(bad)).toBe(false);
		}
	});
});

describe("parseRequirements", () => {
	it("reads a paste of several labels as several labels, never one", () => {
		expect(parseRequirements("app=web env=lab")).toEqual({
			ok: true,
			requirements: [
				{ key: "app", values: ["web"] },
				{ key: "env", values: ["lab"] },
			],
		});
		expect(parseRequirements(" app = web ,env=lab\n")).toEqual({
			ok: true,
			requirements: [
				{ key: "app", values: ["web"] },
				{ key: "env", values: ["lab"] },
			],
		});
		expect(parseRequirements("   ")).toEqual({ ok: true, requirements: [] });
	});

	it("reads a key's alternatives only where they are allowed", () => {
		expect(
			parseRequirements("app = checkout | payments", { alternatives: true }),
		).toEqual({
			ok: true,
			requirements: [{ key: "app", values: ["checkout", "payments"] }],
		});
		const one = parseRequirements("app=checkout|payments");
		expect(one.ok).toBe(false);
	});

	it("refuses, with the reason, what it cannot read wholly in the grammar", () => {
		expect(parseRequirements("app")).toEqual({
			ok: false,
			error: labelFormHint,
		});
		expect(parseRequirements("app=web env")).toEqual({
			ok: false,
			error: labelFormHint,
		});
		expect(parseRequirements("tier=")).toEqual({
			ok: false,
			error: "tier has no value.",
		});
		const eq = parseRequirements("a=b=c");
		expect(
			eq.ok === false && eq.error.startsWith('"b=c" is not a label value'),
		).toBe(true);
		const key = parseRequirements("-app=web");
		expect(
			key.ok === false && key.error.startsWith('"-app" is not a label key'),
		).toBe(true);
	});
});

describe("showing a stored label", () => {
	it("quotes what is outside the grammar, so it never reads as two labels", () => {
		expect(shownLabelText("web", "value")).toBe("web");
		expect(shownLabelText("web env=lab", "value")).toBe('"web env=lab"');
		expect(shownLabelText("team/owner", "key")).toBe("team/owner");
		expect(shownLabelText("team/owner", "value")).toBe('"team/owner"');
		expect(labelOutsideGrammar("app", "web env=lab")).toBe(true);
		expect(labelOutsideGrammar("app", "web")).toBe(false);
	});
});
