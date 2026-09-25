import { afterEach, expect, test } from "bun:test";
import type { ScrollBoxRenderable } from "@opentui/core";
import { testRender } from "@opentui/solid";
import type { AssistantMessage } from "../../runtime/agent";
import {
	type TurnActivityModel,
	type TurnActivitySection,
	TurnActivitySectionList,
} from "./turn-activity-view";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

function activityModel(sectionCount: number): TurnActivityModel {
	const sections: TurnActivitySection[] = Array.from(
		{ length: sectionCount },
		(_, index) => ({
			kind: "assistant",
			id: `section-${index}`,
			message: {
				role: "assistant",
				content: [
					{ type: "text", text: `Step ${index}` },
					{
						type: "toolCall",
						id: `tool-${index}`,
						name: "read",
						arguments: { path: `file-${index}.ts` },
					},
				],
				timestamp: index + 1,
			} as AssistantMessage,
			toolResults: new Map(),
		}),
	);
	const sectionsById = new Map(
		sections.map((section) => [section.id, section]),
	);
	return {
		sections: () => sections,
		sectionOrder: () => sections.map((section) => section.id),
		sectionsById: () => sectionsById,
		turnLiveTools: () => ({}),
		toolCallCount: () => 0,
		stepCount: () => sections.length,
		initiallyLive: false,
	};
}

test("activity sections are direct scrollbox children for viewport culling", async () => {
	let scroll: ScrollBoxRenderable | undefined;
	const model = activityModel(20);
	testSetup = await testRender(
		() => (
			<scrollbox
				ref={(value) => {
					scroll = value;
				}}
				width={60}
				height={8}
				scrollY
				contentOptions={{ flexDirection: "column", width: "100%" }}
			>
				<TurnActivitySectionList model={model} />
			</scrollbox>
		),
		{ width: 60, height: 8 },
	);
	await testSetup.renderOnce();

	expect(scroll?.getChildren()).toHaveLength(20);
});
