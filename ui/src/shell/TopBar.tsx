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
		<header className="flex h-11 shrink-0 items-center gap-4 border-b border-border bg-surface-sidebar pr-4 pl-5">
			<nav
				aria-label="Breadcrumb"
				className="flex min-w-0 items-center gap-2 text-[13px]"
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
								i === crumbs.length - 1
									? "truncate text-foreground"
									: "text-foreground-tertiary"
							}
						>
							{c.label}
						</span>
					</Fragment>
				))}
			</nav>
			<div className="ml-auto flex items-center gap-3">
				<label className="flex h-8 w-[260px] items-center gap-1.5 rounded border border-input bg-card px-2.5 text-muted-foreground">
					<span className="font-mono text-[12px]" aria-hidden="true">
						⌕
					</span>
					<input
						type="search"
						placeholder="Search workloads, labels, rules…"
						aria-label="Search"
						disabled
						title="Search arrives with the read screens"
						className="min-w-0 flex-1 bg-transparent text-[12px] text-foreground placeholder:text-muted-foreground focus:outline-none disabled:cursor-default"
					/>
					<Kbd>⌘K</Kbd>
				</label>
				<AccountPopover />
			</div>
		</header>
	);
}
