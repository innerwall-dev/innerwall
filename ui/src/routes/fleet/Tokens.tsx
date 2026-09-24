import { useState } from "react";
import type { ProblemError } from "@/api/client";
import { revokeProvisioningToken } from "@/api/fleet";
import {
	ProblemType,
	type ProvisioningToken,
	type TokenState,
} from "@/api/schema";
import { LabelChip } from "@/components/fleet/status";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogBody,
	DialogClose,
	DialogContent,
	DialogFooter,
	DialogHeader,
} from "@/components/ui/dialog";
import { ago, count, labelPairs, relative } from "@/lib/format";
import { type Resource, useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { MintDialog } from "./MintDialog";

const status: Record<TokenState, { label: string; cls: string }> = {
	valid: { label: "active", cls: "text-token-active" },
	revoked: { label: "revoked", cls: "text-token-revoked" },
	expired: { label: "expired", cls: "text-token-expired" },
	invalid: { label: "invalid", cls: "text-token-expired" },
};

// Tokens is the provisioning-tokens tab: every token by its listing
// prefix and metadata (the secret is never stored, so never listed),
// minting, and revocation. A token minted before the control plane kept
// a prefix lists without one.
export function Tokens({
	tokens,
	reload,
}: {
	tokens: Resource<{ tokens: ProvisioningToken[] }>;
	reload: () => void;
}) {
	const [minting, setMinting] = useState(false);
	const [revoking, setRevoking] = useState<ProvisioningToken | null>(null);

	return (
		<div className="flex flex-1 flex-col gap-[18px] overflow-auto px-6 pt-5 pb-8">
			<div className="flex items-center gap-3">
				<p className="max-w-[620px] text-[12px] text-foreground-tertiary">
					A token enrolls any number of workloads within its label scope until
					it expires or is revoked. The plaintext is shown exactly once, at
					mint; only its hash is stored.
				</p>
				<Button className="ml-auto" onClick={() => setMinting(true)}>
					Mint token
				</Button>
			</div>
			{tokens.status === "loading" ? <LoadingRow what="tokens" /> : null}
			{tokens.status === "error" ? (
				<ProblemNotice what="tokens" error={tokens.error} onRetry={reload} />
			) : null}
			{tokens.status === "ready" && tokens.data.tokens.length === 0 ? (
				<p className="py-5 text-[12px] text-muted-foreground">
					No tokens yet. Mint one to enroll your first workload.
				</p>
			) : null}
			{tokens.status === "ready" && tokens.data.tokens.length > 0 ? (
				<>
					<table className="w-full border-collapse text-[12.5px]">
						<thead>
							<tr className="text-left text-[11px] uppercase tracking-[0.05em] text-muted-foreground">
								<th className="py-2 pr-2 font-medium">Name</th>
								<th className="p-2 font-medium">Assigns labels</th>
								<th className="p-2 font-medium">Status</th>
								<th className="p-2 text-right font-medium">Enrollments</th>
								<th className="p-2 text-right font-medium">Last used</th>
								<th className="p-2 text-right font-medium">Expires</th>
								<th className="py-2 pl-2 font-medium">
									<span className="sr-only">Actions</span>
								</th>
							</tr>
						</thead>
						<tbody>
							{tokens.data.tokens.map((t) => (
								<TokenRow key={t.id} t={t} onRevoke={() => setRevoking(t)} />
							))}
						</tbody>
					</table>
					<p className="text-[11px] text-muted-foreground">
						Revoking stops future enrollments only — workloads already enrolled
						keep their credentials and identity.
					</p>
				</>
			) : null}
			<MintDialog open={minting} onOpenChange={setMinting} onMinted={reload} />
			<RevokeDialog
				token={revoking}
				onClose={() => setRevoking(null)}
				onRevoked={() => {
					setRevoking(null);
					reload();
				}}
			/>
		</div>
	);
}

function TokenRow({
	t,
	onRevoke,
}: {
	t: ProvisioningToken;
	onRevoke: () => void;
}) {
	const cell = "border-t border-border px-2 py-[9px]";
	const s = status[t.state];
	return (
		<tr>
			<td className={cn(cell, "pl-0 font-mono")}>
				{t.name}
				{t.prefix ? (
					<div className="text-[11px] text-muted-foreground">{t.prefix}…</div>
				) : null}
			</td>
			<td className={cell}>
				<div className="flex flex-wrap gap-1">
					{labelPairs(t.labels).map(([k, v]) => (
						<LabelChip key={k} k={k} v={v} />
					))}
				</div>
			</td>
			<td className={cell}>
				<span className={cn("text-[12px]", s.cls)}>{s.label}</span>
			</td>
			<td className={cn(cell, "text-right font-mono")}>{count(t.use_count)}</td>
			<td className={cn(cell, "text-right font-mono text-foreground-tertiary")}>
				{t.last_used_at ? ago(t.last_used_at) : "never"}
			</td>
			<td className={cn(cell, "text-right font-mono text-foreground-tertiary")}>
				{t.state === "revoked" ? "—" : relative(t.expires_at)}
			</td>
			<td className={cn(cell, "pr-0 text-right")}>
				{t.state === "valid" ? (
					<button
						type="button"
						onClick={onRevoke}
						aria-label={`Revoke ${t.name}`}
						className="cursor-pointer rounded-chip border border-status-blocked-border px-[9px] py-1 text-[11.5px] text-destructive hover:bg-status-blocked-bg"
					>
						Revoke
					</button>
				) : null}
			</td>
		</tr>
	);
}

// RevokeDialog confirms a revocation, saying what it does and does not
// touch before anything is sent.
function RevokeDialog({
	token,
	onClose,
	onRevoked,
}: {
	token: ProvisioningToken | null;
	onClose: () => void;
	onRevoked: () => void;
}) {
	const write = useWrite();
	const [submitting, setSubmitting] = useState(false);
	const [problem, setProblem] = useState<ProblemError | null>(null);

	async function revoke() {
		if (!token) return;
		setSubmitting(true);
		setProblem(null);
		try {
			await write(() => revokeProvisioningToken(token.id));
			onRevoked();
		} catch (err) {
			const p = err as ProblemError;
			// Already revoked elsewhere is the outcome asked for.
			if (p.type === ProblemType.alreadyRevoked) onRevoked();
			else setProblem(p);
		} finally {
			setSubmitting(false);
		}
	}

	return (
		<Dialog
			open={token !== null}
			onOpenChange={(o) => {
				if (!o) {
					setProblem(null);
					onClose();
				}
			}}
		>
			<DialogContent width={480}>
				<DialogHeader title="Revoke this token?">
					<p className="text-[12px] text-foreground-tertiary">
						<span className="font-mono text-foreground">{token?.name}</span>{" "}
						will refuse every future enrollment. Workloads it already enrolled
						keep their credentials and identity. Revocation cannot be undone.
					</p>
				</DialogHeader>
				{problem ? (
					<DialogBody>
						<p
							role="alert"
							className="flex items-start gap-2 text-[12px] text-destructive"
						>
							<span className="font-mono" aria-hidden="true">
								✕
							</span>
							<span>{problem.problem.detail ?? problem.problem.title}</span>
						</p>
					</DialogBody>
				) : null}
				<DialogFooter>
					<DialogClose asChild>
						<Button variant="secondary">Cancel</Button>
					</DialogClose>
					<button
						type="button"
						disabled={submitting}
						onClick={revoke}
						className="cursor-pointer rounded border border-status-blocked-border bg-status-blocked-bg px-3 py-[7px] text-[13px] font-semibold text-destructive disabled:opacity-50"
					>
						{submitting ? "Revoking…" : "Revoke token"}
					</button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
