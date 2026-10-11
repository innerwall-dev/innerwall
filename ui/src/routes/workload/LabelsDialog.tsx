import { type KeyboardEvent, useEffect, useState } from "react";
import type { ProblemError } from "@/api/client";
import { getWorkloadLabels, putWorkloadLabels } from "@/api/fleet";
import {
	type LabelMap,
	type Labels,
	ProblemType,
	type Workload,
} from "@/api/schema";
import { LabelChip } from "@/components/fleet/status";
import { Icon } from "@/components/Icon";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
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
import { labelPairs } from "@/lib/format";
import { useResource, useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { labelRequirements } from "../fleet/WorkloadList";
import {
	type LabelChange,
	labelChanges,
	outsideGrammar,
	withLabels,
	withoutLabel,
} from "./labels";

// LabelsDialog edits a workload's labels. It reads the labels as the
// resource the edit is conditioned on, takes typed or pasted labels
// through the console's one label parser (ADR-0022), and before saving
// says what the change is and what it does: labels are what scopes and
// peer selectors match, so the save re-renders every ruleset they touch.
// The control plane admits the set and is the authority; a set changed
// since it was read is refused with its version, never overwritten.
export function LabelsDialog({
	workload: w,
	open,
	onOpenChange,
	onSaved,
}: {
	workload: Workload;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onSaved: () => void;
}) {
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent width={560}>
				{open ? <Editor w={w} onSaved={onSaved} /> : null}
			</DialogContent>
		</Dialog>
	);
}

type Conflict = { stored: LabelMap; version: string };

function Editor({ w, onSaved }: { w: Workload; onSaved: () => void }) {
	const write = useWrite();
	const { resource, reload } = useResource(
		() => getWorkloadLabels(w.id),
		[w.id],
	);
	// base is the set as read, the one the write is conditioned on; draft
	// is the set being edited.
	const [base, setBase] = useState<Labels | null>(null);
	const [draft, setDraft] = useState<LabelMap | null>(null);
	const [text, setText] = useState("");
	const [textError, setTextError] = useState<string | null>(null);
	const [step, setStep] = useState<"edit" | "confirm">("edit");
	const [saving, setSaving] = useState(false);
	const [refusal, setRefusal] = useState<string | null>(null);
	const [conflict, setConflict] = useState<Conflict | null>(null);

	useEffect(() => {
		if (resource.status === "ready" && base === null) {
			setBase(resource.data);
			setDraft(resource.data.labels);
		}
	}, [resource, base]);

	if (resource.status === "loading" || (resource.status === "ready" && !base)) {
		return (
			<>
				<DialogHeader title={`Edit labels of ${w.hostname}`} />
				<DialogBody>
					<LoadingRow what="the labels" />
				</DialogBody>
			</>
		);
	}
	if (resource.status === "error" || !base || !draft) {
		return (
			<>
				<DialogHeader title={`Edit labels of ${w.hostname}`} />
				<DialogBody>
					{resource.status === "error" ? (
						<ProblemNotice
							what="the labels"
							error={resource.error}
							onRetry={reload}
						/>
					) : null}
				</DialogBody>
			</>
		);
	}

	const changes = labelChanges(base.labels, draft);
	const outside = outsideGrammar(draft);

	// commit reads what was typed through the one label parser: text it
	// cannot read wholly as labels in the grammar stays in the field with
	// the reason, adding nothing.
	function commit(): LabelMap | null {
		if (!draft) return null;
		if (text.trim() === "") return draft;
		const parsed = labelRequirements(text);
		if (!parsed.ok) {
			setTextError(parsed.error);
			return null;
		}
		const next = withLabels(draft, parsed.requirements);
		setDraft(next);
		setText("");
		setTextError(null);
		return next;
	}

	function onKey(e: KeyboardEvent<HTMLInputElement>) {
		const ends =
			e.key === "Enter" ||
			e.key === "," ||
			(e.key === " " && labelRequirements(text).ok && text.trim() !== "");
		if (ends) {
			e.preventDefault();
			commit();
		}
	}

	function review() {
		const next = commit();
		if (next === null) return;
		setRefusal(null);
		if (labelChanges(base?.labels ?? {}, next).length > 0) setStep("confirm");
	}

	async function save() {
		if (!base || !draft) return;
		setSaving(true);
		setRefusal(null);
		try {
			await write(() => putWorkloadLabels(w.id, draft, base.version));
			onSaved();
		} catch (err) {
			const p = err as ProblemError;
			setStep("edit");
			if (p.type === ProblemType.preconditionFailed) {
				// The edit is kept and goes on top of the set as stored now,
				// which the operator sees before saving again.
				try {
					const fresh = await write(() => getWorkloadLabels(w.id));
					setBase(fresh);
					setConflict({ stored: fresh.labels, version: fresh.version });
				} catch (again) {
					setRefusal((again as ProblemError).problem.detail ?? null);
				}
			} else {
				setRefusal(
					p.problem.errors?.map((f) => f.message).join(" ") ||
						p.problem.detail ||
						p.problem.title,
				);
			}
		} finally {
			setSaving(false);
		}
	}

	if (step === "confirm") {
		return (
			<>
				<DialogHeader title="Save these labels?">
					<p className="type-body text-secondary">
						The labels of{" "}
						<span className="type-mono-ui text-primary">{w.hostname}</span>{" "}
						change as follows. Nothing is written until you save.
					</p>
				</DialogHeader>
				<DialogBody>
					<Changes changes={changes} />
					<div
						role="note"
						data-testid="label-effect"
						className="flex gap-2.5 rounded-lg border border-default bg-subtle px-4 py-3 type-body text-secondary"
					>
						<Icon name="info" className="mt-0.5 shrink-0" />
						<span>
							Saving writes the labels at once and re-renders every ruleset they
							touch. Scopes and peers select workloads by label, so this can
							move {w.hostname} into or out of a ruleset's scope, changing what
							it enforces, and into or out of other workloads' rules as a peer,
							changing who may reach them. Each affected agent takes its new
							policy as it applies its next version.
							{w.mode === "enforced"
								? ` ${w.hostname} is enforced: traffic to it that no rule in its new scope admits is dropped.`
								: ""}
						</span>
					</div>
				</DialogBody>
				<DialogFooter>
					<Button
						variant="secondary"
						className="mr-auto"
						disabled={saving}
						onClick={() => setStep("edit")}
					>
						Back
					</Button>
					<DialogClose asChild>
						<Button variant="secondary">Cancel</Button>
					</DialogClose>
					<Button disabled={saving} onClick={() => void save()}>
						{saving ? "Saving…" : "Save labels"}
					</Button>
				</DialogFooter>
			</>
		);
	}

	return (
		<>
			<DialogHeader title={`Edit labels of ${w.hostname}`}>
				<p className="type-body text-secondary">
					Labels are what ruleset scopes and peers select. Type{" "}
					<span className="type-mono-ui">key=value</span>; a key carries one
					value, so a new value replaces the old.
				</p>
			</DialogHeader>
			<DialogBody>
				{conflict ? (
					<div
						role="alert"
						className="flex flex-col gap-1.5 rounded-lg border border-status-warn-border bg-status-warn-bg px-3 py-2.5 type-ui"
					>
						<SeverityNote level="alert" className="font-medium">
							The labels changed since this editor read them; nothing was saved
						</SeverityNote>
						<span className="text-secondary">
							As stored now, at version{" "}
							<span className="type-mono-ui">{conflict.version}</span>:
						</span>
						<div className="flex flex-wrap gap-1">
							{labelPairs(conflict.stored).length === 0 ? (
								<span className="text-tertiary">no labels</span>
							) : (
								labelPairs(conflict.stored).map(([k, v]) => (
									<LabelChip key={k} k={k} v={v} />
								))
							)}
						</div>
						<span className="text-secondary">
							Your edit is kept. Review it against these and save again.
						</span>
					</div>
				) : null}
				<div className="flex flex-col gap-1.5">
					<span className="type-label text-secondary">Labels</span>
					<div
						className={cn(
							"flex min-h-control-md flex-wrap items-center gap-1 rounded-md border bg-app px-2 py-[5px] has-[input:focus-visible]:focus-ring",
							textError ? "border-status-critical-fg" : "border-strong",
						)}
					>
						{labelPairs(draft).map(([k, v]) => (
							<button
								key={k}
								type="button"
								aria-label={`Remove ${JSON.stringify(k)}=${JSON.stringify(v)}`}
								onClick={() => {
									setDraft(withoutLabel(draft, k));
									setConflict(null);
								}}
								className="cursor-pointer"
							>
								<LabelChip k={k} v={v} size="input" />
							</button>
						))}
						<input
							aria-label="Add a label"
							aria-invalid={textError !== null || undefined}
							placeholder="key=value"
							value={text}
							onChange={(e) => {
								setText(e.target.value);
								setTextError(null);
							}}
							onKeyDown={onKey}
							onBlur={() => commit()}
							className="h-5 min-w-[120px] flex-1 bg-transparent px-1 type-mono-sm text-primary outline-none placeholder:text-tertiary focus-visible:shadow-none"
						/>
					</div>
					{textError ? (
						<SeverityNote level="error" className="type-caption">
							{textError}
						</SeverityNote>
					) : null}
					{Object.keys(draft).length === 0 ? (
						<SeverityNote level="alert" size="sm" className="type-caption">
							With no labels, no ruleset's scope selects this workload and no
							rule can name it as a peer.
						</SeverityNote>
					) : null}
					{outside.length > 0 ? (
						<SeverityNote level="alert" size="sm" className="type-caption">
							{outside.length === 1
								? "One label is"
								: `${outside.length} labels are`}{" "}
							outside the label grammar, stored before it existed. The control
							plane refuses a set that holds one: remove it, or type its key
							with a new value to replace it.
						</SeverityNote>
					) : null}
				</div>
				{refusal ? (
					<SeverityNote level="error" role="alert" className="type-ui">
						{refusal}
					</SeverityNote>
				) : null}
			</DialogBody>
			<DialogFooter>
				<span className="mr-auto type-caption text-tertiary">
					{changes.length === 0
						? "No changes yet."
						: `${changes.length} ${changes.length === 1 ? "change" : "changes"}`}
				</span>
				<DialogClose asChild>
					<Button variant="secondary">Cancel</Button>
				</DialogClose>
				<Button
					disabled={
						outside.length > 0 || (changes.length === 0 && text.trim() === "")
					}
					onClick={review}
				>
					Review change
				</Button>
			</DialogFooter>
		</>
	);
}

// Changes lists what a save changes, key by key: a label added, removed,
// or given a new value.
function Changes({ changes }: { changes: LabelChange[] }) {
	return (
		<ul className="flex flex-col gap-1.5" aria-label="Label changes">
			{changes.map((c) => (
				<li key={c.key} className="flex flex-wrap items-center gap-2 type-ui">
					<span className="w-[60px] type-label text-tertiary">{c.kind}</span>
					{c.kind === "changed" ? (
						<>
							<LabelChip k={c.key} v={c.before ?? ""} />
							<span className="text-tertiary" aria-hidden="true">
								→
							</span>
							<span className="sr-only">to</span>
							<LabelChip k={c.key} v={c.after ?? ""} />
						</>
					) : (
						<LabelChip k={c.key} v={(c.after ?? c.before) as string} />
					)}
				</li>
			))}
		</ul>
	);
}
