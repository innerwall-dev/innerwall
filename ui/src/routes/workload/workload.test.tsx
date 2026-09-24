import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Flow, RenderedPolicy, Workload } from "@/api/schema";
import { minutesAgo, workload } from "@/test/fixtures";
import {
	mockSurface,
	problem,
	type Route,
	renderApp,
	signedIn,
} from "@/test/harness";

const db = workload({
	hostname: "db-1",
	labels: { env: "prod", role: "db" },
	mode: "simulation",
	listening_services: [
		{
			protocol: "tcp",
			port: 5432,
			process_name: "postgres",
			process_path: "/usr/lib/postgresql/16/bin/postgres",
		},
		{
			protocol: "tcp",
			port: 9100,
			process_name: "node_exporter",
			process_path: "/usr/local/bin/node_exporter",
		},
		{
			protocol: "udp",
			port: 5353,
			process_name: "avahi-daemon",
			process_path: "/usr/sbin/avahi-daemon",
		},
	],
	sync: {
		state: "degraded",
		applied_version: 41,
		latest_version: 42,
		latest_rendered_at: minutesAgo(4),
		error:
			"nft: Error: Could not process rule: set innerwall_peers_r7 exceeds element limit (4096)",
		last_snapshot_sent_at: null,
	},
	health: {
		dropped_flow_records: 42,
		credential: {
			state: "renewal-failed",
			last_error: "renewal refused: authority unreachable",
		},
	},
});

const rule = "aa5b70e7-40c0-4c80-b418-d369e7f5a628";
const policy: RenderedPolicy = {
	workload: { id: db.id, hostname: db.hostname, labels: db.labels },
	version: 42,
	mode: "simulation",
	rendered_at: minutesAgo(4),
	terminal_verdict: "would_block",
	rules: [
		{
			id: `${rule}/tcp`,
			authored_rule_id: rule,
			ruleset: {
				id: "7fbebf19-b959-4f42-8194-b63c5db61219",
				name: "web-to-db",
			},
			description: "postgres from web",
			created_at: minutesAgo(3 * 24 * 60),
			updated_at: minutesAgo(2 * 60),
			protocol: "tcp",
			ports: [{ start: 5432, end: 5432 }],
			peer_cidrs: ["10.0.0.10/32", "10.0.0.11/32"],
			verdict: "allowed",
		},
		{
			id: "0d9c11aa-0000-4000-8000-000000000001/tcp",
			authored_rule_id: "0d9c11aa-0000-4000-8000-000000000001",
			ruleset: null,
			description: "",
			created_at: null,
			updated_at: null,
			protocol: "tcp",
			ports: [],
			peer_cidrs: ["10.40.0.0/16"],
			verdict: "allowed",
		},
	],
};

let flowId = 0;
function flow(overrides: Partial<Flow>): Flow {
	flowId += 1;
	return {
		id: flowId,
		window_start: minutesAgo(65),
		window_end: minutesAgo(60),
		peer: {
			kind: "workload",
			workload_id: "01a0d588-f03f-7c14-933a-b0f6bed93770",
			name: "web-1",
			labels: { role: "web" },
		},
		src_address: "10.0.0.10",
		dst_address: "10.0.0.20",
		service: { protocol: "tcp", port: 5432 },
		direction: "inbound",
		verdict: "allowed",
		rule: { id: `${rule}/tcp`, authored_rule_id: rule, protocol: "tcp" },
		connection_count: 22910,
		byte_count: 1000,
		first_seen: minutesAgo(65),
		last_seen: minutesAgo(1),
		process_name: "postgres",
		...overrides,
	};
}

const allowed = flow({});
const office = flow({
	verdict: "would_block",
	rule: null,
	src_address: "192.0.2.7",
	peer: {
		kind: "address_group",
		address_group_id: "dae0e910-3b78-4071-8980-60d76ff60010",
		name: "office",
		labels: {},
	},
	connection_count: 14,
});
const stranger = flow({
	verdict: "would_block",
	rule: null,
	src_address: "198.51.100.7",
	peer: { kind: "unknown", address: "198.51.100.7", labels: {} },
	service: { protocol: "tcp", port: 22 },
	process_name: undefined,
	connection_count: 9,
});

function rollup(
	groups: {
		service?: { protocol: "tcp" | "udp" | "icmp"; port: number };
		conns: number;
	}[],
	flowCount: number,
) {
	return {
		from: minutesAgo(14 * 24 * 60),
		to: minutesAgo(0),
		effective_from: null,
		effective_to: null,
		group_by: ["dst", "service"],
		groups: groups.map((g) => ({
			keys: { service: g.service },
			flow_count: 1,
			connection_count: g.conns,
			byte_count: 0,
			first_seen: minutesAgo(60),
			last_seen: minutesAgo(1),
		})),
		group_count: groups.length,
		truncated: false,
		totals: { flow_count: flowCount, connection_count: 0, byte_count: 0 },
	};
}

function surface(
	opts: {
		workload?: () => Workload;
		flows?: (q: URLSearchParams) => {
			flows: Flow[];
			next_cursor: string | null;
		};
		extra?: Route[];
	} = {},
) {
	return mockSurface([
		signedIn,
		{
			method: "GET",
			path: "/api/v1/workloads",
			reply: { status: 200, json: { workloads: [db], next_cursor: null } },
		},
		{
			method: "GET",
			path: "/api/v1/rulesets",
			reply: { status: 200, json: { rulesets: [], state_version: "0" } },
		},
		{
			method: "GET",
			path: `/api/v1/workloads/${db.id}`,
			reply: () => ({ status: 200, json: opts.workload?.() ?? db }),
		},
		{
			method: "GET",
			path: `/api/v1/workloads/${db.id}/rendered-policy`,
			reply: { status: 200, json: policy },
		},
		{
			method: "GET",
			path: "/api/v1/flows",
			reply: (_b, q) => ({
				status: 200,
				json: {
					workload: { id: db.id, hostname: "db-1", labels: db.labels },
					from: q.get("from"),
					to: minutesAgo(0),
					...(opts.flows?.(q) ?? {
						flows: [allowed, office, stranger],
						next_cursor: null,
					}),
				},
			}),
		},
		{
			method: "GET",
			path: "/api/v1/flows/rollup",
			reply: (_b, q) => {
				const v = q.get("verdict");
				if (q.get("group_by") === "dst,service") {
					return {
						status: 200,
						json:
							v === "would_block"
								? rollup(
										[{ service: { protocol: "tcp", port: 9100 }, conns: 1190 }],
										1,
									)
								: rollup(
										[
											{
												service: { protocol: "tcp", port: 5432 },
												conns: 435_120,
											},
											{ service: { protocol: "tcp", port: 9100 }, conns: 1190 },
										],
										3,
									),
					};
				}
				const counts: Record<string, number> = {
					allowed: 4,
					would_block: 2,
					observed: 1,
					blocked: 0,
				};
				return {
					status: 200,
					json: rollup([], v ? (counts[v] ?? 0) : 7),
				};
			},
		},
		...(opts.extra ?? []),
	]);
}

describe("workload detail", () => {
	it("leads with the degraded state: versions, the apply error, and the health it carries", async () => {
		surface();
		renderApp(`/workloads/${db.id}`);
		const card = await screen.findByTestId("status-card");
		expect(card).toHaveTextContent("Degraded — last apply failed");
		expect(card).toHaveTextContent("appliedv41");
		expect(card).toHaveTextContent("renderedv42 (not applied)");
		expect(card).toHaveTextContent("rendered at4m ago");
		expect(card).toHaveTextContent("last snapshotnone recorded");
		expect(card).toHaveTextContent(
			"set innerwall_peers_r7 exceeds element limit (4096)",
		);
		expect(card).toHaveTextContent("Host stays on v41 (its last good policy).");

		expect(screen.getByRole("heading", { name: "db-1" })).toBeInTheDocument();
		expect(screen.getByText(db.id)).toBeInTheDocument();
		expect(screen.getByText("renewal failed · retrying")).toBeInTheDocument();
		expect(
			screen.getByText("renewal refused: authority unreachable"),
		).toBeInTheDocument();
		expect(screen.getByText("42")).toBeInTheDocument();
		expect(
			screen.getByText(/the flows shown are incomplete/),
		).toBeInTheDocument();
		expect(
			screen.getByText("debian 13 · linux 6.12 · amd64"),
		).toBeInTheDocument();

		// The frame names the workload and keeps it one click away.
		expect(
			screen.getByRole("navigation", { name: "Breadcrumb" }),
		).toHaveTextContent("Workloads/db-1");
		const nav = screen.getByRole("navigation", { name: "Sections" });
		expect(within(nav).getByRole("link", { name: /db-1/ })).toHaveTextContent(
			"▲",
		);
	});

	it("lists flow windows by the required workload filter, with counts per decision", async () => {
		const { calls } = surface();
		const user = userEvent.setup();
		renderApp(`/workloads/${db.id}`);
		await screen.findByText("192.0.2.7");

		expect(
			screen.getByRole("tab", { name: /Inbound flows/ }),
		).toHaveTextContent("Inbound flows7");
		expect(
			screen.getByRole("button", { name: /^all\s*\d*$/ }),
		).toHaveTextContent("all7");
		expect(screen.getByRole("button", { name: /allowed/ })).toHaveTextContent(
			"allowed4",
		);
		expect(
			screen.getByRole("button", { name: /would block/ }),
		).toHaveTextContent("would block2");
		expect(
			screen.queryByRole("button", { name: /blocked 0/ }),
		).not.toBeInTheDocument();

		const first = screen.getByText("10.0.0.10").closest("tr") as HTMLElement;
		expect(within(first).getByText("web-1 · role=web")).toBeInTheDocument();
		expect(within(first).getByText("postgres from web")).toBeInTheDocument();
		expect(within(first).getByText("aa5b70e7/tcp")).toBeInTheDocument();
		expect(within(first).getByText("22,910")).toBeInTheDocument();
		expect(within(first).getByText("1m")).toBeInTheDocument();
		const group = screen.getByText("192.0.2.7").closest("tr") as HTMLElement;
		expect(within(group).getByText("address group office")).toBeInTheDocument();
		expect(within(group).getByText("— (no rule matched)")).toBeInTheDocument();
		const bare = screen.getByText("198.51.100.7").closest("tr") as HTMLElement;
		expect(within(bare).getByText("not managed")).toBeInTheDocument();
		expect(within(bare).getByText("tcp/22")).toBeInTheDocument();

		const flowReads = calls.filter((c) => c.path.startsWith("/api/v1/flows?"));
		const q = new URL(flowReads[0]?.path ?? "", "http://x").searchParams;
		expect(q.get("workload")).toBe(db.id);
		expect(q.get("from")).not.toBeNull();

		await user.click(screen.getByRole("button", { name: /would block/ }));
		await waitFor(() => {
			const last = calls
				.filter((c) => c.path.startsWith("/api/v1/flows?"))
				.at(-1);
			expect(
				new URL(last?.path ?? "", "http://x").searchParams.get("verdict"),
			).toBe("would_block");
		});
	});

	it("pages flows by cursor within the same range", async () => {
		const { calls } = surface({
			flows: (q) =>
				q.get("cursor") === "f2"
					? { flows: [stranger], next_cursor: null }
					: { flows: [allowed, office], next_cursor: "f2" },
		});
		const user = userEvent.setup();
		renderApp(`/workloads/${db.id}`);
		await screen.findByText("192.0.2.7");
		await user.click(screen.getByRole("button", { name: "Load more" }));
		expect(await screen.findByText("198.51.100.7")).toBeInTheDocument();
		const reads = calls
			.filter((c) => c.path.startsWith("/api/v1/flows?"))
			.map((c) => new URL(c.path, "http://x").searchParams);
		expect(reads.at(-1)?.get("cursor")).toBe("f2");
		expect(reads.at(-1)?.get("from")).toBe(reads[0]?.get("from"));
	});

	it("says so when the range holds no flows", async () => {
		surface({ flows: () => ({ flows: [], next_cursor: null }) });
		renderApp(`/workloads/${db.id}`);
		expect(
			await screen.findByText("No inbound flows recorded in the last 14 days."),
		).toBeInTheDocument();
	});

	it("pairs each listening service with what connects to it and the rules that admit it", async () => {
		surface();
		renderApp(`/workloads/${db.id}/services`);
		const pg = (await screen.findByText("tcp/5432")).closest(
			"tr",
		) as HTMLElement;
		expect(within(pg).getByText("435k conns")).toBeInTheDocument();
		expect(within(pg).getByText("postgres from web")).toBeInTheDocument();
		const exporter = screen.getByText("tcp/9100").closest("tr") as HTMLElement;
		expect(
			within(exporter).getByText("1,190 conns · would block"),
		).toBeInTheDocument();
		// A rule with no ports admits every port of its protocol.
		expect(within(exporter).getByText("0d9c11aa/tcp")).toBeInTheDocument();
		const avahi = screen.getByText("udp/5353").closest("tr") as HTMLElement;
		expect(
			within(avahi).getByText("no inbound flows in 14d"),
		).toBeInTheDocument();
		expect(within(avahi).getByText("—")).toBeInTheDocument();
		expect(
			screen.getByRole("tab", { name: /Listening services/ }),
		).toHaveTextContent("Listening services3");
	});

	it("shows the rendered policy with each rule's provenance and instants", async () => {
		surface();
		renderApp(`/workloads/${db.id}/policy`);
		expect(
			await screen.findByText(
				"▲ rendered 4m ago, not applied — host is on v41",
			),
		).toBeInTheDocument();
		const card = screen.getByRole("article", { name: "postgres from web" });
		expect(within(card).getByText("aa5b70e7/tcp")).toBeInTheDocument();
		expect(
			within(card).getByRole("link", { name: "web-to-db" }),
		).toBeInTheDocument();
		expect(within(card).getByText("tcp 5432")).toBeInTheDocument();
		expect(within(card).getByText("2 host routes (/32)")).toBeInTheDocument();
		expect(within(card).getByTestId("rule-instants")).toHaveTextContent(
			"changed 2h ago · added 3d ago",
		);
		const gone = screen.getByRole("article", { name: "0d9c11aa/tcp" });
		expect(
			within(gone).getByText("authored rule since removed"),
		).toBeInTheDocument();
		expect(within(gone).getByText("tcp all ports")).toBeInTheDocument();
		expect(within(gone).getByTestId("rule-instants")).toHaveTextContent(
			"authored rule no longer exists",
		);
		expect(
			screen.getByRole("tab", { name: /Applied policy/ }),
		).toHaveTextContent("Applied policy2");
	});

	it("reports an unknown workload as such", async () => {
		mockSurface([
			signedIn,
			{
				method: "GET",
				path: "/api/v1/workloads/0190f2a3-dead-7000-8000-000000000000",
				reply: { status: 404, problem: problem("not-found", 404) },
			},
		]);
		renderApp("/workloads/0190f2a3-dead-7000-8000-000000000000");
		expect(
			await screen.findByRole("heading", { name: "No such workload" }),
		).toBeInTheDocument();
	});
});

describe("resend snapshot", () => {
	it("fires the directive and shows success as the snapshot instant moving on a later read", async () => {
		const sentAt = minutesAgo(0);
		let reads = 0;
		const { calls } = surface({
			workload: () => {
				reads += 1;
				// The directive's effect is not immediate: the read right
				// after it still shows the old instant; a later one moves.
				return reads >= 3
					? { ...db, sync: { ...db.sync, last_snapshot_sent_at: sentAt } }
					: db;
			},
			extra: [
				{
					method: "POST",
					path: `/api/v1/workloads/${db.id}/resend-snapshot`,
					reply: { status: 200, json: { last_snapshot_sent_at: null } },
				},
			],
		});
		const user = userEvent.setup();
		renderApp(`/workloads/${db.id}`);
		const card = await screen.findByTestId("status-card");
		await user.click(
			within(card).getByRole("button", { name: "Resend snapshot" }),
		);
		expect(await within(card).findByRole("status")).toHaveTextContent(
			"Reconnect requested. The snapshot instant above moves once the agent has reconnected.",
		);
		expect(within(card).getByTestId("snapshot-instant")).toHaveTextContent(
			"none recorded",
		);
		await user.click(within(card).getByRole("button", { name: "Read again" }));
		await waitFor(() =>
			expect(within(card).getByTestId("snapshot-instant")).toHaveTextContent(
				"0s ago",
			),
		);
		expect(within(card).getByRole("status")).toHaveTextContent(
			"The agent reconnected and was sent a fresh snapshot.",
		);
		expect(calls.filter((c) => c.method === "POST")).toHaveLength(1);
	});

	it("renders the offline refusal with the instant the agent was last seen", async () => {
		const lastSeen = "2026-09-24T18:02:11Z";
		surface({
			extra: [
				{
					method: "POST",
					path: `/api/v1/workloads/${db.id}/resend-snapshot`,
					reply: {
						status: 409,
						problem: {
							...problem("agent-offline", 409),
							last_seen_at: lastSeen,
						},
					},
				},
			],
		});
		const user = userEvent.setup();
		renderApp(`/workloads/${db.id}`);
		const card = await screen.findByTestId("status-card");
		await user.click(
			within(card).getByRole("button", { name: "Resend snapshot" }),
		);
		const alert = await within(card).findByRole("alert");
		expect(alert).toHaveTextContent(
			"The agent is offline, so nothing was sent.",
		);
		expect(alert).toHaveTextContent(`(${lastSeen})`);
		expect(within(alert).getByText(/ago$/)).toHaveAttribute(
			"dateTime",
			lastSeen,
		);
	});

	it("says when an offline agent was never heard from", async () => {
		surface({
			extra: [
				{
					method: "POST",
					path: `/api/v1/workloads/${db.id}/resend-snapshot`,
					reply: {
						status: 409,
						problem: { ...problem("agent-offline", 409), last_seen_at: null },
					},
				},
			],
		});
		const user = userEvent.setup();
		renderApp(`/workloads/${db.id}`);
		const card = await screen.findByTestId("status-card");
		await user.click(
			within(card).getByRole("button", { name: "Resend snapshot" }),
		);
		expect(await within(card).findByRole("alert")).toHaveTextContent(
			"It has never been heard from.",
		);
	});
});

describe("mode change from the detail", () => {
	it("changes this one workload by id", async () => {
		const { calls } = surface({
			extra: [
				{
					method: "POST",
					path: "/api/v1/mode-changes",
					reply: {
						status: 200,
						json: {
							mode_change_id: "5e0c1c7e-0000-4000-8000-000000000002",
							matched: 1,
							desired_updated: 1,
						},
					},
				},
			],
		});
		const user = userEvent.setup();
		renderApp(`/workloads/${db.id}`);
		await user.click(await screen.findByRole("button", { name: "change…" }));
		const dialog = await screen.findByRole("dialog", { name: "Change mode" });
		expect(dialog).toHaveTextContent("this workload");
		await user.click(within(dialog).getByRole("radio", { name: /Enforced/ }));
		await user.click(
			within(dialog).getByRole("button", { name: "Change to enforced" }),
		);
		await waitFor(() =>
			expect(calls.find((c) => c.method === "POST")?.body).toEqual({
				workload_ids: [db.id],
				target_mode: "enforced",
				expected_match_count: 1,
			}),
		);
	});
});
