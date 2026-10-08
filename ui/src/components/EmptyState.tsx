import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// EmptyState is the fresh-install layout the routes share: a heading,
// the explanation, optional numbered steps, and the actions, stacked on
// the design's measure and gap. Centred in the screen unless the caller
// places it.
export function EmptyState({
	title,
	children,
	steps,
	actions,
	width,
	gap = 12,
	className,
}: {
	title: string;
	children: ReactNode;
	steps?: { lead: string; rest: string }[];
	actions?: ReactNode;
	width: number;
	gap?: 12 | 14;
	className?: string;
}) {
	return (
		<section
			className={cn(
				"flex flex-col",
				gap === 14 ? "gap-3.5" : "gap-3",
				className,
			)}
			style={{ maxWidth: width }}
		>
			<h1 className="type-title-section">{title}</h1>
			<p className="type-body text-secondary">{children}</p>
			{steps ? (
				<ol className="mt-1.5 grid grid-cols-[24px_1fr] gap-2.5 type-ui">
					{steps.map((s, i) => (
						<li key={s.lead} className="contents">
							<span
								className={cn(
									"flex size-[22px] items-center justify-center rounded-full border type-mono-xs",
									i === 0
										? "border-strong bg-active text-primary"
										: "border-default text-tertiary",
								)}
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
