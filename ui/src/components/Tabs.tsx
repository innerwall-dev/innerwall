import { NavLink } from "react-router";
import { cn } from "@/lib/utils";

// UnderlineTab is a screen's tab, in the header register (13/20 at 500):
// text-secondary at rest and text-primary on hover; the active one is
// text-primary over a 2px text-primary underline that overlaps the row's
// hairline. The underline is neutral, never the accent: the accent pill
// marks primary navigation only. A count is shown only when it is known.
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
					"-mb-px flex h-tab-row shrink-0 items-center border-b-2 px-0.5 type-header-register focus-visible:rounded-inner focus-visible:focus-ring-inset",
					isActive
						? "border-(--text-primary) text-primary"
						: "border-transparent text-secondary hover:text-primary",
				)
			}
		>
			{label}
			{count !== undefined ? (
				<span className="ml-1.5 type-mono-xs text-tertiary">{count}</span>
			) : null}
		</NavLink>
	);
}

// TabList is the tab row: 40px, the page gutter on either side, 20px
// between tabs, on a bottom hairline. Its right side is reserved for the
// state chips (aside); there is no further chrome row.
export function TabList({
	children,
	aside,
}: {
	children: React.ReactNode;
	aside?: React.ReactNode;
}) {
	return (
		<div className="flex h-tab-row shrink-0 items-center gap-5 border-b border-hairline px-5">
			<div className="flex h-full min-w-0 items-center gap-5" role="tablist">
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
