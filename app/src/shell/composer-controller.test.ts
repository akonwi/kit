import { describe, expect, test } from "bun:test";
import { createCommandRegistry } from "../features/commands";
import { ImageAttachment } from "../features/images/attachment";
import { CodeReviewAttachment } from "../features/review/attachment";
import type { AgentMessage } from "../runtime/agent";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { createAttachmentsController } from "./attachments-controller";
import {
	createComposerController,
	extractPendingComposerText,
	mergePendingMessagesIntoComposer,
	type TextareaHandle,
} from "./composer-controller";

describe("queued message restoration", () => {
	test("restores only editable text from multipart messages", () => {
		const message = {
			role: "user",
			content: [
				{ type: "text", text: "describe this" },
				{ type: "image", data: "data", mimeType: "image/png" },
			],
			timestamp: 1,
		} as AgentMessage;
		expect(extractPendingComposerText(message)).toBe("describe this");
	});

	test("restores queued messages in order", () => {
		expect(mergePendingMessagesIntoComposer(["first", "second"], "")).toBe(
			"first\n\nsecond",
		);
	});

	test("preserves an existing composer draft after queued messages", () => {
		expect(
			mergePendingMessagesIntoComposer(["first", "second"], "current draft"),
		).toBe("first\n\nsecond\n\ncurrent draft");
	});

	test("restores queued text and attachments", () => {
		const queued = {
			role: "user",
			content: [
				{ type: "text", text: "queued text" },
				{
					type: "image",
					data: "base64-data",
					mimeType: "image/png",
					filename: "screenshot.png",
					sourcePath: "/tmp/screenshot.png",
				},
				{
					type: "code-review",
					review: {
						submittedAt: "2026-07-27T00:00:00.000Z",
						files: [
							{
								path: "src/example.ts",
								fileComment: "Please simplify this.",
								ranges: [],
							},
						],
					},
				},
			],
			timestamp: 1,
		} as AgentMessage;
		const runtime = {
			drainPendingMessages: () => [queued],
		} as unknown as AgentRuntime;
		const attachments = createAttachmentsController();
		attachments.attach({
			id: "existing",
			type: "test",
			icon: "",
			summary: "existing",
			toMessagePart: () => ({ type: "text", text: "existing" }),
			toPromptText: () => "existing",
		});
		const controller = createComposerController({
			runtime,
			commands: createCommandRegistry(),
			fileIndex: {} as never,
			threadIndex: null,
			attachments,
			reviewDrafts: {} as never,
			reviewWorkspace: {} as never,
			toast: () => {},
			_reload: async () => {},
			openCustomOverlay: async () => undefined as never,
		});
		let text = "current draft";
		controller.setTextarea({
			get plainText() {
				return text;
			},
			cursorOffset: 0,
			setText: (value) => {
				text = value;
			},
			insertText: (value) => {
				text += value;
			},
			focus: () => {},
		} satisfies TextareaHandle);
		expect(controller.restorePendingMessages()).toBe(true);
		expect(text).toBe("queued text\n\ncurrent draft");
		expect(attachments.attachments()[0]?.id).toBe("existing");
		const image = attachments
			.attachments()
			.find((attachment) => attachment instanceof ImageAttachment);
		expect(image).toMatchObject({
			filename: "screenshot.png",
			mimeType: "image/png",
			data: "base64-data",
			sourcePath: "/tmp/screenshot.png",
		});
		const review = attachments
			.attachments()
			.find((attachment) => attachment instanceof CodeReviewAttachment);
		expect(review).toMatchObject({
			review: {
				files: [
					{
						path: "src/example.ts",
						fileComment: "Please simplify this.",
					},
				],
			},
		});
	});
});
