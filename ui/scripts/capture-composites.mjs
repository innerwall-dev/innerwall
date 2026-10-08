#!/usr/bin/env node
// Captures the console's review composites: every screen state in the
// dark theme beside the same state in the light, captured in the same
// interaction. The running console is its own reference; the design
// canvas stays outside the repository. Output is
// docs/img/console/<name>.jpg, 1960 wide and as tall as the screen's
// frame needs (700 for the 1440x900 screens), so they compare at a
// glance.
//
// It drives running control planes through their own console, logged
// in with the operator password, at 1440x900 and twice the pixel
// density. Four are expected: one serving the seeded fleet and the
// review estate; one serving an empty database for the fresh-install
// states; one whose login the run may throttle, for the login screen's
// failure states (a separate process, because the throttle is held in
// process; any database with the operator password set); and one whose
// database has no operator password, for the login screen's
// fresh-install state. For example, with a Postgres at $DB:
//
//   innerwall dev seed --database-url $DB/seeded --estate
//   innerwall serve --database-url $DB/seeded --gateway-advertise-address localhost:8443 ...
//   innerwall serve --database-url $DB/fresh --operator-listen :8090 --listen :8453 ...
//   innerwall serve --database-url $DB/fresh --operator-listen :8100 --listen :8463 ...
//   innerwall serve --database-url $DB/unset --operator-listen :8110 --listen :8473 ...
//   node ui/scripts/capture-composites.mjs --password ...
//
// The login failure scenes spend the throttling control plane's login
// attempts: after a run it refuses logins for a minute.
//
// Options (environment variables in brackets):
//   --seeded URL   the seeded control plane [CONSOLE_URL, https://127.0.0.1:8080]
//   --fresh URL    the empty control plane [FRESH_CONSOLE_URL, https://127.0.0.1:8090]
//   --login URL    the control plane the login failure scenes may throttle
//                  [LOGIN_CONSOLE_URL, https://127.0.0.1:8100]
//   --unset URL    a control plane with no operator password
//                  [UNSET_CONSOLE_URL, https://127.0.0.1:8110]
//   --password PW  the operator password [CONSOLE_PASSWORD]
//   --out DIR      where composites go [docs/img/console]
//   --only a,b     capture only the scenes whose names start with these
//
// Playwright is resolved from the global install when the console's own
// dependencies do not carry it; it is a review tool, not a dependency.

import { execSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { createRequire } from "node:module";
import { join, resolve } from "node:path";
import { parseArgs } from "node:util";

const { values: opt } = parseArgs({
	options: {
		seeded: {
			type: "string",
			default: process.env.CONSOLE_URL ?? "https://127.0.0.1:8080",
		},
		fresh: {
			type: "string",
			default: process.env.FRESH_CONSOLE_URL ?? "https://127.0.0.1:8090",
		},
		login: {
			type: "string",
			default: process.env.LOGIN_CONSOLE_URL ?? "https://127.0.0.1:8100",
		},
		unset: {
			type: "string",
			default: process.env.UNSET_CONSOLE_URL ?? "https://127.0.0.1:8110",
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

// A scene is one screen state: where it is and how to reach it. cp names
// the control plane it is captured on; an anonymous scene is captured
// signed out. height is the viewport's, when the screen's frame is
// taller than 900.
const metricsScrape = encodeURIComponent(
	"would_block|w:app=metrics-collector env=prod tier=infra|tcp/9100",
);
const billingToLedger = encodeURIComponent(
	"allowed|w:app=billing env=prod tier=api|tcp/8443",
);
// The policy editor's refusal: a new row with a prefix that is
// not one and a key without values, saved, so the control plane's
// findings land on the elements they name.
const refusedRow = async (page) => {
	await page.getByRole("button", { name: "Add rule" }).click();
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
		cp: "seeded",
		path: `/simulation?ruleset=checkout-inbound&sel=${metricsScrape}`,
		ready: 'aside[aria-label="Pair detail"] table',
	},
	{
		name: "02-simulation-review-matrix",
		cp: "seeded",
		path: `/simulation?ruleset=checkout-inbound&take=matrix&sel=${metricsScrape}`,
		ready: 'aside[aria-label="Pair detail"] table',
	},
	{
		name: "03-simulation-review-by-recency",
		cp: "seeded",
		path: `/simulation?ruleset=checkout-inbound&take=recency&sel=${metricsScrape}`,
		ready: 'aside[aria-label="Pair detail"] table',
	},
	{
		name: "04-simulation-review-promote-dialog",
		cp: "seeded",
		path: `/simulation?ruleset=checkout-inbound&sel=${metricsScrape}`,
		ready: 'aside[aria-label="Pair detail"] table',
		act: openPromote,
	},
	{
		name: "05-simulation-review-safe-to-enforce",
		cp: "seeded",
		path: `/simulation?ruleset=ledger-inbound&sel=${billingToLedger}`,
		ready: 'aside[aria-label="Pair detail"] table',
	},
	{
		name: "06-simulation-review-safe-promote-dialog",
		cp: "seeded",
		path: `/simulation?ruleset=ledger-inbound&sel=${billingToLedger}`,
		ready: 'aside[aria-label="Pair detail"] table',
		act: openPromote,
	},
	// The fleet's db-1 lost evidence inside the windows its scope is
	// judged over, so the review of that scope fails on it alone.
	{
		name: "simulation-review-evidence-gaps",
		cp: "seeded",
		path: "/simulation?ruleset=web-to-db",
		ready: 'section[aria-label="Verdict"] li',
	},
	{
		name: "simulation-review-evidence-gaps-promote-dialog",
		cp: "seeded",
		path: "/simulation?ruleset=web-to-db",
		ready: 'section[aria-label="Verdict"] li',
		act: openPromote,
	},
	{
		name: "17-fresh-install-simulation-review",
		cp: "fresh",
		path: "/simulation",
		ready: "text=Nothing to review yet",
	},
	{
		name: "07-flow-map-graph",
		cp: "seeded",
		path: "/map?label=env%3Dprod&sel=e%3Ag%3Ametrics-collector%3Eg%3Acheckout",
		ready: '[data-testid="flow-graph"] .react-flow__edge',
	},
	{
		name: "08-flow-map-matrix",
		cp: "seeded",
		path: "/map?label=env%3Dprod&take=matrix&sel=e%3Ag%3Ametrics-collector%3Eg%3Acheckout",
		ready: '[data-testid="flow-matrix"]',
	},
	{
		name: "flow-map-node-selected",
		cp: "seeded",
		path: "/map?label=env%3Dprod&sel=n%3Ag%3Acheckout",
		ready: '[data-testid="flow-graph"] .react-flow__edge',
	},
	{
		name: "flow-map-pair-flows",
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
		cp: "seeded",
		path: "/map?label=app%3Dsearch&label=tier%3Dinfra&range=1h",
		ready: "text=No flows in this range",
	},
	{
		name: "09-workload-detail-degraded-flows",
		cp: "seeded",
		path: async (api) => `/workloads/${await workloadId(api, "db-1")}`,
		ready: '[data-testid="status-card"]',
	},
	{
		name: "10-workload-detail-listening-services",
		cp: "seeded",
		path: async (api) => `/workloads/${await workloadId(api, "db-1")}/services`,
		ready: "text=What is listening on this host",
	},
	{
		name: "11-workload-detail-applied-policy",
		cp: "seeded",
		path: async (api) => `/workloads/${await workloadId(api, "db-1")}/policy`,
		ready: "text=Rendered policy",
	},
	// The fleet's cache-1 is offline: a directed resend is refused with
	// when its agent was last heard from, and nothing is sent.
	{
		name: "workload-detail-offline-resend-refused",
		cp: "seeded",
		path: async (api) => `/workloads/${await workloadId(api, "cache-1")}`,
		ready: '[data-testid="status-card"]',
		act: async (page) => {
			await page.getByRole("button", { name: "Resend snapshot" }).click();
			await page
				.getByText(/The agent is offline, so nothing was sent/)
				.waitFor();
		},
	},
	{
		name: "workload-detail-evidence-gaps",
		cp: "seeded",
		path: async (api) => `/workloads/${await workloadId(api, "db-1")}`,
		ready: '[data-testid="evidence-gaps"]',
	},
	{
		name: "13-fleet-workloads",
		cp: "seeded",
		path: "/workloads",
		ready: "table",
	},
	{
		name: "14-fleet-provisioning-tokens",
		cp: "seeded",
		path: "/workloads/tokens",
		ready: "table",
	},
	{
		name: "15-fleet-mint-token-form",
		cp: "seeded",
		path: "/workloads/tokens",
		ready: "table",
		act: async (page) => {
			await fillMintForm(page);
		},
	},
	{
		name: "fleet-revoke-token-confirmation",
		cp: "seeded",
		path: "/workloads/tokens",
		ready: "table",
		act: async (page) => {
			await page.getByRole("button", { name: "Revoke prod" }).click();
			await page.getByRole("dialog").waitFor();
		},
	},
	{
		name: "16-fleet-mint-token-shown-once",
		cp: "seeded",
		path: "/workloads/tokens",
		ready: "table",
		act: async (page) => {
			await showSyntheticSecret(page);
			const form = await fillMintForm(page);
			await form.getByRole("button", { name: "Mint token" }).click();
			await page.getByText("Token minted").waitFor();
		},
	},
	{
		name: "12-policy-editor-validation",
		cp: "seeded",
		path: "/policy?ruleset=checkout-inbound",
		height: 1060,
		ready: "text=matched on",
		act: refusedRow,
	},
	{
		name: "policy-editor-dry-run",
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
		cp: "fresh",
		path: "/policy",
		ready: "text=Create your first ruleset",
	},
	{
		name: "fresh-install-policy-new-ruleset",
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
		cp: "fresh",
		path: "/map",
		ready: "text=No flows observed yet",
	},
	{
		name: "20-fresh-install-workloads",
		cp: "fresh",
		path: "/workloads",
		ready: "text=No workloads enrolled",
	},
	{
		name: "21-account-popover-theme-toggle",
		cp: "fresh",
		path: "/simulation",
		ready: "text=Nothing to review yet",
		act: async (page) => {
			await page.getByRole("button", { name: "Account" }).click();
			await page.getByRole("dialog", { name: "Account" }).waitFor();
		},
	},
	{
		name: "fleet-change-mode-for-selected",
		cp: "seeded",
		path: "/workloads",
		ready: "table",
		act: async (page) => {
			for (const host of ["checkout-prod-07", "db-1"]) {
				await page.getByRole("checkbox", { name: `Select ${host}` }).check();
			}
			await page
				.getByRole("button", { name: "Change mode for selected…" })
				.click();
			const dialog = page.getByRole("dialog");
			await dialog.getByRole("radio", { name: /Enforced/ }).check({
				force: true,
			});
		},
	},
	// The login screen, signed out: as it opens, refused a wrong password,
	// throttled, and on a control plane with no operator password.
	{
		name: "login",
		cp: "login",
		anonymous: true,
		path: "/login",
		ready: "text=Sign in",
	},
	{
		name: "login-wrong-password",
		cp: "login",
		anonymous: true,
		path: "/login",
		ready: "text=Sign in",
		act: async (page) => {
			await signIn(page, "not-the-operator-password");
			await page.getByRole("alert").waitFor();
		},
	},
	{
		name: "login-throttled",
		cp: "login",
		anonymous: true,
		path: "/login",
		ready: "text=Sign in",
		act: async (page) => {
			// Spend the window's attempts, then the console's own attempt
			// is the one the throttle refuses.
			for (let i = 0; i < 12; i++) {
				const r = await page.request.post("/api/v1/session", {
					data: { password: "not-the-operator-password" },
				});
				if (r.status() === 429) break;
			}
			await signIn(page, "not-the-operator-password");
			await page.getByText(/Too many attempts/).waitFor();
		},
	},
	{
		name: "login-fresh-install",
		cp: "unset",
		anonymous: true,
		path: "/login",
		ready: "text=Sign in",
		act: async (page) => {
			await signIn(page, "any-password-at-all");
			await page.getByText("No operator password has been set").waitFor();
		},
	},
];

async function signIn(page, password) {
	await page.getByLabel(/password/i).fill(password);
	await page.getByRole("button", { name: "Sign in" }).click();
}

// fillMintForm fills the mint dialog the way the shown-once scene does,
// short of minting.
async function fillMintForm(page) {
	await page.getByRole("button", { name: "Mint token" }).click();
	const form = page.getByRole("dialog");
	await form.getByLabel("Name").fill("checkout-prod-image");
	await form.getByLabel("Label").fill("app=checkout");
	await form.getByLabel("Label").press("Enter");
	await form.getByLabel("Label").fill("env=prod");
	await form.getByLabel("Label").press("Enter");
	await form.getByRole("radio", { name: "30 d" }).check({ force: true });
	return form;
}

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

// syntheticSecret is what the shown-once dialog displays in a capture. A
// composite is published in the repository, so it must never picture a
// real secret: the mint is real (the control plane stores its digest and
// the listing shows its non-secret prefix), but its response is rewritten
// in the browser before the console renders it, so the plaintext on screen
// is plainly not a credential.
const syntheticSecret = "iw_EXAMPLE-synthetic-not-a-token";

async function showSyntheticSecret(page) {
	await page.route("**/api/v1/provisioning-tokens", async (route) => {
		if (route.request().method() !== "POST") return route.fallback();
		const response = await route.fetch();
		const body = await response.json();
		if (typeof body.token !== "string")
			throw new Error("mint response carries no token to replace");
		await route.fulfill({
			response,
			json: { ...body, token: syntheticSecret },
		});
	});
}

// assertNoRealSecret refuses to save a capture whose shown-once dialog
// holds anything but the synthetic value.
async function assertNoRealSecret(page) {
	const shown = page.getByTestId("minted-secret");
	if ((await shown.count()) === 0) return;
	const text = (await shown.innerText()).trim();
	if (text !== syntheticSecret)
		throw new Error(
			"the shown-once dialog holds a real secret; refusing to capture it",
		);
}

async function shoot(browser, scene, theme) {
	const base = opt[scene.cp];
	const ctx = await browser.newContext({
		baseURL: base,
		viewport: { width: 1440, height: scene.height ?? 900 },
		deviceScaleFactor: 2,
		ignoreHTTPSErrors: true,
		colorScheme: theme,
		storageState: scene.anonymous ? undefined : await session(browser, base),
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
	await assertNoRealSecret(page);
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
	} finally {
		await browser.close();
	}
}

main().catch((err) => {
	console.error(err.message ?? err);
	process.exit(1);
});
