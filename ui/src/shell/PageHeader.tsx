import { Fragment } from "react";
import { Icon } from "@/components/Icon";
import { Kbd } from "@/components/ui/kbd";
import { SidebarTrigger } from "@/components/ui/sidebar";

export interface Crumb {
	label: string;
}

// The page header strip, topbar-tall above every screen: the current
// screen's breadcrumb as its title, and the search field on the right
// (a placeholder until search exists; the shortcut hint is the
// design's). Where the sidebar is a sheet, the strip carries the
// control that opens it.
export function PageHeader({ crumbs }: { crumbs: Crumb[] }) {
	return (
		<header className="flex h-topbar shrink-0 items-center gap-3 border-b border-default bg-app px-6 max-md:px-4">
			<SidebarTrigger compact className="-ml-1.5" />
			<nav
				aria-label="Breadcrumb"
				className="flex min-w-0 items-center gap-1.5 type-ui text-tertiary"
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
									? "truncate type-body-strong text-primary"
									: undefined
							}
						>
							{c.label}
						</span>
					</Fragment>
				))}
			</nav>
			<label className="ml-auto flex h-control-sm w-[260px] items-center gap-2 rounded-md border border-default bg-app px-2.5 text-tertiary max-md:hidden">
				<Icon name="search" />
				<input
					type="search"
					placeholder="Search workloads, labels, rules…"
					aria-label="Search"
					disabled
					title="Search arrives with the read screens"
					className="min-w-0 flex-1 bg-transparent p-0 type-caption text-primary placeholder:text-tertiary focus:outline-none disabled:cursor-default"
				/>
				<Kbd>⌘K</Kbd>
			</label>
		</header>
	);
}
