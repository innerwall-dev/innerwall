import { request } from "./client";
import type { Me } from "./schema";

// The three endpoints of the session and identity surface (ADR-0021).

// createSession exchanges the password for a session cookie and returns
// the operator. A control plane with no password refuses with the
// no-password problem; a wrong password with invalid-credentials; too
// many attempts from one address with too-many-attempts.
export function createSession(password: string): Promise<Me> {
	return request<Me>("POST", "/session", { password });
}

// deleteSession revokes the current session and clears the cookie.
export function deleteSession(): Promise<Record<string, never>> {
	return request<Record<string, never>>("DELETE", "/session");
}

// getMe is the authenticated operator and the site label; an
// unauthenticated request is the unauthenticated problem.
export function getMe(): Promise<Me> {
	return request<Me>("GET", "/me");
}
