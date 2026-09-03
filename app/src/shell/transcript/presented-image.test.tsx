import { afterEach, expect, test } from "bun:test";
import { type BaseRenderable, ImageRenderable } from "@opentui/core";
import { testRender } from "@opentui/solid";
import {
	extractPresentedToolImage,
	PresentedImage,
	type PresentedToolImage,
} from "./presented-image";

const PNG_1X1 =
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4AWP4z8DwHwAFAAH/e+m+7wAAAABJRU5ErkJggg==";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

function findImage(root: BaseRenderable): ImageRenderable | null {
	if (root instanceof ImageRenderable) return root;
	for (const child of root.getChildren()) {
		const image = findImage(child);
		if (image) return image;
	}
	return null;
}

const image: PresentedToolImage = {
	data: PNG_1X1,
	mimeType: "image/png",
	path: "/tmp/pixel.png",
	filename: "pixel.png",
	width: 1,
	height: 1,
};

test("extractPresentedToolImage accepts show_image presentation results", () => {
	expect(
		extractPresentedToolImage("show_image", {
			content: [
				{ type: "text", text: "Displayed image" },
				{ type: "image", data: "encoded", mimeType: "image/png" },
			],
			details: {
				presentation: "transcript-image",
				path: "/tmp/image.png",
				filename: "image.png",
				caption: "Result",
				mimeType: "image/png",
				width: 800,
				height: 600,
				bytes: 10,
			},
		}),
	).toEqual({
		data: "encoded",
		mimeType: "image/png",
		path: "/tmp/image.png",
		filename: "image.png",
		caption: "Result",
		width: 800,
		height: 600,
	});
});

test("extractPresentedToolImage ignores ordinary image tool results", () => {
	expect(
		extractPresentedToolImage("screenshot", {
			content: [{ type: "image", data: "encoded", mimeType: "image/png" }],
			details: {},
		}),
	).toBeNull();
});

test("restored presented images remain collapsed until requested", async () => {
	testSetup = await testRender(
		() => (
			<PresentedImage
				image={image}
				expanded={false}
				onExpandedChange={() => {}}
				showToast={() => {}}
			/>
		),
		{ width: 80, height: 20 },
	);
	await testSetup.renderOnce();

	expect(testSetup.captureCharFrame()).toContain("pixel.png");
	expect(findImage(testSetup.renderer.root)).toBeNull();
});

test("newly presented images render through OpenTUI's image component", async () => {
	testSetup = await testRender(
		() => (
			<PresentedImage
				image={image}
				expanded
				onExpandedChange={() => {}}
				showToast={() => {}}
			/>
		),
		{ width: 80, height: 20 },
	);
	const renderable = findImage(testSetup.renderer.root);
	await renderable?.loadPromise;
	await testSetup.renderOnce();

	expect(renderable).toBeInstanceOf(ImageRenderable);
	expect(testSetup.captureCharFrame()).toContain("█");
});
