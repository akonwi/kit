import { afterEach, describe, expect, test } from "bun:test";
import { RGBA } from "@opentui/core";
import { createMockMouse } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import { canRenderMermaid, KitMarkdown } from "./KitMarkdown";
import { renderMermaidCode } from "./mermaid-render";
import { theme } from "./theme";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

const FLOWCHART = `graph LR
  A[Start] --> B[Done]`;

function fenced(source: string, closed = true, language = "mermaid"): string {
	return `\`\`\`${language}\n${source}${closed ? "\n```\n" : ""}`;
}

describe("Mermaid Markdown", () => {
	test("renders a complete Mermaid flowchart as terminal text", async () => {
		const output = await renderMermaidCode(FLOWCHART);

		expect(output).not.toBeNull();
		expect(output).toContain("Start");
		expect(output).toContain("Done");
		expect(output).toContain("►");
	});

	test("renders a complete Mermaid sequence diagram", async () => {
		const source = `sequenceDiagram
  Alice->>Bob: Hello
  Bob-->>Alice: Hi`;
		const output = await renderMermaidCode(source);

		expect(output).not.toBeNull();
		expect(output).toContain("Alice");
		expect(output).toContain("Bob");
		expect(output).toContain("Hello");
	});

	test("renders a complete Mermaid state diagram", async () => {
		const source = `stateDiagram-v2
  [*] --> Active
  Active --> [*]`;
		const output = await renderMermaidCode(source);

		expect(output).not.toBeNull();
		expect(output).toContain("Active");
		expect(output).toContain("●");
	});

	test("waits for a closing fence before rendering", () => {
		expect(canRenderMermaid(FLOWCHART, fenced(FLOWCHART, false))).toBeFalse();
	});

	test("uses theme-aware text for raw Mermaid source", async () => {
		testSetup = await testRender(
			() => <KitMarkdown content={fenced(FLOWCHART, false)} />,
			{ width: 80, height: 5 },
		);

		await testSetup.renderOnce();
		const sourceSpan = testSetup
			.captureSpans()
			.lines.flatMap((line) => line.spans)
			.find((span) => span.text.includes("graph LR"));
		expect(sourceSpan?.fg.toInts()).toEqual(
			RGBA.fromHex(theme.textSecondary).toInts(),
		);
	});

	test("falls back for malformed Mermaid", async () => {
		expect(await renderMermaidCode("this is not a diagram")).toBeNull();
	});

	test("falls back before rendering oversized source", () => {
		const source = `graph LR\n  A[${"x".repeat(8_001)}] --> B`;
		expect(canRenderMermaid(source, fenced(source))).toBeFalse();
	});

	test("rejects compact diagrams with excessive graph complexity", () => {
		const chain = Array.from(
			{ length: 100 },
			(_, index) => `N${index}[x]`,
		).join(" --> ");
		const source = `graph LR\n${chain}`;
		expect(canRenderMermaid(source, fenced(source))).toBeFalse();
	});

	test("offers a visual preview for diagrams above inline limits", async () => {
		const chain = Array.from(
			{ length: 25 },
			(_, index) => `N${index}[Step ${index}]`,
		).join(" --> ");
		const source = `graph LR\n${chain}`;
		let openedSource = "";
		testSetup = await testRender(
			() => (
				<KitMarkdown
					content={fenced(source)}
					onOpenMermaid={(value) => {
						openedSource = value;
					}}
				/>
			),
			{ width: 100, height: 12 },
		);

		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		expect(frame).toContain("Open diagram");
		expect(frame).toContain("24 relationships");
		const row = frame
			.split("\n")
			.findIndex((line) => line.includes("Open diagram"));
		const column = frame.split("\n")[row]?.indexOf("Open diagram") ?? -1;
		expect(row).toBeGreaterThanOrEqual(0);
		expect(column).toBeGreaterThanOrEqual(0);
		const mouse = createMockMouse(testSetup.renderer);
		await mouse.pressDown(column, row);
		expect(openedSource).toBe("");
		await mouse.release(column, row);
		expect(openedSource).toBe(source);
		expect(testSetup.renderer.currentFocusedRenderable).toBeNull();
	});

	test("measures rendered output in terminal cells", async () => {
		const source = `graph LR\n  A[${"表".repeat(235)}]`;
		expect(await renderMermaidCode(source)).toBeNull();
	});

	test("replaces a Mermaid fence in the shared Markdown component", async () => {
		testSetup = await testRender(
			() => <KitMarkdown content={fenced(FLOWCHART)} />,
			{ width: 80, height: 10 },
		);

		let frame = "";
		for (let attempt = 0; attempt < 20; attempt += 1) {
			await Bun.sleep(25);
			await testSetup.renderOnce();
			frame = testSetup.captureCharFrame();
			if (frame.includes("►")) break;
		}
		expect(frame).toContain("Start");
		expect(frame).toContain("Done");
		expect(frame).toContain("►");
		expect(frame).not.toContain("graph LR");
		const diagramSpan = testSetup
			.captureSpans()
			.lines.flatMap((line) => line.spans)
			.find((span) => span.text.includes("Start"));
		expect(diagramSpan?.fg.toInts()).toEqual(
			RGBA.fromHex(theme.textSecondary).toInts(),
		);
	});

	test("renders diagrams that initially exceed worker queue capacity", async () => {
		const content = Array.from({ length: 9 }, (_, index) =>
			fenced(
				`graph LR\n  A${index}[Start ${index}] --> B${index}[Done ${index}]`,
			),
		).join("\n");
		testSetup = await testRender(() => <KitMarkdown content={content} />, {
			width: 80,
			height: 80,
		});

		let frame = "";
		for (let attempt = 0; attempt < 100; attempt += 1) {
			await Bun.sleep(25);
			await testSetup.renderOnce();
			frame = testSetup.captureCharFrame();
			if (!frame.includes("graph LR")) break;
		}
		expect(frame).toContain("Start 0");
		expect(frame).toContain("Done 8");
		expect(frame).not.toContain("graph LR");
	});

	test("preserves non-Mermaid code blocks", async () => {
		const source = "const answer = 42;";
		testSetup = await testRender(
			() => <KitMarkdown content={fenced(source, true, "typescript")} />,
			{ width: 80, height: 5 },
		);

		await testSetup.renderOnce();
		expect(testSetup.captureCharFrame()).toContain(source);
	});
});
