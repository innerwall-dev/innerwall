import type { ProblemError } from "@/api/client";
import { StatusGlyph } from "@/components/StatusGlyph";
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
			<div className="flex items-center gap-2 type-body-strong text-status-critical-fg">
				<StatusGlyph status="error" />
				<span>Could not load {what}</span>
			</div>
			<p className="type-caption text-secondary">
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
		<p className="py-6 type-mono-sm text-tertiary" aria-busy="true">
			Loading {what}…
		</p>
	);
}
