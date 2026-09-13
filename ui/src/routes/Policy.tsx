import { EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";

// Fresh-install state of policy (design screen 19): the ruleset rail
// with nothing in it, the definitions below it, and the invitation to
// author the first ruleset. The editor arrives with the write screens;
// until then the action is present and inert.
export function Policy() {
	return (
		<div className="flex min-h-0 flex-1">
			<aside
				className="flex w-[220px] shrink-0 flex-col border-r border-border px-5 pt-5"
				aria-label="Rulesets"
			>
				<div className="flex items-center justify-between">
					<Label>Rulesets</Label>
					<button
						type="button"
						className="text-[12px] text-link hover:text-link-hover"
						aria-disabled="true"
						title="The policy editor arrives with the write screens"
					>
						+ new
					</button>
				</div>
				<p className="mt-4 text-[13px] text-foreground-tertiary">
					No rulesets yet.
				</p>
				<Label className="mt-8">Definitions</Label>
				<ul className="mt-3 flex flex-col gap-3 text-[13px]">
					<li className="flex items-center justify-between">
						<span>Service definitions</span>
						<span className="font-mono text-[11px] text-muted-foreground">
							0
						</span>
					</li>
					<li className="flex items-center justify-between">
						<span>Address groups</span>
						<span className="font-mono text-[11px] text-muted-foreground">
							0
						</span>
					</li>
				</ul>
			</aside>
			<EmptyState
				title="Create your first ruleset"
				className="items-start pt-[60px]"
				width={560}
				actions={
					<Button
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
	);
}
