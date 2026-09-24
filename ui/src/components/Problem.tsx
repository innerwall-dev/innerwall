import type { ProblemError } from "@/api/client";
import { Button } from "@/components/ui/button";

// ProblemNotice is a failed read as a screen shows it: what could not be
// loaded, the surface's own words, and a retry.
export function ProblemNotice({
	what,
	error,
	onRetry,
}: {
	what: string;
	error: ProblemError;
	onRetry?: () => void;
}) {
	return (
		<div role="alert" className="flex max-w-[560px] flex-col gap-2 py-6">
			<div className="flex items-center gap-2 font-semibold text-destructive">
				<span className="font-mono" aria-hidden="true">
					✕
				</span>
				<span>Could not load {what}</span>
			</div>
			<p className="text-[12px] text-foreground-tertiary">
				{error.problem.detail ?? error.problem.title}
			</p>
			{onRetry ? (
				<Button
					variant="secondary"
					size="sm"
					className="self-start"
					onClick={onRetry}
				>
					Retry
				</Button>
			) : null}
		</div>
	);
}

// Loading is a read in flight, in the table's own measure.
export function LoadingRow({ what }: { what: string }) {
	return (
		<p
			className="py-6 font-mono text-[12px] text-muted-foreground"
			aria-busy="true"
		>
			Loading {what}…
		</p>
	);
}
