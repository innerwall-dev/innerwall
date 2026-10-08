import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Icon } from "./Icon";
import { type GlyphStatus, StatusGlyph, tone } from "./StatusGlyph";

const all: GlyphStatus[] = [
	"observed",
	"allowed",
	"would-block",
	"blocked",
	"synced",
	"pending",
	"degraded",
	"offline",
	"alert",
	"error",
];

describe("status glyphs", () => {
	it("draws every status and severity in its own shape and tone", () => {
		const { container } = render(
			<div>
				{all.map((s) => (
					<StatusGlyph key={s} status={s} label={s} />
				))}
			</div>,
		);
		const shapes = new Set<string>();
		for (const s of all) {
			const g = screen.getByRole("img", { name: s });
			expect(g).toHaveAttribute("data-status", s);
			expect(g).toHaveClass(tone[s]);
			shapes.add(g.innerHTML.replace(/ class="[^"]*"/g, ""));
		}
		expect(shapes.size).toBe(all.length);
		expect(container.querySelectorAll("svg.lucide")).toHaveLength(0);
	});

	it("hides a glyph from assistive technology unless it is labelled", () => {
		const { container } = render(<StatusGlyph status="blocked" />);
		expect(container.querySelector("svg")).toHaveAttribute(
			"aria-hidden",
			"true",
		);
	});
});

describe("chrome icons", () => {
	it("are Lucide at 16px and stroke 2, in icon-default, never a status color", () => {
		const { container } = render(<Icon name="server" />);
		const svg = container.querySelector("svg");
		expect(svg).toHaveClass("lucide", "text-icon-default");
		expect(svg).toHaveAttribute("width", "16");
		expect(svg).toHaveAttribute("stroke-width", "2");
		expect(svg?.getAttribute("class")).not.toMatch(
			/text-(status|flow|health)-/,
		);
	});
});
