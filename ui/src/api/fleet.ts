import { type Query, request, withQuery } from "./client";
import type {
	AddressGroup,
	FlowsPage,
	MintedToken,
	MintTokenRequest,
	ModeChangeAck,
	ModeChangeRequest,
	ProvisioningToken,
	RenderedPolicy,
	ResendSnapshotAck,
	Rollup,
	RollupGrouping,
	Verdict,
	Workload,
	WorkloadsPage,
} from "./schema";

// The fleet, workload, flow, and token endpoints the console reads and
// writes. Each function is one request against the public surface; the
// screens hold no other path to the control plane (ADR-0007).

export interface WorkloadFilter {
	// label is the domain's selector form: `key=value` requirements,
	// ANDed across keys and ORed within one key.
	label?: readonly string[];
	sync_state?: Workload["sync"]["state"];
	mode?: Workload["mode"];
}

export function listWorkloads(
	filter: WorkloadFilter,
	cursor?: string,
	limit?: number,
): Promise<WorkloadsPage> {
	return request("GET", withQuery("/workloads", { ...filter, cursor, limit }));
}

export function getWorkload(id: string): Promise<Workload> {
	return request("GET", `/workloads/${encodeURIComponent(id)}`);
}

export function getRenderedPolicy(id: string): Promise<RenderedPolicy> {
	return request("GET", `/workloads/${encodeURIComponent(id)}/rendered-policy`);
}

export function resendSnapshot(id: string): Promise<ResendSnapshotAck> {
	return request(
		"POST",
		`/workloads/${encodeURIComponent(id)}/resend-snapshot`,
	);
}

export function createModeChange(
	body: ModeChangeRequest,
): Promise<ModeChangeAck> {
	return request("POST", "/mode-changes", body);
}

export interface FlowFilter {
	workload: string;
	from?: string;
	to?: string;
	verdict?: Verdict;
	// peer is one stored peer key: a workload id, an address group id,
	// or a bare address.
	peer?: string;
}

export function listFlows(
	filter: FlowFilter,
	cursor?: string,
): Promise<FlowsPage> {
	return request("GET", withQuery("/flows", { ...filter, cursor }));
}

export function getRollup(
	query: Query & { group_by: RollupGrouping },
): Promise<Rollup> {
	return request("GET", withQuery("/flows/rollup", query));
}

export function listAddressGroups(): Promise<{
	address_groups: AddressGroup[];
}> {
	return request("GET", "/address-groups");
}

export function listProvisioningTokens(): Promise<{
	tokens: ProvisioningToken[];
}> {
	return request("GET", "/provisioning-tokens");
}

export function mintProvisioningToken(
	body: MintTokenRequest,
): Promise<MintedToken> {
	return request("POST", "/provisioning-tokens", body);
}

export function revokeProvisioningToken(id: string): Promise<void> {
	return request("DELETE", `/provisioning-tokens/${encodeURIComponent(id)}`);
}
