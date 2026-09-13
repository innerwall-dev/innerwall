import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// EmptyState is the fresh-install layout every route shares: a heading,
// the explanation, optional numbered steps, and the actions. It is
// centered in the screen with the text left-aligned, as the screens are.
export function EmptyState({
	title,
	children,
	steps,
	actions,
	className,
	width = 520,
}: {
	title: string;
	children: ReactNode;
	steps?: { lead: string; rest: string }[];
	actions?: ReactNode;
	className?: string;
	// width is the text block's measure; the screens set it per state.
	width?: number;
}) {
	return (
		<div
			className={cn(
				"flex flex-1 items-center justify-center px-8 py-12",
				className,
			)}
		>
			<section className="w-full" style={{ maxWidth: width }}>
				<h1 className="text-[18px] font-semibold leading-tight tracking-[-0.01em]">
					{title}
				</h1>
				<p className="mt-3 text-[13px] leading-[1.6] text-foreground-tertiary">
					{children}
				</p>
				{steps ? (
					<ol className="mt-4 flex flex-col gap-2">
						{steps.map((s, i) => (
							<li
								key={s.lead}
								className="flex items-start gap-3 text-[12px] leading-[1.6]"
							>
								<span
									className={cn(
										"mt-0.5 inline-flex size-[22px] shrink-0 items-center justify-center rounded-full border font-mono text-[11px]",
										i === 0
											? "border-primary text-primary"
											: "border-foreground-separator text-foreground-tertiary",
									)}
									aria-hidden="true"
								>
									{i + 1}
								</span>
								<span className="text-foreground-tertiary">
									<span className="text-foreground">{s.lead}</span> — {s.rest}
								</span>
							</li>
						))}
					</ol>
				) : null}
				{actions ? (
					<div className="mt-5 flex flex-wrap items-center gap-2.5">
						{actions}
					</div>
				) : null}
			</section>
		</div>
	);
}
