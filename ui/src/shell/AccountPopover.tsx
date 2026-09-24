import { useState } from "react";
import { useNavigate } from "react-router";
import { useMe, useSession } from "@/auth/SessionProvider";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { type Theme, useTheme } from "@/theme/ThemeProvider";

// initials reduces a display name to the two letters the avatar shows;
// a control plane whose operator has no display name gets the generic
// operator mark.
export function initials(name: string | null): string {
	if (!name) return "OP";
	const parts = name
		.trim()
		.split(/[\s.\-_]+/)
		.filter(Boolean);
	if (parts.length === 0) return "OP";
	if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
	return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

// Avatar is the initials disc. The trigger's is 28px with a hairline
// that turns gold on hover; the popover's own is 30px.
function Avatar({
	name,
	className,
}: {
	name: string | null;
	className?: string;
}) {
	return (
		<span
			className={cn(
				"inline-flex shrink-0 items-center justify-center rounded-full border border-input-strong bg-muted font-sans text-[11px] font-semibold text-foreground",
				className,
			)}
			aria-hidden="true"
		>
			{initials(name)}
		</span>
	);
}

export function AccountPopover() {
	const me = useMe();
	const { logout } = useSession();
	const { theme, setTheme } = useTheme();
	const navigate = useNavigate();
	const [open, setOpen] = useState(false);
	const [signingOut, setSigningOut] = useState(false);
	const [failure, setFailure] = useState<string | null>(null);

	async function signOut() {
		setSigningOut(true);
		setFailure(null);
		try {
			await logout();
			setOpen(false);
			navigate("/login", { replace: true });
		} catch (err) {
			setFailure(err instanceof Error ? err.message : "sign out failed");
		} finally {
			setSigningOut(false);
		}
	}

	const name = me.display_name ?? "Operator";
	const identity = me.site ? `operator · ${me.site}` : "operator";

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger asChild>
				<button
					type="button"
					aria-label="Account"
					className="group shrink-0 rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
				>
					<Avatar
						name={me.display_name}
						className="size-7 tracking-[0.02em] transition-colors group-hover:border-ring"
					/>
				</button>
			</PopoverTrigger>
			<PopoverContent
				className="flex w-[236px] flex-col gap-0.5 p-1.5"
				sideOffset={8}
				aria-label="Account"
			>
				<div className="mb-1 flex items-center gap-2.5 border-b border-input px-2.5 pt-2 pb-2.5">
					<Avatar name={me.display_name} className="size-[30px]" />
					<div className="flex min-w-0 flex-col">
						<div className="truncate text-[13px] font-semibold">{name}</div>
						<div
							className="truncate font-mono text-[11px] text-muted-foreground"
							data-testid="identity-line"
						>
							{identity}
						</div>
					</div>
				</div>
				<div className="flex items-center justify-between px-2.5 py-[7px] text-[12.5px]">
					<span>Theme</span>
					<ThemeToggle theme={theme} onChange={setTheme} />
				</div>
				<button
					type="button"
					className="flex cursor-pointer items-center gap-2.5 rounded px-2.5 py-[7px] text-left text-[12.5px] hover:bg-muted disabled:cursor-default disabled:opacity-50"
					onClick={signOut}
					disabled={signingOut}
				>
					<span
						className="w-3.5 font-mono text-foreground-glyph"
						aria-hidden="true"
					>
						→
					</span>
					Sign out
				</button>
				{failure ? (
					<p className="px-2.5 pb-1 text-[12px] text-destructive" role="alert">
						{failure}
					</p>
				) : null}
			</PopoverContent>
		</Popover>
	);
}

// The segmented control: native radio inputs, visually two segments in
// one bordered group, the checked one the gold fill.
function ThemeToggle({
	theme,
	onChange,
}: {
	theme: Theme;
	onChange: (t: Theme) => void;
}) {
	const options: { value: Theme; label: string }[] = [
		{ value: "dark", label: "Dark" },
		{ value: "light", label: "Light" },
	];
	return (
		<fieldset className="flex overflow-hidden rounded border border-input-strong text-[11.5px]">
			<legend className="sr-only">Theme</legend>
			{options.map((o, i) => (
				<label
					key={o.value}
					className={cn(
						"cursor-pointer px-2.5 py-[3px] transition-colors",
						i > 0 && "border-l border-input-strong",
						theme === o.value
							? "bg-primary font-semibold text-primary-foreground"
							: "text-foreground-tertiary hover:text-foreground",
					)}
				>
					<input
						type="radio"
						name="theme"
						value={o.value}
						checked={theme === o.value}
						onChange={() => onChange(o.value)}
						className="sr-only"
					/>
					{o.label}
				</label>
			))}
		</fieldset>
	);
}
