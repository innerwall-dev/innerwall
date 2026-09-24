import type { ProvisioningToken, Workload } from "@/api/schema";

// Fixtures in the surface's own shapes, with instants relative to now so
// the relative times the screens print are stable.

export const minutesAgo = (m: number) =>
	new Date(Date.now() - m * 60_000).toISOString();
export const hoursFromNow = (h: number) =>
	new Date(Date.now() + h * 3_600_000 + 30_000).toISOString();

let serial = 0;

export function workload(
	overrides: Partial<Omit<Workload, "sync" | "health">> & {
		sync?: Partial<Workload["sync"]>;
		health?: Partial<Omit<Workload["health"], "credential">> & {
			credential?: Partial<Workload["health"]["credential"]>;
		};
	} = {},
): Workload {
	serial += 1;
	const { sync, health, ...rest } = overrides;
	return {
		id: `0190f2a3-0000-7000-8000-${String(serial).padStart(12, "0")}`,
		hostname: `host-${serial}`,
		labels: { app: "checkout", env: "prod" },
		mode: "visibility",
		enrolled_at: minutesAgo(14 * 24 * 60),
		addresses: ["10.20.4.17"],
		os: {
			family: "linux",
			name: "debian",
			version: "13",
			kernel_version: "6.12",
			architecture: "amd64",
		},
		agent: { version: "0.3.0", capabilities: ["nftables", "conntrack"] },
		listening_services: [],
		...rest,
		sync: {
			state: "synced",
			applied_version: 4,
			latest_version: 4,
			latest_rendered_at: minutesAgo(60),
			error: "",
			last_snapshot_sent_at: minutesAgo(60),
			...sync,
		},
		health: {
			last_seen_at: minutesAgo(1),
			dropped_flow_records: 0,
			...health,
			credential: {
				state: "renews",
				expires_at: hoursFromNow(9),
				last_renewed_at: null,
				last_error: "",
				...health?.credential,
			},
		},
	};
}

export function token(
	overrides: Partial<ProvisioningToken> = {},
): ProvisioningToken {
	serial += 1;
	return {
		id: `7f000000-0000-4000-8000-${String(serial).padStart(12, "0")}`,
		prefix: "iw_Qm8x",
		name: `token-${serial}`,
		labels: { app: "checkout", env: "prod" },
		state: "valid",
		created_at: minutesAgo(3 * 24 * 60),
		expires_at: hoursFromNow(19 * 24),
		revoked_at: null,
		use_count: 42,
		last_used_at: minutesAgo(3 * 24 * 60),
		...overrides,
	};
}
