import { NavLink } from "react-router";
import { EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// Fresh-install state of workloads (design screen 20): the fleet tabs
// and the invitation to mint the first provisioning token. The fleet
// table and the token screens arrive with the read screens; the mint
// action is present and inert until then.
export function Workloads({ tab }: { tab: "fleet" | "tokens" }) {
	return (
		<div className="flex min-h-0 flex-1 flex-col">
			<div
				className="flex h-[46px] shrink-0 items-end gap-1 border-b border-border px-4"
				role="tablist"
			>
				<Tab to="/workloads" label="Workloads" count={0} end />
				<Tab to="/workloads/tokens" label="Provisioning tokens" count={0} />
			</div>
			{tab === "fleet" ? (
				<EmptyState
					title="No workloads enrolled"
					actions={<MintButton />}
					width={560}
				>
					Workloads appear here when an agent enrolls with a provisioning token.
					The token's label scope becomes the workload's labels.
				</EmptyState>
			) : (
				<EmptyState title="No provisioning tokens" actions={<MintButton />}>
					A provisioning token carries the labels an enrolling agent receives.
					Mint one, run the installer on a host with it, and the workload
					appears in the fleet.
				</EmptyState>
			)}
		</div>
	);
}

function MintButton() {
	return (
		<Button
			aria-disabled="true"
			title="Minting tokens arrives with the fleet screens"
		>
			Mint a provisioning token
		</Button>
	);
}

function Tab({
	to,
	label,
	count,
	end,
}: {
	to: string;
	label: string;
	count: number;
	end?: boolean;
}) {
	return (
		<NavLink
			to={to}
			end={end}
			role="tab"
			className={({ isActive }) =>
				cn(
					"-mb-px flex h-9 items-center gap-2 border-b-2 px-3 text-[13px]",
					isActive
						? "border-primary text-foreground"
						: "border-transparent text-foreground-tertiary hover:text-foreground",
				)
			}
		>
			<span>{label}</span>
			<span className="font-mono text-[11px] text-muted-foreground">
				{count}
			</span>
		</NavLink>
	);
}
