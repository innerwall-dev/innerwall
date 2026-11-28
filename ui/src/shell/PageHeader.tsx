import { Fragment, useEffect, useState } from "react";
import { Icon } from "@/components/Icon";
import { Kbd } from "@/components/ui/kbd";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { AccountPopover } from "./AccountPopover";
import { LogoMark, Wordmark } from "./Logo";
import { Palette } from "./Palette";

export interface Crumb {
	label: string;
}

// The console's header, 48px across the whole width above the sidebar
// and the screen, on the subtle rule: the logo and the wordmark at the
// left, then the screen's path in the header register (13/20), with the
// hierarchy carried by color alone: the section in text 3 and the
// current screen in text 1, behind text-4 separators. The search field
// is at the right, a pill which opens the search palette, as ⌘K
// (Ctrl+K) does from anywhere, and the operator's account avatar is at
// the far right. A screen that draws its own identity in
// the header (a workload's detail) gets a slot in place of the path:
// identity receives it. Where the sidebar is a sheet, the header leads
// with the control that opens it.
export function PageHeader({
	crumbs,
	identity,
}: {
	crumbs: Crumb[];
	identity?: (slot: HTMLElement | null) => void;
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
		<header className="flex h-header-strip shrink-0 items-center gap-2.5 border-b border-hairline bg-canvas pr-4 pl-[13px] type-header-register max-md:pl-2">
			<SidebarTrigger compact />
			<span className="flex shrink-0 items-center gap-2.5">
				<LogoMark />
				<Wordmark className="mr-0.5 max-sm:hidden" />
			</span>
			<span className="text-disabled max-sm:hidden" aria-hidden="true">
				/
			</span>
			{identity ? (
				<div ref={identity} className="flex min-w-0 flex-1 items-center" />
			) : (
				<nav
					aria-label="Breadcrumb"
					className="flex min-w-0 items-center gap-2.5 text-tertiary"
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
									i === crumbs.length - 1 ? "truncate text-primary" : undefined
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
				className="ml-auto flex h-control w-[240px] shrink-0 cursor-pointer items-center gap-2 rounded-full border border-strong bg-field-sunk pr-[9px] pl-2.5 text-tertiary hover:border-control-hover focus-visible:focus-ring max-md:hidden"
			>
				<Icon name="search" />
				<span className="min-w-0 flex-1 truncate text-left type-control">
					Search workloads and rulesets…
				</span>
				<Kbd className="border-0 px-0">⌘K</Kbd>
			</button>
			<span className="flex shrink-0 max-md:ml-auto">
				<AccountPopover />
			</span>
			<Palette open={searching} onOpenChange={setSearching} />
		</header>
	);
}
