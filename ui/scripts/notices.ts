// Third-party notices for the built console. The console bundles code and
// font files from its production dependency tree, and each of those
// packages is under its own license: Innerwall's Apache-2.0 covers
// Innerwall's code, not theirs. Most of those licenses require their text
// to travel with every copy, and the embedded console is a copy, so the
// build writes the texts into dist/ beside what they cover, read from the
// installed packages at build time so they can never fall behind the
// lockfile (ADR-0013).

import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import type { Plugin } from "vite";

// NoticeFile is one file the build writes into dist/.
export type NoticeFile = { fileName: string; text: string };

// Dependency is one installed package of the production tree.
export type Dependency = {
	name: string;
	version: string;
	license: string;
	// The package's own license texts, file by file; empty when the
	// package ships none and its manifest's license field is all there is.
	texts: { file: string; text: string }[];
};

// NOTICES is the notices file's path in dist/.
export const NOTICES = "THIRD-PARTY-NOTICES.txt";

const licenseFile = /^(licen[cs]e|copying|notice)(\.[a-z]+)?$/i;

type Manifest = {
	name: string;
	version: string;
	license?: string | { type: string };
	licenses?: { type: string }[];
	dependencies?: Record<string, string>;
	optionalDependencies?: Record<string, string>;
};

function readManifest(dir: string): Manifest {
	return JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
}

function licenseOf(m: Manifest): string {
	if (typeof m.license === "string") return m.license;
	if (m.license?.type) return m.license.type;
	if (m.licenses?.length) return m.licenses.map((l) => l.type).join(" OR ");
	return "UNKNOWN";
}

// resolve finds an installed package the way Node does: in the
// node_modules beside the dependent, then in each directory above it,
// up to the console's root.
function resolve(name: string, from: string, root: string): string | null {
	let dir = from;
	for (;;) {
		const candidate = join(dir, "node_modules", name);
		if (existsSync(join(candidate, "package.json"))) return candidate;
		if (dir === root) return null;
		const up = dirname(dir);
		if (up === dir) return null;
		dir = up;
	}
}

// productionTree walks the console's production dependencies from
// ui/package.json, every installed package once, sorted by name and
// version. Type-only packages (@types/*) carry no code into the bundle
// and are left out; an optional dependency that is not installed is
// skipped.
export function productionTree(root: string): Dependency[] {
	const seen = new Map<string, Dependency>();
	const visit = (from: string, deps: Record<string, string>) => {
		for (const name of Object.keys(deps)) {
			if (name.startsWith("@types/")) continue;
			const dir = resolve(name, from, root);
			if (!dir) continue;
			const m = readManifest(dir);
			const key = `${m.name}@${m.version}`;
			if (seen.has(key)) continue;
			const texts = readdirSync(dir)
				.filter((f) => licenseFile.test(f))
				.sort()
				.map((f) => ({
					file: f,
					text: readFileSync(join(dir, f), "utf8").trimEnd(),
				}));
			seen.set(key, {
				name: m.name,
				version: m.version,
				license: licenseOf(m),
				texts,
			});
			visit(dir, { ...m.dependencies, ...m.optionalDependencies });
		}
	};
	visit(root, readManifest(root).dependencies ?? {});
	return [...seen.values()].sort((a, b) =>
		a.name === b.name
			? a.version.localeCompare(b.version)
			: a.name.localeCompare(b.name),
	);
}

// fontLicenseName is where a font package's license goes: beside the
// font files, which Vite writes to dist/assets/.
export function fontLicenseName(pkg: string): string {
	return `assets/${pkg.replace(/^@[^/]+\//, "")}-LICENSE.txt`;
}

const rule = "=".repeat(72);

// noticeFiles is every file the build writes: the notices file at the
// console's root, naming each package with its license and that license's
// text, and, for every package under the SIL Open Font License, its
// license again beside the font files it covers.
export function noticeFiles(root: string): NoticeFile[] {
	const tree = productionTree(root);
	const fonts = tree.filter((d) => d.license === "OFL-1.1");
	const head = [
		"Third-party software in the Innerwall console",
		"",
		"Innerwall's own code is licensed under Apache-2.0 (LICENSE in the",
		"source repository). The built console also contains code and font",
		"files from the packages below, the production dependency tree of",
		"the console, each under its own license, reproduced here as the",
		"package ships it.",
		"",
		"The font files in assets/ are licensed under the SIL Open Font",
		"License 1.1, not Apache-2.0. Each font family's license is also",
		"beside the fonts:",
		...fonts.map(
			(d) => `  ${fontLicenseName(d.name)} (${d.name} ${d.version})`,
		),
		"",
	];
	const body = tree.flatMap((d) => [
		rule,
		`${d.name} ${d.version}`,
		`License: ${d.license}`,
		rule,
		...(d.texts.length
			? d.texts.flatMap((t) => [t.text, ""])
			: [
					`The package ships no license file; its manifest declares ${d.license}.`,
					"",
				]),
	]);
	const files: NoticeFile[] = [
		{ fileName: NOTICES, text: `${[...head, ...body].join("\n")}\n` },
	];
	for (const d of fonts) {
		files.push({
			fileName: fontLicenseName(d.name),
			text: `${d.texts.map((t) => t.text).join("\n\n")}\n`,
		});
	}
	return files;
}

// thirdPartyNotices is the Vite plugin that writes the notice files into
// the production build.
export function thirdPartyNotices(root: string): Plugin {
	return {
		name: "innerwall-third-party-notices",
		apply: "build",
		generateBundle() {
			for (const f of noticeFiles(root)) {
				this.emitFile({ type: "asset", fileName: f.fileName, source: f.text });
			}
		},
	};
}
