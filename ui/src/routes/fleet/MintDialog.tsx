import { type KeyboardEvent, useEffect, useId, useState } from "react";
import type { ProblemError } from "@/api/client";
import { mintProvisioningToken } from "@/api/fleet";
import type { MintedToken } from "@/api/schema";
import { useMe } from "@/auth/SessionProvider";
import { ChoiceChips, LabelChip } from "@/components/fleet/status";
import { Icon } from "@/components/Icon";
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
import { labelPairs, span } from "@/lib/format";
import { useWrite } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { labelRequirements } from "./WorkloadList";

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
	const [draftInvalid, setDraftInvalid] = useState<string | null>(null);
	const [lifetime, setLifetime] = useState<Lifetime>("30d");
	const [customDays, setCustomDays] = useState("90");
	const [submitting, setSubmitting] = useState(false);
	const [problem, setProblem] = useState<ProblemError | null>(null);

	// commitDraft reads the draft through the console's one label parser
	// (ADR-0022): typed or pasted, `app=web env=lab` is two labels, and
	// text it cannot read wholly as labels in the grammar stays in the
	// field with the reason, adding nothing.
	function commitDraft(): string[] | null {
		if (draft.trim() === "") return labels;
		const parsed = labelRequirements(draft);
		if (!parsed.ok) {
			setDraftInvalid(parsed.error);
			return null;
		}
		let next = labels;
		for (const req of parsed.requirements) {
			const key = req.slice(0, req.indexOf("="));
			// A token assigns one value per key; a new value replaces the old.
			next = [...next.filter((l) => !l.startsWith(`${key}=`)), req];
		}
		setLabels(next);
		setDraft("");
		setDraftInvalid(null);
		return next;
	}

	function onKey(e: KeyboardEvent<HTMLInputElement>) {
		// A space ends a label only once the draft reads as one, so
		// `app = web` can be typed with spaces around the "=".
		const ends =
			e.key === "Enter" ||
			e.key === "," ||
			(e.key === " " && labelRequirements(draft).ok && draft.trim() !== "");
		if (ends) {
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
				<p className="type-body text-secondary">
					Workloads enrolling with this token receive exactly these labels. They
					cannot choose their own.
				</p>
			</DialogHeader>
			<DialogBody>
				<label htmlFor={nameId} className="flex flex-col gap-1.5">
					<span className="type-label text-secondary">Name</span>
					<input
						id={nameId}
						value={name}
						onChange={(e) => setName(e.target.value)}
						className="h-control-md rounded-md border border-strong bg-app px-3 type-mono-ui text-primary outline-none focus-visible:focus-ring"
					/>
				</label>
				<div className="flex flex-col gap-1.5">
					<span className="type-label text-secondary">Assigns labels</span>
					<div
						className={cn(
							"flex min-h-control-md flex-wrap items-center gap-1 rounded-md border bg-app px-2 py-[5px] has-[input:focus-visible]:focus-ring",
							draftInvalid ? "border-status-critical-fg" : "border-strong",
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
							aria-invalid={draftInvalid !== null || undefined}
							placeholder="key=value"
							value={draft}
							onChange={(e) => {
								setDraft(e.target.value);
								setDraftInvalid(null);
							}}
							onKeyDown={onKey}
							onBlur={() => commitDraft()}
							className="h-5 min-w-[120px] flex-1 bg-transparent px-1 type-mono-sm text-primary outline-none placeholder:text-tertiary focus-visible:shadow-none"
						/>
					</div>
					{draftInvalid ? (
						<SeverityNote level="error" className="type-caption">
							{draftInvalid}
						</SeverityNote>
					) : null}
				</div>
				<div className="flex flex-col gap-1.5">
					<span className="type-label text-secondary">Expires</span>
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
							<label className="ml-1 flex items-center gap-1.5 type-ui text-secondary">
								<input
									aria-label="Days"
									inputMode="numeric"
									value={customDays}
									onChange={(e) => setCustomDays(e.target.value)}
									className="h-control-sm w-14 rounded-md border border-strong bg-app px-2 text-right type-mono-ui text-primary outline-none focus-visible:focus-ring"
								/>
								days
							</label>
						) : null}
					</div>
				</div>
				{problem ? (
					<SeverityNote level="error" role="alert" className="type-ui">
						{problem.problem.errors?.map((f) => f.message).join(" ") ||
							problem.problem.detail ||
							problem.problem.title}
					</SeverityNote>
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
				<SeverityNote level="alert" className="type-ui">
					This is the only time the plaintext is shown. Only its hash is stored.
				</SeverityNote>
			</DialogHeader>
			<DialogBody className="gap-3">
				<div className="flex items-center gap-2 rounded-md border border-strong bg-subtle py-2 pr-2 pl-3">
					<span
						className="min-w-0 flex-1 break-all type-mono-sm text-primary"
						data-testid="minted-secret"
					>
						{minted.token}
					</span>
					<Button variant="secondary" size="sm" onClick={copy}>
						<Icon name="copy" className="size-3.5" />
						{copied === "copied"
							? "Copied"
							: copied === "failed"
								? "Select and copy"
								: "Copy"}
					</Button>
				</div>
				<div className="flex flex-col gap-1.5">
					<span className="type-label text-secondary">Enroll a host</span>
					<pre className="rounded-md border border-default bg-subtle px-3 py-2 type-mono-sm whitespace-pre-wrap text-secondary">
						{`innerwall-agent enroll --server ${gateway} \\\n  --token ${masked(minted.token)} --bootstrap-ca ./innerwall-ca.crt`}
					</pre>
					<span className="type-caption text-tertiary">
						Distribute the CA certificate alongside the token; the agent uses it
						to verify the control plane on first contact.
						{configured
							? null
							: " The agent gateway is the control plane's agent listener, host:port; set --gateway-advertise-address on the control plane to fill it in."}
					</span>
				</div>
			</DialogBody>
			<DialogFooter>
				<span className="mr-auto type-caption text-tertiary">
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
