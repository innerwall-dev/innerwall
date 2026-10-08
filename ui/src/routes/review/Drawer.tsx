import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import type { ProblemError } from "@/api/client";
import { getRollup, listFlows } from "@/api/fleet";
import { listServices, putRule } from "@/api/policy";
import {
	type AddressGroup,
	type Entry,
	type FlowsPage,
	ProblemType,
	type RollupGroup,
	type Rule,
	type Ruleset,
} from "@/api/schema";
import { Eyebrow, VerdictPill, verdicts } from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { Button } from "@/components/ui/button";
import { count, shortId, since } from "@/lib/format";
import { asProblem, useResource, useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { peerName } from "../map/Drawer";
import { peerKey } from "../map/model";
import { editorPath } from "../policy/link";
import { rowLimit } from "./data";
import { type ReviewRow, scopeRequirements, scopeText } from "./model";

const inert = "Address groups have no screen in this version";

// ReviewDrawer is one row expanded: what it is, the rule that admitted
// it or the fact that none did, the disabled rules of the ruleset an
// operator may enable, and the workload pairs behind it, each opening
// its stored windows.
export function ReviewDrawer({
	row,
	ruleset,
	rulesets,
	addressGroups,
	from,
	to,
	onClose,
	onChanged,
}: {
	row: ReviewRow;
	ruleset: Ruleset;
	rulesets: Ruleset[];
	addressGroups: AddressGroup[];
	from: string;
	to: string;
	onClose: () => void;
	onChanged: () => void;
}) {
	const scope = useMemo(() => scopeRequirements(ruleset.scope), [ruleset]);
	const keys = useMemo(() => new Set(row.members.map((m) => m.key)), [row]);
	// The pairs behind the row: the source-by-destination rollup of the
	// scope under the row's decision, on the row's service, restricted to
	// the row's peers.
	const { resource: pairs, reload } = useResource(
		() =>
			getRollup({
				group_by: "src,dst",
				verdict: row.verdict,
				service: row.service,
				label: scope,
				from,
				to,
				limit: rowLimit,
			}).then((r) => ({
				truncated: r.truncated,
				groups: r.groups.filter(
					(g) => g.keys.src && keys.has(peerKey(g.keys.src)),
				),
			})),
		[row.id, from, to],
	);
	const exact =
		pairs.status === "ready" && !pairs.data.truncated
			? new Set(pairs.data.groups.map((g) => g.keys.dst?.id)).size
			: null;

	return (
		<aside
			aria-label="Pair detail"
			className="flex w-[400px] shrink-0 flex-col overflow-hidden border-l border-default bg-subtle"
		>
			<div className="flex flex-col gap-2 border-b border-default px-4 py-3.5">
				<div className="flex items-center gap-2">
					<VerdictPill verdict={row.verdict} />
					<button
						type="button"
						aria-label="Close"
						onClick={onClose}
						className="ml-auto cursor-pointer text-[16px] leading-none text-tertiary hover:text-primary"
					>
						×
					</button>
				</div>
				<div className="font-mono text-[13px] break-words">
					{row.peer.full ?? row.peer.title}{" "}
					<span className="text-disabled" aria-hidden="true">
						→
					</span>
					<span className="sr-only">on</span> {row.service}
				</div>
				<p className="text-[12px] text-secondary" data-testid="explain">
					{explain(row, exact)}
				</p>
				<div className="flex flex-wrap gap-1.5">
					{row.peer.kind === "address" ? (
						<InertAction label="Add to an address group" />
					) : null}
					<Button variant="secondary" size="sm" className="rounded-sm" asChild>
						<Link to={editorPath(ruleset.name)}>Open in policy editor</Link>
					</Button>
				</div>
			</div>
			<div className="min-h-0 flex-1 overflow-auto">
				<RuleSection
					row={row}
					ruleset={ruleset}
					rulesets={rulesets}
					addressGroups={addressGroups}
					scope={scope}
					keys={keys}
					from={from}
					to={to}
					onChanged={onChanged}
				/>
				<div className="px-4 pt-2.5 pb-1">
					<Eyebrow>
						Flows ·{" "}
						<span className="font-mono">
							{pairs.status === "ready" ? pairs.data.groups.length : "…"}
						</span>{" "}
						{pairs.status === "ready" && pairs.data.groups.length === 1
							? "pair"
							: "pairs"}
					</Eyebrow>
				</div>
				<div className="px-4 pb-4">
					{pairs.status === "loading" ? (
						<LoadingRow what="the pairs" />
					) : pairs.status === "error" ? (
						<ProblemNotice
							what="the pairs"
							error={pairs.error}
							onRetry={reload}
						/>
					) : (
						<Pairs groups={pairs.data.groups} row={row} from={from} to={to} />
					)}
				</div>
			</div>
		</aside>
	);
}

function InertAction({ label }: { label: string }) {
	return (
		<Button
			variant="secondary"
			size="sm"
			aria-disabled="true"
			title={inert}
			className="rounded-sm"
			onClick={(ev) => ev.preventDefault()}
		>
			{label}
		</Button>
	);
}

// explain says what the row is in a sentence, from its facts alone.
export function explain(row: ReviewRow, exact: number | null): string {
	const who =
		row.peer.kind === "workloads" || row.peer.kind === "unlabeled"
			? `${row.members.length === 1 ? "the workload" : `the ${row.members.length} workloads`} ${row.peer.kind === "unlabeled" ? "with no labels" : `labeled ${row.peer.full ?? row.peer.title}`}`
			: row.peer.kind === "group"
				? `address group ${row.peer.title}`
				: row.peer.kind === "address"
					? `${row.peer.title}, which no workload has and no address group contains`
					: `the unrecognized stored peer ${row.peer.title}`;
	const n = exact ?? row.workloads;
	const reach = `${n}${exact === null && !row.workloadsExact ? " or more" : ""} ${n === 1 ? "workload" : "workloads"} in scope`;
	return row.verdict === "would_block"
		? `Traffic from ${who} to ${row.service} matched no enabled rule, so enforcing this scope would drop it. It reached ${reach}, last ${since(row.lastSeen)} ago.`
		: `Traffic from ${who} to ${row.service} matched an enabled rule; enforcing leaves it flowing. It reached ${reach}, last ${since(row.lastSeen)} ago.`;
}

// --- the rule ----------------------------------------------------------------

function RuleSection({
	row,
	ruleset,
	rulesets,
	addressGroups,
	scope,
	keys,
	from,
	to,
	onChanged,
}: {
	row: ReviewRow;
	ruleset: Ruleset;
	rulesets: Ruleset[];
	addressGroups: AddressGroup[];
	scope: string[];
	keys: ReadonlySet<string>;
	from: string;
	to: string;
	onChanged: () => void;
}) {
	return (
		<div className="flex flex-col gap-2 border-b border-default px-4 py-3">
			<Eyebrow>Rule</Eyebrow>
			{row.verdict === "allowed" ? (
				<MatchedRules
					row={row}
					rulesets={rulesets}
					scope={scope}
					keys={keys}
					from={from}
					to={to}
				/>
			) : (
				<>
					<p
						className="text-[12px] text-secondary"
						data-testid="rule-statement"
					>
						No enabled rule matched: nothing in the policy these workloads run
						admits this peer on {row.service}.
					</p>
					<Candidates
						ruleset={ruleset}
						addressGroups={addressGroups}
						onChanged={onChanged}
					/>
				</>
			)}
		</div>
	);
}

// MatchedRules names the rules that admitted an allowed row's traffic:
// the rule-by-peer rollup on the row's service, restricted to its peers,
// resolved to the authored rules the listing holds.
function MatchedRules({
	row,
	rulesets,
	scope,
	keys,
	from,
	to,
}: {
	row: ReviewRow;
	rulesets: Ruleset[];
	scope: string[];
	keys: ReadonlySet<string>;
	from: string;
	to: string;
}) {
	const { resource, reload } = useResource(
		() =>
			getRollup({
				group_by: "rule,peer",
				verdict: "allowed",
				service: row.service,
				label: scope,
				from,
				to,
				limit: rowLimit,
			}),
		[row.id, from, to],
	);
	if (resource.status === "loading") return <LoadingRow what="the rule" />;
	if (resource.status === "error") {
		return (
			<ProblemNotice what="the rule" error={resource.error} onRetry={reload} />
		);
	}
	const authored = new Set<string>();
	for (const g of resource.data.groups) {
		if (g.keys.peer && keys.has(peerKey(g.keys.peer)) && g.keys.rule) {
			authored.add(g.keys.rule.authored_rule_id ?? g.keys.rule.id);
		}
	}
	const found = [...authored].map((id) => {
		for (const rs of rulesets) {
			const rule = rs.rules.find((r) => r.id === id);
			if (rule) return { id, rule, ruleset: rs };
		}
		return { id, rule: null, ruleset: null };
	});
	if (found.length === 0) {
		return (
			<p className="text-[12px] text-secondary">
				The windows name no rule this listing holds.
			</p>
		);
	}
	return (
		<ul
			className="flex flex-col gap-1 text-[12px]"
			data-testid="rule-statement"
		>
			{found.map((f) => (
				<li key={f.id}>
					{f.rule && f.ruleset ? (
						<>
							Matched{" "}
							<span className="font-mono text-primary">{ruleName(f.rule)}</span>{" "}
							in <span className="font-mono">{f.ruleset.name}</span>
						</>
					) : (
						<>
							Matched rule <span className="font-mono">{shortId(f.id)}</span>,
							which no longer exists
						</>
					)}
				</li>
			))}
		</ul>
	);
}

export function ruleName(r: Rule): string {
	return r.description || shortId(r.id ?? "");
}

function entryText(e: Entry): string {
	const ports = e.ports ?? [];
	if (ports.length === 0)
		return e.protocol === "icmp" ? "icmp" : `${e.protocol} (every port)`;
	return ports.map((p) => `${e.protocol}/${p}`).join(", ");
}

type Outcome =
	| { rule: string; kind: "enabled" }
	| { rule: string; kind: "stale"; current?: string }
	| { rule: string; kind: "failed"; message: string };

// Candidates are the ruleset's disabled rules, offered to enable. The
// console does not decide whether one covers the row's traffic: that is
// the policy's own evaluation, which only rendering performs. Enabling
// is conditioned on the version the review read, so a rule changed since
// is refused with its current version and never overwritten.
function Candidates({
	ruleset,
	addressGroups,
	onChanged,
}: {
	ruleset: Ruleset;
	addressGroups: AddressGroup[];
	onChanged: () => void;
}) {
	const write = useWrite();
	const disabled = ruleset.rules.filter((r) => r.enabled === false);
	const { resource: services } = useResource(
		() =>
			disabled.length > 0 ? listServices() : Promise.resolve({ services: [] }),
		[disabled.length > 0],
	);
	const [busy, setBusy] = useState<string | null>(null);
	const [outcome, setOutcome] = useState<Outcome | null>(null);
	const groupName = (id: string) =>
		addressGroups.find((g) => g.id === id)?.name ??
		`address group ${shortId(id)}`;
	const serviceNameOf = (id: string) =>
		services.status === "ready"
			? (services.data.services.find((s) => s.id === id)?.name ?? shortId(id))
			: shortId(id);

	async function enable(rule: Rule) {
		const id = rule.id ?? "";
		setBusy(id);
		setOutcome(null);
		try {
			await write(() => putRule(ruleset.id ?? "", rule, { enabled: true }));
			setOutcome({ rule: ruleName(rule), kind: "enabled" });
			onChanged();
		} catch (err) {
			const p = err as ProblemError;
			setOutcome(
				p.type === ProblemType.preconditionFailed
					? {
							rule: ruleName(rule),
							kind: "stale",
							current: p.problem.current_version,
						}
					: {
							rule: ruleName(rule),
							kind: "failed",
							message:
								p.problem.errors?.map((f) => f.message).join(" ") ||
								p.problem.detail ||
								p.problem.title,
						},
			);
		} finally {
			setBusy(null);
		}
	}

	return (
		<div className="flex flex-col gap-2">
			{outcome?.kind === "enabled" ? (
				<p role="status" className="text-[12px] text-flow-allowed">
					<span aria-hidden="true">✓</span> Enabled{" "}
					<span className="font-mono">{outcome.rule}</span>. It takes effect as
					each workload in scope applies its next version; stored windows keep
					the decision they were recorded with.
				</p>
			) : outcome?.kind === "stale" ? (
				<div
					role="alert"
					className="flex flex-col gap-1 rounded-md border border-status-warn-border bg-status-warn-bg px-3 py-2.5 text-[12px]"
				>
					<span className="font-semibold text-status-warn-fg">
						▲ {outcome.rule} changed since this review read it
					</span>
					<span className="text-secondary">
						The rule is now at version{" "}
						<span className="font-mono">{outcome.current ?? "unknown"}</span>
						{"; nothing was changed."} Reload to see the rule as it stands, then
						decide again.
					</span>
					<Button
						variant="secondary"
						size="sm"
						className="mt-1 self-start rounded-sm"
						onClick={() => {
							setOutcome(null);
							onChanged();
						}}
					>
						Reload
					</Button>
				</div>
			) : outcome?.kind === "failed" ? (
				<p role="alert" className="text-[12px] text-status-critical-fg">
					<span aria-hidden="true">✕</span> {outcome.message}
				</p>
			) : null}
			{disabled.length === 0 ? (
				<p className="text-[12px] text-secondary">
					{ruleset.name} has no disabled rules.
				</p>
			) : (
				<>
					<p className="text-[12px] text-secondary">
						Disabled rules in <span className="font-mono">{ruleset.name}</span>.
						Whether one covers this traffic is for you to judge; enabling it
						renders a new version for the scope.
					</p>
					<ul className="flex flex-col gap-1.5" aria-label="Disabled rules">
						{disabled.map((r) => (
							<li
								key={r.id}
								className="flex items-start gap-2 rounded-md border border-strong bg-app px-3 py-2 text-[12px]"
							>
								<div className="flex min-w-0 flex-1 flex-col gap-0.5">
									<span className="font-mono text-primary">{ruleName(r)}</span>
									<span className="font-mono text-[11px] text-tertiary break-words">
										{(r.peers ?? [])
											.map((p) =>
												p.workloads
													? scopeText(p.workloads)
													: p.address_group
														? groupName(p.address_group)
														: (p.cidr ?? ""),
											)
											.join(", ") || "no peers"}
										{" · "}
										{[
											...(r.services ?? []).map(serviceNameOf),
											...(r.entries ?? []).map(entryText),
										].join(", ")}
									</span>
								</div>
								<Button
									size="sm"
									className="shrink-0 rounded-sm"
									disabled={busy !== null}
									onClick={() => enable(r)}
								>
									{busy === r.id ? "Enabling…" : `Enable ${ruleName(r)}`}
								</Button>
							</li>
						))}
					</ul>
				</>
			)}
		</div>
	);
}

// --- the pairs ---------------------------------------------------------------

type PairGroup = RollupGroup;

function Pairs({
	groups,
	row,
	from,
	to,
}: {
	groups: PairGroup[];
	row: ReviewRow;
	from: string;
	to: string;
}) {
	const [open, setOpen] = useState<string | null>(null);
	if (groups.length === 0) {
		return (
			<p className="text-[12px] text-secondary">No pairs in this range.</p>
		);
	}
	const v = verdicts[row.verdict];
	return (
		<table className="w-full border-collapse text-[12px]">
			<caption className="sr-only">Workload pairs behind this row</caption>
			<thead>
				<tr className="text-left text-[11px] text-tertiary">
					<th className="py-1.5 font-normal">Source</th>
					<th className="py-1.5 font-normal">Destination</th>
					<th className="py-1.5 pl-3 text-right font-normal">Conns</th>
					<th className="py-1.5 pl-3 text-right font-normal">Last</th>
				</tr>
			</thead>
			<tbody className="font-mono">
				{groups.map((g) => {
					const src = g.keys.src;
					const dst = g.keys.dst;
					if (!src || !dst) return null;
					const k = `${dst.id}|${peerKey(src)}`;
					const isOpen = open === k;
					return (
						<PairRow
							key={k}
							isOpen={isOpen}
							onToggle={() => setOpen(isOpen ? null : k)}
							cells={
								<>
									<td className="border-t border-default py-1.5 text-secondary">
										<button
											type="button"
											aria-expanded={isOpen}
											onClick={() => setOpen(isOpen ? null : k)}
											className="cursor-pointer text-left"
										>
											{peerName(src)}
										</button>
									</td>
									<td className="border-t border-default py-1.5">
										<Link
											to={`/workloads/${dst.id}`}
											className="text-link hover:underline"
											onClick={(ev) => ev.stopPropagation()}
										>
											{dst.hostname}
										</Link>
										<span className="text-tertiary">
											{row.service.includes("/")
												? `:${row.service.split("/")[1]}`
												: ""}
										</span>
									</td>
									<td
										className={cn(
											"border-t border-default py-1.5 pl-3 text-right",
											v.text,
										)}
									>
										{count(g.connection_count)}
									</td>
									<td className="border-t border-default py-1.5 pl-3 text-right text-secondary">
										{since(g.last_seen)}
									</td>
								</>
							}
							windows={
								isOpen ? (
									<PairWindows
										workload={dst.id}
										peer={peerKey(src)}
										row={row}
										from={from}
										to={to}
									/>
								) : null
							}
						/>
					);
				})}
			</tbody>
		</table>
	);
}

function PairRow({
	cells,
	windows,
	isOpen,
	onToggle,
}: {
	cells: React.ReactNode;
	windows: React.ReactNode;
	isOpen: boolean;
	onToggle: () => void;
}) {
	return (
		<>
			<tr
				onClick={onToggle}
				className={cn("cursor-pointer", isOpen && "bg-selection-bg")}
			>
				{cells}
			</tr>
			{windows ? (
				<tr>
					<td colSpan={4} className="pb-2 pl-2">
						{windows}
					</td>
				</tr>
			) : null}
		</>
	);
}

// PairWindows is one pair's stored windows on the row's service under
// its decision, newest first, from GET /flows: each window's count and
// the process that listened, which no rollup carries.
function PairWindows({
	workload,
	peer,
	row,
	from,
	to,
}: {
	workload: string;
	peer: string;
	row: ReviewRow;
	from: string;
	to: string;
}) {
	const filter = useMemo(
		() => ({
			workload,
			peer,
			verdict: row.verdict,
			service: row.service,
			from,
			to,
		}),
		[workload, peer, row, from, to],
	);
	const { resource, reload } = useResource(() => listFlows(filter), [filter]);
	const [pages, setPages] = useState<FlowsPage[]>([]);
	const [more, setMore] = useState<{ loading: boolean; error?: string }>({
		loading: false,
	});
	useEffect(() => {
		if (resource.status === "ready") setPages([resource.data]);
	}, [resource]);
	const cursor = pages.at(-1)?.next_cursor ?? null;

	async function loadMore() {
		if (!cursor) return;
		setMore({ loading: true });
		try {
			const page = await listFlows(filter, cursor);
			setPages((p) => [...p, page]);
			setMore({ loading: false });
		} catch (err) {
			setMore({ loading: false, error: asProblem(err).message });
		}
	}

	if (resource.status === "loading") return <LoadingRow what="windows" />;
	if (resource.status === "error") {
		return (
			<ProblemNotice what="windows" error={resource.error} onRetry={reload} />
		);
	}
	const flows = pages.flatMap((p) => p.flows);
	return (
		<div className="flex flex-col gap-1 font-sans" data-testid="pair-windows">
			<table className="w-full border-collapse text-[11.5px]">
				<caption className="sr-only">Stored windows of this pair</caption>
				<thead>
					<tr className="text-left text-[11px] text-tertiary">
						<th className="py-1 font-normal">window</th>
						<th className="py-1 font-normal">process</th>
						<th className="py-1 text-right font-normal">conns</th>
						<th className="py-1 text-right font-normal">last seen</th>
					</tr>
				</thead>
				<tbody className="font-mono">
					{flows.map((f) => (
						<tr key={f.id} className="border-t border-default">
							<td className="py-1 text-secondary">{since(f.window_end)} ago</td>
							<td className="py-1">{f.process_name || "—"}</td>
							<td className="py-1 text-right">{count(f.connection_count)}</td>
							<td className="py-1 text-right text-secondary">
								{since(f.last_seen)}
							</td>
						</tr>
					))}
				</tbody>
			</table>
			{cursor ? (
				<button
					type="button"
					onClick={loadMore}
					disabled={more.loading}
					className="cursor-pointer self-start text-[11px] text-link hover:underline"
				>
					{more.loading ? "Loading…" : "Load more windows"}
				</button>
			) : null}
			{more.error ? (
				<span role="alert" className="text-[11px] text-status-critical-fg">
					{more.error}
				</span>
			) : null}
		</div>
	);
}
