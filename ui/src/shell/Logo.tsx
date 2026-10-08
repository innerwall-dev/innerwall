import { cn } from "@/lib/utils";

// The brand mark, drawn to the design's geometry on a 22-unit grid: an
// outlined square (the frame rect at x 1, y 1, 20 by 20, rx 3, with a 2
// stroke) holding a square-cornered bar (x 12, y 6, 4 by 10) that stands
// right of centre, a wall inside the room. The frame is
// logo-outline and the bar logo-fill, aliases of text-secondary and
// text-primary in both themes: the mark is gray, never a status or
// accent color. At its native 22px (logo-size) every edge lands on a
// whole pixel, so the mark is never drawn at other sizes.
export function LogoMark() {
	return (
		<svg
			width={22}
			height={22}
			viewBox="0 0 22 22"
			aria-hidden="true"
			className="shrink-0"
		>
			<rect
				x="1"
				y="1"
				width="20"
				height="20"
				rx="3"
				fill="none"
				className="stroke-logo-outline"
				strokeWidth="2"
			/>
			<rect x="12" y="6" width="4" height="10" className="fill-logo-fill" />
		</svg>
	);
}

// The lockup: the mark and the wordmark, the one arrangement the console
// shows its name in.
export function Lockup() {
	return (
		<span className="flex items-center gap-2">
			<LogoMark />
			<Wordmark />
		</span>
	);
}

// Wordmark is the console's name as the lockup sets it.
export function Wordmark({ className }: { className?: string }) {
	return (
		<span
			className={cn(
				"font-sans text-[14px] leading-5 font-semibold tracking-[-0.01em] text-primary",
				className,
			)}
		>
			Innerwall
		</span>
	);
}
