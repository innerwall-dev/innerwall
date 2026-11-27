import type { ReactNode } from "react";
import type { IconName } from "@/components/Icon";
import { cn } from "@/lib/utils";

// EmptyState is the fresh-install layout the routes share: a heading
// (the page title), the explanation in text 2, optional numbered steps
// as rows of a bordered card on the surface, and the actions, stacked on
// the design's measure at a 20px gap. icon names the section the state
// belongs to; v4 draws no icon disc above the heading, so it is kept
// for the caller's record only. Centred in the screen unless the caller
// places it.
export function EmptyState({
	title,
	page = false,
	children,
	steps,
	actions,
	width,
	className,
}: {
	title: string;
	icon?: IconName;
	page?: boolean;
	children: ReactNode;
	steps?: { lead: string; rest: string }[];
	actions?: ReactNode;
	width: number;
	gap?: 12 | 14;
	className?: string;
}) {
	return (
		<section
			className={cn("flex w-full flex-col gap-5 px-6", className)}
			style={{ maxWidth: width + 48 }}
			data-page={page || undefined}
		>
			<div className="flex flex-col gap-2">
				<h1 className="type-page-title">{title}</h1>
				<p className="type-dense text-secondary">{children}</p>
			</div>
			{steps ? (
				<ol className="m-0 flex list-none flex-col rounded-panel border border-overlay bg-card p-0 type-dense">
					{steps.map((s, i) => (
						<li
							key={s.lead}
							className="flex gap-3.5 border-t border-hairline px-4 py-[13px] first:border-t-0"
						>
							<span
								className="pt-px type-mono-sm text-tertiary"
								aria-hidden="true"
							>
								{i + 1}
							</span>
							<span>
								<span className="text-primary">{s.lead}</span>{" "}
								<span className="text-tertiary">— {s.rest}</span>
							</span>
						</li>
					))}
				</ol>
			) : null}
			{actions}
		</section>
	);
}

// Centered fills the screen and centres its child, the placement every
// fresh state but the policy editor's uses.
export function Centered({ children }: { children: ReactNode }) {
	return (
		<div className="flex flex-1 items-center justify-center">{children}</div>
	);
}
