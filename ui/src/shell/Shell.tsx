import { useCallback, useMemo, useState } from "react";
import {
	matchPath,
	Navigate,
	Outlet,
	useLocation,
	useOutletContext,
} from "react-router";
import { request } from "@/api/client";
import { listWorkloads } from "@/api/fleet";
import type { SyncState } from "@/api/schema";
import { useSession } from "@/auth/SessionProvider";
import { useResource } from "@/lib/resource";
import { Sidebar } from "./Sidebar";
import { type Crumb, TopBar } from "./TopBar";

// OpenWorkload is the workload a detail screen last showed: the sidebar
// keeps it one click away, and the breadcrumb names it.
export interface OpenWorkload {
	id: string;
	hostname: string;
	state: SyncState;
}

// ShellContext is what the screens tell the frame: the workload they
// show, and that the fleet changed so the frame's own reads refresh.
export interface ShellContext {
	openWorkload: (w: OpenWorkload) => void;
	fleetChanged: () => void;
}

export function useShell(): ShellContext {
	return useOutletContext<ShellContext>();
}

// crumbsFor names the screen for the top bar.
function crumbsFor(pathname: string, open: OpenWorkload | null): Crumb[] {
	if (pathname.startsWith("/simulation"))
		return [{ label: "Simulation review" }];
	if (pathname.startsWith("/map")) return [{ label: "Flow map" }];
	if (pathname.startsWith("/workloads/tokens"))
		return [{ label: "Workloads" }, { label: "Provisioning tokens" }];
	const detail = matchPath("/workloads/:id/*", pathname);
	if (detail) {
		const name =
			open && open.id === detail.params.id ? open.hostname : "Workload";
		return [{ label: "Workloads" }, { label: name }];
	}
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
	return <Frame />;
}

function Frame() {
	const location = useLocation();
	const [open, setOpen] = useState<OpenWorkload | null>(null);
	const [generation, setGeneration] = useState(0);

	// The frame reports only what it can state exactly. Whether the fleet
	// is empty takes one row; the fleet's per-state totals have no read,
	// so the sidebar shows none. The ruleset listing is complete, so its
	// length is the policy count.
	const { resource: fleet } = useResource(
		() => listWorkloads({}, undefined).then((p) => p.workloads.length === 0),
		[generation],
	);
	const { resource: rulesets } = useResource(
		() =>
			request<{ rulesets: unknown[] }>("GET", "/rulesets").then(
				(r) => r.rulesets.length,
			),
		[],
	);
	const fleetEmpty = fleet.status === "ready" ? fleet.data : null;

	const openWorkload = useCallback((w: OpenWorkload) => {
		setOpen((cur) =>
			cur &&
			cur.id === w.id &&
			cur.hostname === w.hostname &&
			cur.state === w.state
				? cur
				: w,
		);
	}, []);
	const fleetChanged = useCallback(() => setGeneration((g) => g + 1), []);
	const context = useMemo<ShellContext>(
		() => ({ openWorkload, fleetChanged }),
		[openWorkload, fleetChanged],
	);

	const counts: Partial<Record<string, number>> = {};
	if (fleetEmpty === true) counts["/workloads"] = 0;
	if (rulesets.status === "ready") counts["/policy"] = rulesets.data;

	return (
		<div className="flex h-dvh w-full overflow-hidden bg-background text-foreground">
			<Sidebar counts={counts} fleetEmpty={fleetEmpty} open={open} />
			<div className="flex min-w-0 flex-1 flex-col">
				<TopBar crumbs={crumbsFor(location.pathname, open)} />
				<main className="relative flex min-h-0 flex-1 flex-col overflow-auto">
					<Outlet context={context} />
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
