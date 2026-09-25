import { type KeyboardEvent, useEffect, useId, useState } from "react";
import type { ProblemError } from "@/api/client";
import { mintProvisioningToken } from "@/api/fleet";
import type { MintedToken } from "@/api/schema";
import { useMe } from "@/auth/SessionProvider";
import { ChoiceChips, LabelChip } from "@/components/fleet/status";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogBody,
	DialogClose,
	DialogContent,
	DialogFooter,
	DialogHeader,
} from "@/components/ui/dialog";
import { labelPairs, span } from "@/lib/format";
import { useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { parseRequirement } from "./WorkloadList";

const hour = 3600;
const lifetimes = [
	{ key: "24h", label: "24 h", seconds: 24 * hour },
	{ key: "7d", label: "7 d", seconds: 7 * 24 * hour },
	{ key: "30d", label: "30 d", seconds: 30 * 24 * hour },
] as const;

type Lifetime = (typeof lifetimes)[number]["key"] | "custom";

// MintDialog mints a provisioning token. Its secret is in the mint
// response and nowhere else, ever: the dialog holds it in its own state
// for the one step that shows it, and closing the dialog drops it. The
// listing that follows has only the prefix.
export function MintDialog({
	open,
	onOpenChange,
	onMinted,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onMinted: () => void;
}) {
	const [minted, setMinted] = useState<MintedToken | null>(null);

	// Closing forgets the secret; reopening starts a fresh form.
	useEffect(() => {
		if (!open) setMinted(null);
	}, [open]);

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent
				width={560}
				onInteractOutside={(e) => {
					// A stray click must not throw away the only showing of a
					// secret; Done and Escape still close.
					if (minted) e.preventDefault();
				}}
			>
				{minted ? (
					<ShownOnce minted={minted} />
				) : (
					<MintForm
						onMinted={(m) => {
							setMinted(m);
							onMinted();
						}}
					/>
				)}
			</DialogContent>
		</Dialog>
	);
}

function MintForm({ onMinted }: { onMinted: (m: MintedToken) => void }) {
	const write = useWrite();
	const nameId = useId();
	const [name, setName] = useState("");
	const [labels, setLabels] = useState<string[]>([]);
	const [draft, setDraft] = useState("");
	const [draftInvalid, setDraftInvalid] = useState(false);
	const [lifetime, setLifetime] = useState<Lifetime>("30d");
	const [customDays, setCustomDays] = useState("90");
	const [submitting, setSubmitting] = useState(false);
	const [problem, setProblem] = useState<ProblemError | null>(null);

	function commitDraft(): string[] | null {
		if (draft.trim() === "") return labels;
		const req = parseRequirement(draft);
		if (!req) {
			setDraftInvalid(true);
			return null;
		}
		const key = req.slice(0, req.indexOf("="));
		// A token assigns one value per key; a new value replaces the old.
		const next = [...labels.filter((l) => !l.startsWith(`${key}=`)), req];
		setLabels(next);
		setDraft("");
		return next;
	}

	function onKey(e: KeyboardEvent<HTMLInputElement>) {
		if (e.key === "Enter" || e.key === "," || e.key === " ") {
			e.preventDefault();
			commitDraft();
		} else if (e.key === "Backspace" && draft === "" && labels.length > 0) {
			setLabels(labels.slice(0, -1));
		}
	}

	const days = Number.parseInt(customDays, 10);
	const ttl =
		lifetime === "custom"
			? Number.isFinite(days) && days > 0
				? days * 24 * hour
				: null
			: lifetimes.find((l) => l.key === lifetime)?.seconds;

	async function mint() {
		const committed = commitDraft();
		if (committed === null || !ttl) return;
		setSubmitting(true);
		setProblem(null);
		try {
			const labelMap = Object.fromEntries(
				committed.map((l) => {
					const i = l.indexOf("=");
					return [l.slice(0, i), l.slice(i + 1)];
				}),
			);
			const m = await write(() =>
				mintProvisioningToken({
					name: name.trim(),
					labels: labelMap,
					ttl_seconds: ttl,
				}),
			);
			onMinted(m);
		} catch (err) {
			setProblem(err as ProblemError);
		} finally {
			setSubmitting(false);
		}
	}

	return (
		<>
			<DialogHeader title="Mint a provisioning token">
				<p className="text-[12px] text-foreground-tertiary">
					Workloads enrolling with this token receive exactly these labels. They
					cannot choose their own.
				</p>
			</DialogHeader>
			<DialogBody className="text-[12.5px]">
				<label htmlFor={nameId} className="flex flex-col gap-[5px]">
					<span className="text-[12px] text-foreground-tertiary">Name</span>
					<input
						id={nameId}
						value={name}
						onChange={(e) => setName(e.target.value)}
						className="rounded border border-input-strong bg-background px-2.5 py-[7px] font-mono outline-none focus-visible:border-ring"
					/>
				</label>
				<div className="flex flex-col gap-[5px]">
					<span className="text-[12px] text-foreground-tertiary">
						Assigns labels
					</span>
					<div
						className={cn(
							"flex flex-wrap items-center gap-1.5 rounded border bg-background px-2.5 py-[7px]",
							draftInvalid ? "border-destructive" : "border-input-strong",
						)}
					>
						{labelPairs(
							Object.fromEntries(
								labels.map((l) => [
									l.slice(0, l.indexOf("=")),
									l.slice(l.indexOf("=") + 1),
								]),
							),
						).map(([k, v]) => (
							<button
								key={k}
								type="button"
								aria-label={`Remove ${k}=${v}`}
								onClick={() =>
									setLabels(labels.filter((l) => l !== `${k}=${v}`))
								}
								className="cursor-pointer"
							>
								<LabelChip k={k} v={v} size="input" />
							</button>
						))}
						<input
							aria-label="Label"
							aria-invalid={draftInvalid || undefined}
							placeholder="key=value"
							value={draft}
							onChange={(e) => {
								setDraft(e.target.value);
								setDraftInvalid(false);
							}}
							onKeyDown={onKey}
							onBlur={() => commitDraft()}
							className="min-w-[120px] flex-1 bg-transparent px-1 py-0.5 font-mono text-[12px] outline-none placeholder:text-muted-foreground"
						/>
					</div>
					{draftInvalid ? (
						<span className="text-[11px] text-destructive">
							A label is written key=value.
						</span>
					) : null}
				</div>
				<div className="flex flex-col gap-[5px]">
					<span className="text-[12px] text-foreground-tertiary">Expires</span>
					<div className="flex items-center gap-1.5">
						<ChoiceChips<Lifetime>
							legend="Expires"
							name="lifetime"
							value={lifetime}
							onChange={setLifetime}
							options={[
								...lifetimes.map((l) => ({ value: l.key, label: l.label })),
								{ value: "custom", label: "custom" },
							]}
						/>
						{lifetime === "custom" ? (
							<label className="ml-1 flex items-center gap-1.5 text-foreground-tertiary">
								<input
									aria-label="Days"
									inputMode="numeric"
									value={customDays}
									onChange={(e) => setCustomDays(e.target.value)}
									className="w-14 rounded border border-input-strong bg-background px-2 py-1 text-right font-mono text-foreground outline-none focus-visible:border-ring"
								/>
								days
							</label>
						) : null}
					</div>
				</div>
				{problem ? (
					<p
						role="alert"
						className="flex items-start gap-2 text-[12px] text-destructive"
					>
						<span className="font-mono" aria-hidden="true">
							✕
						</span>
						<span>
							{problem.problem.errors?.map((f) => f.message).join(" ") ||
								problem.problem.detail ||
								problem.problem.title}
						</span>
					</p>
				) : null}
			</DialogBody>
			<DialogFooter>
				<DialogClose asChild>
					<Button variant="secondary">Cancel</Button>
				</DialogClose>
				<Button disabled={submitting || !ttl} onClick={mint}>
					{submitting ? "Minting…" : "Mint token"}
				</Button>
			</DialogFooter>
		</>
	);
}

// masked is the secret as the install line abbreviates it: its first
// characters and its last three.
function masked(secret: string): string {
	return secret.length > 12
		? `${secret.slice(0, 7)}…${secret.slice(-3)}`
		: secret;
}

function ShownOnce({ minted }: { minted: MintedToken }) {
	// The gateway address is what the control plane was configured to
	// advertise; unset, the command keeps a placeholder to fill in.
	const advertised = useMe().gateway_address;
	const configured = advertised !== null && advertised !== undefined;
	const gateway = advertised ?? "<agent-gateway>";
	const [copied, setCopied] = useState<"idle" | "copied" | "failed">("idle");
	const lifetime = span(
		Date.parse(minted.expires_at) - Date.parse(minted.created_at),
	);
	const pairs = labelPairs(minted.labels);

	async function copy() {
		try {
			await navigator.clipboard.writeText(minted.token);
			setCopied("copied");
		} catch {
			setCopied("failed");
		}
	}

	return (
		<>
			<DialogHeader title="Token minted — copy it now">
				<p className="text-[12px] text-status-degraded">
					▲ This is the only time the plaintext is shown. Only its hash is
					stored.
				</p>
			</DialogHeader>
			<DialogBody className="gap-3">
				<div className="flex items-center gap-2 rounded border border-input-strong bg-background px-3 py-2.5 font-mono text-[12.5px] break-all">
					<span className="flex-1" data-testid="minted-secret">
						{minted.token}
					</span>
					<button
						type="button"
						onClick={copy}
						className="cursor-pointer whitespace-nowrap rounded-chip border border-input-strong bg-card px-[9px] py-1 font-sans text-[11.5px]"
					>
						{copied === "copied"
							? "Copied"
							: copied === "failed"
								? "Select and copy"
								: "Copy"}
					</button>
				</div>
				<div className="flex flex-col gap-[5px]">
					<span className="text-[12px] text-foreground-tertiary">
						Enroll a host
					</span>
					<pre className="rounded border border-input-strong bg-background px-3 py-2.5 font-mono text-[11.5px] whitespace-pre-wrap text-foreground-secondary">
						{`innerwall-agent enroll --server ${gateway} \\\n  --token ${masked(minted.token)} --bootstrap-ca ./innerwall-ca.crt`}
					</pre>
					<span className="text-[11px] text-muted-foreground">
						Distribute the CA certificate alongside the token; the agent uses it
						to verify the control plane on first contact.
						{configured
							? null
							: " The agent gateway is the control plane's agent listener, host:port; set --gateway-advertise-address on the control plane to fill it in."}
					</span>
				</div>
			</DialogBody>
			<DialogFooter>
				<span className="mr-auto text-[11px] text-muted-foreground">
					Expires in {lifetime}
					{pairs.length > 0
						? ` · assigns ${pairs.map(([k, v]) => `${k}=${v}`).join(" ")}`
						: " · assigns no labels"}
				</span>
				<DialogClose asChild>
					<Button>Done</Button>
				</DialogClose>
			</DialogFooter>
		</>
	);
}
