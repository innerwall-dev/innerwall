import { useCallback, useMemo, useState } from "react";
import {
	matchPath,
	Navigate,
	Outlet,
	useLocation,
	useOutletContext,
} from "react-router";
import { countWorkloads } from "@/api/fleet";
import { listRulesets } from "@/api/policy";
import type { SyncState } from "@/api/schema";
import { useSession } from "@/auth/SessionProvider";
import { SidebarProvider } from "@/components/ui/sidebar";
import { useResource } from "@/lib/resource";
import { type Crumb, PageHeader } from "./PageHeader";
import { attentionBadge, type Badge, Sidebar } from "./Sidebar";

// OpenWorkload is the workload a detail screen last showed: the sidebar
// keeps it one click away.
export interface OpenWorkload {
	id: string;
	hostname: string;
	state: SyncState;
	// removed is the agent reporting its firewall table gone from the
	// host, which outranks the sync state (issue #64).
	removed: boolean;
	// reconnecting is an offline sync stream whose flow reports still
	// arrive (issue #61).
	reconnecting: boolean;
}

// ShellContext is what the screens tell the frame: the workload they
// show, and that the fleet or the policy changed so the frame's own
// reads refresh. strip is the header strip's identity slot on a screen
// that draws its own identity there (a workload's detail), null
// elsewhere and until the strip has mounted.
export interface ShellContext {
	openWorkload: (w: OpenWorkload) => void;
	fleetChanged: () => void;
	policyChanged: () => void;
	strip: HTMLElement | null;
}

export function useShell(): ShellContext {
	return useOutletContext<ShellContext>();
}

// crumbsFor names the screen for the page header.
function crumbsFor(pathname: string, search: string): Crumb[] {
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
	// A workload's detail draws its own identity into the strip.
	if (identityRoute(pathname)) return [];
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
	// Every address with a screen is named above; the rest have none.
	return [{ label: "Not found" }];
}

// identityRoute says the screen at pathname draws its identity in the
// header strip: a workload's detail, whose identity is the workload.
function identityRoute(pathname: string): boolean {
	const detail = matchPath("/workloads/:id/*", pathname);
	return detail !== null && detail.params.id !== "tokens";
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
	const [strip, setStrip] = useState<HTMLElement | null>(null);

	// The frame reports only what it can state exactly: the fleet's
	// counts are the control plane's, read in one request, and the
	// ruleset listing is complete, so its length is the policy count.
	const { resource: fleet } = useResource(countWorkloads, [generation]);
	const { resource: rulesets } = useResource(
		() => listRulesets().then((r) => r.rulesets.length),
		[policyGeneration],
	);
	const fleetEmpty = fleet.status === "ready" ? fleet.data.total === 0 : null;

	const openWorkload = useCallback((w: OpenWorkload) => {
		setOpen((cur) =>
			cur &&
			cur.id === w.id &&
			cur.hostname === w.hostname &&
			cur.state === w.state &&
			cur.removed === w.removed &&
			cur.reconnecting === w.reconnecting
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
		() => ({ openWorkload, fleetChanged, policyChanged, strip }),
		[openWorkload, fleetChanged, policyChanged, strip],
	);

	const counts: Partial<Record<string, Badge>> = {};
	if (fleet.status === "ready") {
		const b = attentionBadge(fleet.data);
		if (b) counts["/workloads"] = b;
	}
	if (rulesets.status === "ready") counts["/policy"] = { n: rulesets.data };

	return (
		<SidebarProvider className="h-dvh overflow-hidden bg-canvas text-primary">
			<Sidebar counts={counts} fleetEmpty={fleetEmpty} open={open} />
			<div className="flex min-w-0 flex-1 flex-col">
				<PageHeader
					crumbs={crumbsFor(location.pathname, location.search)}
					identity={identityRoute(location.pathname) ? setStrip : undefined}
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
			className="flex h-dvh items-center justify-center bg-canvas text-tertiary"
			aria-busy="true"
		>
			<span className="type-mono-sm">…</span>
		</div>
	);
}
