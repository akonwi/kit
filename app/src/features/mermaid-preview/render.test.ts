import { describe, expect, test } from "bun:test";
import { renderMermaidPreviewImage } from "./render";

const COLORS = {
	background: "#0a0a0a",
	surface: "#171717",
	text: "#fafafa",
	mutedText: "#a1a1a1",
	border: "#404040",
	line: "#d4d4d4",
};

describe("Mermaid visual preview", () => {
	test("renders a complex diagram as bounded PNG bytes", async () => {
		const relationships = Array.from(
			{ length: 30 },
			(_, index) =>
				`N${index}[Step ${index}] --> N${index + 1}[Step ${index + 1}]`,
		).join("\n");
		const image = await renderMermaidPreviewImage(
			`flowchart TD\n${relationships}`,
			COLORS,
		);

		expect(Array.from(image.png.slice(0, 8))).toEqual([
			137, 80, 78, 71, 13, 10, 26, 10,
		]);
		expect(image.width).toBeGreaterThan(0);
		expect(image.width).toBeLessThanOrEqual(1_600);
		expect(image.height).toBeGreaterThan(0);
		expect(image.png.byteLength).toBeLessThan(8_000_000);
	});

	test("downscales diagrams whose natural SVG exceeds preview dimensions", async () => {
		const chain = Array.from(
			{ length: 40 },
			(_, index) => `N${index}[Stage ${index}]`,
		).join(" --> ");
		const image = await renderMermaidPreviewImage(
			`flowchart LR\n${chain}`,
			COLORS,
		);

		expect(image.width).toBeLessThanOrEqual(2_000);
		expect(image.height).toBeLessThanOrEqual(8_000);
		expect(image.width * image.height).toBeLessThanOrEqual(10_000_000);
	});

	test("rejects malformed Mermaid source", async () => {
		expect(
			renderMermaidPreviewImage("not a Mermaid diagram", COLORS),
		).rejects.toBeInstanceOf(Error);
	});
});
