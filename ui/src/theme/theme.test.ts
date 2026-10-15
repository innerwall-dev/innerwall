import { readFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { resolveTheme, storageKey } from "./ThemeProvider";

// The pre-paint script is the one inline script in index.html. It and
// the provider must open a load in the same theme, or the first paint
// shows one theme and the app the other.
const html = readFileSync(join(process.cwd(), "index.html"), "utf8");
const prePaint = (() => {
	const m = html.match(/<script>([\s\S]*?)<\/script>/);
	if (!m) throw new Error("index.html carries no inline script");
	return m[1];
})();

function arrange(system: "dark" | "light", stored: string | null | Error) {
	vi.stubGlobal("matchMedia", (q: string) => ({
		matches: q === "(prefers-color-scheme: dark)" && system === "dark",
	}));
	vi.spyOn(localStorage, "getItem").mockImplementation((k) => {
		if (stored instanceof Error) throw stored;
		return k === storageKey ? stored : null;
	});
	const meta = document.createElement("meta");
	meta.name = "color-scheme";
	meta.content = "dark light";
	document.head.append(meta);
	return meta;
}

afterEach(() => {
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
	for (const m of document.head.querySelectorAll("meta")) m.remove();
});

describe("pre-paint theme", () => {
	it("reads the provider's storage key", () => {
		expect(prePaint).toContain(`"${storageKey}"`);
	});

	const stored = [
		null,
		"dark",
		"light",
		"sepia",
		new Error("storage refused"),
	] as const;
	for (const system of ["dark", "light"] as const) {
		for (const s of stored) {
			const name = s instanceof Error ? "a refusing store" : String(s);
			it(`agrees with the provider: system ${system}, stored ${name}`, () => {
				const meta = arrange(system, s);
				new Function(prePaint)();
				const want = resolveTheme();
				const expected =
					s === "dark" || s === "light" ? s : (system as "dark" | "light");
				expect(want).toBe(expected);
				const root = document.documentElement;
				expect(root.dataset.theme).toBe(want);
				expect(root.classList.contains("dark")).toBe(want === "dark");
				expect(meta.content).toBe(want);
			});
		}
	}
});
