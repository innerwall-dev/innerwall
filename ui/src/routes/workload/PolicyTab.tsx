import { Link } from "react-router";
import type { RenderedPolicy, RenderedRule, Workload } from "@/api/schema";
import { verdicts } from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { ago } from "@/lib/format";
import type { Resource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { version } from "../fleet/describe";
import { peersText, portsText, ruleId } from "./rules";

// PolicyTab is the workload's persisted rendered policy: the resolved
// rules its agent is sent, each traced to the authored rule and ruleset
// it came from, with that rule's own instants. The surface persists the
// latest render only, so when the host has not applied it the header
// says which version the host is on and the rules shown are the latest.
export function PolicyTab({
	w,
	policy,
}: {
	w: Workload;
	policy: Resource<RenderedPolicy>;
}) {
	if (policy.status === "loading") return <LoadingRow what="policy" />;
	if (policy.status === "error") {
		return <ProblemNotice what="the rendered policy" error={policy.error} />;
	}
	const p = policy.data;
	const applied = w.sync.applied_version;
	const behind = p.version > applied;

	if (p.version === 0) {
		return (
			<p className="py-6 text-[12px] text-tertiary">
				No policy has been rendered for this workload yet.
			</p>
		);
	}

	return (
		<div>
			<div className="mb-3 flex items-center gap-3 text-[12px]">
				<span className="text-secondary">
					{behind ? "Rendered policy" : "Rendered policy applied on host"}
				</span>
				<span className="rounded-sm border border-strong px-2 py-0.5 font-mono">
					v{p.version}
				</span>
				{behind ? (
					<span className="text-status-warn-fg">
						▲ rendered{p.rendered_at ? ` ${ago(p.rendered_at)}` : ""}, not
						applied —{" "}
						{applied === 0
							? "host has applied none"
							: `host is on ${version(applied)}`}
					</span>
				) : null}
			</div>
			{p.rules.length === 0 ? (
				<p className="py-4 text-[12px] text-tertiary">
					No rule admits traffic to this workload.
				</p>
			) : (
				<div className="flex flex-col gap-2">
					{p.rules.map((r) => (
						<RuleCard key={r.id} r={r} />
					))}
				</div>
			)}
			<p className="mt-3 text-[11px] text-tertiary">
				Agents receive fully resolved rules (concrete peer addresses); selectors
				are resolved by the control plane. Rule ids carry the authored rule for
				provenance. Traffic no rule admits is{" "}
				<span className={verdicts[p.terminal_verdict].text}>
					{verdicts[p.terminal_verdict].glyph}{" "}
					{verdicts[p.terminal_verdict].label}
				</span>{" "}
				in {p.mode}.
			</p>
		</div>
	);
}

function RuleCard({ r }: { r: RenderedRule }) {
	return (
		<article
			className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 rounded-md border border-default bg-subtle px-3.5 py-2.5"
			aria-label={r.description || ruleId(r)}
		>
			<div className="flex items-center gap-2">
				<span className="font-mono text-primary">{ruleId(r)}</span>
				{r.ruleset ? (
					<>
						<span className="text-[11px] text-tertiary">from ruleset</span>
						<Link
							to="/policy"
							className="text-[12px] text-link hover:underline"
						>
							{r.ruleset.name}
						</Link>
					</>
				) : (
					<span className="text-[11px] text-tertiary">
						authored rule since removed
					</span>
				)}
			</div>
			<div className="text-right font-mono">{portsText(r)}</div>
			<div className="text-[12px] text-secondary">{r.description || "—"}</div>
			<div className="text-right font-mono text-[11px] text-tertiary">
				{peersText(r)}
			</div>
			<div
				className={cn("col-span-2 font-mono text-[11px] text-tertiary")}
				data-testid="rule-instants"
			>
				{instants(r)}
			</div>
		</article>
	);
}

// instants is when the authored rule was written and last changed.
function instants(r: RenderedRule): string {
	if (!r.created_at || !r.updated_at) return "authored rule no longer exists";
	if (r.created_at === r.updated_at) return `added ${ago(r.created_at)}`;
	return `changed ${ago(r.updated_at)} · added ${ago(r.created_at)}`;
}
