import { cn } from "@/lib/utils";

// Switch is an on/off control drawn as the design's toggle: the
// selection fill when on (it is a checkbox in another form), the strong
// hairline gray when off. It is a button with
// the switch role, so its state is announced, and its label names what
// it turns on.
export function Switch({
	on,
	label,
	onChange,
	disabled,
	size = "row",
	className,
}: {
	on: boolean;
	label: string;
	onChange: (on: boolean) => void;
	disabled?: boolean;
	size?: "row" | "header";
	className?: string;
}) {
	return (
		<button
			type="button"
			role="switch"
			aria-checked={on}
			aria-label={label}
			disabled={disabled}
			onClick={() => onChange(!on)}
			className={cn(
				"relative inline-flex shrink-0 cursor-pointer items-center rounded-full transition-colors disabled:cursor-default disabled:opacity-disabled",
				size === "header" ? "h-[16px] w-[30px]" : "h-[14px] w-[26px]",
				on ? "bg-selection-fg" : "bg-(--border-strong)",
				className,
			)}
		>
			<span
				aria-hidden="true"
				className={cn(
					"absolute rounded-full bg-app transition-[left]",
					size === "header" ? "size-[10px]" : "size-[9px]",
					on
						? size === "header"
							? "left-[17px]"
							: "left-[14px]"
						: "left-[3px]",
				)}
			/>
		</button>
	);
}
