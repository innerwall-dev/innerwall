import type {
	EvidenceGap,
	PeerRef,
	Rollup,
	RollupGroup,
	Rule,
	Ruleset,
	Selector,
} from "@/api/schema";
import type { ByVerdict, ReviewRollups } from "@/routes/review/model";
import { minutesAgo } from "./fixtures";
import { rollupOf } from "./map";

// Simulation-review fixtures in the surface's own shapes: peer-and-service
// groups with their workload counts, and rulesets with persisted rules.

export function ps(
	peer: PeerRef,
	service: string,
	connections: number,
	workloads: number,
	lastSeenMinutesAgo = 5,
): RollupGroup {
	const [protocol, port] = service.split("/");
	return {
		keys: {
			peer,
			service: {
				protocol: protocol as "tcp" | "udp" | "icmp",
				port: port ? Number(port) : 0,
			},
		},
		workload_count: workloads,
		flow_count: 2,
		connection_count: connections,
		byte_count: connections * 100,
		first_seen: minutesAgo(120),
		last_seen: minutesAgo(lastSeenMinutesAgo),
	};
}

// gap is an evidence gap on a workload, minutes before now; a source
// overrun on the packet log unless told otherwise.
export function gap(
	w: { id: string; hostname: string; labels?: Record<string, string> },
	fromMinutesAgo: number,
	toMinutesAgo: number,
	overrides: Partial<Omit<EvidenceGap, "workload">> = {},
): EvidenceGap {
	return {
		workload: { id: w.id, hostname: w.hostname, labels: w.labels ?? {} },
		kind: "source_overrun",
		source: "nflog",
		from: minutesAgo(fromMinutesAgo),
		to: minutesAgo(toMinutesAgo),
		count: null,
		...overrides,
	};
}

export function peerService(
	groups: RollupGroup[],
	opts: { truncated?: boolean; effective?: [string, string] | null } = {},
): Rollup {
	return { ...rollupOf(groups, opts), group_by: ["peer", "service"] };
}

// rollups assembles the four rollups a review reads, each decision's
// source-by-destination rollup empty unless given.
export function rollups(
	ps: Partial<ByVerdict<RollupGroup[]>>,
	sd: Partial<ByVerdict<RollupGroup[]>> = {},
	opts: { truncated?: boolean } = {},
): ReviewRollups {
	return {
		peerService: {
			would_block: peerService(ps.would_block ?? [], opts),
			allowed: peerService(ps.allowed ?? [], opts),
		},
		srcDst: {
			would_block: rollupOf(sd.would_block ?? []),
			allowed: rollupOf(sd.allowed ?? []),
		},
	};
}

let serial = 0;

export function rule(overrides: Partial<Rule> = {}): Rule {
	serial += 1;
	return {
		id: `5e000000-0000-4000-8000-${String(serial).padStart(12, "0")}`,
		direction: "inbound",
		enabled: true,
		description: `rule-${serial}`,
		peers: [{ workloads: { app: ["storefront-api"] } }],
		entries: [{ protocol: "tcp", ports: ["8443"] }],
		version: "3",
		created_at: minutesAgo(600),
		updated_at: minutesAgo(600),
		...overrides,
	};
}

export function ruleset(
	name: string,
	scope: Selector,
	rules: Rule[] = [],
	overrides: Partial<Ruleset> = {},
): Ruleset {
	serial += 1;
	return {
		id: `4e000000-0000-4000-8000-${String(serial).padStart(12, "0")}`,
		name,
		enabled: true,
		scope,
		rules,
		version: "7",
		created_at: minutesAgo(600),
		updated_at: minutesAgo(600),
		...overrides,
	};
}
