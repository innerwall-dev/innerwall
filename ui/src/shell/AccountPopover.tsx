import { useState } from "react";
import { useNavigate } from "react-router";
import { useMe, useSession } from "@/auth/SessionProvider";
import { Icon } from "@/components/Icon";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { useSidebar } from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";
import { type Theme, useTheme } from "@/theme/ThemeProvider";

// Avatar is the operator's disc: the user icon in an avatar-sized
// circle on bg-active behind the default hairline.
function Avatar() {
	return (
		<span
			className="flex size-avatar shrink-0 items-center justify-center rounded-full border border-default bg-active"
			aria-hidden="true"
		>
			<Icon name="user" />
		</span>
	);
}

// AccountPopover is the sidebar footer's operator entry: the avatar,
// the display name and site label, and the popover with the identity,
// the theme toggle, and sign out. It opens upward from the expanded
// rail and to the right of the collapsed one.
export function AccountPopover() {
	const me = useMe();
	const { logout } = useSession();
	const { theme, setTheme } = useTheme();
	const { state, isMobile } = useSidebar();
	const collapsed = state === "collapsed" && !isMobile;
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
					className={cn(
						"flex h-11 w-full cursor-pointer items-center gap-2 rounded-md px-2 text-left hover:bg-sidebar-item-hover aria-expanded:bg-sidebar-item-hover",
						"group-data-[collapsible=icon]:w-10 group-data-[collapsible=icon]:px-1.5",
					)}
				>
					<Avatar />
					<span className="flex min-w-0 flex-1 flex-col group-data-[collapsible=icon]:hidden">
						<span className="truncate text-[13px] leading-4 font-medium text-primary">
							{name}
						</span>
						{me.site ? (
							<span
								className="truncate type-caption text-tertiary"
								data-testid="site-label"
							>
								{me.site}
							</span>
						) : null}
					</span>
					<Icon
						name="chevrons-up-down"
						className="group-data-[collapsible=icon]:hidden"
					/>
				</button>
			</PopoverTrigger>
			<PopoverContent
				className="flex w-popover flex-col p-1"
				side={collapsed ? "right" : "top"}
				align={collapsed ? "end" : "start"}
				sideOffset={collapsed ? 12 : 4}
				aria-label="Account"
			>
				<div className="mb-1 flex flex-col border-b border-subtle p-2">
					<div className="truncate text-[13px] leading-4 font-medium">
						{name}
					</div>
					<div
						className="truncate type-caption text-tertiary"
						data-testid="identity-line"
					>
						{identity}
					</div>
				</div>
				<div className="flex min-h-control-md items-center justify-between gap-2 px-2 type-ui text-secondary">
					<span>Theme</span>
					<ThemeToggle theme={theme} onChange={setTheme} />
				</div>
				<button
					type="button"
					className="mt-1 flex h-control-md cursor-pointer items-center gap-2 rounded-b-md border-t border-subtle px-2 text-left type-ui text-primary hover:bg-hover disabled:cursor-default disabled:opacity-disabled"
					onClick={signOut}
					disabled={signingOut}
				>
					<Icon name="log-out" />
					Sign out
				</button>
				{failure ? (
					<p
						className="px-2 pb-1 type-caption text-status-critical-fg"
						role="alert"
					>
						{failure}
					</p>
				) : null}
			</PopoverContent>
		</Popover>
	);
}

// The segmented control: native radio inputs, two segments in one
// hairline group on bg-subtle, the checked one the pressed fill with its
// icon at icon-active.
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
		<fieldset className="flex gap-0.5 rounded-md border border-default bg-subtle p-0.5">
			<legend className="sr-only">Theme</legend>
			{options.map((o) => (
				<label
					key={o.value}
					className={cn(
						"inline-flex h-6 cursor-pointer items-center gap-1 rounded-sm px-2 type-caption has-[:focus-visible]:focus-ring",
						theme === o.value
							? "bg-active font-medium text-primary [&_[data-slot=icon]]:text-icon-active"
							: "text-secondary hover:text-primary",
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
					<Icon name={o.icon} className="size-3.5" />
					{o.label}
				</label>
			))}
		</fieldset>
	);
}
