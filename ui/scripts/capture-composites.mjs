#!/usr/bin/env node
// Captures the console's review composites: each screen beside its design
// shot, or, for a state the design has no shot of, the dark theme beside
// the light. Output is docs/img/console/<name>.jpg, 1960 wide and as tall
// as the screen's frame needs (700 for the 1440x900 screens), so they
// compare at a glance.
//
// It drives a running control plane through its own console, logged in
// with the operator password, at 1440x900 and twice the pixel density.
// Two control planes are expected: one serving the seeded fleet and the
// review estate, one serving an empty database for the fresh-install
// states. For example, with a Postgres at $DB:
//
//   innerwall dev seed --database-url $DB/seeded --estate
//   innerwall serve --database-url $DB/seeded --gateway-advertise-address localhost:8443 ...
//   innerwall serve --database-url $DB/fresh --operator-listen :8090 --listen :8453 ...
//   node ui/scripts/capture-composites.mjs --design <package>/project/shots
//
// Options (environment variables in brackets):
//   --design DIR   the design package's shots directory, holding dark/ and
//                  light/ [DESIGN_SHOTS]; required for designed scenes
//   --seeded URL   the seeded control plane [CONSOLE_URL, https://127.0.0.1:8080]
//   --fresh URL    the empty control plane [FRESH_CONSOLE_URL, https://127.0.0.1:8090]
//   --password PW  the operator password on both [CONSOLE_PASSWORD]
//   --out DIR      where composites go [docs/img/console]
//   --only a,b     capture only the scenes whose names start with these
//
// Playwright is resolved from the global install when the console's own
// dependencies do not carry it; it is a review tool, not a dependency.

import { execSync } from "node:child_process";
import { mkdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join, resolve } from "node:path";
import { parseArgs } from "node:util";

const { values: opt } = parseArgs({
	options: {
		design: { type: "string", default: process.env.DESIGN_SHOTS },
		seeded: {
			type: "string",
			default: process.env.CONSOLE_URL ?? "https://127.0.0.1:8080",
		},
		fresh: {
			type: "string",
			default: process.env.FRESH_CONSOLE_URL ?? "https://127.0.0.1:8090",
		},
		password: { type: "string", default: process.env.CONSOLE_PASSWORD },
		out: {
			type: "string",
			default: resolve(import.meta.dirname, "../../docs/img/console"),
		},
		only: { type: "string" },
	},
});

function loadPlaywright() {
	const local = createRequire(import.meta.url);
	try {
		return local("playwright");
	} catch {
		const root = execSync("npm root -g", { encoding: "utf8" }).trim();
		return createRequire(join(root, "noop.js"))("playwright");
	}
}

const themeKey = "innerwall.console.theme";

// A scene is one screen state: where it is, how to reach it, and the
// design shot it answers to (null when the design has none). height is
// the viewport's, when the design's frame is taller than 900.
const metricsScrape = encodeURIComponent(
	"would_block|w:app=metrics-collector env=prod tier=infra|tcp/9100",
);
const billingToLedger = encodeURIComponent(
	"allowed|w:app=billing env=prod tier=api|tcp/8443",
);
// The policy editor's designed refusal: a new row with a prefix that is
// not one and a key without values, saved, so the control plane's
// findings land on the elements they name.
const refusedRow = async (page) => {
	await page.getByRole("button", { name: "+ Add rule" }).click();
	const peer = page.getByRole("textbox", { name: "Add a peer" });
	for (const t of ["10.40.0.0/33", "tier="]) {
		await peer.fill(t);
		await peer.press("Enter");
	}
	const svc = page.getByRole("textbox", { name: "Add a service" });
	for (const t of ["ssh", "tcp/389"]) {
		await svc.fill(t);
		await svc.press("Enter");
	}
	await page.getByRole("button", { name: /^Save — applies/ }).click();
	await page.getByText(/errors? blocks? saving/).waitFor();
};
const openPromote = async (page) => {
	await page.getByRole("button", { name: "Promote to enforced…" }).click();
	await page.waitForSelector('[data-testid="partition"]');
};

const scenes = [
	{
		name: "01-simulation-review-grouped",
		design: "01-simulation-review-grouped",
		cp: "seeded",
		path: `/simulation?ruleset=checkout-inbound&sel=${metricsScrape}`,
		ready: 'aside[aria-label="Pair detail"] table',
	},
	{
		name: "02-simulation-review-matrix",
		design: "02-simulation-review-matrix",
		cp: "seeded",
		path: `/simulation?ruleset=checkout-inbound&take=matrix&sel=${metricsScrape}`,
		ready: 'aside[aria-label="Pair detail"] table',
	},
	{
		name: "03-simulation-review-by-recency",
		design: "03-simulation-review-by-recency",
		cp: "seeded",
		path: `/simulation?ruleset=checkout-inbound&take=recency&sel=${metricsScrape}`,
		ready: 'aside[aria-label="Pair detail"] table',
	},
	{
		name: "04-simulation-review-promote-dialog",
		design: "04-simulation-review-promote-dialog",
		cp: "seeded",
		path: `/simulation?ruleset=checkout-inbound&sel=${metricsScrape}`,
		ready: 'aside[aria-label="Pair detail"] table',
		act: openPromote,
	},
	{
		name: "05-simulation-review-safe-to-enforce",
		design: "05-simulation-review-safe-to-enforce",
		cp: "seeded",
		path: `/simulation?ruleset=ledger-inbound&sel=${billingToLedger}`,
		ready: 'aside[aria-label="Pair detail"] table',
	},
	{
		name: "06-simulation-review-safe-promote-dialog",
		design: "06-simulation-review-safe-promote-dialog",
		cp: "seeded",
		path: `/simulation?ruleset=ledger-inbound&sel=${billingToLedger}`,
		ready: 'aside[aria-label="Pair detail"] table',
		act: openPromote,
	},
	{
		name: "17-fresh-install-simulation-review",
		design: "17-fresh-install-simulation-review",
		cp: "fresh",
		path: "/simulation",
		ready: "text=Nothing to review yet",
	},
	{
		name: "07-flow-map-graph",
		design: "07-flow-map-graph",
		cp: "seeded",
		path: "/map?label=env%3Dprod&sel=e%3Ag%3Ametrics-collector%3Eg%3Acheckout",
		ready: '[data-testid="flow-graph"] .react-flow__edge',
	},
	{
		name: "08-flow-map-matrix",
		design: "08-flow-map-matrix",
		cp: "seeded",
		path: "/map?label=env%3Dprod&take=matrix&sel=e%3Ag%3Ametrics-collector%3Eg%3Acheckout",
		ready: '[data-testid="flow-matrix"]',
	},
	{
		name: "flow-map-node-selected",
		design: null,
		cp: "seeded",
		path: "/map?label=env%3Dprod&sel=n%3Ag%3Acheckout",
		ready: '[data-testid="flow-graph"] .react-flow__edge',
	},
	{
		name: "flow-map-pair-flows",
		design: null,
		cp: "seeded",
		path: "/map?label=env%3Dprod&take=matrix&sel=e%3Aunknown%3Eg%3Aauth",
		ready: '[data-testid="flow-matrix"]',
		act: async (page) => {
			await page.locator('aside button[aria-expanded="false"]').first().click();
			await page.waitForSelector('[data-testid="pair-flows"]');
		},
	},
	{
		name: "flow-map-no-flows-in-range",
		design: null,
		cp: "seeded",
		path: "/map?label=app%3Dsearch&label=tier%3Dinfra&range=1h",
		ready: "text=No flows in this range",
	},
	{
		name: "09-workload-detail-degraded-flows",
		design: "09-workload-detail-degraded-flows",
		cp: "seeded",
		path: async (api) => `/workloads/${await workloadId(api, "db-1")}`,
		ready: '[data-testid="status-card"]',
	},
	{
		name: "13-fleet-workloads",
		design: "13-fleet-workloads-mixed-modes",
		cp: "seeded",
		path: "/workloads",
		ready: "table",
	},
	{
		name: "16-fleet-mint-token-shown-once",
		design: "16-fleet-mint-token-shown-once",
		cp: "seeded",
		path: "/workloads/tokens",
		ready: "table",
		act: async (page) => {
			await page.getByRole("button", { name: "Mint token" }).click();
			const form = page.getByRole("dialog");
			await form.getByLabel("Name").fill("checkout-prod-image");
			await form.getByLabel("Label").fill("app=checkout");
			await form.getByLabel("Label").press("Enter");
			await form.getByLabel("Label").fill("env=prod");
			await form.getByLabel("Label").press("Enter");
			await form.getByRole("radio", { name: "30 d" }).check({ force: true });
			await form.getByRole("button", { name: "Mint token" }).click();
			await page.getByText("Token minted").waitFor();
		},
	},
	{
		name: "12-policy-editor-validation",
		design: "12-policy-editor-validation",
		cp: "seeded",
		path: "/policy?ruleset=checkout-inbound",
		height: 1060,
		ready: "text=matched on",
		act: refusedRow,
	},
	{
		name: "policy-editor-dry-run",
		design: null,
		cp: "seeded",
		path: "/policy?ruleset=checkout-inbound",
		height: 1060,
		ready: "text=matched on",
		act: async (page) => {
			await page.getByRole("button", { name: "Edit metrics-scrape" }).click();
			await page.getByRole("switch", { name: "This rule enabled" }).click();
			await page.getByRole("button", { name: "Dry run" }).click();
			await page.getByTestId("dryrun-summary").waitFor();
			await page
				.getByRole("region", { name: "Dry run" })
				.scrollIntoViewIfNeeded();
		},
	},
	{
		name: "19-fresh-install-policy",
		design: "19-fresh-install-policy",
		cp: "fresh",
		path: "/policy",
		ready: "text=Create your first ruleset",
	},
	{
		name: "fresh-install-policy-new-ruleset",
		design: null,
		cp: "fresh",
		path: "/policy",
		ready: "text=Create your first ruleset",
		act: async (page) => {
			await page
				.getByRole("link", { name: "New ruleset", exact: true })
				.click();
			await page.getByLabel("Ruleset name").fill("checkout-inbound");
			const scope = page.getByLabel("Add a scope requirement");
			await scope.fill("app = checkout");
			await scope.press("Enter");
			await page.getByTestId("match-count").waitFor();
		},
	},
	{
		name: "18-fresh-install-flow-map",
		design: "18-fresh-install-flow-map",
		cp: "fresh",
		path: "/map",
		ready: "text=No flows observed yet",
	},
];

async function workloadId(api, hostname) {
	const res = await api.get("/api/v1/workloads?limit=500");
	const { workloads } = await res.json();
	const w = workloads.find((x) => x.hostname === hostname);
	if (!w) throw new Error(`no workload ${hostname}`);
	return w.id;
}

// sessions holds one logged-in session per control plane: the login
// endpoint throttles repeated attempts, so every shot reuses it.
const sessions = new Map();

async function session(browser, base) {
	if (!sessions.has(base)) {
		const ctx = await browser.newContext({
			baseURL: base,
			ignoreHTTPSErrors: true,
		});
		const login = await ctx.request.post("/api/v1/session", {
			data: { password: opt.password },
		});
		if (!login.ok()) throw new Error(`login at ${base}: ${login.status()}`);
		sessions.set(base, await ctx.storageState());
		await ctx.close();
	}
	return sessions.get(base);
}

async function shoot(browser, scene, theme) {
	const base = scene.cp === "fresh" ? opt.fresh : opt.seeded;
	const ctx = await browser.newContext({
		baseURL: base,
		viewport: { width: 1440, height: scene.height ?? 900 },
		deviceScaleFactor: 2,
		ignoreHTTPSErrors: true,
		colorScheme: theme,
		storageState: await session(browser, base),
	});
	await ctx.addInitScript(
		([k, v]) => localStorage.setItem(k, v),
		[themeKey, theme],
	);
	const page = await ctx.newPage();
	const path =
		typeof scene.path === "function"
			? await scene.path(ctx.request)
			: scene.path;
	await page.goto(path);
	await page.waitForSelector(scene.ready, { timeout: 20_000 });
	if (scene.act) await scene.act(page);
	await page.evaluate(() => document.fonts.ready);
	await page.waitForTimeout(400);
	const png = await page.screenshot({ type: "png" });
	await ctx.close();
	return png;
}

function dataUrl(buf) {
	return `data:image/png;base64,${buf.toString("base64")}`;
}

// composite puts two 1440-wide screens side by side at 960 wide, each
// as tall as its frame (600 for 900).
async function composite(browser, left, right, file, height = 900) {
	const img = Math.round((960 * height) / 1440);
	const ctx = await browser.newContext({
		viewport: { width: 1960, height: img + 100 },
	});
	const page = await ctx.newPage();
	const pane = (img, caption) => `
		<figure><img src="${img.src}"><figcaption>${caption}</figcaption></figure>`;
	await page.setContent(`<!doctype html><style>
		body{margin:0;background:#28282c;display:flex;gap:18px;padding:12px;
			font:13px system-ui,sans-serif;color:#d4d4da}
		figure{margin:0;display:flex;flex-direction:column;gap:8px}
		img{width:960px;height:${img}px;border:1px solid #3c3c42;display:block}
	</style>${pane(left, left.caption)}${pane(right, right.caption)}`);
	await page.evaluate(() =>
		Promise.all([...document.images].map((i) => i.decode())),
	);
	await page.screenshot({ path: file, type: "jpeg", quality: 85 });
	await ctx.close();
}

async function main() {
	if (!opt.password)
		throw new Error("--password (or CONSOLE_PASSWORD) is required");
	mkdirSync(opt.out, { recursive: true });
	const only = opt.only?.split(",") ?? null;
	const chosen = scenes.filter(
		(s) => !only || only.some((o) => s.name.startsWith(o)),
	);
	const { chromium } = loadPlaywright();
	const browser = await chromium.launch();
	try {
		for (const scene of chosen) {
			if (scene.design) {
				if (!opt.design)
					throw new Error("--design is required for designed scenes");
				for (const theme of ["dark", "light"]) {
					const shot = await shoot(browser, scene, theme);
					const design = readFileSync(
						join(opt.design, theme, `${scene.design}.png`),
					);
					const n = scene.design.slice(0, 2);
					const file = join(opt.out, `${scene.name}-${theme}.jpg`);
					await composite(
						browser,
						{ src: dataUrl(shot), caption: `console (${theme})` },
						{ src: dataUrl(design), caption: `design shot ${n} (${theme})` },
						file,
						scene.height,
					);
					console.log(file);
				}
			} else {
				const dark = await shoot(browser, scene, "dark");
				const light = await shoot(browser, scene, "light");
				const file = join(opt.out, `${scene.name}.jpg`);
				await composite(
					browser,
					{ src: dataUrl(dark), caption: "console (dark)" },
					{ src: dataUrl(light), caption: "console (light)" },
					file,
					scene.height,
				);
				console.log(file);
			}
		}
	} finally {
		await browser.close();
	}
}

main().catch((err) => {
	console.error(err.message ?? err);
	process.exit(1);
});
