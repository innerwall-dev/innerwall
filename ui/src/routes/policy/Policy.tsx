import { useEffect } from "react";
import { Link, useSearchParams } from "react-router";
import type { Ruleset } from "@/api/schema";
import { EmptyState } from "@/components/EmptyState";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { Button } from "@/components/ui/button";
import { useResource } from "@/lib/resource";
import { useShell } from "@/shell/Shell";
import { loadEditor } from "./data";
import { NewRuleset } from "./NewRuleset";
import { Rail } from "./Rail";
import { RulesetEditor } from "./RulesetEditor";

// Policy is the policy editor (design 12): the rulesets down the rail,
// and the ruleset the address names, edited live. There is no draft and
// nothing staged: every save is a write the control plane admits or
// refuses at once, conditioned on the version the editor read. What the
// address holds is the ruleset shown (`ruleset`) or the new one being
// started (`new`), so a link can open the editor at either.
export function Policy() {
	const [params, setParams] = useSearchParams();
	const name = params.get("ruleset");
	const creating = params.has("new");
	const shell = useShell();
	const { resource, reload } = useResource(loadEditor, []);
	const data = resource.status === "ready" ? resource.data : null;
	const current: Ruleset | null =
		data && !creating
			? (data.rulesets.find((rs) => rs.name === name) ??
				data.rulesets[0] ??
				null)
			: null;

	// The address names what is shown: the first ruleset when none was
	// named, or when the one named no longer exists.
	useEffect(() => {
		if (current && current.name !== name) {
			setParams({ ruleset: current.name }, { replace: true });
		}
	}, [current, name, setParams]);

	return (
		<div className="flex min-h-0 flex-1">
			<Rail
				rulesets={data?.rulesets ?? null}
				current={current?.name ?? null}
				creating={creating}
				services={data?.services.length ?? null}
				groups={data?.groups.length ?? null}
			/>
			<div className="flex min-w-0 flex-1 flex-col gap-5 overflow-auto px-6 pt-5 pb-8">
				{resource.status === "loading" ? (
					<LoadingRow what="the policy" />
				) : resource.status === "error" ? (
					<ProblemNotice
						what="the policy"
						error={resource.error}
						onRetry={reload}
					/>
				) : creating && data ? (
					<NewRuleset
						data={data}
						onCreated={(rs) => {
							shell.policyChanged();
							reload();
							setParams({ ruleset: rs.name });
						}}
					/>
				) : current && data ? (
					<RulesetEditor
						key={current.id}
						data={data}
						ruleset={current}
						reload={reload}
						onChanged={shell.policyChanged}
					/>
				) : (
					<FirstRuleset />
				)}
			</div>
		</div>
	);
}

// FirstRuleset is the fresh state (design 19): no ruleset yet, and the
// invitation to author the first.
function FirstRuleset() {
	return (
		<EmptyState
			title="Create your first ruleset"
			width={560}
			className="mx-auto my-10"
			actions={
				<Button className="self-start" asChild>
					<Link to="/policy?new=1">New ruleset</Link>
				</Button>
			}
		>
			A ruleset has a scope (which workloads it protects) and inbound rules (who
			may reach them, on what). New rules render immediately, but workloads in
			visibility mode ignore policy — nothing is blocked until you switch a
			workload to simulation, then enforced.
		</EmptyState>
	);
}
