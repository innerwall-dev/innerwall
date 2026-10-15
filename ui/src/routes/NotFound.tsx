import { Link, useLocation } from "react-router";
import { EmptyState } from "@/components/EmptyState";
import { page } from "@/components/Page";
import { cn } from "@/lib/utils";

// NotFound is an address the console has no screen for: a mistyped or
// stale link says so, in the form an unknown workload does, rather than
// landing on a real screen that reads as if the link had worked.
export function NotFound() {
	const { pathname } = useLocation();
	return (
		<div className={cn(page, "pt-6")}>
			<EmptyState title="No such page" page width={480}>
				The console has no screen at{" "}
				<span className="type-mono-data break-all">{pathname}</span>.{" "}
				<Link to="/workloads" className="link">
					Back to the fleet
				</Link>
			</EmptyState>
		</div>
	);
}
