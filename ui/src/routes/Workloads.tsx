import { Link, NavLink } from "react-router";
import { Centered, EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// Fresh-install state of workloads (design screen 20): the fleet tabs,
// the invitation to mint the first provisioning token, and the tokens
// tab's own empty listing. The fleet table and minting arrive with the
// fleet screens; the mint action is present and inert until then.
export function Workloads({ tab }: { tab: "fleet" | "tokens" }) {
	return (
		<div className="flex min-h-0 flex-1 flex-col">
			<div
				className="flex shrink-0 gap-0.5 border-b border-border px-6 pt-3"
				role="tablist"
			>
				<Tab to="/workloads" label="Workloads" count={0} end />
				<Tab to="/workloads/tokens" label="Provisioning tokens" count={0} />
			</div>
			{tab === "fleet" ? (
				<Centered>
					<EmptyState
						title="No workloads enrolled"
						width={560}
						actions={
							<Button asChild className="self-start">
								<Link to="/workloads/tokens">Mint a provisioning token</Link>
							</Button>
						}
					>
						Workloads appear here when an agent enrolls with a provisioning
						token. The token's label scope becomes the workload's labels.
					</EmptyState>
				</Centered>
			) : (
				<div className="flex flex-1 flex-col gap-[18px] overflow-auto px-6 pt-5 pb-8">
					<div className="flex items-center gap-3">
						<p className="max-w-[620px] text-[12px] text-foreground-tertiary">
							A token enrolls any number of workloads within its label scope
							until it expires or is revoked. The plaintext is shown exactly
							once, at mint; only its hash is stored.
						</p>
						<Button
							className="ml-auto"
							aria-disabled="true"
							title="Minting tokens arrives with the fleet screens"
						>
							Mint token
						</Button>
					</div>
					<p className="py-5 text-[12px] text-muted-foreground">
						No tokens yet. Mint one to enroll your first workload.
					</p>
				</div>
			)}
		</div>
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
					"-mb-px border-b-2 px-3 py-2 text-[12.5px]",
					isActive
						? "border-[var(--checkbox-accent)] text-foreground"
						: "border-transparent text-foreground-tertiary hover:text-foreground",
				)
			}
		>
			{label}
			<span className="ml-1.5 font-mono text-muted-foreground">{count}</span>
		</NavLink>
	);
}
