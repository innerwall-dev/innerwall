import { Link } from "react-router";
import type { Ruleset } from "@/api/schema";
import { cn } from "@/lib/utils";
import { editorPath } from "./link";

const railHeading =
	"text-[11px] uppercase tracking-[0.05em] text-muted-foreground";

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
			className="flex w-[220px] shrink-0 flex-col gap-0.5 overflow-auto border-r border-border bg-surface-sidebar px-2.5 py-3.5"
			aria-label="Rulesets"
		>
			<div className="flex items-center px-2 pb-2">
				<span className={railHeading}>Rulesets</span>
				<Link
					to="/policy?new=1"
					className="ml-auto text-[11px] text-link hover:text-link-hover"
					aria-label="Start a new ruleset"
				>
					+ new
				</Link>
			</div>
			{rulesets === null ? null : rulesets.length === 0 && !creating ? (
				<p className="p-2 text-[12px] text-muted-foreground">
					No rulesets yet.
				</p>
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
									"flex items-center gap-2 rounded-chip px-2 py-1.5 font-mono text-[13px]",
									selected
										? "bg-surface-nav-active text-foreground"
										: "text-foreground-secondary hover:text-foreground",
								)}
							>
								<span
									aria-hidden="true"
									className={cn(
										"size-[6px] shrink-0 rounded-full",
										on ? "bg-status-allowed" : "bg-foreground-separator",
									)}
								/>
								<span className="min-w-0 flex-1 truncate">{rs.name}</span>
								<span className="sr-only">
									{on ? "enabled" : "disabled"}, {rs.rules.length}{" "}
									{rs.rules.length === 1 ? "rule" : "rules"}
								</span>
								<span
									aria-hidden="true"
									className="text-[11px] text-muted-foreground"
								>
									{rs.rules.length}
								</span>
							</Link>
						);
					})}
					{creating ? (
						<span
							aria-current="page"
							className="flex items-center gap-2 rounded-chip bg-surface-nav-active px-2 py-1.5 text-[13px] text-foreground"
						>
							<span
								aria-hidden="true"
								className="size-[6px] shrink-0 rounded-full border border-foreground-separator"
							/>
							New ruleset
						</span>
					) : null}
				</nav>
			)}
			<div className={`${railHeading} px-2 pt-3.5 pb-1.5`}>Definitions</div>
			<Definition label="Service definitions" n={services} />
			<Definition label="Address groups" n={groups} />
		</aside>
	);
}

function Definition({ label, n }: { label: string; n: number | null }) {
	return (
		<div className="flex gap-2 rounded-chip px-2 py-1.5 text-[12.5px] text-foreground-secondary">
			<span className="flex-1">{label}</span>
			<span className="font-mono text-[11px] text-muted-foreground">
				{n ?? "…"}
			</span>
		</div>
	);
}
