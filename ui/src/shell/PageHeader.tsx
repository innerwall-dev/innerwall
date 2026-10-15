import { Fragment, useEffect, useState } from "react";
import { Icon } from "@/components/Icon";
import { Kbd } from "@/components/ui/kbd";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { Palette } from "./Palette";

export interface Crumb {
	label: string;
}

// The page header strip, topbar-tall above every screen: the current
// screen's breadcrumb as its title, and the search field on the right,
// which opens the search palette, as ⌘K (Ctrl+K) does from anywhere.
// Where the sidebar is a sheet, the strip carries the control that
// opens it.
export function PageHeader({ crumbs }: { crumbs: Crumb[] }) {
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
		<header className="flex h-header-strip shrink-0 items-center gap-3 border-b border-hairline bg-canvas px-6 max-md:px-4">
			<SidebarTrigger compact className="-ml-1.5" />
			<nav
				aria-label="Breadcrumb"
				className="flex min-w-0 items-center gap-1.5 type-dense text-tertiary"
			>
				{crumbs.map((c, i) => (
					<Fragment key={c.label}>
						{i > 0 ? (
							<span className="text-disabled" aria-hidden="true">
								/
							</span>
						) : null}
						<span
							className={
								i === crumbs.length - 1
									? "truncate type-dense-strong text-primary"
									: undefined
							}
						>
							{c.label}
						</span>
					</Fragment>
				))}
			</nav>
			<button
				type="button"
				onClick={() => setSearching(true)}
				aria-keyshortcuts="Meta+K Control+K"
				className="ml-auto flex h-control w-[260px] cursor-pointer items-center gap-2 rounded-control border border-hairline bg-canvas px-2.5 text-tertiary hover:bg-wash-hover focus-visible:focus-ring max-md:hidden"
			>
				<Icon name="search" />
				<span className="min-w-0 flex-1 truncate text-left type-caption">
					Search workloads and rulesets…
				</span>
				<Kbd>⌘K</Kbd>
			</button>
			<Palette open={searching} onOpenChange={setSearching} />
		</header>
	);
}
