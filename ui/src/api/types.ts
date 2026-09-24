// Hand-written types for the operator surface's session and identity
// endpoints (ADR-0021). They mirror internal/api on the control plane and
// stand until api/openapi.yaml lands, when scripts/generate-api.mjs
// produces src/api/generated.ts and these are replaced by it.

// Me is the operator as the console sees it: the display name set with
// the password (null until one is set) and the site label configured on
// the control plane (empty when none is).
export interface Me {
	display_name: string | null;
	site: string;
}

// Problem is the error document every failed request carries. The type
// is a stable identifier the console branches on; the rest is for people.
export interface Problem {
	type: string;
	title: string;
	status: number;
	detail?: string;
}

const base = "urn:innerwall:problem:";

// The problem types the surface emits that the console branches on.
export const ProblemType = {
	unauthenticated: `${base}unauthenticated`,
	invalidCredentials: `${base}invalid-credentials`,
	noPassword: `${base}no-password`,
	crossOrigin: `${base}cross-origin`,
	tooManyAttempts: `${base}too-many-attempts`,
	invalidRequest: `${base}invalid-request`,
	invalidParameter: `${base}invalid-parameter`,
	notFound: `${base}not-found`,
	methodNotAllowed: `${base}method-not-allowed`,
	internal: `${base}internal`,
} as const;
