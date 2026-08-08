import { describe, expect, test } from "bun:test";
import { createCommandRegistry } from "../features/commands";
import { ImageAttachment } from "../features/images/attachment";
import { CodeReviewAttachment } from "../features/review/attachment";
import type { AgentMessage } from "../runtime/agent";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { createAttachmentsController } from "./attachments-controller";
import {
	commandDisplayName,
	commandPaletteDescription,
	createComposerController,
	extractPendingComposerText,
	mergePendingMessagesIntoComposer,
	type TextareaHandle,
} from "./composer-controller";

describe("command presentation", () => {
	test("keeps duplicate local names distinct by canonical command identity", () => {
		const commands = createCommandRegistry([
			{
				name: "plugin-a.deploy",
				displayName: "deploy",
				description: "Deploy with A",
				execute: () => {},
			},
			{
				name: "plugin-b.deploy",
				displayName: "deploy",
				description: "Deploy with B",
				execute: () => {},
			},
		]);

		expect(
			commands.getAll().map((command) => ({
				canonical: command.name,
				display: commandDisplayName(command),
				description: commandPaletteDescription(command),
			})),
		).toEqual([
			{
				canonical: "plugin-a.deploy",
				display: "deploy",
				description: "Deploy with A · plugin-a",
			},
			{
				canonical: "plugin-b.deploy",
				display: "deploy",
				description: "Deploy with B · plugin-b",
			},
		]);
	});
});

describe("composer submission", () => {
	function setupFailedAttachmentSubmission() {
		let rejectSubmission: ((error: Error) => void) | undefined;
		const runtime = {
			getSession: () => ({ id: "session-1" }),
			submitMessage: () =>
				new Promise<never>((_resolve, reject) => {
					rejectSubmission = reject;
				}),
		} as unknown as AgentRuntime;
		const attachments = createAttachmentsController();
		const attachment = (summary: string) => ({
			id: "code-review",
			type: "test",
			icon: "",
			summary,
			toMessagePart: () => ({ type: "text" as const, text: summary }),
			toPromptText: () => summary,
		});
		attachments.attach(attachment("old"));
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
		return {
			attachment,
			attachments,
			controller,
			rejectSubmission: () => rejectSubmission,
		};
	}

	async function withoutConsoleError(run: () => Promise<void>): Promise<void> {
		const originalConsoleError = console.error;
		console.error = () => {};
		try {
			await run();
		} finally {
			console.error = originalConsoleError;
		}
	}

	test("does not replace a newer attachment when submission fails", async () => {
		const setup = setupFailedAttachmentSubmission();
		await withoutConsoleError(async () => {
			const pending = setup.controller.handleSubmit();
			expect(setup.attachments.attachments()).toHaveLength(0);
			setup.attachments.attach(setup.attachment("new"));
			setup.rejectSubmission()?.(new Error("failed"));
			await pending;

			expect(setup.attachments.attachments()).toHaveLength(1);
			expect(setup.attachments.attachments()[0]?.summary).toBe("new");
		});
	});

	test("does not resurrect an attachment removed while submission is pending", async () => {
		const setup = setupFailedAttachmentSubmission();
		await withoutConsoleError(async () => {
			const pending = setup.controller.handleSubmit();
			setup.attachments.attach(setup.attachment("new"));
			setup.attachments.detach("code-review");
			setup.rejectSubmission()?.(new Error("failed"));
			await pending;

			expect(setup.attachments.attachments()).toHaveLength(0);
		});
	});

	test("message submission bypasses composer bash execution", async () => {
		let submitted: unknown;
		let bashExecutions = 0;
		const runtime = {
			getSession: () => ({ id: "session-1" }),
			submitMessage: async (parts: unknown) => {
				submitted = parts;
			},
			executeBash: async () => {
				bashExecutions += 1;
			},
		} as unknown as AgentRuntime;
		const controller = createComposerController({
			runtime,
			commands: createCommandRegistry(),
			fileIndex: {} as never,
			threadIndex: null,
			attachments: createAttachmentsController(),
			reviewDrafts: {} as never,
			reviewWorkspace: {} as never,
			toast: () => {},
			_reload: async () => {},
			openCustomOverlay: async () => undefined as never,
		});
		let text = "!explain this";
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
		});

		await controller.handleMessageSubmit();

		expect(bashExecutions).toBe(0);
		expect(submitted).toEqual([{ type: "text", text: "!explain this" }]);
	});
});

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
