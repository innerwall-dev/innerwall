import { NavLink } from "react-router";
import { cn } from "@/lib/utils";

// UnderlineTab is a screen's tab: the active one carries a text-primary
// rule under it (location, so never blue). A count is shown only when it is known.
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
					"-mb-px flex h-10 items-center border-b-2 px-3 type-ui",
					isActive
						? "border-(--text-primary) font-medium text-primary"
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

export function TabList({ children }: { children: React.ReactNode }) {
	return (
		<div
			className="flex shrink-0 gap-0.5 border-b border-default px-6"
			role="tablist"
		>
			{children}
		</div>
	);
}
