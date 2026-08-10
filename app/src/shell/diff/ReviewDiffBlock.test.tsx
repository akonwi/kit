import { afterEach, expect, test } from "bun:test";
import { createMockMouse } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import { createSignal } from "solid-js";
import { estimateWrappedRows, ReviewDiffBlock } from "./ReviewDiffBlock";
import type { ReviewHunk } from "./types";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

const sourceText =
	"\t'\\ttest(\"keeps wrapped content inside a narrow gutterless view\", async () => {';";

const hunk: ReviewHunk = {
	id: "hunk-wrap",
	noteKey: "hunk-wrap",
	header: "@@ -1,0 +13,2 @@",
	context: "",
	lines: [
		{ kind: "add", text: sourceText, additionLineNumber: 13 },
		{ kind: "add", text: "next();", additionLineNumber: 14 },
	],
	changeCount: 2,
	rawPatch: "",
	patchStartLine: 0,
	patchLineCount: 2,
	additionStart: 13,
	additionCount: 2,
	deletionStart: 1,
	deletionCount: 0,
	collapsedBefore: 0,
};

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

function ResizableDiff() {
	const [width, setWidth] = createSignal(121);
	let boxRef: { width: number } | undefined;
	return (
		<box
			ref={(value) => {
				boxRef = value as typeof boxRef;
			}}
			width="100%"
			height={6}
			onSizeChange={() => setWidth(boxRef?.width ?? 121)}
		>
			<scrollbox flexGrow={1} scrollY>
				<box width="100%">
					<box width="100%">
						<ReviewDiffBlock
							hunk={hunk}
							view="unified"
							filetype="typescript"
							lineNumberWidth={2}
							contentColumns={width() - 8}
						/>
					</box>
				</box>
			</scrollbox>
		</box>
	);
}

test("line markers remain part of the clickable diff gutter", async () => {
	let clicks = 0;
	testSetup = await testRender(
		() => (
			<ReviewDiffBlock
				hunk={hunk}
				view="unified"
				lineNumberWidth={2}
				lineMarker={() => "anchor"}
				onLineMouseDown={() => {
					clicks += 1;
				}}
			/>
		),
		{ width: 40, height: 4 },
	);
	await testSetup.renderOnce();
	expect(testSetup.captureCharFrame().split("\n")[0]).toContain("+◆");

	const mouse = createMockMouse(testSetup.renderer);
	await mouse.pressDown(6, 0);
	expect(clicks).toBe(1);
	await mouse.release(6, 0);
});

test("raw patch rows reserve an overlaid scrollbar column", async () => {
	testSetup = await testRender(
		() => (
			<ReviewDiffBlock
				rawPatch={`+${"x".repeat(15)}`}
				view="unified"
				contentColumns={() => 14}
				contentRightInset={1}
			/>
		),
		{ width: 20, height: 3 },
	);
	await testSetup.renderOnce();
	await testSetup.renderOnce();

	const lines = testSetup.captureCharFrame().split("\n");
	expect(lines[0].trimEnd()).toBe(`   + ${"x".repeat(14)}`);
	expect(lines[1].trimEnd()).toBe("     x");
});

test("split rows remeasure both cells against the resized scrollbar-safe width", async () => {
	testSetup = await testRender(
		() => (
			<ReviewDiffBlock
				hunk={hunk}
				view="split"
				lineNumberWidth={2}
				contentColumns={26}
				contentRightInset={1}
			/>
		),
		{ width: 60, height: 20 },
	);
	await testSetup.renderOnce();
	testSetup.resize(30, 20);
	await testSetup.renderOnce();
	await testSetup.renderOnce();

	const lines = testSetup.captureCharFrame().split("\n");
	const nextLineTop = lines.findIndex((line) => line.includes("next();"));
	expect(nextLineTop).toBe(estimateWrappedRows(sourceText, 10));
});

test("unified rows wrap syntax-highlighted content after resizing", async () => {
	testSetup = await testRender(() => <ResizableDiff />, {
		width: 121,
		height: 6,
	});
	await testSetup.renderOnce();
	const wideLines = testSetup
		.captureCharFrame()
		.split("\n")
		.map((line) => line.replace(/█+$/, "").trimEnd());
	expect(wideLines[1]).toBe("   14+ next();");

	testSetup.resize(76, 6);
	await testSetup.renderOnce();
	await testSetup.renderOnce();
	const lines = testSetup
		.captureCharFrame()
		.split("\n")
		.map((line) => line.replace(/█+$/, "").trimEnd());

	expect(lines[0]).toBe(
		'   13+   \'\\ttest("keeps wrapped content inside a narrow gutterless view",',
	);
	expect(lines[1]).toBe("       async () => {';");
	expect(lines[2]).toBe("   14+ next();");
});
