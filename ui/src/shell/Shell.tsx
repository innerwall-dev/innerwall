import { Navigate, Outlet, useLocation } from "react-router";
import { useSession } from "@/auth/SessionProvider";
import { Sidebar } from "./Sidebar";
import { type Crumb, TopBar } from "./TopBar";

// crumbsFor names the screen for the top bar. The read screens extend
// this with the scope or workload they show.
function crumbsFor(pathname: string): Crumb[] {
	if (pathname.startsWith("/simulation"))
		return [{ label: "Simulation review" }];
	if (pathname.startsWith("/map")) return [{ label: "Flow map" }];
	if (pathname.startsWith("/workloads"))
		return [{ label: "Workloads" }, { label: "Fleet" }];
	if (pathname.startsWith("/policy")) return [{ label: "Policy" }];
	return [{ label: "Innerwall" }];
}

// Shell is the authenticated frame: sidebar, top bar, and the screen.
// An anonymous session is sent to the login screen, remembering where
// it was headed.
export function Shell() {
	const { session } = useSession();
	const location = useLocation();
	if (session.status === "loading") {
		return <Loading />;
	}
	if (session.status === "anonymous") {
		return <Navigate to="/login" replace state={{ from: location.pathname }} />;
	}
	return (
		<div className="flex h-dvh w-full overflow-hidden bg-background text-foreground">
			<Sidebar />
			<div className="flex min-w-0 flex-1 flex-col">
				<TopBar crumbs={crumbsFor(location.pathname)} />
				<main className="relative flex min-h-0 flex-1 flex-col overflow-auto">
					<Outlet />
				</main>
			</div>
		</div>
	);
}

function Loading() {
	return (
		<div
			className="flex h-dvh items-center justify-center bg-background text-muted-foreground"
			aria-busy="true"
		>
			<span className="font-mono text-[12px]">…</span>
		</div>
	);
}
