import { EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";

const railHeading =
	"text-[11px] uppercase tracking-[0.05em] text-muted-foreground";

// Fresh-install state of policy (design screen 19): the ruleset rail
// with nothing in it, the definitions below it, and the invitation to
// author the first ruleset. The editor arrives with the write screens;
// until then its actions are present and inert.
export function Policy() {
	return (
		<div className="flex min-h-0 flex-1">
			<aside
				className="flex w-[220px] shrink-0 flex-col gap-0.5 overflow-auto border-r border-border bg-surface-sidebar px-2.5 py-3.5"
				aria-label="Rulesets"
			>
				<div className="flex items-center px-2 pb-2">
					<span className={railHeading}>Rulesets</span>
					<button
						type="button"
						className="ml-auto text-[11px] text-link hover:text-link-hover"
						aria-disabled="true"
						title="The policy editor arrives with the write screens"
					>
						+ new
					</button>
				</div>
				<p className="p-2 text-[12px] text-muted-foreground">
					No rulesets yet.
				</p>
				<div className={`${railHeading} px-2 pt-3.5 pb-1.5`}>Definitions</div>
				<Definition label="Service definitions" />
				<Definition label="Address groups" />
			</aside>
			<div className="flex min-w-0 flex-1 flex-col gap-[18px] overflow-auto px-6 pt-5 pb-8">
				<EmptyState
					title="Create your first ruleset"
					width={560}
					className="mx-auto my-10"
					actions={
						<Button
							className="self-start"
							aria-disabled="true"
							title="The policy editor arrives with the write screens"
						>
							New ruleset
						</Button>
					}
				>
					A ruleset has a scope (which workloads it protects) and inbound rules
					(who may reach them, on what). New rules render immediately, but
					workloads in visibility mode ignore policy — nothing is blocked until
					you switch a workload to simulation, then enforced.
				</EmptyState>
			</div>
		</div>
	);
}

function Definition({ label }: { label: string }) {
	return (
		<div className="flex gap-2 rounded-chip px-2 py-1.5 text-[12.5px] text-foreground-secondary">
			<span className="flex-1">{label}</span>
			<span className="font-mono text-[11px] text-muted-foreground">0</span>
		</div>
	);
}
