import { useId, useState } from "react";
import type { ProblemError } from "@/api/client";
import {
	createAddressGroup,
	createService,
	putAddressGroup,
	putService,
} from "@/api/policy";
import {
	type AddressGroup,
	type Entry,
	type Finding,
	ProblemType,
	type Service,
} from "@/api/schema";
import { SeverityNote } from "@/components/StatusGlyph";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogBody,
	DialogClose,
	DialogContent,
	DialogFooter,
	DialogHeader,
} from "@/components/ui/dialog";
import { useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { AddInput, ChipView, FindingLines } from "./Chips";
import {
	entryChip,
	type Keyed,
	nextKey,
	parseEntry,
	splitList,
	type Unknown,
} from "./model";

export type Definition = Service | AddressGroup;

// The words each kind of definition is written about in.
export const kindText = {
	service: {
		noun: "service",
		items: "Entries",
		placeholder: "tcp/5432 · udp/53 · icmp",
		about:
			"A named set of protocol and port entries. Rules reference it by name; creating it changes no workload's policy until one does.",
	},
	group: {
		noun: "address group",
		items: "CIDRs",
		placeholder: "10.0.0.0/8 · fd00::/8",
		about:
			"A named set of CIDRs for peers that are not managed workloads. Rules name it as a peer; creating it changes no workload's policy until one does.",
	},
} as const;

// DefinitionDialog creates a service or an address group, or edits one.
// The write goes through the same admission as the command line's, and
// its findings land on the field or the element they name. An edit is
// conditioned on the version the listing holds: a definition changed
// since is refused with its version, the listing is read again, and the
// edit is kept to save over what is stored now.
export function DefinitionDialog({
	kind,
	editing,
	name = "",
	references = 0,
	open,
	onOpenChange,
	onSaved,
	onStale,
}: {
	kind: Unknown["kind"];
	// editing is the definition as the listing holds it now; null creates.
	editing: Definition | null;
	// name is a new definition's name to start from: the one a rule
	// named and found missing.
	name?: string;
	// references is how many rules reference the definition edited.
	references?: number;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onSaved: (saved: Definition, created: boolean) => void;
	onStale?: () => void;
}) {
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent width={560}>
				<DefinitionForm
					kind={kind}
					editing={editing}
					name={name}
					references={references}
					onSaved={onSaved}
					onStale={onStale}
				/>
			</DialogContent>
		</Dialog>
	);
}

// A definition's members as the form holds them, each with a key.
function entriesOf(d: Definition | null): Keyed<Entry>[] {
	return d && "entries" in d
		? d.entries.map((value) => ({ key: nextKey(), value }))
		: [];
}

function cidrsOf(d: Definition | null): Keyed<string>[] {
	return d && "cidrs" in d
		? d.cidrs.map((value) => ({ key: nextKey(), value }))
		: [];
}

function DefinitionForm({
	kind,
	editing,
	name: initialName,
	references,
	onSaved,
	onStale,
}: {
	kind: Unknown["kind"];
	editing: Definition | null;
	name: string;
	references: number;
	onSaved: (saved: Definition, created: boolean) => void;
	onStale?: () => void;
}) {
	const write = useWrite();
	const nameId = useId();
	const words = kindText[kind];
	const [name, setName] = useState(editing?.name ?? initialName);
	const [entries, setEntries] = useState(() => entriesOf(editing));
	const [cidrs, setCidrs] = useState(() => cidrsOf(editing));
	const [findings, setFindings] = useState<Finding[]>([]);
	// The elements' keys in the order the refused write sent them, so a
	// finding stays on the element it named while others are removed.
	const [sent, setSent] = useState<string[]>([]);
	const [failure, setFailure] = useState<string | null>(null);
	const [conflict, setConflict] = useState<string | null>(null);
	const [saving, setSaving] = useState(false);

	const listKey = kind === "service" ? "entries" : "cidrs";
	const items: Keyed<unknown>[] = kind === "service" ? entries : cidrs;
	// Findings by what they name: the name, one element (`entries[2]`,
	// `entries[2].ports[0]`), the list as a whole, or the rest.
	const at = (path: string) => findings.filter((f) => f.path === path);
	const onItem = (key: string) => {
		const i = sent.indexOf(key);
		return i < 0
			? []
			: findings.filter(
					(f) =>
						f.path === `${listKey}[${i}]` ||
						f.path.startsWith(`${listKey}[${i}].`),
				);
	};
	const rest = findings.filter(
		(f) =>
			f.path !== "name" &&
			f.path !== listKey &&
			!f.path.startsWith(`${listKey}[`),
	);

	async function save() {
		setSaving(true);
		setFindings([]);
		setFailure(null);
		setConflict(null);
		setSent(items.map((item) => item.key));
		const trimmed = name.trim();
		try {
			const saved = await write<Definition>(() =>
				kind === "service"
					? editing
						? putService(editing as Service, {
								name: trimmed,
								entries: entries.map((e) => e.value),
							})
						: createService({
								name: trimmed,
								entries: entries.map((e) => e.value),
							})
					: editing
						? putAddressGroup(editing as AddressGroup, {
								name: trimmed,
								cidrs: cidrs.map((c) => c.value),
							})
						: createAddressGroup({
								name: trimmed,
								cidrs: cidrs.map((c) => c.value),
							}),
			);
			onSaved(saved, editing === null);
		} catch (err) {
			const p = err as ProblemError;
			if (p.type === ProblemType.validation) {
				setFindings(p.problem.errors ?? []);
			} else if (p.type === ProblemType.duplicateName) {
				setFindings([
					{
						path: "name",
						rule: "name-duplicate",
						message: `Another ${words.noun} is already named ${trimmed}; nothing was ${editing ? "changed" : "created"}.`,
					},
				]);
			} else if (p.type === ProblemType.preconditionFailed && editing) {
				setConflict(
					`${editing.name} changed since this dialog opened · it is now at version ${p.problem.current_version ?? "unknown"}; nothing was changed. Your edit is kept: saving again replaces what is stored now.`,
				);
				onStale?.();
			} else {
				setFailure(p.problem.detail ?? p.problem.title);
			}
		} finally {
			setSaving(false);
		}
	}

	const title = editing
		? `Edit ${words.noun} ${editing.name}`
		: `New ${words.noun}`;
	const n = (x: number) => `${x} ${x === 1 ? "rule" : "rules"}`;
	return (
		<>
			<DialogHeader title={title}>
				<p className="type-dense text-secondary">
					{editing
						? references > 0
							? `Referenced by ${n(references)}: saving changes what they admit, at once, on the workloads their rulesets scope. Rules hold it by id, so a rename keeps every reference.`
							: "No rule references it: saving changes no workload's policy."
						: words.about}
				</p>
			</DialogHeader>
			<DialogBody>
				<div className="flex flex-col gap-1.5">
					<label htmlFor={nameId} className="type-control text-secondary">
						Name
					</label>
					<input
						id={nameId}
						value={name}
						onChange={(e) => {
							setFindings((f) => f.filter((x) => x.path !== "name"));
							setName(e.target.value);
						}}
						aria-invalid={at("name").length > 0 || undefined}
						className={cn(
							"h-control rounded-control border bg-field-sunk px-3 type-mono-data text-primary outline-none focus-visible:focus-ring",
							at("name").length > 0
								? "border-status-critical-fg"
								: "border-strong",
						)}
					/>
					<FindingLines findings={at("name")} />
				</div>
				<div className="flex flex-col gap-1.5">
					<span className="type-control text-secondary">{words.items}</span>
					{items.length > 0 ? (
						<span className="flex flex-wrap gap-1" data-testid="items">
							{kind === "service"
								? entries.map((e) => (
										<ChipView
											key={e.key}
											chip={entryChip(e.value)}
											findings={onItem(e.key)}
											onRemove={() =>
												setEntries(entries.filter((x) => x.key !== e.key))
											}
										/>
									))
								: cidrs.map((c) => (
										<ChipView
											key={c.key}
											chip={{ tag: "CIDR", text: c.value, reference: false }}
											findings={onItem(c.key)}
											onRemove={() =>
												setCidrs(cidrs.filter((x) => x.key !== c.key))
											}
										/>
									))}
						</span>
					) : null}
					{items.map((item) => (
						<FindingLines key={item.key} findings={onItem(item.key)} />
					))}
					<FindingLines findings={at(listKey)} />
					<AddInput
						label={kind === "service" ? "Add an entry" : "Add a CIDR"}
						placeholder={words.placeholder}
						commitOnBlur
						onAdd={(text) => {
							setFindings((f) => f.filter((x) => x.path !== listKey));
							if (kind === "group") {
								setCidrs([
									...cidrs,
									...splitList(text).map((value) => ({
										key: nextKey(),
										value,
									})),
								]);
								return undefined;
							}
							const parsed = text.trim().split(/\s+/).map(parseEntry);
							const refused = parsed.find((p) => "error" in p);
							if (refused && "error" in refused) return refused.error;
							setEntries([
								...entries,
								...parsed.flatMap((p) =>
									"entry" in p ? [{ key: nextKey(), value: p.entry }] : [],
								),
							]);
							return undefined;
						}}
					/>
				</div>
				<FindingLines findings={rest} />
				{conflict ? (
					<SeverityNote level="error" role="alert" prose className="type-dense">
						{conflict}
					</SeverityNote>
				) : null}
				{failure ? (
					<SeverityNote level="error" role="alert" className="type-dense">
						{failure}
					</SeverityNote>
				) : null}
			</DialogBody>
			<DialogFooter>
				<DialogClose asChild>
					<Button variant="secondary">Cancel</Button>
				</DialogClose>
				<Button disabled={saving} onClick={() => void save()}>
					{saving
						? "Saving…"
						: editing
							? `Save ${words.noun}`
							: `Create ${words.noun}`}
				</Button>
			</DialogFooter>
		</>
	);
}
