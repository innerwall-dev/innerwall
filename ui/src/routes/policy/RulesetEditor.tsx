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
import { Icon } from "@/components/Icon";
import { SeverityNote, StatusGlyph } from "@/components/StatusGlyph";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { useResource, useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { ChipView, FindingLines } from "./Chips";
import { DryRunCard, type DryRunState } from "./DryRunCard";
import { type EditorData, loadTraffic } from "./data";
import { useScopeMatch } from "./hooks";
import { type Suggestion, viaText } from "./link";
import {
	attachFindings,
	bannerText,
	combinedSave,
	draftInput,
	draftOf,
	entryChip,
	freshness,
	type Names,
	namesOf,
	newTempId,
	nextKey,
	noFindings,
	peerChip,
	planDryRun,
	type RuleDraft,
	ruleLabeler,
	sameSelector,
	scopeFindings,
	scopeMix,
	selectorText,
	serviceRefChip,
	servicesCell,
	shortRuleId,
} from "./model";
import { type RowState, RulesCard, saveKey } from "./RulesTable";
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

// suggestedRow is the new-rule row a pair of observed traffic opens:
// its peers and services, unsaved, nothing written until the operator
// saves it.
function suggestedRow(s: Suggestion): RowState {
	const d = draftOf(null, newTempId());
	return {
		draft: {
			...d,
			peers: s.peers.map((value) => ({ key: nextKey(), value })),
			refs: s.refs.map((value) => ({ key: nextKey(), value })),
			entries: s.entries.map((value) => ({ key: nextKey(), value })),
		},
		findings: noFindings,
		refused: null,
	};
}

// RulesetEditor is one ruleset, edited live: its header with its name,
// description, and enabled switch, the immediate-effect statement, the
// scope with what it matches now, the rules, and the dry run of whatever
// is unsaved. Opened from a pair of observed traffic (a suggestion), it
// says so, and points at the rule the traffic was attributed to when
// this ruleset holds it, or else opens a new rule from the pair.
export function RulesetEditor({
	data,
	ruleset,
	reload,
	onChanged,
	onRenamed,
	suggestion = null,
	onSuggestionDone,
}: {
	data: EditorData;
	ruleset: Ruleset;
	reload: () => void;
	onChanged: () => void;
	onRenamed: (saved: Ruleset) => void;
	suggestion?: Suggestion | null;
	// onSuggestionDone is called once the row a suggestion opened is saved
	// or discarded, so the address stops carrying the pair.
	onSuggestionDone?: () => void;
}) {
	const write = useWrite();
	const names = useMemo(
		() => namesOf(data.services, data.groups),
		[data.services, data.groups],
	);
	const id = ruleset.id ?? "";
	const enabled = ruleset.enabled !== false;

	// The name and description being edited, and a refused save's
	// findings on them; null when the header is not being edited.
	const [about, setAbout] = useState<{
		name: string;
		description: string;
	} | null>(null);
	const [aboutRefused, setAboutRefused] = useState<Finding[]>([]);
	const [scopeDraft, setScopeDraft] = useState<Selector | null>(null);
	const [scopeRefused, setScopeRefused] = useState<Finding[]>([]);
	// The rule a pair's traffic was attributed to, when this ruleset holds
	// it; otherwise the pair opens a new rule. Both are decided once, as
	// the editor opens.
	const [highlight] = useState<string | null>(() =>
		suggestion && highlightable(suggestion, ruleset) ? suggestion.rule : null,
	);
	const [row, setRow] = useState<RowState | null>(() =>
		suggestion && !highlightable(suggestion, ruleset)
			? suggestedRow(suggestion)
			: null,
	);
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

	// saveAbout writes the name and description, conditioned on the
	// ruleset's version exactly as a scope save is: a ruleset that moved
	// since it was read is refused with its current version, and the
	// edit is kept to reapply on top of what is stored now.
	async function saveAbout() {
		if (!about) return;
		setBusy(true);
		setHeadNotice(null);
		setAboutRefused([]);
		try {
			const saved = await write(() =>
				putRuleset(ruleset, {
					name: about.name,
					description: about.description,
				}),
			);
			setAbout(null);
			const renamed = saved.name !== ruleset.name;
			setHeadNotice({
				tone: "ok",
				text: renamed
					? `Renamed ${ruleset.name} to ${saved.name}. Its id, scope, and rules are unchanged, and no workload's policy changes.`
					: `Saved the description of ${saved.name}. No workload's policy changes.`,
			});
			reload();
			onChanged();
			if (renamed) onRenamed(saved);
		} catch (err) {
			const p = err as ProblemError;
			if (p.type === ProblemType.validation) {
				setAboutRefused(p.problem.errors ?? []);
			} else if (p.type === ProblemType.duplicateName) {
				setAboutRefused([
					{
						path: "name",
						rule: "name-duplicate",
						message: `Another ruleset is already named ${about.name}; nothing was changed.`,
					},
				]);
			} else if (p.type === ProblemType.preconditionFailed) {
				setHeadNotice(
					conflict(ruleset.name, p, {
						label: "Reload and reapply",
						run: () => {
							// The edit is kept; it goes on top of what is
							// stored now.
							reload();
							setHeadNotice({
								tone: "conflict",
								text: "Reloaded: your name and description edit is kept on top of the ruleset as stored now. Save to apply it.",
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
		if (edited && scopeDraft) {
			await saveBoth(row, scopeDraft);
			return;
		}
		const d = row.draft;
		const input = draftInput(d);
		setSaving(true);
		setRulesNotice(null);
		try {
			const saved = await write(() =>
				d.base ? putRule(id, d.base, input) : createRule(id, input),
			);
			setRow(null);
			if (!d.base && suggestion) onSuggestionDone?.();
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
					refused: saveKey(d),
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

	// saveBoth writes a pending scope edit and the row as one write of the
	// ruleset, conditioned on its version: the combination a dry run
	// previews lands at once, or nothing does. Saving the rule first would
	// apply it under the scope as saved, a state never previewed.
	async function saveBoth(current: RowState, scope: Selector) {
		const d = current.draft;
		// The row is written as part of the ruleset as read now, so a rule
		// that moved, or went, since the row opened is a conflict here
		// rather than overwritten or brought back.
		const stored = d.base
			? ruleset.rules.find((r) => r.id === d.base?.id)
			: undefined;
		if (d.base && stored?.version !== d.base.version) {
			const ruleId = d.base.id ?? "";
			setRulesNotice({
				tone: "conflict",
				text: (
					<>
						<span className="font-mono">{ruleLabel(d.base)}</span> changed since
						this row opened
						{stored ? (
							<>
								{" "}
								· it is now at version{" "}
								<span className="font-mono">{stored.version}</span>
							</>
						) : (
							" · it no longer exists"
						)}
						; nothing was changed.
					</>
				),
				action: {
					label: "Reload and reapply",
					run: () => {
						setReapply({ rule: ruleId, at: data.stateVersion });
						reload();
					},
				},
			});
			return;
		}
		const { change, index } = combinedSave(ruleset, scope, d);
		setSaving(true);
		setBusy(true);
		setRulesNotice(null);
		setHeadNotice(null);
		setScopeRefused([]);
		try {
			const saved = await write(() => putRuleset(ruleset, change));
			const rule = saved.rules[index];
			setRow(null);
			if (!d.base && suggestion) onSuggestionDone?.();
			setScopeDraft(null);
			setRulesNotice({
				tone: "ok",
				text: `Saved the scope of ${saved.name} and ${rule ? ruleLabel(rule) : "the rule"} in one write, at version ${saved.version}. The workloads the scope now selects take both as each agent applies its next version.`,
			});
			reload();
		} catch (err) {
			const p = err as ProblemError;
			if (p.type === ProblemType.validation) {
				const findings = p.problem.errors ?? [];
				setRow({
					...current,
					findings: attachFindings(d, findings, `rules[${index}]`),
					refused: saveKey(d, scope),
				});
				setScopeRefused(findings.filter((f) => f.path.startsWith("scope")));
			} else if (p.type === ProblemType.preconditionFailed) {
				setRulesNotice(
					conflict(ruleset.name, p, {
						label: "Reload and reapply",
						run: () => {
							// Both edits are kept: the scope as it is, the row on
							// top of its rule as stored now.
							if (d.base) {
								setReapply({ rule: d.base.id ?? "", at: data.stateVersion });
							}
							reload();
						},
					}),
				);
			} else {
				setRulesNotice({ tone: "failed", text: problemText(p) });
			}
		} finally {
			setSaving(false);
			setBusy(false);
		}
	}

	function discardRow() {
		if (row && !row.draft.base && suggestion) onSuggestionDone?.();
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
	// The stored scope's refusal shows too: one stored before the label
	// grammar (ADR-0022) is refused by the preview, and says where.
	const previewed = scopeFindings(
		edited ? draft.findings : persisted.findings,
		"selector",
	);
	const findings = {
		keys: { ...previewed.keys, ...refused.keys },
		whole: [...previewed.whole, ...refused.whole],
	};
	const draftCount =
		draft.match?.status === "ready" ? draft.match.data.count : null;

	const aboutAt = (path: string) => aboutRefused.filter((f) => f.path === path);
	const aboutRest = aboutRefused.filter(
		(f) => f.path !== "name" && f.path !== "description",
	);
	const aboutUnchanged =
		about !== null &&
		about.name === ruleset.name &&
		about.description === (ruleset.description ?? "");

	return (
		<>
			<header className="flex flex-col gap-1.5">
				<div className="flex flex-wrap items-center gap-x-8 gap-y-2">
					{about ? (
						<div className="flex flex-col gap-1">
							<input
								aria-label="Ruleset name"
								placeholder="ruleset name"
								value={about.name}
								onChange={(ev) => {
									setAboutRefused((f) => f.filter((x) => x.path !== "name"));
									setAbout({ ...about, name: ev.target.value });
								}}
								className={cn(
									"h-control-md w-[320px] rounded-control border bg-field-sunk px-3 type-dialog-title font-mono text-primary placeholder:font-normal placeholder:text-tertiary",
									aboutAt("name").length > 0
										? "border-status-critical-fg"
										: "border-strong",
								)}
							/>
							<FindingLines findings={aboutAt("name")} />
						</div>
					) : (
						<span className="flex items-baseline gap-3">
							<h1 className="type-section font-mono">{ruleset.name}</h1>
							<button
								type="button"
								onClick={() => {
									setHeadNotice(null);
									setAboutRefused([]);
									setAbout({
										name: ruleset.name,
										description: ruleset.description ?? "",
									});
								}}
								className="cursor-pointer type-caption link"
								aria-label="Edit name and description"
							>
								Edit
							</button>
						</span>
					)}
					<span className="flex items-center gap-2 type-dense text-secondary">
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
				{about ? (
					<div className="flex flex-col gap-2">
						<Input
							aria-label="Ruleset description"
							placeholder="What this ruleset protects, in a sentence"
							value={about.description}
							onChange={(ev) => {
								setAboutRefused((f) =>
									f.filter((x) => x.path !== "description"),
								);
								setAbout({ ...about, description: ev.target.value });
							}}
							className="max-w-[640px]"
						/>
						<FindingLines findings={aboutAt("description")} />
						<FindingLines findings={aboutRest} />
						<div className="flex flex-wrap items-center gap-2">
							<span className="type-caption text-tertiary">
								A rename changes the name links and the command line use; the
								ruleset's id, scope, and rules stay, and no workload's policy
								changes.
							</span>
							<Button
								variant="secondary"
								className="ml-auto"
								disabled={busy}
								onClick={() => {
									setAbout(null);
									setAboutRefused([]);
								}}
							>
								Cancel
							</Button>
							<Button
								disabled={busy || aboutUnchanged}
								onClick={() => void saveAbout()}
							>
								Save name and description
							</Button>
						</div>
					</div>
				) : ruleset.description ? (
					<p className="type-dense text-secondary">{ruleset.description}</p>
				) : null}
			</header>
			{headNotice ? <NoticeLine notice={headNotice} /> : null}
			<div
				role="note"
				className="flex gap-2.5 rounded-panel border border-hairline bg-card px-4 py-3 type-dense text-secondary"
				data-testid="banner"
			>
				<Icon name="info" className="mt-0.5" />
				<span>
					{bannerText({
						enabled,
						count: inScope,
						mix,
						bothPending: edited && row !== null,
					})}
				</span>
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
						<div className="flex items-center gap-2 border-t border-hairline pt-3">
							<span className="type-caption text-tertiary">
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
			{suggestion ? (
				<PairNote
					suggestion={suggestion}
					names={names}
					highlighted={highlight !== null}
				/>
			) : null}
			<RulesCard
				highlight={highlight}
				rules={ruleset.rules}
				names={names}
				traffic={traffic}
				row={row}
				busy={busy}
				notice={rulesNotice ? <NoticeLine notice={rulesNotice} inset /> : null}
				inScope={inScope}
				pendingScope={
					edited && scopeDraft ? { scope: scopeDraft, count: draftCount } : null
				}
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

function highlightable(s: Suggestion, rs: Ruleset): boolean {
	return s.rule !== null && rs.rules.some((r) => r.id === s.rule);
}

// PairNote is the pair the editor was opened from: where it came from,
// its source and services as a rule names them, and what the editor did
// with it.
function PairNote({
	suggestion: s,
	names,
	highlighted,
}: {
	suggestion: Suggestion;
	names: Names;
	highlighted: boolean;
}) {
	const services = [
		...s.refs.map((r) => serviceRefChip(r, names)),
		...s.entries.map(entryChip),
	];
	return (
		<div
			role="note"
			data-testid="pair-context"
			className="flex gap-2.5 rounded-panel border border-hairline bg-card px-4 py-3 type-dense text-secondary"
		>
			<Icon name="info" className="mt-0.5 shrink-0" />
			<div className="flex min-w-0 flex-col gap-1.5">
				<div className="flex flex-wrap items-center gap-1.5">
					<span>From {viaText[s.via]}: traffic from</span>
					{s.peers.length > 0 ? (
						s.peers.map((p) => (
							<ChipView key={JSON.stringify(p)} chip={peerChip(p, names)} />
						))
					) : (
						<span className="text-tertiary">
							a source no rule can name as a peer
						</span>
					)}
					{services.length > 0 ? (
						<>
							<span>on</span>
							{services.map((c) => (
								<ChipView key={`${c.tag}|${c.text}`} chip={c} />
							))}
						</>
					) : null}
				</div>
				<span>
					{highlighted
						? "The stored windows attribute it to the rule highlighted below."
						: "A new rule below starts from it. Nothing is written until you save it; review its peers and services first."}
				</span>
				{s.unread.map((u) => (
					<SeverityNote
						key={u}
						level="alert"
						size="sm"
						className="type-caption"
					>
						Not carried over: {u}
					</SeverityNote>
				))}
			</div>
		</div>
	);
}

// NoticeLine is one write's outcome, in its tone, with its way back: a
// success under the allowed glyph, a conflict as a warning on the warn
// tint, a refusal as an error.
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
				"flex flex-wrap items-center gap-x-3 gap-y-1.5 type-dense",
				inset ? "mx-4 mb-3" : "",
				notice.tone === "conflict" &&
					"rounded-panel border border-status-critical-border bg-status-critical-bg px-3 py-2.5 text-primary",
			)}
		>
			{notice.tone === "failed" ? (
				<SeverityNote level="error">{notice.text}</SeverityNote>
			) : (
				<span className="inline-flex min-w-0 items-start gap-1.5">
					<StatusGlyph
						status={notice.tone === "ok" ? "allowed" : "error"}
						className="mt-[3px]"
					/>
					<span className={notice.tone === "ok" ? "text-secondary" : undefined}>
						{notice.text}
					</span>
				</span>
			)}
			{notice.action ? (
				<Button variant="secondary" size="sm" onClick={notice.action.run}>
					{notice.action.label}
				</Button>
			) : null}
		</div>
	);
}
