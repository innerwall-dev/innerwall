import type { ReactNode } from "react";
import { Icon, type IconName } from "@/components/Icon";
import { cn } from "@/lib/utils";

// EmptyState is the fresh-install layout the routes share: the
// section's chrome icon in a 40px hairline disc on the overlay surface,
// a heading, the explanation in text-secondary, optional numbered steps,
// and the actions, stacked on the design's measure and gap. The heading
// is the dialog-title style, or the page title on a non-dense page (the
// 404). Centred in the screen unless the caller places it.
export function EmptyState({
	title,
	icon,
	page = false,
	children,
	steps,
	actions,
	width,
	gap = 12,
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
			className={cn(
				"flex flex-col",
				gap === 14 ? "gap-3.5" : "gap-3",
				className,
			)}
			style={{ maxWidth: width }}
		>
			{icon ? (
				<span
					className="flex size-10 items-center justify-center rounded-full border border-hairline bg-overlay"
					aria-hidden="true"
				>
					<Icon name={icon} />
				</span>
			) : null}
			<h1 className={page ? "type-page-title" : "type-dialog-title"}>
				{title}
			</h1>
			<p className="type-dense text-secondary">{children}</p>
			{steps ? (
				<ol className="mt-1.5 grid grid-cols-[24px_1fr] gap-2.5 type-dense">
					{steps.map((s, i) => (
						<li key={s.lead} className="contents">
							<span
								className={cn(
									"flex size-[22px] items-center justify-center rounded-full border type-mono-xs",
									i === 0
										? "border-overlay bg-overlay text-primary"
										: "border-hairline text-tertiary",
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
