import { useState } from "react";
import { useNavigate } from "react-router";
import { useMe, useSession } from "@/auth/SessionProvider";
import { Button } from "@/components/ui/button";
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
				"inline-flex size-7 items-center justify-center rounded-full border border-input-strong bg-muted font-sans text-[11px] font-semibold text-foreground",
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
					className="rounded-full transition-colors hover:ring-1 hover:ring-ring focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
				>
					<Avatar name={me.display_name} />
				</button>
			</PopoverTrigger>
			<PopoverContent className="w-[232px] p-3" aria-label="Account">
				<div className="flex items-center gap-2.5 px-1 pt-0.5">
					<Avatar name={me.display_name} />
					<div className="min-w-0">
						<div className="truncate text-[13px] font-semibold leading-tight">
							{name}
						</div>
						<div
							className="truncate font-mono text-[11px] text-muted-foreground"
							data-testid="identity-line"
						>
							{identity}
						</div>
					</div>
				</div>
				<div className="my-3 border-t border-border" />
				<div className="flex items-center justify-between px-1">
					<span className="text-[13px]">Theme</span>
					<ThemeToggle theme={theme} onChange={setTheme} />
				</div>
				<div className="my-3 border-t border-border" />
				<Button
					variant="ghost"
					className="h-8 w-full justify-start gap-3 px-1 text-[13px]"
					onClick={signOut}
					disabled={signingOut}
				>
					<span
						className="w-3 text-center font-mono text-[11px] text-foreground-glyph"
						aria-hidden="true"
					>
						→
					</span>
					Sign out
				</Button>
				{failure ? (
					<p className="mt-1 px-1 text-[12px] text-destructive" role="alert">
						{failure}
					</p>
				) : null}
			</PopoverContent>
		</Popover>
	);
}

// The segmented control: native radio inputs, visually a two-segment
// switch whose checked segment is the gold fill.
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
		<fieldset className="inline-flex h-7 items-stretch overflow-hidden rounded border border-input p-0.5">
			<legend className="sr-only">Theme</legend>
			{options.map((o) => (
				<label
					key={o.value}
					className={cn(
						"inline-flex cursor-pointer items-center rounded-pill px-2.5 text-[12px] leading-none transition-colors",
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
