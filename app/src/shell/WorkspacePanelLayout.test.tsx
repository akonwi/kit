import { afterEach, describe, expect, test } from "bun:test";
import { testRender } from "@opentui/solid";
import {
	WorkspacePanelHeader,
	WorkspacePanelLayout,
} from "./WorkspacePanelLayout";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

describe("WorkspacePanelLayout", () => {
	test("omits the context row when a pane has no metadata", async () => {
		testSetup = await testRender(
			() => (
				<WorkspacePanelLayout footer={<text>footer</text>}>
					<text>body</text>
				</WorkspacePanelLayout>
			),
			{ width: 32, height: 6 },
		);
		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		expect(frame.startsWith("body")).toBeTrue();
		expect(frame).toContain("footer");
	});

	test("presents pane scope and live status as context", async () => {
		testSetup = await testRender(
			() => (
				<WorkspacePanelLayout
					header={
						<WorkspacePanelHeader
							left={<text>working tree</text>}
							right={<text>3 files</text>}
						/>
					}
					footer={<text>footer</text>}
				>
					<text>body</text>
				</WorkspacePanelLayout>
			),
			{ width: 32, height: 7 },
		);
		await testSetup.renderOnce();
		const firstRow = testSetup.captureCharFrame().split("\n")[0];
		expect(firstRow).toContain("working tree");
		expect(firstRow.trimEnd().endsWith("3 files")).toBeTrue();
	});
});
