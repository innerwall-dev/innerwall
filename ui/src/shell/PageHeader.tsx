import { Fragment, useEffect, useState } from "react";
import { Icon } from "@/components/Icon";
import { Kbd } from "@/components/ui/kbd";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";
import { Palette } from "./Palette";

export interface Crumb {
	label: string;
}

// The page header strip, 48px above every screen and one quiet line:
// everything in it is the header register (13/20 at 500), with the
// hierarchy carried by color alone. The current screen's breadcrumb is
// its title, the screen in text-primary after its section in
// text-secondary, and the search field is on the right,
// which opens the search palette, as ⌘K (Ctrl+K) does from anywhere.
// A screen that draws its own identity in the strip (a workload's
// detail) gets a slot in place of the breadcrumb: identity receives it.
// Where the sidebar is a sheet, the strip carries the control that
// opens it. The strip and its hairline span the content area (it is
// chrome); its contents align to the page container below it, unless
// the screen runs full-bleed (bleed).
export function PageHeader({
	crumbs,
	identity,
	bleed = false,
}: {
	crumbs: Crumb[];
	identity?: (slot: HTMLElement | null) => void;
	bleed?: boolean;
}) {
	const [searching, setSearching] = useState(false);
	useEffect(() => {
		const onKey = (e: KeyboardEvent) => {
			if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
				e.preventDefault();
				setSearching(true);
			}
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, []);
	return (
		<header className="flex h-header-strip shrink-0 border-b border-hairline bg-canvas type-header-register">
			<div
				className={cn(
					"flex w-full min-w-0 items-center gap-3 px-5 max-md:px-4",
					!bleed && "mx-auto max-w-page",
				)}
			>
				<SidebarTrigger compact className="-ml-1.5" />
				{identity ? (
					<div ref={identity} className="flex min-w-0 flex-1 items-center" />
				) : (
					<nav
						aria-label="Breadcrumb"
						className="flex min-w-0 items-center gap-1.5 text-secondary"
					>
						{crumbs.map((c, i) => (
							<Fragment key={c.label}>
								{i > 0 ? (
									<span className="text-tertiary" aria-hidden="true">
										/
									</span>
								) : null}
								<span
									className={
										i === crumbs.length - 1
											? "truncate text-primary"
											: undefined
									}
								>
									{c.label}
								</span>
							</Fragment>
						))}
					</nav>
				)}
				<button
					type="button"
					onClick={() => setSearching(true)}
					aria-keyshortcuts="Meta+K Control+K"
					className="ml-auto flex h-control w-[260px] cursor-pointer items-center gap-2 rounded-control border border-strong bg-field-sunk px-2.5 text-tertiary hover:border-control-hover focus-visible:focus-ring max-md:hidden"
				>
					<Icon name="search" />
					<span className="min-w-0 flex-1 truncate text-left type-control font-normal">
						Search workloads and rulesets…
					</span>
					<Kbd>⌘K</Kbd>
				</button>
			</div>
			<Palette open={searching} onOpenChange={setSearching} />
		</header>
	);
}
