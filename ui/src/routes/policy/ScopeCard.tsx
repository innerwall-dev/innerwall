import { Fragment, type ReactNode, useState } from "react";
import { Link } from "react-router";
import type { Finding, Selector } from "@/api/schema";
import { Eyebrow, modes, syncStates } from "@/components/fleet/status";
import { count } from "@/lib/format";
import type { Resource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { AddInput, FindingLines } from "./Chips";
import {
	type MatchedHost,
	matchedHosts,
	parseRequirement,
	type ScopeMatch,
	scopeMix,
	withoutKey,
	withRequirement,
} from "./model";

// The hosts the card lists before the rest fold behind "+N more".
const shownHosts = 5;

// ScopeCard is the ruleset's scope as requirements the operator edits in
// place, and beside it what the scope matches right now: the control
// plane's count, how the matched workloads divide by mode and by what
// keeps any from its latest policy, and the first of them by name.
export function ScopeCard({
	scope,
	edited,
	match,
	findings,
	onChange,
	footer,
}: {
	scope: Selector;
	// edited: the scope shown differs from the one stored.
	edited: boolean;
	match: Resource<ScopeMatch> | null;
	findings: { keys: Record<string, Finding[]>; whole: Finding[] };
	onChange: (scope: Selector) => void;
	footer?: ReactNode;
}) {
	const keys = Object.keys(scope).sort();
	return (
		<section
			aria-label="Scope"
			className="flex flex-col gap-3 rounded border border-border bg-surface-sidebar px-4 py-3.5"
		>
			<Eyebrow>Scope — workloads this ruleset protects</Eyebrow>
			<div className="flex flex-wrap gap-6">
				<div className="flex min-w-[320px] flex-1 flex-col gap-2">
					<div className="flex flex-wrap items-center gap-2">
						{keys.map((k, i) => (
							<Fragment key={k}>
								{i > 0 ? (
									<span className="text-[11px] text-muted-foreground">AND</span>
								) : null}
								<Requirement
									k={k}
									values={scope[k] ?? []}
									failed={(findings.keys[k]?.length ?? 0) > 0}
									onRemove={() => onChange(withoutKey(scope, k))}
								/>
							</Fragment>
						))}
						{keys.length > 0 ? (
							<span className="text-[11px] text-muted-foreground">AND</span>
						) : null}
						<AddInput
							label="Add a scope requirement"
							placeholder="key = value | value"
							onAdd={(text) => {
								const [k, vs] = parseRequirement(text);
								onChange(withRequirement(scope, k, vs));
							}}
						/>
					</div>
					{keys.map((k) => (
						<FindingLines key={k} findings={findings.keys[k]} />
					))}
					<FindingLines findings={findings.whole} />
					<p className="text-[12px] text-muted-foreground">
						Keys are ANDed; several values for one key are ORed. An empty scope
						matches nothing.
					</p>
				</div>
				<div className="flex w-[300px] shrink-0 flex-col gap-1.5 border-l border-border pl-5">
					<Matches match={match} edited={edited} />
				</div>
			</div>
			{footer}
		</section>
	);
}

function Requirement({
	k,
	values,
	failed,
	onRemove,
}: {
	k: string;
	values: string[];
	failed: boolean;
	onRemove: () => void;
}) {
	return (
		<span
			className={cn(
				"inline-flex items-center gap-2 rounded border bg-card px-2.5 py-1 font-mono text-[12.5px]",
				failed ? "border-dashed border-destructive" : "border-input-strong",
			)}
			data-testid="requirement"
		>
			<span>
				{k} <span className="text-muted-foreground">=</span>{" "}
				{values.length > 0 ? (
					values.join(" | ")
				) : (
					<span className="text-muted-foreground">(no values)</span>
				)}
			</span>
			<button
				type="button"
				aria-label={`Remove ${k}`}
				onClick={onRemove}
				className="cursor-pointer text-muted-foreground hover:text-foreground"
			>
				×
			</button>
		</span>
	);
}

function Matches({
	match,
	edited,
}: {
	match: Resource<ScopeMatch> | null;
	edited: boolean;
}) {
	const [all, setAll] = useState(false);
	if (match === null) {
		return (
			<p className="text-[12px] text-muted-foreground">
				Add a requirement to see what this scope matches.
			</p>
		);
	}
	if (match.status === "loading") {
		return (
			<p
				className="font-mono text-[12px] text-muted-foreground"
				aria-busy="true"
			>
				Matching…
			</p>
		);
	}
	if (match.status === "error") {
		return (
			<p
				className="text-[12px] text-muted-foreground"
				data-testid="match-count"
			>
				{edited
					? "This scope matches nothing as written."
					: `Could not resolve the scope: ${match.error.problem.detail ?? match.error.problem.title}`}
			</p>
		);
	}
	const m = match.data;
	const mix = scopeMix(m);
	const hosts = matchedHosts(m);
	const shown = all ? hosts : hosts.slice(0, shownHosts);
	const parts = (
		[
			[
				mix.simulation,
				modes.simulation.glyph,
				"simulation",
				modes.simulation.cls,
			],
			[
				mix.visibility,
				modes.visibility.glyph,
				"visibility",
				modes.visibility.cls,
			],
			[mix.enforced, modes.enforced.glyph, "enforced", modes.enforced.cls],
			[
				mix.degraded,
				syncStates.degraded.glyph,
				"degraded",
				syncStates.degraded.cls,
			],
			[
				mix.offline,
				syncStates.offline.glyph,
				"offline",
				syncStates.offline.cls,
			],
			[
				mix.pending,
				syncStates.pending.glyph,
				"pending",
				syncStates.pending.cls,
			],
		] as const
	).filter(([n]) => n > 0);
	return (
		<>
			<div className="flex items-baseline gap-2" data-testid="match-count">
				<span className="font-mono text-[20px]">{count(m.count)}</span>
				<span className="text-[13px] text-foreground-tertiary">
					{m.count === 1 ? "workload matches" : "workloads match"}{" "}
					{edited ? "this scope" : "right now"}
				</span>
			</div>
			{parts.length > 0 ? (
				<div
					className="flex flex-wrap gap-x-3 gap-y-1 text-[12px] text-foreground-tertiary"
					data-testid="match-mix"
				>
					{parts.map(([n, glyph, label, cls]) => (
						<span key={label}>
							<span aria-hidden="true" className={cls.split(" ")[0]}>
								{glyph}
							</span>{" "}
							{count(n)} {label}
						</span>
					))}
				</div>
			) : null}
			{hosts.length > 0 ? (
				<p className="font-mono text-[12px] leading-[1.6] text-muted-foreground">
					{shown.map((h, i) => (
						<Fragment key={h.id}>
							{i > 0 ? (
								<span className="text-foreground-separator"> · </span>
							) : null}
							<Host h={h} />
						</Fragment>
					))}
					{!all && hosts.length > shownHosts ? (
						<>
							<span className="text-foreground-separator"> · </span>
							<button
								type="button"
								onClick={() => setAll(true)}
								className="cursor-pointer text-link hover:text-link-hover"
							>
								+{count(hosts.length - shownHosts)} more
							</button>
						</>
					) : null}
				</p>
			) : null}
		</>
	);
}

// Host is one matched workload; one needing attention links to its
// detail, where its state is explained.
function Host({ h }: { h: MatchedHost }) {
	if (!h.issue) return <span>{h.hostname}</span>;
	return (
		<Link
			to={`/workloads/${encodeURIComponent(h.id)}`}
			className="text-link hover:text-link-hover"
			title={syncStates[h.issue].label}
		>
			{h.hostname}
		</Link>
	);
}
