import { useState } from "react";
import { Link } from "react-router";
import type { AddressGroup, Service } from "@/api/schema";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ChipView } from "./Chips";
import { type Definition, DefinitionDialog } from "./DefinitionDialog";
import type { EditorData } from "./data";
import { editorPath } from "./link";
import {
	entryChip,
	entryText,
	type Reference,
	referencesTo,
	shortRuleId,
	type Unknown,
} from "./model";
import { type Notice, NoticeLine } from "./RulesetEditor";

// The two kinds of definition a rule references, as their views name
// them: the address each lives at, and the words it is written in.
export const definitionViews = {
	service: {
		path: "/policy/services",
		title: "Service definitions",
		about:
			"A service is a named set of protocol and port entries. A rule references it by name in its services, beside or instead of entries stated inline; changing one changes what every rule referencing it admits.",
		items: "Entries",
		none: "No service definitions yet. A rule can state its protocols and ports inline, or reference a service defined here by name.",
		action: "New service",
	},
	group: {
		path: "/policy/address-groups",
		title: "Address groups",
		about:
			"An address group is a named set of CIDRs for peers that are not managed workloads. A rule names one as a peer; changing one changes who every rule naming it admits.",
		items: "CIDRs",
		none: "No address groups yet. A rule can name a CIDR as a peer directly, or an address group defined here by name.",
		action: "New address group",
	},
} as const;

const cell = "px-3 py-2 align-top type-dense first:pl-4 last:pr-4";
const head =
	"h-row-header whitespace-nowrap border-b border-hairline px-3 text-left type-meta-label text-tertiary first:pl-4 last:pr-4";

// Definitions is one kind of definition, listed: each service with its
// entries, or each address group with its CIDRs, and the rules that
// reference it, each a way into the ruleset that holds it. A definition
// is created or edited here, in a dialog, through the same admission as
// the command line's.
export function Definitions({
	kind,
	data,
	reload,
}: {
	kind: Unknown["kind"];
	data: EditorData;
	reload: () => void;
}) {
	const view = definitionViews[kind];
	const defs: Definition[] = kind === "service" ? data.services : data.groups;
	// The dialog: closed, creating, or editing one definition by id, read
	// from the listing each render so a reload after a conflict hands it
	// the version stored now.
	const [dialog, setDialog] = useState<"new" | { id: string } | null>(null);
	const [notice, setNotice] = useState<Notice | null>(null);
	const editing =
		dialog !== null && dialog !== "new"
			? (defs.find((d) => d.id === dialog.id) ?? null)
			: null;

	return (
		<>
			<header className="flex flex-col gap-1.5">
				<div className="flex flex-wrap items-center gap-x-8 gap-y-2">
					<h1 className="type-section">{view.title}</h1>
					<Button
						className="ml-auto"
						onClick={() => {
							setNotice(null);
							setDialog("new");
						}}
					>
						{view.action}
					</Button>
				</div>
				<p className="max-w-[720px] type-dense text-secondary">{view.about}</p>
			</header>
			{notice ? <NoticeLine notice={notice} /> : null}
			<section
				aria-label={view.title}
				className="flex shrink-0 flex-col overflow-hidden rounded-panel border border-hairline bg-card"
			>
				<table
					aria-label={view.title}
					className="w-full table-fixed border-collapse text-left"
				>
					<colgroup>
						<col className="w-[22%]" />
						<col className="w-[30%]" />
						<col />
						<col className="w-[84px]" />
					</colgroup>
					<thead>
						<tr className="bg-card">
							<th className={head}>Name</th>
							<th className={head}>{view.items}</th>
							<th className={head}>Referenced by</th>
							<th className={head}>
								<span className="sr-only">Actions</span>
							</th>
						</tr>
					</thead>
					<tbody>
						{defs.length === 0 ? (
							<tr>
								<td colSpan={4} className="px-4 py-5 type-dense text-tertiary">
									{view.none}
								</td>
							</tr>
						) : null}
						{defs.map((d) => (
							<DefinitionRow
								key={d.id}
								def={d}
								references={referencesTo(data.rulesets, kind, d.id)}
								onEdit={() => {
									setNotice(null);
									setDialog({ id: d.id });
								}}
							/>
						))}
					</tbody>
				</table>
			</section>
			<DefinitionDialog
				kind={kind}
				editing={editing}
				references={
					editing ? referencesTo(data.rulesets, kind, editing.id).length : 0
				}
				open={dialog !== null}
				onOpenChange={(open) => {
					if (!open) setDialog(null);
				}}
				onStale={reload}
				onSaved={(saved, created) => {
					setDialog(null);
					setNotice({
						tone: "ok",
						text: savedText(kind, saved, created, data),
					});
					reload();
				}}
			/>
		</>
	);
}

// savedText is a definition's write as its notice says it: what it now
// stands for, and which workloads take the change.
function savedText(
	kind: Unknown["kind"],
	saved: Definition,
	created: boolean,
	data: EditorData,
): string {
	const what =
		kind === "service"
			? (saved as Service).entries.map(entryText).join(", ")
			: (saved as AddressGroup).cidrs.join(", ");
	const noun = kind === "service" ? "service" : "address group";
	if (created) {
		return `Created ${noun} ${saved.name} (${what}). A rule can reference it by name now; no workload's policy changes until one does.`;
	}
	const n = referencesTo(data.rulesets, kind, saved.id).length;
	return n === 0
		? `Saved ${saved.name} (${what}) at version ${saved.version}. No rule references it, so no workload's policy changes.`
		: `Saved ${saved.name} (${what}) at version ${saved.version}. The ${n === 1 ? "rule" : `${n} rules`} referencing it change at once; each workload they reach takes the change as its agent applies its next version.`;
}

function DefinitionRow({
	def,
	references,
	onEdit,
}: {
	def: Definition;
	references: Reference[];
	onEdit: () => void;
}) {
	return (
		<tr
			aria-label={def.name}
			className="h-row-pitch border-b border-hairline last:border-b-0 hover:bg-wash-hover"
		>
			<td className={cell}>
				<div className="flex flex-col gap-0.5">
					<span className="break-all type-mono-data text-primary">
						{def.name}
					</span>
					<span className="type-mono-sm text-tertiary">
						version {def.version}
					</span>
				</div>
			</td>
			<td className={cell}>
				<div className="flex flex-wrap gap-1">
					{"entries" in def
						? def.entries.map((e) => (
								<ChipView key={entryText(e)} chip={entryChip(e)} />
							))
						: def.cidrs.map((c) => (
								<ChipView
									key={c}
									chip={{ tag: "CIDR", text: c, reference: false }}
								/>
							))}
				</div>
			</td>
			<td className={cell} data-testid="references">
				{references.length === 0 ? (
					<span className="text-tertiary">No rule references it.</span>
				) : (
					<ul className="flex flex-col gap-0.5">
						{references.map(({ ruleset, rule }) => {
							const off = ruleset.enabled === false || rule.enabled === false;
							return (
								<li
									key={rule.id}
									className="flex min-w-0 items-baseline gap-1.5"
								>
									<Link
										to={editorPath(ruleset.name)}
										className="shrink-0 type-mono-data link"
									>
										{ruleset.name}
									</Link>
									<span
										className={cn(
											"min-w-0 truncate",
											off ? "text-tertiary" : "text-secondary",
										)}
										title={rule.description || undefined}
									>
										· {rule.description || shortRuleId(rule.id ?? "")}
										{off
											? ruleset.enabled === false
												? " (ruleset disabled)"
												: " (disabled)"
											: null}
									</span>
								</li>
							);
						})}
					</ul>
				)}
			</td>
			<td className={cn(cell, "text-right")}>
				<button
					type="button"
					onClick={onEdit}
					className="cursor-pointer type-caption link"
					aria-label={`Edit ${def.name}`}
				>
					Edit
				</button>
			</td>
		</tr>
	);
}
