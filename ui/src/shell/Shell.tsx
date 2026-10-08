import { useCallback, useMemo, useState } from "react";
import {
	matchPath,
	Navigate,
	Outlet,
	useLocation,
	useOutletContext,
} from "react-router";
import { listWorkloads } from "@/api/fleet";
import { listRulesets } from "@/api/policy";
import type { SyncState } from "@/api/schema";
import { useSession } from "@/auth/SessionProvider";
import { SidebarProvider } from "@/components/ui/sidebar";
import { useResource } from "@/lib/resource";
import { type Crumb, PageHeader } from "./PageHeader";
import { Sidebar } from "./Sidebar";

// OpenWorkload is the workload a detail screen last showed: the sidebar
// keeps it one click away, and the breadcrumb names it.
export interface OpenWorkload {
	id: string;
	hostname: string;
	state: SyncState;
}

// ShellContext is what the screens tell the frame: the workload they
// show, and that the fleet or the policy changed so the frame's own
// reads refresh.
export interface ShellContext {
	openWorkload: (w: OpenWorkload) => void;
	fleetChanged: () => void;
	policyChanged: () => void;
}

export function useShell(): ShellContext {
	return useOutletContext<ShellContext>();
}

// crumbsFor names the screen for the page header.
function crumbsFor(
	pathname: string,
	search: string,
	open: OpenWorkload | null,
): Crumb[] {
	if (pathname.startsWith("/simulation")) {
		// The ruleset under review, which the review names in the address.
		const ruleset = new URLSearchParams(search).get("ruleset");
		return ruleset
			? [{ label: "Simulation review" }, { label: ruleset }]
			: [{ label: "Simulation review" }];
	}
	if (pathname.startsWith("/map")) {
		// The map's scope, when it has one, names what the map shows.
		const scope = new URLSearchParams(search).getAll("label");
		return scope.length > 0
			? [{ label: "Flow map" }, { label: scope.join(" ") }]
			: [{ label: "Flow map" }];
	}
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
	if (pathname.startsWith("/policy")) {
		// The ruleset the editor shows, which it names in the address.
		const q = new URLSearchParams(search);
		const ruleset = q.get("ruleset");
		if (q.has("new")) return [{ label: "Policy" }, { label: "New ruleset" }];
		return ruleset
			? [{ label: "Policy" }, { label: ruleset }]
			: [{ label: "Policy" }];
	}
	return [{ label: "Innerwall" }];
}

// Shell is the authenticated frame: sidebar, page header, and the screen.
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
	const [policyGeneration, setPolicyGeneration] = useState(0);

	// The frame reports only what it can state exactly. Whether the fleet
	// is empty takes one row; the fleet's per-state totals have no read,
	// so the sidebar shows none. The ruleset listing is complete, so its
	// length is the policy count.
	const { resource: fleet } = useResource(
		() => listWorkloads({}, undefined).then((p) => p.workloads.length === 0),
		[generation],
	);
	const { resource: rulesets } = useResource(
		() => listRulesets().then((r) => r.rulesets.length),
		[policyGeneration],
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
	const policyChanged = useCallback(
		() => setPolicyGeneration((g) => g + 1),
		[],
	);
	const context = useMemo<ShellContext>(
		() => ({ openWorkload, fleetChanged, policyChanged }),
		[openWorkload, fleetChanged, policyChanged],
	);

	const counts: Partial<Record<string, number>> = {};
	if (fleetEmpty === true) counts["/workloads"] = 0;
	if (rulesets.status === "ready") counts["/policy"] = rulesets.data;

	return (
		<SidebarProvider className="h-dvh overflow-hidden bg-app text-primary">
			<Sidebar counts={counts} fleetEmpty={fleetEmpty} open={open} />
			<div className="flex min-w-0 flex-1 flex-col">
				<PageHeader
					crumbs={crumbsFor(location.pathname, location.search, open)}
				/>
				<main className="relative flex min-h-0 flex-1 flex-col overflow-auto">
					<Outlet context={context} />
				</main>
			</div>
		</SidebarProvider>
	);
}

function Loading() {
	return (
		<div
			className="flex h-dvh items-center justify-center bg-app text-tertiary"
			aria-busy="true"
		>
			<span className="type-mono-sm">…</span>
		</div>
	);
}
