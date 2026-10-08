import { Fragment, type ReactNode, useState } from "react";
import { Link } from "react-router";
import type { Finding, Selector } from "@/api/schema";
import { Eyebrow, modes, syncStates } from "@/components/fleet/status";
import { Icon } from "@/components/Icon";
import { count } from "@/lib/format";
import { parseRequirements, shownLabelText } from "@/lib/labels";
import type { Resource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { AddInput, FindingLines } from "./Chips";
import {
	type MatchedHost,
	matchedHosts,
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
			className="flex flex-col gap-3 rounded-lg border border-default bg-app p-4"
		>
			<Eyebrow>Scope — workloads this ruleset protects</Eyebrow>
			<div className="flex flex-wrap gap-6">
				<div className="flex min-w-[320px] flex-1 flex-col gap-2">
					<div className="flex flex-wrap items-center gap-2">
						{keys.map((k, i) => (
							<Fragment key={k}>
								{i > 0 ? (
									<span className="type-label text-tertiary">AND</span>
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
							<span className="type-label text-tertiary">AND</span>
						) : null}
						<AddInput
							label="Add a scope requirement"
							placeholder="key = value | value"
							onAdd={(text) => {
								// A paste of several requirements adds each; text
								// outside the label grammar adds nothing.
								const parsed = parseRequirements(text, { alternatives: true });
								if (!parsed.ok) return parsed.error;
								let next = scope;
								for (const r of parsed.requirements) {
									next = withRequirement(next, r.key, r.values);
								}
								onChange(next);
								return undefined;
							}}
						/>
					</div>
					{keys.map((k) => (
						<FindingLines key={k} findings={findings.keys[k]} />
					))}
					<FindingLines findings={findings.whole} />
					<p className="type-caption text-tertiary">
						Keys are ANDed; several values for one key are ORed. An empty scope
						matches nothing.
					</p>
				</div>
				<div className="flex w-[300px] shrink-0 flex-col gap-1.5 border-l border-subtle pl-5">
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
				"inline-flex h-6 items-center gap-1.5 rounded-sm border bg-subtle px-1.5 type-mono-sm text-primary",
				failed ? "border-dashed border-status-critical-fg" : "border-strong",
			)}
			data-testid="requirement"
		>
			<span>
				<span className="text-tertiary">
					{shownLabelText(k, "key")} <span>=</span>
				</span>{" "}
				{values.length > 0 ? (
					// A value stored before the label grammar is quoted, so it
					// never reads as several requirements (ADR-0022).
					values.map((v) => shownLabelText(v, "value")).join(" | ")
				) : (
					<span className="text-tertiary">(no values)</span>
				)}
			</span>
			<button
				type="button"
				aria-label={`Remove ${k}`}
				onClick={onRemove}
				className="group/rm -mr-0.5 inline-flex cursor-pointer items-center rounded-sm"
			>
				<Icon name="x" className="size-3.5 group-hover/rm:text-icon-active" />
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
			<p className="type-caption text-tertiary">
				Add a requirement to see what this scope matches.
			</p>
		);
	}
	if (match.status === "loading") {
		return (
			<p className="type-mono-sm text-tertiary" aria-busy="true">
				Matching…
			</p>
		);
	}
	if (match.status === "error") {
		return (
			<p className="type-caption text-tertiary" data-testid="match-count">
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
			[mix.simulation, modes.simulation.glyph, "simulation"],
			[mix.visibility, modes.visibility.glyph, "visibility"],
			[mix.enforced, modes.enforced.glyph, "enforced"],
			[mix.degraded, syncStates.degraded.glyph, "degraded"],
			[mix.offline, syncStates.offline.glyph, "offline"],
			[mix.pending, syncStates.pending.glyph, "pending"],
		] as const
	).filter(([n]) => n > 0);
	return (
		<>
			<div className="flex items-baseline gap-2" data-testid="match-count">
				<span className="type-title-page font-mono tabular-nums">
					{count(m.count)}
				</span>
				<span className="type-ui text-secondary">
					{m.count === 1 ? "workload matches" : "workloads match"}{" "}
					{edited ? "this scope" : "right now"}
				</span>
			</div>
			{parts.length > 0 ? (
				<div
					className="flex flex-wrap gap-x-3 gap-y-1 type-caption text-secondary"
					data-testid="match-mix"
				>
					{parts.map(([n, glyph, label]) => (
						<span key={label} className="inline-flex items-center gap-1.5">
							{glyph}{" "}
							<span>
								{count(n)} {label}
							</span>
						</span>
					))}
				</div>
			) : null}
			{hosts.length > 0 ? (
				<p className="type-mono-sm leading-5 text-tertiary">
					{shown.map((h, i) => (
						<Fragment key={h.id}>
							{i > 0 ? <span className="text-disabled"> · </span> : null}
							<Host h={h} />
						</Fragment>
					))}
					{!all && hosts.length > shownHosts ? (
						<>
							<span className="text-disabled"> · </span>
							<button
								type="button"
								onClick={() => setAll(true)}
								className="cursor-pointer text-link hover:underline"
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
			className="text-link hover:underline"
			title={syncStates[h.issue].label}
		>
			{h.hostname}
		</Link>
	);
}
