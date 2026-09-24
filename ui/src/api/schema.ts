import type { components } from "./generated";

// The operator surface's shapes as the console names them. Every type
// here is an alias into src/api/generated.ts, which is generated from
// api/openapi.yaml; the description is the contract and nothing in this
// file restates a field.

type Schemas = components["schemas"];

export type Me = Schemas["Me"];
export type Problem = Schemas["Problem"];

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
	notFound: "urn:innerwall:problem:not-found",
	methodNotAllowed: "urn:innerwall:problem:method-not-allowed",
	internal: "urn:innerwall:problem:internal",
} as const satisfies Record<string, ProblemTypeURN>;
