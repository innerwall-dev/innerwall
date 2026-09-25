import type {
	AddressGroup,
	PeerRef,
	Rollup,
	RollupGroup,
	Verdict,
	WorkloadRef,
} from "@/api/schema";
import { minutesAgo } from "./fixtures";

// Flow-map fixtures in the surface's own shapes: rollup groups keyed by
// source peer and destination workload, one rollup per decision.

export function wref(
	id: string,
	hostname: string,
	labels: Record<string, string>,
): WorkloadRef {
	return { id, hostname, labels };
}

export const peer = {
	workload: (w: WorkloadRef): PeerRef => ({
		kind: "workload",
		workload_id: w.id,
		name: w.hostname,
		labels: w.labels,
	}),
	group: (id: string, name: string): PeerRef => ({
		kind: "group",
		address_group_id: id,
		name,
		labels: {},
	}),
	address: (address: string): PeerRef => ({
		kind: "address",
		address,
		labels: {},
	}),
	unknown: (key: string): PeerRef => ({
		kind: "unknown",
		address: key,
		labels: {},
	}),
};

export function row(
	src: PeerRef,
	dst: WorkloadRef,
	connections: number,
	lastSeenMinutesAgo = 5,
): RollupGroup {
	return {
		keys: { src, dst },
		flow_count: 2,
		connection_count: connections,
		byte_count: connections * 100,
		first_seen: minutesAgo(120),
		last_seen: minutesAgo(lastSeenMinutesAgo),
	};
}

export function rollupOf(
	groups: RollupGroup[],
	opts: {
		truncated?: boolean;
		effective?: [string, string] | null;
		from?: string;
		to?: string;
	} = {},
): Rollup {
	const totals = groups.reduce(
		(t, g) => ({
			flow_count: t.flow_count + g.flow_count,
			connection_count: t.connection_count + g.connection_count,
			byte_count: t.byte_count + g.byte_count,
		}),
		{ flow_count: 0, connection_count: 0, byte_count: 0 },
	);
	const eff =
		opts.effective === undefined
			? groups.length > 0
				? ([minutesAgo(125), minutesAgo(55)] as [string, string])
				: null
			: opts.effective;
	return {
		from: opts.from ?? minutesAgo(24 * 60),
		to: opts.to ?? minutesAgo(0),
		effective_from: eff?.[0] ?? null,
		effective_to: eff?.[1] ?? null,
		group_by: ["src", "dst"],
		groups,
		group_count: groups.length + (opts.truncated ? 10 : 0),
		truncated: opts.truncated ?? false,
		totals,
	};
}

export type ByVerdict = Partial<Record<Verdict, RollupGroup[]>>;

export function addressGroup(
	id: string,
	name: string,
	cidrs: string[],
): AddressGroup {
	return {
		id,
		name,
		cidrs,
		version: "1",
		created_at: minutesAgo(600),
		updated_at: minutesAgo(600),
	};
}
