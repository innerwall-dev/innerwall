import { Link } from "react-router";
import type { Ruleset } from "@/api/schema";
import { Icon } from "@/components/Icon";
import { cn } from "@/lib/utils";
import { definitionViews } from "./Definitions";
import { editorPath } from "./link";
import type { Unknown } from "./model";

const railHeading = "type-meta-label text-tertiary";

// Rail lists the rulesets, each with whether it is enabled and how many
// rules it holds, and below them the definitions rules reference, each
// kind with how many there are and leading to its list.
export function Rail({
	rulesets,
	current,
	creating,
	section = null,
	services,
	groups,
}: {
	rulesets: Ruleset[] | null;
	current: string | null;
	creating: boolean;
	// section is the definitions list shown, when one is.
	section?: Unknown["kind"] | null;
	services: number | null;
	groups: number | null;
}) {
	return (
		<aside
			className="flex w-[216px] shrink-0 flex-col gap-0.5 overflow-auto border-r border-hairline bg-canvas px-2 py-4"
			aria-label="Rulesets"
		>
			<div className="flex items-center pr-0.5 pb-2 pl-2">
				<span className={railHeading}>Rulesets</span>
				<Link
					to="/policy?new=1"
					className="group/new ml-auto inline-flex h-6 items-center gap-1 rounded-control px-1.5 type-caption text-secondary hover:bg-wash-hover hover:text-primary"
					aria-label="Start a new ruleset"
				>
					<Icon
						name="plus"
						className="size-3.5 group-hover/new:text-icon-active"
					/>
					new
				</Link>
			</div>
			{rulesets === null ? null : rulesets.length === 0 && !creating ? (
				<p className="p-2 type-caption text-tertiary">No rulesets yet.</p>
			) : (
				<nav aria-label="Ruleset list" className="flex flex-col gap-0.5">
					{rulesets.map((rs) => {
						const on = rs.enabled !== false;
						const selected =
							!creating && section === null && rs.name === current;
						return (
							<Link
								key={rs.id}
								to={editorPath(rs.name)}
								aria-current={selected ? "page" : undefined}
								className={cn(
									"flex h-[30px] items-center gap-2 rounded-control px-2 type-dense focus-visible:focus-ring",
									selected
										? "bg-wash-selected text-primary"
										: "text-secondary hover:bg-wash-hover hover:text-primary",
								)}
							>
								<span
									aria-hidden="true"
									className={cn(
										"size-[7px] shrink-0 rounded-full",
										on ? "bg-(--text-primary)" : "border border-strong",
									)}
								/>
								<span className="min-w-0 flex-1 truncate">{rs.name}</span>
								<span className="sr-only">
									{on ? "enabled" : "disabled"}, {rs.rules.length}{" "}
									{rs.rules.length === 1 ? "rule" : "rules"}
								</span>
								<span aria-hidden="true" className="type-caption text-tertiary">
									{rs.rules.length}
								</span>
							</Link>
						);
					})}
					{creating ? (
						<span
							aria-current="page"
							className="flex h-[30px] items-center gap-2 rounded-control bg-wash-selected px-2 type-dense text-primary"
						>
							<span
								aria-hidden="true"
								className="size-[7px] shrink-0 rounded-full border border-strong"
							/>
							New ruleset
						</span>
					) : null}
				</nav>
			)}
			<div
				className={`${railHeading} mt-3.5 -mx-2 border-t border-hairline px-4 pt-4 pb-2`}
			>
				Definitions
			</div>
			<nav aria-label="Definitions" className="flex flex-col gap-0.5">
				<Definition
					kind="service"
					n={services}
					selected={section === "service"}
				/>
				<Definition kind="group" n={groups} selected={section === "group"} />
			</nav>
		</aside>
	);
}

function Definition({
	kind,
	n,
	selected,
}: {
	kind: Unknown["kind"];
	n: number | null;
	selected: boolean;
}) {
	const view = definitionViews[kind];
	return (
		<Link
			to={view.path}
			aria-current={selected ? "page" : undefined}
			className={cn(
				"flex h-[30px] items-center gap-2 rounded-control px-2 type-dense focus-visible:focus-ring",
				selected
					? "bg-wash-selected text-primary"
					: "text-secondary hover:bg-wash-hover hover:text-primary",
			)}
		>
			<span className="flex-1">{view.title}</span>
			<span className="type-caption text-tertiary">{n ?? "…"}</span>
		</Link>
	);
}
