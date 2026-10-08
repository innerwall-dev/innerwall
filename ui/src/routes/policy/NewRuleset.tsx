import { useState } from "react";
import { Link } from "react-router";
import type { ProblemError } from "@/api/client";
import { createRuleset } from "@/api/policy";
import {
	type Finding,
	ProblemType,
	type Ruleset,
	type Selector,
} from "@/api/schema";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { FindingLines } from "./Chips";
import type { EditorData } from "./data";
import { useScopeMatch } from "./hooks";
import { editorPath } from "./link";
import { scopeFindings } from "./model";
import { NoticeLine } from "./RulesetEditor";
import { ScopeCard } from "./ScopeCard";

// NewRuleset starts a ruleset: its name, what it is for, whether it is
// enabled, and its scope, with what the scope matches as it is typed.
// Creating it writes at once; its rules are added in the editor after,
// one row at a time. The design has no shot of this state: it is the
// editor's own header and scope card, empty.
export function NewRuleset({
	data,
	onCreated,
}: {
	data: EditorData;
	onCreated: (rs: Ruleset) => void;
}) {
	const write = useWrite();
	const [name, setName] = useState("");
	const [description, setDescription] = useState("");
	const [enabled, setEnabled] = useState(true);
	const [scope, setScope] = useState<Selector>({});
	const [findings, setFindings] = useState<Finding[]>([]);
	const [failure, setFailure] = useState<string | null>(null);
	const [saving, setSaving] = useState(false);
	const empty = Object.keys(scope).length === 0;
	const live = useScopeMatch(empty ? null : scope);
	const count = live.match?.status === "ready" ? live.match.data.count : null;

	const at = (path: string) => findings.filter((f) => f.path === path);
	const refused = scopeFindings(
		findings.filter((f) => f.path.startsWith("scope")),
		"scope",
	);
	const previewed = scopeFindings(live.findings, "selector");
	const rest = findings.filter(
		(f) =>
			f.path !== "name" &&
			f.path !== "description" &&
			!f.path.startsWith("scope"),
	);

	async function create() {
		setSaving(true);
		setFindings([]);
		setFailure(null);
		try {
			const rs = await write(() =>
				createRuleset({ name, description, enabled, scope, rules: [] }),
			);
			onCreated(rs);
		} catch (err) {
			const p = err as ProblemError;
			if (p.type === ProblemType.validation) {
				setFindings(p.problem.errors ?? []);
			} else {
				setFailure(p.problem.detail ?? p.problem.title);
			}
		} finally {
			setSaving(false);
		}
	}

	const first = data.rulesets[0];
	return (
		<>
			<header className="flex flex-col gap-2">
				<div className="flex flex-wrap items-center gap-x-8 gap-y-2">
					<div className="flex flex-col gap-1">
						<input
							aria-label="Ruleset name"
							placeholder="ruleset name"
							value={name}
							onChange={(ev) => setName(ev.target.value)}
							className={cn(
								"w-[320px] rounded-md border bg-raised px-2.5 py-1 font-mono text-[18px] font-semibold text-primary placeholder:font-normal placeholder:text-tertiary focus:outline-none focus-visible:border-selection-fg",
								at("name").length > 0
									? "border-status-critical-fg"
									: "border-strong",
							)}
						/>
						<FindingLines findings={at("name")} />
					</div>
					<span className="flex items-center gap-2 text-[13px] text-secondary">
						<Switch
							size="header"
							on={enabled}
							label="New ruleset enabled"
							onChange={setEnabled}
						/>
						{enabled ? "Enabled" : "Disabled"}
					</span>
				</div>
				<input
					aria-label="Ruleset description"
					placeholder="What this ruleset protects, in a sentence"
					value={description}
					onChange={(ev) => setDescription(ev.target.value)}
					className="max-w-[640px] rounded-md border border-strong bg-raised px-2.5 py-1 text-[13px] text-primary placeholder:text-tertiary focus:outline-none focus-visible:border-selection-fg"
				/>
				<FindingLines findings={at("description")} />
			</header>
			<div
				role="note"
				className="flex gap-3 rounded-md border border-strong bg-subtle px-4 py-3 text-[13px] text-secondary"
				data-testid="banner"
			>
				<span aria-hidden="true" className="font-mono text-icon-default">
					i
				</span>
				<span>
					There is no draft. Creating the ruleset writes it at once; it holds no
					rules, so it admits nothing until you add them, and each rule you save
					after renders immediately.
				</span>
			</div>
			<ScopeCard
				scope={scope}
				edited={true}
				match={live.match}
				findings={{
					keys: { ...previewed.keys, ...refused.keys },
					whole: [...previewed.whole, ...refused.whole],
				}}
				onChange={(s) => {
					setFindings((f) => f.filter((x) => !x.path.startsWith("scope")));
					setScope(s);
				}}
			/>
			{rest.length > 0 ? <FindingLines findings={rest} /> : null}
			{failure ? (
				<NoticeLine notice={{ tone: "failed", text: failure }} />
			) : null}
			<div className="flex items-center gap-2">
				<Button variant="secondary" className="ml-auto" asChild>
					<Link to={editorPath(first?.name)}>Cancel</Link>
				</Button>
				<Button disabled={saving} onClick={() => void create()}>
					{count === null
						? "Create ruleset"
						: `Create ruleset — applies to ${count} ${count === 1 ? "workload" : "workloads"} now`}
				</Button>
			</div>
		</>
	);
}
