import { NavLink } from "react-router";
import { cn } from "@/lib/utils";

// UnderlineTab is a screen's tab (13/20): text 3 at rest and text 1 on
// hover; the active one is text 1 over a 1.5px text-1 underline that
// overlaps the row's rule. The underline is neutral, never the accent:
// selection is neutral in v4. A count is shown only when it is known.
export function UnderlineTab({
	to,
	label,
	count,
	end,
}: {
	to: string;
	label: string;
	count?: number;
	end?: boolean;
}) {
	return (
		<NavLink
			to={to}
			end={end}
			role="tab"
			className={({ isActive }) =>
				cn(
					"flex h-tab-row shrink-0 items-center gap-2 type-header-register focus-visible:rounded-inner focus-visible:focus-ring-inset",
					isActive
						? "text-primary shadow-[inset_0_-1.5px_0_var(--text-primary)]"
						: "text-tertiary hover:text-primary",
				)
			}
		>
			{label}
			{count !== undefined ? (
				<span className="type-caption text-tertiary">{count}</span>
			) : null}
		</NavLink>
	);
}

// TabList is the tab row: 34px, the page gutter on either side, 24px
// between tabs, on the subtle rule. Its right side is reserved for the
// state chips (aside); there is no further chrome row.
export function TabList({
	children,
	aside,
}: {
	children: React.ReactNode;
	aside?: React.ReactNode;
}) {
	return (
		<div className="flex h-tab-row shrink-0 items-center gap-6 border-b border-hairline px-8">
			<div className="flex h-full min-w-0 items-center gap-6" role="tablist">
				{children}
			</div>
			{aside ? (
				<div className="flex min-w-0 flex-1 items-center justify-end">
					{aside}
				</div>
			) : null}
		</div>
	);
}
