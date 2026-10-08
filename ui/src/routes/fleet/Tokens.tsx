import { useState } from "react";
import type { ProblemError } from "@/api/client";
import { revokeProvisioningToken } from "@/api/fleet";
import {
	ProblemType,
	type ProvisioningToken,
	type TokenState,
} from "@/api/schema";
import { LabelChip } from "@/components/fleet/status";
import { Icon, type IconName } from "@/components/Icon";
import { LoadingRow, ProblemNotice } from "@/components/Problem";
import { SeverityNote } from "@/components/StatusGlyph";
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

// A token's status is its icon, gray like every chrome icon, and its
// word in the status tone.
const status: Record<
	TokenState,
	{ label: string; icon: IconName; cls: string }
> = {
	valid: { label: "active", icon: "key-round", cls: "text-status-ok-fg" },
	revoked: {
		label: "revoked",
		icon: "circle-minus",
		cls: "text-status-critical-fg",
	},
	expired: { label: "expired", icon: "clock", cls: "text-status-neutral-fg" },
	invalid: {
		label: "invalid",
		icon: "circle-minus",
		cls: "text-status-neutral-fg",
	},
};

const th =
	"h-row-header whitespace-nowrap border-b border-default bg-subtle px-3 text-left type-label text-tertiary";

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
		<div className="flex flex-1 flex-col gap-5 overflow-auto px-6 pt-5 pb-8">
			<div className="flex items-center gap-3">
				<p className="max-w-[620px] type-ui text-secondary">
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
				<p className="rounded-lg border border-default bg-subtle px-4 py-5 type-ui text-secondary">
					No tokens yet. Mint one to enroll your first workload.
				</p>
			) : null}
			{tokens.status === "ready" && tokens.data.tokens.length > 0 ? (
				<>
					<div className="overflow-hidden rounded-lg border border-default">
						<table className="w-full border-separate border-spacing-0">
							<thead>
								<tr>
									<th className={th}>Name</th>
									<th className={th}>Assigns labels</th>
									<th className={th}>Status</th>
									<th className={cn(th, "text-right")}>Enrollments</th>
									<th className={cn(th, "text-right")}>Last used</th>
									<th className={cn(th, "text-right")}>Expires</th>
									<th className={th}>
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
					</div>
					<p className="-mt-2 type-caption text-tertiary">
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
	const cell =
		"h-row-dense border-b border-subtle px-3 py-2 type-ui group-last:border-b-0";
	const s = status[t.state];
	return (
		<tr className="group hover:bg-hover">
			<td className={cell}>
				<span className="type-mono-ui font-medium text-primary">{t.name}</span>
				{t.prefix ? (
					<div className="type-mono-sm text-tertiary">{t.prefix}…</div>
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
				<span className="flex items-center gap-1.5 whitespace-nowrap type-label">
					<Icon name={s.icon} className="size-3.5" />
					<span className={s.cls}>{s.label}</span>
				</span>
			</td>
			<td className={cn(cell, "text-right type-mono-ui")}>
				{count(t.use_count)}
			</td>
			<td
				className={cn(
					cell,
					"whitespace-nowrap text-right type-mono-ui text-secondary",
				)}
			>
				{t.last_used_at ? ago(t.last_used_at) : "never"}
			</td>
			<td
				className={cn(
					cell,
					"whitespace-nowrap text-right type-mono-ui text-secondary",
				)}
			>
				{t.state === "revoked" ? "—" : relative(t.expires_at)}
			</td>
			<td className={cn(cell, "text-right")}>
				{t.state === "valid" ? (
					<Button
						variant="secondary"
						size="sm"
						onClick={onRevoke}
						aria-label={`Revoke ${t.name}`}
					>
						Revoke
					</Button>
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
					<p className="type-body text-secondary">
						<span className="font-mono text-primary">{token?.name}</span> will
						refuse every future enrollment. Workloads it already enrolled keep
						their credentials and identity. Revocation cannot be undone.
					</p>
				</DialogHeader>
				{problem ? (
					<DialogBody>
						<SeverityNote level="error" role="alert" className="type-ui">
							{problem.problem.detail ?? problem.problem.title}
						</SeverityNote>
					</DialogBody>
				) : null}
				<DialogFooter>
					<DialogClose asChild>
						<Button variant="secondary">Cancel</Button>
					</DialogClose>
					<Button disabled={submitting} onClick={revoke}>
						{submitting ? "Revoking…" : "Revoke token"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
