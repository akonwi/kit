import { afterEach, expect, test } from "bun:test";
import type { ImageRenderable, ScrollBoxRenderable } from "@opentui/core";
import { testRender } from "@opentui/solid";
import { applyImagePreviewCanvasLayout } from "./ImagePreviewPanel";

const PNG_1X1 = Uint8Array.from(
	Buffer.from(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4AWP4z8DwHwAFAAH/e+m+7wAAAABJRU5ErkJggg==",
		"base64",
	),
);

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

test("image preview canvas enables two-dimensional overflow", async () => {
	let scroll: ScrollBoxRenderable | undefined;
	let image: ImageRenderable | undefined;
	testSetup = await testRender(
		() => (
			<scrollbox
				ref={(value) => {
					scroll = value;
				}}
				width={40}
				height={10}
				scrollX
				scrollY
			>
				<image
					ref={(value) => {
						image = value;
					}}
					source={PNG_1X1}
					width={1}
					height={1}
					flexShrink={0}
				/>
			</scrollbox>
		),
		{ width: 40, height: 10 },
	);
	await testSetup.renderOnce();

	applyImagePreviewCanvasLayout(scroll, image, 80, 20);
	await testSetup.renderOnce();

	expect(image?.width).toBe(80);
	expect(image?.height).toBe(20);
	expect(scroll?.scrollWidth).toBe(80);
	expect(scroll?.scrollHeight).toBe(20);
	scroll?.scrollBy({ x: 4, y: 3 });
	expect(scroll?.scrollLeft).toBe(4);
	expect(scroll?.scrollTop).toBe(3);
});
