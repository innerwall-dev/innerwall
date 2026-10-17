import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type {
	AddressGroup,
	Finding,
	RuleInput,
	Ruleset,
	Service,
} from "@/api/schema";
import { minutesAgo } from "@/test/fixtures";
import {
	mockSurface,
	problem,
	type Route,
	renderApp,
	signedIn,
} from "@/test/harness";
import { addressGroup, rollupOf } from "@/test/map";
import { rule, ruleset } from "@/test/review";

// The definitions rules reference, listed and written in the console:
// the first-ruleset guide's estate (a db tier, its ruleset, a service
// and an address group it needs), as a console-only operator meets it.

const service = (id: string, name: string, ports: string[]): Service => ({
	id,
	name,
	entries: [{ protocol: "tcp", ports }],
	version: "1",
	created_at: minutesAgo(600),
	updated_at: minutesAgo(600),
});

const ssh = service("5f000000-0000-4000-8000-0000000000aa", "ssh", ["22"]);
const vpn = addressGroup("6f000000-0000-4000-8000-0000000000bb", "corp-vpn", [
	"10.40.0.0/16",
]);
const sshRule = rule({
	description: "Operator SSH via bastion.",
	services: [ssh.id],
	entries: undefined,
});
const vpnRule = rule({
	description: "Engineers on VPN.",
	peers: [{ address_group: vpn.id }],
	services: [ssh.id],
	entries: undefined,
	enabled: false,
});
const web = ruleset("web-inbound", { role: ["web"] }, [sshRule, vpnRule]);

interface Estate {
	rulesets: Ruleset[];
	services: Service[];
	groups: AddressGroup[];
}

// surface is a control plane holding the estate, which its writes
// change; a definition's write is admitted the way the control plane
// admits one (a name, at least one member, names distinct), and a rule
// added to the ruleset rulesOf resolves its references by name against
// what is defined when it lands.
function surface(estate: Estate, extra: Route[] = [], rulesOf?: string) {
	let serial = 0;
	const id = (prefix: string) => {
		serial += 1;
		return `${prefix}000000-0000-4000-8000-${String(serial).padStart(12, "0")}`;
	};
	const refused = (errors: Finding[]) => ({
		status: 400,
		problem: { ...problem("validation", 400), errors },
	});
	// A rule's write, admitted against what is defined when it lands.
	const writeRule = (rsId: string): Route => ({
		method: "POST",
		path: `/api/v1/rulesets/${rsId}/rules`,
		reply: (body) => {
			const b = body as RuleInput;
			const errors: Finding[] = [];
			(b.services ?? []).forEach((ref, i) => {
				if (!estate.services.some((x) => x.id === ref || x.name === ref)) {
					errors.push({
						path: `services[${i}]`,
						rule: "service-unknown",
						message: `rule references a service that does not exist (${ref})`,
					});
				}
			});
			b.peers.forEach((p, i) => {
				const g = p.address_group;
				if (
					g !== undefined &&
					!estate.groups.some((x) => x.id === g || x.name === g)
				) {
					errors.push({
						path: `peers[${i}]`,
						rule: "address-group-unknown",
						message: `rule references an address group that does not exist (${g})`,
					});
				}
			});
			if (errors.length > 0) return refused(errors);
			const saved = rule({
				...b,
				id: id("5e"),
				version: "1",
				services: (b.services ?? []).map(
					(ref) => estate.services.find((x) => x.name === ref)?.id ?? ref,
				),
				peers: b.peers.map((p) =>
					p.address_group === undefined
						? p
						: {
								address_group:
									estate.groups.find((x) => x.name === p.address_group)?.id ??
									p.address_group,
							},
				),
			});
			estate.rulesets = estate.rulesets.map((rs) =>
				rs.id === rsId ? { ...rs, rules: [...rs.rules, saved] } : rs,
			);
			return { status: 201, json: saved };
		},
	});
	const s = mockSurface([
		...extra,
		...(rulesOf ? [writeRule(rulesOf)] : []),
		signedIn,
		{
			method: "GET",
			path: "/api/v1/rulesets",
			reply: () => ({
				status: 200,
				json: { rulesets: estate.rulesets, state_version: `sv-${serial}` },
			}),
		},
		{
			method: "GET",
			path: "/api/v1/services",
			reply: () => ({ status: 200, json: { services: estate.services } }),
		},
		{
			method: "GET",
			path: "/api/v1/address-groups",
			reply: () => ({
				status: 200,
				json: { address_groups: estate.groups },
			}),
		},
		{
			method: "POST",
			path: "/api/v1/services",
			reply: (body) => {
				const b = body as { name: string; entries: Service["entries"] };
				if (b.entries.length === 0) {
					return refused([
						{
							path: "entries",
							rule: "entries-required",
							message: "service defines no entries",
						},
					]);
				}
				if (estate.services.some((x) => x.name === b.name)) {
					return { status: 409, problem: problem("duplicate-name", 409) };
				}
				const created = { ...service(id("5f"), b.name, []), ...b };
				estate.services = [...estate.services, created];
				return { status: 201, json: created };
			},
		},
		{
			method: "POST",
			path: "/api/v1/address-groups",
			reply: (body) => {
				const b = body as { name: string; cidrs: string[] };
				// The one fault this estate's tests make: an octet past 255.
				const bad = b.cidrs.findIndex((c) => c.includes("999"));
				if (bad >= 0) {
					return refused([
						{
							path: `cidrs[${bad}]`,
							rule: "cidr",
							message: `address is not a valid CIDR (${b.cidrs[bad]})`,
						},
					]);
				}
				const created = { ...addressGroup(id("6f"), b.name, []), ...b };
				estate.groups = [...estate.groups, created];
				return { status: 201, json: created };
			},
		},
		{
			method: "POST",
			path: "/api/v1/selectors/preview",
			reply: { status: 200, json: { matched: [], count: 2 } },
		},
		{
			method: "GET",
			path: "/api/v1/workloads",
			reply: { status: 200, json: { workloads: [], next_cursor: null } },
		},
		{
			method: "GET",
			path: "/api/v1/flows/rollup",
			reply: { status: 200, json: { ...rollupOf([]), group_by: ["rule"] } },
		},
	]);
	return s;
}

describe("service definitions", () => {
	it("opens from the rail and lists each service, what it expands to, and what references it", async () => {
		surface({ rulesets: [web], services: [ssh], groups: [vpn] });
		const user = userEvent.setup();
		renderApp("/policy");
		await screen.findByRole("heading", { name: "web-inbound" });
		const defs = screen.getByRole("navigation", { name: "Definitions" });
		await user.click(
			within(defs).getByRole("link", { name: /Service definitions/ }),
		);
		expect(
			await screen.findByRole("heading", { name: "Service definitions" }),
		).toBeInTheDocument();
		expect(
			within(defs).getByRole("link", { name: /Service definitions/ }),
		).toHaveAttribute("aria-current", "page");
		// No ruleset is the current one while a definitions list is shown.
		expect(
			within(screen.getByRole("navigation", { name: "Ruleset list" }))
				.getByRole("link", { name: /web-inbound/ })
				.getAttribute("aria-current"),
		).toBeNull();
		expect(
			screen.getByRole("navigation", { name: "Breadcrumb" }),
		).toHaveTextContent("Policy/Service definitions");
		const r = within(
			screen.getByRole("table", { name: "Service definitions" }),
		).getByRole("row", { name: "ssh" });
		expect(within(r).getByTestId("chip")).toHaveTextContent("TCP22");
		const refs = within(r).getByTestId("references");
		expect(refs).toHaveTextContent(
			"web-inbound· Operator SSH via bastion.web-inbound· Engineers on VPN. (disabled)",
		);
		expect(
			within(refs).getAllByRole("link", { name: "web-inbound" })[0],
		).toHaveAttribute("href", "/policy?ruleset=web-inbound");
	});

	it("says what each reference stands for where a rule names it", async () => {
		surface({ rulesets: [web], services: [ssh], groups: [vpn] });
		renderApp("/policy?ruleset=web-inbound");
		const table = await screen.findByRole("table", { name: "Rules" });
		const chips = within(table).getAllByTestId("chip");
		expect(chips.find((c) => c.textContent === "REFssh")).toHaveAttribute(
			"title",
			"tcp/22",
		);
		expect(
			chips.find((c) => c.textContent === "GROUPcorp-vpn"),
		).toHaveAttribute("title", "10.40.0.0/16");
	});

	it("creates a service through admission, its findings on what they name", async () => {
		const estate = { rulesets: [web], services: [ssh], groups: [] };
		const { calls } = surface(estate);
		const user = userEvent.setup();
		renderApp("/policy/services");
		await user.click(
			await screen.findByRole("button", { name: "New service" }),
		);
		const dialog = await screen.findByRole("dialog", { name: "New service" });
		// No entries: the control plane's refusal, under the list.
		await user.type(within(dialog).getByLabelText("Name"), "ssh");
		await user.click(
			within(dialog).getByRole("button", { name: "Create service" }),
		);
		expect(await within(dialog).findByTestId("finding")).toHaveTextContent(
			"service defines no entries",
		);
		// Text that is no entry is refused before anything is sent.
		const add = within(dialog).getByRole("textbox", { name: "Add an entry" });
		await user.type(add, "postgres{Enter}");
		expect(dialog).toHaveTextContent(
			"an entry is a protocol with its ports: tcp/5432, udp/53, tcp/6000-6010, or icmp (postgres)",
		);
		await user.clear(add);
		// The command line's form reads too; a name taken is refused on it.
		await user.type(add, "tcp:5432{Enter}");
		await user.click(
			within(dialog).getByRole("button", { name: "Create service" }),
		);
		expect(await within(dialog).findByTestId("finding")).toHaveTextContent(
			"Another service is already named ssh; nothing was created.",
		);
		const name = within(dialog).getByLabelText("Name");
		await user.clear(name);
		await user.type(name, "postgres");
		await user.click(
			within(dialog).getByRole("button", { name: "Create service" }),
		);
		await waitFor(() =>
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
		);
		const posts = calls.filter(
			(c) => c.method === "POST" && c.path === "/api/v1/services",
		);
		expect(posts.at(-1)?.body).toEqual({
			name: "postgres",
			entries: [{ protocol: "tcp", ports: ["5432"] }],
		});
		expect(screen.getByRole("status")).toHaveTextContent(
			"Created service postgres (tcp/5432). A rule can reference it by name now; no workload's policy changes until one does.",
		);
		const r = await within(
			screen.getByRole("table", { name: "Service definitions" }),
		).findByRole("row", { name: "postgres" });
		expect(within(r).getByTestId("references")).toHaveTextContent(
			"No rule references it.",
		);
		expect(
			screen.getByRole("navigation", { name: "Definitions" }),
		).toHaveTextContent("Service definitions2");
	});

	it("edits a service conditioned on its version, and keeps the edit across a conflict", async () => {
		const estate = { rulesets: [web], services: [ssh], groups: [vpn] };
		const puts: unknown[] = [];
		const { fetchMock } = surface(estate, [
			{
				method: "PUT",
				path: `/api/v1/services/${ssh.id}`,
				reply: (body) => {
					puts.push(body);
					if (puts.length === 1) {
						// Someone else saved it meanwhile.
						estate.services = [{ ...ssh, version: "2" }];
						return {
							status: 412,
							problem: {
								...problem("precondition-failed", 412),
								current_version: "2",
							},
						};
					}
					const saved = { ...ssh, ...(body as object), version: "3" };
					estate.services = [saved];
					return { status: 200, json: saved };
				},
			},
		]);
		const user = userEvent.setup();
		renderApp("/policy/services");
		await user.click(await screen.findByRole("button", { name: "Edit ssh" }));
		const dialog = await screen.findByRole("dialog", {
			name: "Edit service ssh",
		});
		expect(dialog).toHaveTextContent(
			"Referenced by 2 rules: saving changes what they admit, at once, on the workloads their rulesets scope.",
		);
		await user.type(
			within(dialog).getByRole("textbox", { name: "Add an entry" }),
			"tcp/2222{Enter}",
		);
		await user.click(
			within(dialog).getByRole("button", { name: "Save service" }),
		);
		expect(await within(dialog).findByRole("alert")).toHaveTextContent(
			"ssh changed since this dialog opened · it is now at version 2; nothing was changed. Your edit is kept",
		);
		await user.click(
			within(dialog).getByRole("button", { name: "Save service" }),
		);
		await waitFor(() =>
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
		);
		const sent = fetchMock.mock.calls
			.filter(([, init]) => init?.method === "PUT")
			.map(
				([, init]) =>
					((init as RequestInit).headers as Record<string, string>)["If-Match"],
			);
		// The second write is conditioned on the version read after the
		// conflict, and carries the edit made before it.
		expect(sent).toEqual(['"1"', '"2"']);
		expect(puts[1]).toEqual({
			name: "ssh",
			entries: [
				{ protocol: "tcp", ports: ["22"] },
				{ protocol: "tcp", ports: ["2222"] },
			],
		});
		expect(screen.getByRole("status")).toHaveTextContent(
			"Saved ssh (tcp/22, tcp/2222) at version 3. The 2 rules referencing it change at once",
		);
	});
});

describe("address groups", () => {
	it("lists each group's CIDRs and creates one from pasted CIDRs, a bad one refused on its chip", async () => {
		const estate = { rulesets: [web], services: [ssh], groups: [vpn] };
		const { calls } = surface(estate);
		const user = userEvent.setup();
		renderApp("/policy/address-groups");
		const table = await screen.findByRole("table", { name: "Address groups" });
		const r = within(table).getByRole("row", { name: "corp-vpn" });
		expect(within(r).getByTestId("chip")).toHaveTextContent("CIDR10.40.0.0/16");
		expect(within(r).getByTestId("references")).toHaveTextContent(
			"web-inbound· Engineers on VPN. (disabled)",
		);
		await user.click(screen.getByRole("button", { name: "New address group" }));
		const dialog = await screen.findByRole("dialog", {
			name: "New address group",
		});
		await user.type(within(dialog).getByLabelText("Name"), "corp");
		await user.type(
			within(dialog).getByRole("textbox", { name: "Add a CIDR" }),
			"10.0.0.0/8, 10.0.0.999/8{Enter}",
		);
		await user.click(
			within(dialog).getByRole("button", { name: "Create address group" }),
		);
		expect(await within(dialog).findByTestId("finding")).toHaveTextContent(
			"address is not a valid CIDR (10.0.0.999/8)",
		);
		const chips = within(dialog).getAllByTestId("chip");
		expect(chips.map((c) => c.getAttribute("data-failed"))).toEqual([
			null,
			"true",
		]);
		await user.click(
			within(dialog).getByRole("button", { name: "Remove cidr 10.0.0.999/8" }),
		);
		await user.click(
			within(dialog).getByRole("button", { name: "Create address group" }),
		);
		await waitFor(() =>
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
		);
		expect(
			calls.filter(
				(c) => c.method === "POST" && c.path === "/api/v1/address-groups",
			)[1]?.body,
		).toEqual({ name: "corp", cidrs: ["10.0.0.0/8"] });
		expect(
			await within(table).findByRole("row", { name: "corp" }),
		).toBeInTheDocument();
	});
});

describe("the first ruleset, console only", () => {
	it("authors db-inbound end to end, creating the service and the group its rules name from the refusals", async () => {
		const db = ruleset("db-inbound", { role: ["db"] });
		const estate: Estate = { rulesets: [db], services: [], groups: [] };
		const s = surface(estate, [], db.id);
		const user = userEvent.setup();
		renderApp("/policy?ruleset=db-inbound");
		await screen.findByRole("table", { name: "Rules" });

		// The api tier on postgres: postgres is not defined yet.
		await user.click(screen.getByRole("button", { name: "Add rule" }));
		let editing = await screen.findByRole("row", { name: "New rule" });
		await user.type(
			within(editing).getByRole("textbox", { name: "Add a peer" }),
			"role=api{Enter}",
		);
		await user.type(
			within(editing).getByRole("textbox", { name: "Add a service" }),
			"postgres{Enter}",
		);
		const save = () =>
			screen.getByRole("button", { name: /^Save — applies to 2 workloads/ });
		await waitFor(() => expect(save()).toBeEnabled());
		await user.click(save());
		expect(await within(editing).findByTestId("finding")).toHaveTextContent(
			"rule references a service that does not exist (postgres)",
		);
		expect(save()).toBeDisabled();
		// The finding names the way forward, and the row is kept meanwhile.
		await user.click(
			within(editing).getByRole("button", { name: "Create service postgres…" }),
		);
		const dialog = await screen.findByRole("dialog", { name: "New service" });
		expect(within(dialog).getByLabelText("Name")).toHaveValue("postgres");
		await user.type(
			within(dialog).getByRole("textbox", { name: "Add an entry" }),
			"tcp/5432{Enter}",
		);
		await user.click(
			within(dialog).getByRole("button", { name: "Create service" }),
		);
		await waitFor(() =>
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
		);
		editing = screen.getByRole("row", { name: "New rule" });
		expect(within(editing).queryByTestId("finding")).not.toBeInTheDocument();
		expect(screen.getByTestId("notice")).toHaveTextContent(
			"Created service postgres. The row references it; save the rule to write it.",
		);
		// Once the listing has it, the reference says what it stands for.
		await waitFor(() =>
			expect(
				within(editing)
					.getAllByTestId("chip")
					.find((c) => c.textContent?.startsWith("REF")),
			).toHaveAttribute("title", "tcp/5432"),
		);
		await waitFor(() => expect(save()).toBeEnabled());
		await user.click(save());
		await waitFor(() =>
			expect(
				screen.queryByRole("row", { name: "New rule" }),
			).not.toBeInTheDocument(),
		);

		// The corp network on ssh: corp is not defined yet either.
		await user.click(screen.getByRole("button", { name: "Add rule" }));
		editing = await screen.findByRole("row", { name: "New rule" });
		await user.type(
			within(editing).getByRole("textbox", { name: "Add a peer" }),
			"corp{Enter}",
		);
		await user.type(
			within(editing).getByRole("textbox", { name: "Add a service" }),
			"tcp/22{Enter}",
		);
		await user.click(save());
		await user.click(
			await within(editing).findByRole("button", {
				name: "Create address group corp…",
			}),
		);
		const group = await screen.findByRole("dialog", {
			name: "New address group",
		});
		await user.type(
			within(group).getByRole("textbox", { name: "Add a CIDR" }),
			"10.0.0.0/8{Enter}",
		);
		await user.click(
			within(group).getByRole("button", { name: "Create address group" }),
		);
		await waitFor(() =>
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
		);
		await waitFor(() => expect(save()).toBeEnabled());
		await user.click(save());
		await waitFor(() =>
			expect(
				screen.queryByRole("row", { name: "New rule" }),
			).not.toBeInTheDocument(),
		);

		// The writes the guide's command lines make, made from the console.
		expect(
			s.calls
				.filter((c) => c.method === "POST")
				.filter((c) => !c.path.includes("/selectors/"))
				.map((c) => [c.path, c.body]),
		).toEqual([
			[
				`/api/v1/rulesets/${db.id}/rules`,
				expect.objectContaining({ services: ["postgres"] }),
			],
			[
				"/api/v1/services",
				{ name: "postgres", entries: [{ protocol: "tcp", ports: ["5432"] }] },
			],
			[
				`/api/v1/rulesets/${db.id}/rules`,
				expect.objectContaining({
					peers: [{ workloads: { role: ["api"] } }],
					services: ["postgres"],
				}),
			],
			[
				`/api/v1/rulesets/${db.id}/rules`,
				expect.objectContaining({ peers: [{ address_group: "corp" }] }),
			],
			["/api/v1/address-groups", { name: "corp", cidrs: ["10.0.0.0/8"] }],
			[
				`/api/v1/rulesets/${db.id}/rules`,
				expect.objectContaining({
					peers: [{ address_group: "corp" }],
					entries: [{ protocol: "tcp", ports: ["22"] }],
				}),
			],
		]);
		const rules = within(screen.getByRole("table", { name: "Rules" }));
		expect(rules.getAllByRole("row")).toHaveLength(3);
		expect(
			screen.getByRole("navigation", { name: "Definitions" }),
		).toHaveTextContent("Service definitions1Address groups1");
	});
});
