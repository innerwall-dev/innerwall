import { useState } from "react";
import { useNavigate } from "react-router";
import { useMe, useSession } from "@/auth/SessionProvider";
import { Icon } from "@/components/Icon";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { type Theme, useTheme } from "@/theme/ThemeProvider";

// initials are the operator's display name as the avatar draws it: the
// first letter of its first and last words, or of its one word.
export function initials(name: string): string {
	const words = name.split(/[\s.]+/).filter((w) => w.length > 0);
	if (words.length === 0) return "?";
	const first = words[0].charAt(0);
	const last = words.length > 1 ? words[words.length - 1].charAt(0) : "";
	return `${first}${last}`.toUpperCase();
}

// AccountPopover is the operator's entry at the header's far right: a
// 26px avatar (initials on the selected fill behind the border) whose
// popover holds the identity, the control plane's version, the theme
// toggle, and sign out. It opens below the avatar, anchored to its
// right edge.
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
					title={name}
					className="flex size-avatar shrink-0 cursor-pointer items-center justify-center rounded-full border border-overlay bg-overlay text-[10.5px] leading-none font-medium text-secondary hover:border-control-hover hover:text-primary focus-visible:focus-ring aria-expanded:border-control-hover"
				>
					<span aria-hidden="true">{initials(name)}</span>
				</button>
			</PopoverTrigger>
			<PopoverContent
				className="flex w-popover-account flex-col rounded-dialog p-0"
				side="bottom"
				align="end"
				sideOffset={8}
				collisionPadding={8}
				aria-label="Account"
			>
				<div className="flex flex-col gap-0.5 border-b border-hairline px-4 py-3.5">
					<div className="truncate type-dense-strong text-primary">{name}</div>
					<div
						className="truncate type-caption text-secondary"
						data-testid="identity-line"
					>
						{identity}
					</div>
					{me.version ? (
						<div
							className="truncate type-caption text-tertiary"
							data-testid="version-line"
							title={me.version}
						>
							control plane <span className="type-mono-xs">{me.version}</span>
						</div>
					) : null}
				</div>
				<div className="flex items-center justify-between gap-2 border-b border-hairline px-4 py-2.5 type-dense text-secondary">
					<span>Theme</span>
					<ThemeToggle theme={theme} onChange={setTheme} />
				</div>
				<button
					type="button"
					className="flex h-[38px] cursor-pointer items-center gap-2 rounded-b-dialog px-4 text-left type-dense text-primary hover:bg-wash-hover focus-visible:focus-ring-inset disabled:cursor-default disabled:text-disabled"
					onClick={signOut}
					disabled={signingOut}
				>
					<Icon name="log-out" />
					Sign out
				</button>
				{failure ? (
					<p
						className="px-4 pb-2 type-caption text-status-critical-fg"
						role="alert"
					>
						{failure}
					</p>
				) : null}
			</PopoverContent>
		</Popover>
	);
}

// The segmented control: native radio inputs, two segments on a sunk
// track behind the overlay border, the checked one raised (the raised
// control on the overlay border) with its icon at icon-active.
function ThemeToggle({
	theme,
	onChange,
}: {
	theme: Theme;
	onChange: (t: Theme) => void;
}) {
	const options: { value: Theme; label: string; icon: "sun" | "moon" }[] = [
		{ value: "light", label: "Light", icon: "sun" },
		{ value: "dark", label: "Dark", icon: "moon" },
	];
	return (
		<fieldset className="flex gap-0.5 rounded-[7px] border-0 bg-overlay p-0.5">
			<legend className="sr-only">Theme</legend>
			{options.map((o) => (
				<label
					key={o.value}
					className={cn(
						"inline-flex h-5.5 cursor-pointer items-center gap-1 rounded-[5px] border px-2.5 type-control has-[:focus-visible]:focus-ring",
						theme === o.value
							? "border-overlay bg-control-raised text-primary [&_[data-slot=icon]]:text-icon-active"
							: "border-transparent text-tertiary hover:text-primary",
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
					<Icon name={o.icon} />
					{o.label}
				</label>
			))}
		</fieldset>
	);
}
