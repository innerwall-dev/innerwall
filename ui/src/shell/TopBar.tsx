import { Fragment } from "react";
import { Kbd } from "@/components/ui/kbd";
import { AccountPopover } from "./AccountPopover";

export interface Crumb {
	label: string;
}

// The top bar: the breadcrumb of the current screen, the search field
// (a placeholder until search exists; the shortcut hint is the design's),
// and the account popover.
export function TopBar({ crumbs }: { crumbs: Crumb[] }) {
	return (
		<header className="flex h-11 shrink-0 items-center gap-3 border-b border-border bg-surface-sidebar px-5">
			<nav
				aria-label="Breadcrumb"
				className="flex min-w-0 items-center gap-1.5 text-[12px] text-foreground-tertiary"
			>
				{crumbs.map((c, i) => (
					<Fragment key={c.label}>
						{i > 0 ? (
							<span className="text-foreground-separator" aria-hidden="true">
								/
							</span>
						) : null}
						<span
							className={
								i === crumbs.length - 1 ? "truncate text-foreground" : undefined
							}
						>
							{c.label}
						</span>
					</Fragment>
				))}
			</nav>
			<label className="ml-auto flex w-[260px] items-center gap-2 rounded border border-input px-2.5 py-[5px] text-[12px] text-muted-foreground">
				<span className="font-mono" aria-hidden="true">
					⌕
				</span>
				<input
					type="search"
					placeholder="Search workloads, labels, rules…"
					aria-label="Search"
					disabled
					title="Search arrives with the read screens"
					className="min-w-0 flex-1 bg-transparent p-0 text-[12px] text-foreground placeholder:text-muted-foreground focus:outline-none disabled:cursor-default"
				/>
				<Kbd>⌘K</Kbd>
			</label>
			<AccountPopover />
		</header>
	);
}
