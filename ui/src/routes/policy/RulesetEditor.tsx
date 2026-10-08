import {
	type ReactNode,
	useCallback,
	useEffect,
	useMemo,
	useState,
} from "react";
import { Link } from "react-router";
import type { ProblemError } from "@/api/client";
import { createRule, putRule, putRuleset, renderDryRun } from "@/api/policy";
import {
	type Finding,
	ProblemType,
	type Rule,
	type Ruleset,
	type Selector,
} from "@/api/schema";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { useResource, useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { DryRunCard, type DryRunState } from "./DryRunCard";
import { type EditorData, loadTraffic } from "./data";
import { useScopeMatch } from "./hooks";
import {
	attachFindings,
	bannerText,
	draftInput,
	draftOf,
	freshness,
	namesOf,
	newTempId,
	noFindings,
	peerChip,
	planDryRun,
	type RuleDraft,
	ruleLabeler,
	sameSelector,
	scopeFindings,
	scopeMix,
	selectorText,
	servicesCell,
	shortRuleId,
} from "./model";
import { type RowState, RulesCard } from "./RulesTable";
import { ScopeCard } from "./ScopeCard";

// A notice is the outcome of a write, said where the write was made: a
// success, a conflict with the version the editor read (with the way
// back), or a refusal in the surface's own words.
export interface Notice {
	tone: "ok" | "conflict" | "failed";
	text: ReactNode;
	action?: { label: string; run: () => void };
}

const ruleLabel = (r: Rule) => r.description || shortRuleId(r.id ?? "");

const problemText = (p: ProblemError) =>
	p.problem.errors?.map((f) => f.message).join(" ") ||
	p.problem.detail ||
	p.problem.title;

// RulesetEditor is one ruleset, edited live: its header with the
// enabled switch, the immediate-effect statement, the scope with what it
// matches now, the rules, and the dry run of whatever is unsaved.
export function RulesetEditor({
	data,
	ruleset,
	reload,
	onChanged,
}: {
	data: EditorData;
	ruleset: Ruleset;
	reload: () => void;
	onChanged: () => void;
}) {
	const write = useWrite();
	const names = useMemo(
		() => namesOf(data.services, data.groups),
		[data.services, data.groups],
	);
	const id = ruleset.id ?? "";
	const enabled = ruleset.enabled !== false;

	const [scopeDraft, setScopeDraft] = useState<Selector | null>(null);
	const [scopeRefused, setScopeRefused] = useState<Finding[]>([]);
	const [row, setRow] = useState<RowState | null>(null);
	const [rulesNotice, setRulesNotice] = useState<Notice | null>(null);
	const [headNotice, setHeadNotice] = useState<Notice | null>(null);
	const [busy, setBusy] = useState(false);
	const [saving, setSaving] = useState(false);
	const [dry, setDry] = useState<DryRunState | null>(null);
	// The rule a "reload and reapply" waits on, and the state version it
	// was asked at: the reapply happens once a newer state has been read.
	const [reapply, setReapply] = useState<{ rule: string; at: string } | null>(
		null,
	);
	const [runAfterReload, setRunAfterReload] = useState<string | null>(null);

	const edited =
		scopeDraft !== null && !sameSelector(scopeDraft, ruleset.scope);
	const persisted = useScopeMatch(ruleset.scope, 0);
	const draft = useScopeMatch(edited ? scopeDraft : null);
	const scopeKey = selectorText(ruleset.scope);
	const { resource: traffic } = useResource(
		() => loadTraffic(ruleset.scope),
		[id, scopeKey, ruleset.version],
	);
	const inScope =
		persisted.match?.status === "ready" ? persisted.match.data.count : null;
	const mix =
		persisted.match?.status === "ready" ? scopeMix(persisted.match.data) : null;

	// After "reload and reapply", the row's edits go on top of the rule as
	// it is stored now, and the operator sees what that is before saving.
	useEffect(() => {
		if (reapply === null || !row || data.stateVersion === reapply.at) return;
		const fresh = ruleset.rules.find((r) => r.id === reapply.rule);
		setReapply(null);
		if (!fresh) {
			setRow({ ...row, draft: { ...row.draft, base: null }, refused: null });
			setRulesNotice({
				tone: "conflict",
				text: "The rule no longer exists. Your edits are kept as a new rule; saving creates it.",
			});
			return;
		}
		setRow({ ...row, draft: { ...row.draft, base: fresh }, refused: null });
		setRulesNotice({
			tone: "conflict",
			text: (
				<>
					Your edits are reapplied on top of version{" "}
					<span className="font-mono">{fresh.version}</span>. As stored now, the
					rule admits{" "}
					<span className="font-mono">
						{fresh.peers.map((p) => peerChip(p, names).text).join(", ") ||
							"no peers"}
					</span>{" "}
					on{" "}
					<span className="font-mono">
						{servicesCell(fresh, names)
							.map((c) => `${c.tag.toLowerCase()} ${c.text}`.trim())
							.join(", ") || "nothing"}
					</span>
					{fresh.enabled === false ? " and is disabled" : ""}. Save to replace
					it with your row, or discard the row to keep it.
				</>
			),
		});
	}, [ruleset, reapply, row, names, data.stateVersion]);

	// A conflict is a write refused because the object moved since this
	// editor read it: nothing was changed, and the way back is a reload.
	const conflict = useCallback(
		(what: string, p: ProblemError, action: Notice["action"]): Notice => ({
			tone: "conflict",
			text: (
				<>
					<span className="font-mono">{what}</span> changed since this editor
					read it · it is now at version{" "}
					<span className="font-mono">
						{p.problem.current_version ?? "unknown"}
					</span>
					; nothing was changed.
				</>
			),
			action,
		}),
		[],
	);

	// --- the ruleset ---------------------------------------------------------

	async function setEnabled(on: boolean) {
		setBusy(true);
		setHeadNotice(null);
		try {
			await write(() => putRuleset(ruleset, { enabled: on }));
			setHeadNotice({
				tone: "ok",
				text: on
					? `Enabled ${ruleset.name}: its rules render onto the workloads in scope as each applies its next version.`
					: `Disabled ${ruleset.name}: its rules leave the workloads in scope as each applies its next version.`,
			});
			reload();
			onChanged();
		} catch (err) {
			const p = err as ProblemError;
			setHeadNotice(
				p.type === ProblemType.preconditionFailed
					? conflict(ruleset.name, p, { label: "Reload", run: reload })
					: { tone: "failed", text: problemText(p) },
			);
		} finally {
			setBusy(false);
		}
	}

	async function saveScope() {
		if (!scopeDraft) return;
		setBusy(true);
		setHeadNotice(null);
		setScopeRefused([]);
		try {
			await write(() => putRuleset(ruleset, { scope: scopeDraft }));
			setScopeDraft(null);
			setHeadNotice({
				tone: "ok",
				text: `Saved the scope of ${ruleset.name}; the workloads it now selects render as each applies its next version.`,
			});
			reload();
		} catch (err) {
			const p = err as ProblemError;
			if (p.type === ProblemType.validation) {
				setScopeRefused(p.problem.errors ?? []);
			} else if (p.type === ProblemType.preconditionFailed) {
				setHeadNotice(
					conflict(ruleset.name, p, {
						label: "Reload and reapply",
						run: () => {
							// The scope draft is kept; it goes on top of what is
							// stored now.
							reload();
							setHeadNotice({
								tone: "conflict",
								text: "Reloaded: your scope edit is kept on top of the ruleset as stored now. Save to apply it.",
							});
						},
					}),
				);
			} else {
				setHeadNotice({ tone: "failed", text: problemText(p) });
			}
		} finally {
			setBusy(false);
		}
	}

	// --- rules ---------------------------------------------------------------

	async function toggleRule(rule: Rule, on: boolean) {
		setBusy(true);
		setRulesNotice(null);
		try {
			await write(() => putRule(id, rule, { enabled: on }));
			setRulesNotice({
				tone: "ok",
				text: on
					? `Enabled ${ruleLabel(rule)}. It takes effect as each workload in scope applies its next version; stored windows keep the decision they were recorded with.`
					: `Disabled ${ruleLabel(rule)}. It stops admitting as each workload in scope applies its next version.`,
			});
			reload();
		} catch (err) {
			const p = err as ProblemError;
			setRulesNotice(
				p.type === ProblemType.preconditionFailed
					? conflict(ruleLabel(rule), p, { label: "Reload", run: reload })
					: { tone: "failed", text: problemText(p) },
			);
		} finally {
			setBusy(false);
		}
	}

	function editRule(rule: Rule | null) {
		setRulesNotice(null);
		setRow({
			draft: draftOf(rule, newTempId()),
			findings: noFindings,
			refused: null,
		});
	}

	function setDraft(d: RuleDraft) {
		setRow((cur) => (cur ? { ...cur, draft: d } : cur));
	}

	async function saveRow() {
		if (!row) return;
		const d = row.draft;
		const input = draftInput(d);
		setSaving(true);
		setRulesNotice(null);
		try {
			const saved = await write(() =>
				d.base ? putRule(id, d.base, input) : createRule(id, input),
			);
			setRow(null);
			setRulesNotice({
				tone: "ok",
				text: `Saved ${ruleLabel(saved)} at version ${saved.version}. Each workload in scope takes it as its agent applies its next version.`,
			});
			reload();
		} catch (err) {
			const p = err as ProblemError;
			if (p.type === ProblemType.validation) {
				setRow({
					...row,
					findings: attachFindings(d, p.problem.errors ?? []),
					refused: JSON.stringify(input),
				});
			} else if (p.type === ProblemType.preconditionFailed && d.base) {
				const ruleId = d.base.id ?? "";
				setRulesNotice(
					conflict(ruleLabel(d.base), p, {
						label: "Reload and reapply",
						run: () => {
							setReapply({ rule: ruleId, at: data.stateVersion });
							reload();
						},
					}),
				);
			} else {
				setRulesNotice({ tone: "failed", text: problemText(p) });
			}
		} finally {
			setSaving(false);
		}
	}

	function discardRow() {
		setRow(null);
		setRulesNotice(null);
		if (!edited) setDry(null);
	}

	// --- the dry run ---------------------------------------------------------

	const runDryRun = useCallback(async () => {
		const plan = planDryRun(data.rulesets, data.stateVersion, {
			rulesetId: id,
			ruleset: edited && scopeDraft ? { scope: scopeDraft } : undefined,
			rule: row?.draft ?? null,
		});
		const label = ruleLabeler(data.rulesets, row?.draft ?? null);
		setDry({ status: "running" });
		try {
			const result = await write(() => renderDryRun(plan.body));
			setDry({
				status: "done",
				result,
				freshness: "current",
				label,
			});
		} catch (err) {
			const p = err as ProblemError;
			if (p.type === ProblemType.validation) {
				const findings = p.problem.errors ?? [];
				if (row && plan.rulePrefix) {
					setRow({
						...row,
						findings: attachFindings(row.draft, findings, plan.rulePrefix),
					});
				}
				const scopePrefix = `${plan.rulesetPrefix}.`;
				setScopeRefused(
					findings
						.filter((f) => f.path.startsWith(`${scopePrefix}scope`))
						.map((f) => ({ ...f, path: f.path.slice(scopePrefix.length) })),
				);
				setDry({ status: "refused", findings });
			} else {
				setDry({ status: "failed", message: problemText(p) });
			}
		}
	}, [data, id, edited, scopeDraft, row, write]);

	// "Reload and re-run": the run waits for the reload to land, so it is
	// planned against the state version just read.
	useEffect(() => {
		if (runAfterReload !== null && data.stateVersion !== runAfterReload) {
			setRunAfterReload(null);
			void runDryRun();
		}
	}, [data.stateVersion, runAfterReload, runDryRun]);

	const dryState: DryRunState | null =
		dry?.status === "done"
			? { ...dry, freshness: freshness(dry.result, data.stateVersion) }
			: dry;
	const showDry = row !== null || edited || dry !== null;

	const scopeShown = scopeDraft ?? ruleset.scope;
	const refused = scopeFindings(scopeRefused, "scope");
	const previewed = scopeFindings(edited ? draft.findings : [], "selector");
	const findings = {
		keys: { ...previewed.keys, ...refused.keys },
		whole: [...previewed.whole, ...refused.whole],
	};
	const draftCount =
		draft.match?.status === "ready" ? draft.match.data.count : null;

	return (
		<>
			<header className="flex flex-col gap-1.5">
				<div className="flex flex-wrap items-center gap-x-8 gap-y-2">
					<h1 className="font-mono text-[20px] font-semibold">
						{ruleset.name}
					</h1>
					<span className="flex items-center gap-2 text-[13px] text-secondary">
						<Switch
							size="header"
							on={enabled}
							label={`Ruleset ${ruleset.name} enabled`}
							disabled={busy}
							onChange={(on) => void setEnabled(on)}
						/>
						{enabled ? "Enabled" : "Disabled"}
					</span>
					<Button variant="secondary" className="ml-auto" asChild>
						<Link
							to={`/simulation?${new URLSearchParams({ ruleset: ruleset.name }).toString()}`}
						>
							Review simulation
						</Link>
					</Button>
				</div>
				{ruleset.description ? (
					<p className="text-[13px] text-secondary">{ruleset.description}</p>
				) : null}
			</header>
			{headNotice ? <NoticeLine notice={headNotice} /> : null}
			<div
				role="note"
				className="flex gap-3 rounded-md border border-strong bg-subtle px-4 py-3 text-[13px] text-secondary"
				data-testid="banner"
			>
				<span aria-hidden="true" className="font-mono text-icon-default">
					i
				</span>
				<span>{bannerText({ enabled, count: inScope, mix })}</span>
			</div>
			<ScopeCard
				scope={scopeShown}
				edited={edited}
				match={edited ? draft.match : persisted.match}
				findings={findings}
				onChange={(s) => {
					setScopeRefused([]);
					setScopeDraft(s);
				}}
				footer={
					edited ? (
						<div className="flex items-center gap-2 border-t border-default pt-3">
							<span className="text-[12px] text-tertiary">
								The scope is not saved; the rules still apply to the{" "}
								{inScope ?? "…"} workloads it matches now.
							</span>
							<Button
								variant="secondary"
								className="ml-auto"
								disabled={busy}
								onClick={() => {
									setScopeDraft(null);
									setScopeRefused([]);
									if (!row) setDry(null);
								}}
							>
								Discard scope
							</Button>
							<Button disabled={busy} onClick={() => void saveScope()}>
								{draftCount === null
									? "Save scope"
									: `Save scope — applies to ${draftCount} ${draftCount === 1 ? "workload" : "workloads"} now`}
							</Button>
						</div>
					) : null
				}
			/>
			<RulesCard
				rules={ruleset.rules}
				names={names}
				traffic={traffic}
				row={row}
				busy={busy}
				notice={rulesNotice ? <NoticeLine notice={rulesNotice} inset /> : null}
				inScope={inScope}
				saving={saving}
				onToggle={(r, on) => void toggleRule(r, on)}
				onEdit={(r) => editRule(r)}
				onAdd={() => editRule(null)}
				onDraft={setDraft}
				onDiscard={discardRow}
				onSave={() => void saveRow()}
				onDryRun={() => void runDryRun()}
			/>
			{showDry ? (
				<DryRunCard
					state={dryState}
					onRun={() => void runDryRun()}
					onReloadAndRun={() => {
						setRunAfterReload(data.stateVersion);
						reload();
					}}
				/>
			) : null}
		</>
	);
}

// NoticeLine is one write's outcome, in its tone, with its way back.
export function NoticeLine({
	notice,
	inset = false,
}: {
	notice: Notice;
	inset?: boolean;
}) {
	return (
		<div
			role={notice.tone === "ok" ? "status" : "alert"}
			data-testid="notice"
			className={cn(
				"flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[12px]",
				inset ? "mx-4 mb-3" : "",
				notice.tone === "ok"
					? "text-flow-allowed"
					: notice.tone === "conflict"
						? "rounded-md border border-status-warn-border bg-status-warn-bg px-3 py-2.5 text-secondary"
						: "text-status-critical-fg",
			)}
		>
			<span>
				<span aria-hidden="true">
					{notice.tone === "ok"
						? "✓ "
						: notice.tone === "conflict"
							? "▲ "
							: "✕ "}
				</span>
				{notice.text}
			</span>
			{notice.action ? (
				<Button variant="secondary" size="sm" onClick={notice.action.run}>
					{notice.action.label}
				</Button>
			) : null}
		</div>
	);
}
