import { type FormEvent, useEffect, useId, useState } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { ProblemError } from "@/api/client";
import { ProblemType } from "@/api/types";
import { useSession } from "@/auth/SessionProvider";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Lockup } from "@/shell/Logo";

// The login screen. The password is the only credential a browser
// presents (ADR-0021); the surface's refusals are rendered inline, and
// the one that means "this control plane has no password yet" replaces
// the form with the instruction to set one on the host, since no
// endpoint can.
export function Login() {
	const { session, login } = useSession();
	const location = useLocation();
	const navigate = useNavigate();
	const [password, setPassword] = useState("");
	const [submitting, setSubmitting] = useState(false);
	const [problem, setProblem] = useState<ProblemError | null>(null);
	const [retryIn, setRetryIn] = useState<number | null>(null);
	const passwordId = useId();
	const problemId = useId();

	// A throttled attempt counts down the interval the surface named.
	useEffect(() => {
		if (retryIn === null || retryIn <= 0) return;
		const t = setTimeout(
			() => setRetryIn((s) => (s === null ? null : s - 1)),
			1000,
		);
		return () => clearTimeout(t);
	}, [retryIn]);

	if (session.status === "authenticated") {
		const from = (location.state as { from?: string } | null)?.from;
		return (
			<Navigate to={from && from !== "/login" ? from : "/simulation"} replace />
		);
	}

	async function submit(e: FormEvent) {
		e.preventDefault();
		if (submitting) return;
		setSubmitting(true);
		setProblem(null);
		try {
			await login(password);
			const from = (location.state as { from?: string } | null)?.from;
			navigate(from && from !== "/login" ? from : "/simulation", {
				replace: true,
			});
		} catch (err) {
			if (err instanceof ProblemError) {
				setProblem(err);
				setRetryIn(
					err.type === ProblemType.tooManyAttempts
						? (err.retryAfterSeconds ?? 60)
						: null,
				);
			} else {
				setProblem(
					new ProblemError({
						type: ProblemType.internal,
						title: "Sign in failed",
						status: 0,
						detail:
							err instanceof Error
								? err.message
								: "the control plane could not be reached",
					}),
				);
			}
			setPassword("");
		} finally {
			setSubmitting(false);
		}
	}

	const noPassword = problem?.type === ProblemType.noPassword;

	return (
		<div className="flex min-h-dvh items-center justify-center bg-background px-6 py-12 text-foreground">
			<div className="w-full max-w-[380px] rounded-dialog border border-input-strong bg-card p-7 shadow-popover">
				<div className="flex items-center gap-2.5">
					<Lockup />
					<span className="ml-auto font-mono text-[11px] text-muted-foreground">
						operator console
					</span>
				</div>
				{noPassword ? (
					<FreshInstall onRetry={() => setProblem(null)} />
				) : (
					<form onSubmit={submit} className="mt-7" noValidate>
						<Label htmlFor={passwordId}>Operator password</Label>
						<Input
							id={passwordId}
							name="password"
							type="password"
							autoComplete="current-password"
							autoFocus
							required
							value={password}
							onChange={(e) => setPassword(e.target.value)}
							aria-invalid={problem ? true : undefined}
							aria-describedby={problem ? problemId : undefined}
							className="mt-2 font-mono"
						/>
						{problem ? (
							<p
								id={problemId}
								role="alert"
								className="mt-2.5 flex items-start gap-2 text-[12px] text-destructive"
							>
								<span className="font-mono" aria-hidden="true">
									✕
								</span>
								<span>{describe(problem, retryIn)}</span>
							</p>
						) : null}
						<Button
							type="submit"
							size="block"
							className="mt-5"
							disabled={
								submitting ||
								password === "" ||
								(retryIn !== null && retryIn > 0)
							}
						>
							{submitting ? "Signing in…" : "Sign in"}
						</Button>
					</form>
				)}
			</div>
		</div>
	);
}

// describe turns a refusal into the sentence beside the field. The
// throttle sentence carries the countdown; everything else is the
// surface's own detail.
export function describe(
	problem: ProblemError,
	retryIn: number | null,
): string {
	switch (problem.type) {
		case ProblemType.invalidCredentials:
			return "That password is not correct.";
		case ProblemType.tooManyAttempts:
			return retryIn !== null && retryIn > 0
				? `Too many attempts. Try again in ${retryIn}s.`
				: "Too many attempts. Try again.";
		case ProblemType.crossOrigin:
			return "The request was refused as cross-origin; open the console from the control plane's own address.";
		default:
			return problem.problem.detail ?? problem.problem.title;
	}
}

// FreshInstall is the no-password state: the control plane has no
// operator password and only the command line on its host can set one.
function FreshInstall({ onRetry }: { onRetry: () => void }) {
	return (
		<div className="mt-6" data-testid="fresh-install">
			<h1 className="text-[15px] font-semibold">
				No operator password has been set
			</h1>
			<p className="mt-2 text-[13px] leading-[1.6] text-foreground-tertiary">
				This control plane is freshly installed. The password is set from the
				command line on the control-plane host, and nowhere else:
			</p>
			<pre className="mt-3 overflow-x-auto rounded border border-input-strong bg-muted px-3 py-2.5 font-mono text-[12px] text-foreground">
				innerwall operator set-password
			</pre>
			<p className="mt-3 text-[12px] leading-[1.6] text-muted-foreground">
				The command also takes the display name shown in this console. Once it
				has run, sign in here.
			</p>
			<Button
				variant="secondary"
				size="block"
				className="mt-5"
				onClick={onRetry}
			>
				Sign in
			</Button>
		</div>
	);
}
