import { afterEach, expect, test } from "bun:test";
import type { ScrollBoxRenderable } from "@opentui/core";
import { testRender } from "@opentui/solid";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { TranscriptPane } from "./pane";
import type { TranscriptItem } from "./turns";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

function runtimeStub(): AgentRuntime {
	return {
		getStatus: () => ({ isStreaming: false }),
		getTurns: () => [],
		subscribe: () => () => {},
	} as unknown as AgentRuntime;
}

async function openOverlayStub<T>(): Promise<T> {
	return undefined as T;
}

function userItems(count: number): TranscriptItem[] {
	return Array.from({ length: count }, (_, index) => ({
		kind: "user" as const,
		id: `message-${index}`,
		turnId: `turn-${index}`,
		message: {
			role: "user" as const,
			content: `Message ${index}`,
			timestamp: index + 1,
		},
		aborted: false,
	}));
}

test("transcript turns are direct scrollbox children for viewport culling", async () => {
	testSetup = await testRender(
		() => (
			<TranscriptPane
				runtime={runtimeStub()}
				items={userItems(20)}
				showToast={() => {}}
				openOverlay={openOverlayStub}
				openActivity={() => {}}
				openSubagent={() => false}
				openImage={() => {}}
				openMessageContextMenu={() => {}}
			/>
		),
		{ width: 80, height: 12 },
	);
	await testSetup.renderOnce();

	const scroll = testSetup.renderer.root.findDescendantById(
		"transcript-scrollbox",
	) as ScrollBoxRenderable | undefined;
	expect(scroll?.getChildren()).toHaveLength(20);
});
