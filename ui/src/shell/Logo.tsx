// The brand mark: an outlined rounded square with the gold bar. Colors
// are the logo tokens, which the light theme sets to a darker amber so
// the mark holds on the off-white sidebar.
export function LogoMark({ size = 26 }: { size?: number }) {
	return (
		<svg
			width={size}
			height={size}
			viewBox="0 0 32 32"
			aria-hidden="true"
			className="shrink-0"
		>
			<rect
				x="2"
				y="2"
				width="28"
				height="28"
				rx="7"
				fill="none"
				className="stroke-logo-outline"
				strokeWidth="2.5"
			/>
			<rect
				x="13"
				y="9"
				width="6"
				height="14"
				rx="1"
				className="fill-logo-fill"
			/>
		</svg>
	);
}
