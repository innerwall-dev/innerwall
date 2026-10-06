import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
	fontLicenseName,
	NOTICES,
	noticeFiles,
	productionTree,
} from "./notices";

// The console root: the tests run under a browser-like environment, where
// import.meta.url is not a file URL.
const root = resolve(__dirname, "..");

describe("third-party notices", () => {
	const files = noticeFiles(root);
	const notices = files.find((f) => f.fileName === NOTICES)?.text ?? "";
	const manifest = JSON.parse(readFileSync(`${root}/package.json`, "utf8"));

	it("names every production dependency with its license", () => {
		for (const name of Object.keys(manifest.dependencies)) {
			expect(notices).toContain(`\n${name} `);
		}
		for (const d of productionTree(root)) {
			expect(notices).toContain(
				`${d.name} ${d.version}\nLicense: ${d.license}`,
			);
		}
	});

	it("carries the full text of each font family's license beside the fonts", () => {
		const fonts = Object.keys(manifest.dependencies).filter((n) =>
			n.startsWith("@fontsource/"),
		);
		expect(fonts.length).toBeGreaterThan(0);
		for (const name of fonts) {
			const shipped = readFileSync(
				`${root}/node_modules/${name}/LICENSE`,
				"utf8",
			).trimEnd();
			const beside = files.find((f) => f.fileName === fontLicenseName(name));
			expect(beside?.fileName.startsWith("assets/")).toBe(true);
			expect(beside?.text).toContain(shipped);
			expect(beside?.text).toContain("SIL OPEN FONT LICENSE Version 1.1");
			expect(notices).toContain(shipped);
		}
	});

	it("leaves type-only packages out", () => {
		expect(productionTree(root).some((d) => d.name.startsWith("@types/"))).toBe(
			false,
		);
	});
});
