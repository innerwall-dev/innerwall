import { type KeyboardEvent, useEffect, useId, useState } from "react";
import { useNavigate } from "react-router";
import { listRulesets } from "@/api/policy";
import { Icon } from "@/components/Icon";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { useResource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { editorPath } from "@/routes/policy/link";
import { walkWorkloads } from "@/routes/review/data";
import { type Hit, search } from "./search";

// The most hits a group lists; past it the palette asks for a narrower
// query rather than listing the fleet.
const shown = 8;

// Palette is the console's search: workloads by hostname or label and
// rulesets by name, by substring. Choosing a hit opens it; Escape closes
// the palette. The fleet and the rulesets are read when it opens, so
// what it finds is the state as of that opening.
export function Palette({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent width={560}>
				{open ? <Search onDone={() => onOpenChange(false)} /> : null}
			</DialogContent>
		</Dialog>
	);
}

function Search({ onDone }: { onDone: () => void }) {
	const navigate = useNavigate();
	const listId = useId();
	const [query, setQuery] = useState("");
	const [active, setActive] = useState(0);
	const { resource, reload } = useResource(
		() =>
			Promise.all([walkWorkloads([]), listRulesets()]).then(([w, r]) => ({
				workloads: w,
				rulesets: r.rulesets,
			})),
		[],
	);
	const hits =
		resource.status === "ready"
			? search(
					query,
					resource.data.workloads,
					resource.data.rulesets,
					editorPath,
				)
			: { workloads: [], rulesets: [] };
	const groups = [
		{ label: "Workloads", all: hits.workloads },
		{ label: "Rulesets", all: hits.rulesets },
	].filter((g) => g.all.length > 0);
	const listed = groups.flatMap((g) => g.all.slice(0, shown));

	// A new query starts from its first hit.
	// biome-ignore lint/correctness/useExhaustiveDependencies: the query is what resets it
	useEffect(() => setActive(0), [query]);

	function go(hit: Hit) {
		onDone();
		navigate(hit.to);
	}

	function onKey(e: KeyboardEvent<HTMLInputElement>) {
		if (e.key === "ArrowDown" || e.key === "ArrowUp") {
			e.preventDefault();
			if (listed.length === 0) return;
			const step = e.key === "ArrowDown" ? 1 : -1;
			setActive((a) => (a + step + listed.length) % listed.length);
		} else if (e.key === "Enter") {
			const hit = listed[active];
			if (hit) {
				e.preventDefault();
				go(hit);
			}
		}
	}

	const activeHit = listed[active];
	return (
		<>
			<DialogTitle className="sr-only">Search</DialogTitle>
			<div className="flex h-control-md shrink-0 items-center gap-2 border-b border-hairline px-4 text-tertiary">
				<Icon name="search" />
				<input
					// biome-ignore lint/a11y/noAutofocus: the palette opens to type into
					autoFocus
					role="combobox"
					aria-expanded={listed.length > 0}
					aria-controls={listId}
					aria-activedescendant={
						activeHit ? `${listId}-${activeHit.key}` : undefined
					}
					aria-label="Search workloads and rulesets"
					placeholder="Search workloads and rulesets…"
					value={query}
					onChange={(e) => setQuery(e.target.value)}
					onKeyDown={onKey}
					className="min-w-0 flex-1 bg-transparent p-0 type-dense text-primary placeholder:text-tertiary focus:outline-none"
				/>
			</div>
			<div className="min-h-0 overflow-auto px-2 py-2">
				{resource.status === "loading" ? (
					<div className="px-2">
						<LoadingRow what="the fleet and the rulesets" />
					</div>
				) : resource.status === "error" ? (
					<div className="px-2">
						<ProblemNotice
							what="the fleet and the rulesets"
							error={resource.error}
							onRetry={reload}
						/>
					</div>
				) : query.trim() === "" ? (
					<p className="px-2 py-3 type-caption text-tertiary">
						Workloads by hostname or label, rulesets by name. Enter opens the
						highlighted one; Escape closes.
					</p>
				) : groups.length === 0 ? (
					<p className="px-2 py-3 type-dense text-secondary">
						Nothing matches{" "}
						<span className="type-mono-data">{query.trim()}</span>.
					</p>
				) : (
					<div id={listId} role="listbox" aria-label="Results">
						{groups.map((g) => (
							// A listbox groups its options with the group role; a
							// fieldset groups form controls.
							// biome-ignore lint/a11y/useSemanticElements: see above
							<div
								key={g.label}
								role="group"
								aria-label={g.label}
								className="pb-1"
							>
								<div className="px-2 pt-2 pb-1 type-control text-tertiary">
									{g.label}
								</div>
								{g.all.slice(0, shown).map((hit) => {
									const on = hit === activeHit;
									return (
										<div
											key={hit.key}
											id={`${listId}-${hit.key}`}
											role="option"
											aria-selected={on}
											tabIndex={-1}
											onMouseEnter={() => setActive(listed.indexOf(hit))}
											onMouseDown={(e) => e.preventDefault()}
											onClick={() => go(hit)}
											// The combobox holds focus and moves through the
											// options itself (aria-activedescendant).
											onKeyDown={() => undefined}
											className={cn(
												"flex h-row-pitch cursor-pointer items-center gap-2 rounded-control px-2",
												on ? "bg-wash-selected" : "hover:bg-wash-hover",
											)}
										>
											<Icon
												name={
													hit.kind === "workload" ? "server" : "scroll-text"
												}
												className="text-tertiary"
											/>
											<span className="min-w-0 truncate type-mono-data text-primary">
												{hit.title}
											</span>
											{hit.detail ? (
												<span className="min-w-0 truncate type-mono-sm text-tertiary">
													{hit.detail}
												</span>
											) : null}
										</div>
									);
								})}
								{g.all.length > shown ? (
									<p className="px-2 py-1 type-caption text-tertiary">
										{g.all.length - shown} more {g.label.toLowerCase()} match;
										narrow the search.
									</p>
								) : null}
							</div>
						))}
					</div>
				)}
			</div>
		</>
	);
}
