import type { components, operations } from "./generated";

// The operator surface's shapes as the console names them. Every type
// here is an alias into src/api/generated.ts, which is generated from
// api/openapi.yaml; the description is the contract and nothing in this
// file restates a field.

type Schemas = components["schemas"];

export type Me = Schemas["Me"];
export type Problem = Schemas["Problem"];

export type Mode = Schemas["Mode"];
export type SyncState = Schemas["SyncState"];
export type Verdict = Schemas["Verdict"];
export type Protocol = Schemas["Protocol"];
export type LabelMap = Schemas["LabelMap"];
export type Workload = Schemas["Workload"];
export type WorkloadRef = Schemas["WorkloadRef"];
export type RenderedPolicy = Schemas["RenderedPolicy"];
export type RenderedRule = RenderedPolicy["rules"][number];
export type Flow = Schemas["Flow"];
export type FlowsPage = Schemas["FlowsPage"];
export type PeerRef = Schemas["PeerRef"];
export type RuleRef = Schemas["RuleRef"];
export type Rollup = Schemas["Rollup"];
export type RollupGrouping = NonNullable<
	operations["getFlowsRollup"]["parameters"]["query"]
>["group_by"];
export type ModeChangeRequest = Schemas["ModeChangeRequest"];
export type ModeChangeAck = Schemas["ModeChangeAck"];
export type ResendSnapshotAck = Schemas["ResendSnapshotAck"];
export type ProvisioningToken = Schemas["ProvisioningToken"];
export type TokenState = Schemas["TokenState"];
export type MintTokenRequest =
	operations["mintProvisioningToken"]["requestBody"]["content"]["application/json"];
export type MintedToken =
	operations["mintProvisioningToken"]["responses"][201]["content"]["application/json"];
export type WorkloadsPage =
	operations["listWorkloads"]["responses"][200]["content"]["application/json"];

// ProblemTypeURN is the closed set of problem types the surface emits.
export type ProblemTypeURN = Problem["type"];

// The problem types the console branches on. Each value is checked
// against the description's closed set, so a type the surface stops
// emitting fails the build rather than going quietly unmatched.
export const ProblemType = {
	unauthenticated: "urn:innerwall:problem:unauthenticated",
	invalidCredentials: "urn:innerwall:problem:invalid-credentials",
	noPassword: "urn:innerwall:problem:no-password",
	crossOrigin: "urn:innerwall:problem:cross-origin",
	tooManyAttempts: "urn:innerwall:problem:too-many-attempts",
	invalidRequest: "urn:innerwall:problem:invalid-request",
	invalidParameter: "urn:innerwall:problem:invalid-parameter",
	matchCountMismatch: "urn:innerwall:problem:match-count-mismatch",
	alreadyRevoked: "urn:innerwall:problem:already-revoked",
	agentOffline: "urn:innerwall:problem:agent-offline",
	validation: "urn:innerwall:problem:validation",
	notFound: "urn:innerwall:problem:not-found",
	methodNotAllowed: "urn:innerwall:problem:method-not-allowed",
	internal: "urn:innerwall:problem:internal",
} as const satisfies Record<string, ProblemTypeURN>;
