import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useState,
} from "react";
import { ProblemError } from "@/api/client";
import { createSession, deleteSession, getMe } from "@/api/session";
import type { Me } from "@/api/types";

// The console's view of the one operator principal (ADR-0021). It is
// resolved once on load from the identity endpoint; a 401 anywhere means
// the session is gone and the login screen is the next thing shown.
export type SessionState =
	| { status: "loading" }
	| { status: "anonymous" }
	| { status: "authenticated"; me: Me };

interface SessionContextValue {
	session: SessionState;
	login: (password: string) => Promise<Me>;
	logout: () => Promise<void>;
	// expire marks the session gone without a request, for callers that
	// have just been refused.
	expire: () => void;
}

const SessionContext = createContext<SessionContextValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
	const [session, setSession] = useState<SessionState>({ status: "loading" });

	useEffect(() => {
		let cancelled = false;
		getMe().then(
			(me) => {
				if (!cancelled) setSession({ status: "authenticated", me });
			},
			() => {
				// Any failure to resolve the operator, unauthenticated or
				// otherwise, is the login screen: it is the one place that
				// can say what is wrong and let the operator try again.
				if (!cancelled) setSession({ status: "anonymous" });
			},
		);
		return () => {
			cancelled = true;
		};
	}, []);

	const login = useCallback(async (password: string) => {
		const me = await createSession(password);
		setSession({ status: "authenticated", me });
		return me;
	}, []);

	const logout = useCallback(async () => {
		try {
			await deleteSession();
		} catch (err) {
			// A session the surface no longer recognizes is already ended;
			// anything else is still worth surfacing.
			if (!(err instanceof ProblemError && err.problem.status === 401)) {
				throw err;
			}
		} finally {
			setSession({ status: "anonymous" });
		}
	}, []);

	const expire = useCallback(() => setSession({ status: "anonymous" }), []);

	const value = useMemo(
		() => ({ session, login, logout, expire }),
		[session, login, logout, expire],
	);
	return (
		<SessionContext.Provider value={value}>{children}</SessionContext.Provider>
	);
}

export function useSession(): SessionContextValue {
	const ctx = useContext(SessionContext);
	if (!ctx) {
		throw new Error("useSession called outside SessionProvider");
	}
	return ctx;
}

// useMe is the authenticated operator; only screens behind the shell
// call it.
export function useMe(): Me {
	const { session } = useSession();
	if (session.status !== "authenticated") {
		throw new Error("useMe called outside an authenticated session");
	}
	return session.me;
}
