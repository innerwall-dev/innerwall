// The brand mark, drawn to the design's geometry on a 22-unit grid: an
// outlined square (2 stroke, outer radius 4, inner radius 2) holding a
// square-cornered bar that stands right of centre, a wall inside the
// room. Colors are the logo tokens; the light theme's fill is a darker
// amber than the primary so the mark holds on the off-white sidebar.
// At its native 22px every edge lands on a whole pixel, so the mark is
// never drawn at other sizes.
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
// shows its name in. The wordmark's cap line sits half a pixel above
// where centering lands it; layout offsets snap to whole pixels, so the
// half pixel is a translate, which a 1x screen snaps without softening.
export function Lockup() {
	return (
		<span className="flex h-[22px] items-center gap-2.5">
			<LogoMark />
			<span className="-translate-y-[0.5px] text-[15px] font-semibold leading-none tracking-[-0.02em]">
				Innerwall
			</span>
		</span>
	);
}
