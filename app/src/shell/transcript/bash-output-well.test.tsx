import { afterEach, expect, test } from "bun:test";
import type { ScrollBoxRenderable } from "@opentui/core";
import { createMockMouse } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import { BashOutputWell } from "./bash-output-well";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

test("bash output well clamps long output and shows its line count", async () => {
	const lines = Array.from(
		{ length: 20 },
		(_, index) => `output line ${index + 1}`,
	);
	testSetup = await testRender(() => <BashOutputWell lines={lines} />, {
		width: 60,
		height: 20,
	});

	await testSetup.renderOnce();
	await Bun.sleep(0);
	await testSetup.renderOnce();
	const frame = testSetup.captureCharFrame();
	expect(frame).toContain("output line 1");
	expect(frame).toContain("output line 14");
	expect(frame).not.toContain("output line 15");
	expect(frame).toContain("20 lines");

	const mouse = createMockMouse(testSetup.renderer);
	await mouse.scroll(10, 5, "down");
	await Bun.sleep(0);
	await testSetup.renderOnce();
	const scrolledFrame = testSetup.captureCharFrame();
	expect(scrolledFrame.split("\n")[0]).toContain("output line 2");
	expect(scrolledFrame).toContain("20 lines");
});

test("bash output well does not add position metadata to short output", async () => {
	testSetup = await testRender(
		() => <BashOutputWell lines={["first", "second"]} />,
		{ width: 40, height: 8 },
	);

	await testSetup.renderOnce();
	await Bun.sleep(0);
	await testSetup.renderOnce();
	const frame = testSetup.captureCharFrame();
	expect(frame).toContain("first");
	expect(frame).toContain("second");
	expect(frame).not.toContain("2 lines");
});

test("bash output wheel scrolling stays inner until it reaches an edge", async () => {
	let outerRef: ScrollBoxRenderable | undefined;
	const lines = Array.from(
		{ length: 30 },
		(_, index) => `inner line ${index + 1}`,
	);
	testSetup = await testRender(
		() => (
			<scrollbox
				ref={(value) => {
					outerRef = value;
				}}
				height={12}
				scrollY
				contentOptions={{ flexDirection: "column", width: "100%" }}
			>
				<text>before</text>
				<BashOutputWell lines={lines} />
				<text>after</text>
			</scrollbox>
		),
		{ width: 60, height: 16 },
	);

	await testSetup.renderOnce();
	await Bun.sleep(0);
	await testSetup.renderOnce();
	const mouse = createMockMouse(testSetup.renderer);
	await mouse.scroll(10, 4, "down");
	expect(outerRef?.scrollTop).toBe(0);

	for (let index = 0; index < 40; index += 1) {
		await mouse.scroll(10, 4, "down");
	}
	expect(outerRef?.scrollTop).toBeGreaterThan(0);
});

test("successful output starts at the top", async () => {
	const lines = Array.from(
		{ length: 20 },
		(_, index) => `live line ${index + 1}`,
	);
	testSetup = await testRender(
		() => <BashOutputWell lines={lines} stickyBottom={false} />,
		{ width: 60, height: 20 },
	);

	await testSetup.renderOnce();
	await Bun.sleep(0);
	await testSetup.renderOnce();
	const frame = testSetup.captureCharFrame();
	expect(frame.split("\n")[0]).toContain("live line 1");
});

test("live and failed output starts at the bottom", async () => {
	const lines = Array.from(
		{ length: 20 },
		(_, index) => `tail line ${index + 1}`,
	);
	testSetup = await testRender(
		() => <BashOutputWell lines={lines} stickyBottom />,
		{ width: 60, height: 20 },
	);

	await testSetup.renderOnce();
	await Bun.sleep(0);
	await testSetup.renderOnce();
	const frame = testSetup.captureCharFrame();
	expect(frame).toContain("tail line 20");
});

test("bash output wrapping accounts for tabs and grapheme clusters", async () => {
	const emoji = "👩‍💻";
	testSetup = await testRender(
		() => <BashOutputWell lines={[`a\tb\tc\td\tend ${emoji.repeat(8)}`]} />,
		{ width: 20, height: 10 },
	);

	await testSetup.renderOnce();
	await Bun.sleep(0);
	await testSetup.renderOnce();
	const frame = testSetup.captureCharFrame();
	expect(frame).toContain("a  b  c");
	expect(frame).toContain("end");
	expect(frame).toContain(emoji);
});

test("bash output well measures wrapped rows before applying its height cap", async () => {
	testSetup = await testRender(
		() => <BashOutputWell lines={["x".repeat(600)]} />,
		{ width: 30, height: 22 },
	);

	await testSetup.renderOnce();
	await Bun.sleep(0);
	await testSetup.renderOnce();
	const frame = testSetup.captureCharFrame();
	expect(frame).toContain("1 line");
});
