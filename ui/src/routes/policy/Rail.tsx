import { Link } from "react-router";
import type { Ruleset } from "@/api/schema";
import { Icon } from "@/components/Icon";
import { cn } from "@/lib/utils";
import { editorPath } from "./link";

const railHeading = "type-label text-tertiary";

// Rail lists the rulesets, each with whether it is enabled and how many
// rules it holds, and below them the definitions rules reference. The
// definitions have no screens in this version: their rows state counts
// and lead nowhere.
export function Rail({
	rulesets,
	current,
	creating,
	services,
	groups,
}: {
	rulesets: Ruleset[] | null;
	current: string | null;
	creating: boolean;
	services: number | null;
	groups: number | null;
}) {
	return (
		<aside
			className="flex w-[220px] shrink-0 flex-col gap-0.5 overflow-auto border-r border-default bg-subtle px-2 py-3"
			aria-label="Rulesets"
		>
			<div className="flex items-center pb-1.5 pl-2">
				<span className={railHeading}>Rulesets</span>
				<Link
					to="/policy?new=1"
					className="group/new ml-auto inline-flex h-6 items-center gap-1 rounded-md px-1.5 type-caption text-secondary hover:bg-hover hover:text-primary"
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
						const selected = !creating && rs.name === current;
						return (
							<Link
								key={rs.id}
								to={editorPath(rs.name)}
								aria-current={selected ? "page" : undefined}
								className={cn(
									"flex h-8 items-center gap-2 rounded-md px-2 type-mono-ui",
									selected
										? "bg-active font-medium text-primary"
										: "text-secondary hover:bg-hover hover:text-primary",
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
								<span aria-hidden="true" className="type-mono-xs text-tertiary">
									{rs.rules.length}
								</span>
							</Link>
						);
					})}
					{creating ? (
						<span
							aria-current="page"
							className="flex h-8 items-center gap-2 rounded-md bg-active px-2 type-ui-strong text-primary"
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
			<div className={`${railHeading} px-2 pt-4 pb-1.5`}>Definitions</div>
			<Definition label="Service definitions" n={services} />
			<Definition label="Address groups" n={groups} />
		</aside>
	);
}

function Definition({ label, n }: { label: string; n: number | null }) {
	return (
		<div className="flex h-8 items-center gap-2 rounded-md px-2 type-ui text-secondary">
			<span className="flex-1">{label}</span>
			<span className="type-mono-xs text-tertiary">{n ?? "…"}</span>
		</div>
	);
}
