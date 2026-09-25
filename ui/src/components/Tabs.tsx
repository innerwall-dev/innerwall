import { NavLink } from "react-router";
import { cn } from "@/lib/utils";

// UnderlineTab is a screen's tab: the active one carries the accent rule
// the design draws under it. A count is shown only when it is known.
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
					"-mb-px border-b-2 px-3 py-2 text-[12.5px]",
					isActive
						? "border-[var(--checkbox-accent)] text-foreground"
						: "border-transparent text-foreground-tertiary hover:text-foreground",
				)
			}
		>
			{label}
			{count !== undefined ? (
				<span className="ml-1.5 font-mono text-muted-foreground">{count}</span>
			) : null}
		</NavLink>
	);
}

export function TabList({ children }: { children: React.ReactNode }) {
	return (
		<div
			className="flex shrink-0 gap-0.5 border-b border-border px-6 pt-3"
			role="tablist"
		>
			{children}
		</div>
	);
}
