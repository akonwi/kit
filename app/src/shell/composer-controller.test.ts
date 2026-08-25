import { describe, expect, test } from "bun:test";
import { createCommandRegistry } from "../features/commands";
import { ImageAttachment } from "../features/images/attachment";
import { CodeReviewAttachment } from "../features/review/attachment";
import type { AgentMessage } from "../runtime/agent";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { ToastInput } from "../state/toasts";
import { createAttachmentsController } from "./attachments-controller";
import {
	commandDisplayName,
	commandPaletteDescription,
	createComposerController,
	extractPendingComposerText,
	mergePendingMessagesIntoComposer,
	REFERENCE_PICKER_ESCAPE_DELAY_MS,
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
		let cwd = "/repo-a";
		const runtime = {
			getSession: () => ({ id: "session-1", cwd }),
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
		const textarea: TextareaHandle = {
			plainText: "",
			cursorOffset: 0,
			setText: (value) => {
				textarea.plainText = value;
				textarea.cursorOffset = value.length;
			},
			insertText: (value) => {
				textarea.setText(`${textarea.plainText}${value}`);
			},
			focus: () => {},
		};
		controller.setTextarea(textarea);
		return {
			attachment,
			attachments,
			controller,
			rejectSubmission: () => rejectSubmission,
			setCwd: (next: string) => {
				cwd = next;
			},
			setText: textarea.setText,
			text: () => textarea.plainText,
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

	test("restores unsent text without overwriting a newer draft after a cwd change", async () => {
		const setup = setupFailedAttachmentSubmission();
		setup.attachments.detach("code-review");
		setup.setText("review this");

		const pending = setup.controller.handleSubmit();
		expect(setup.text()).toBe("");
		setup.setCwd("/repo-b");
		await pending;
		expect(setup.text()).toBe("review this");

		setup.setCwd("/repo-a");
		setup.setText("old draft");
		const secondPending = setup.controller.handleSubmit();
		setup.setText("new draft");
		setup.setCwd("/repo-b");
		await secondPending;
		expect(setup.text()).toBe("new draft");
	});

	test("does not resurrect an attachment after the cwd changes", async () => {
		const setup = setupFailedAttachmentSubmission();
		await withoutConsoleError(async () => {
			const pending = setup.controller.handleSubmit();
			setup.setCwd("/repo-b");
			setup.rejectSubmission()?.(new Error("failed"));
			await pending;

			expect(setup.attachments.attachments()).toHaveLength(0);
		});
	});

	test("blocks submission when an attachment is no longer valid", async () => {
		let submissions = 0;
		const toasts: ToastInput[] = [];
		const runtime = {
			getSession: () => ({ id: "session-1" }),
			submitMessage: async () => {
				submissions += 1;
			},
		} as unknown as AgentRuntime;
		const attachments = createAttachmentsController();
		attachments.attach({
			id: "stale",
			type: "test",
			icon: "",
			summary: "stale attachment",
			validate: () => "The underlying file changed.",
			toMessagePart: () => ({ type: "text", text: "stale" }),
			toPromptText: () => "stale",
		});
		const controller = createComposerController({
			runtime,
			commands: createCommandRegistry(),
			fileIndex: {} as never,
			threadIndex: null,
			attachments,
			reviewDrafts: {} as never,
			reviewWorkspace: {} as never,
			toast: (toast) => toasts.push(toast),
			_reload: async () => {},
			openCustomOverlay: async () => undefined as never,
		});

		await controller.handleSubmit();

		expect(submissions).toBe(0);
		expect(attachments.attachments()).toHaveLength(1);
		expect(toasts).toEqual([
			{
				title: "Attachment is no longer valid",
				subtitle: "The underlying file changed.",
				variant: "warning",
			},
		]);
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

describe("reference picker escaping", () => {
	function setupReferenceController(options?: {
		ensureLoaded?: () => Promise<Array<{ path: string; isDir: boolean }>>;
		runtime?: AgentRuntime;
	}) {
		const textarea: TextareaHandle = {
			plainText: "",
			cursorOffset: 0,
			setText: (value) => {
				textarea.plainText = value;
				textarea.cursorOffset = value.length;
			},
			insertText: (value) => {
				textarea.setText(
					`${textarea.plainText.slice(0, textarea.cursorOffset)}${value}${textarea.plainText.slice(textarea.cursorOffset)}`,
				);
			},
			focus: () => {},
		};
		const toasts: ToastInput[] = [];
		const controller = createComposerController({
			runtime: options?.runtime ?? ({} as AgentRuntime),
			commands: createCommandRegistry(),
			fileIndex: {
				ensureLoaded:
					options?.ensureLoaded ??
					(async () => [{ path: "src/index.ts", isDir: false }]),
			} as never,
			threadIndex: {
				suggest: async () => [
					{
						name: "Previous thread",
						description: "recent session",
						value: "thread-1",
					},
				],
			} as never,
			attachments: createAttachmentsController(),
			reviewDrafts: {} as never,
			reviewWorkspace: {} as never,
			toast: (toast) => toasts.push(toast),
			_reload: async () => {},
			openCustomOverlay: async () => undefined as never,
		});
		controller.setTextarea(textarea);
		return { controller, textarea, toasts };
	}

	for (const prefix of ["@", "#"] as const) {
		test(`typing ${prefix}${prefix} during the grace period prevents the picker from opening`, async () => {
			const { controller, textarea } = setupReferenceController();
			textarea.setText(prefix);
			controller.handleTextChange();
			expect(textarea.plainText).toBe("");
			textarea.setText(prefix);
			controller.handleTextChange();
			await Bun.sleep(REFERENCE_PICKER_ESCAPE_DELAY_MS + 10);

			controller.picker.accept();

			expect(textarea.plainText).toBe(prefix);
		});

		test(`typing ${prefix}${prefix} after its picker opens closes it`, async () => {
			const { controller, textarea } = setupReferenceController();
			textarea.setText(prefix);
			controller.handleTextChange();
			await Bun.sleep(REFERENCE_PICKER_ESCAPE_DELAY_MS + 10);

			controller.picker.filter(prefix);
			controller.picker.accept();

			expect(textarea.plainText).toBe(prefix);
		});
	}

	test("restores a literal trigger when whitespace cancels a pending query", async () => {
		const { controller, textarea } = setupReferenceController();
		textarea.setText("@");
		controller.handleTextChange();
		textarea.setText("topic ");
		controller.handleTextChange();

		expect(textarea.plainText).toBe("@topic ");
		await Bun.sleep(REFERENCE_PICKER_ESCAPE_DELAY_MS + 10);
		controller.picker.accept();
		expect(textarea.plainText).toBe("@topic ");
	});

	test("opening the command palette invalidates a pending reference", async () => {
		const { controller, textarea } = setupReferenceController();
		textarea.setText("@");
		controller.handleTextChange();
		textarea.setText("/");
		controller.handleTextChange();
		await Bun.sleep(REFERENCE_PICKER_ESCAPE_DELAY_MS + 10);

		controller.picker.accept();

		expect(textarea.plainText).toBe("");
	});

	test("explicit cancellation removes a pending query", async () => {
		const { controller, textarea } = setupReferenceController();
		textarea.setText("@");
		controller.handleTextChange();
		textarea.setText("src");
		controller.handleTextChange();

		expect(controller.cancelReferenceInteraction()).toBe(true);
		await Bun.sleep(REFERENCE_PICKER_ESCAPE_DELAY_MS + 10);
		controller.picker.accept();
		expect(textarea.plainText).toBe("");
	});

	test("clearing an open picker runs provisional-reference cleanup", async () => {
		const { controller, textarea } = setupReferenceController();
		textarea.setText("@");
		controller.handleTextChange();
		textarea.setText("src");
		controller.handleTextChange();
		await Bun.sleep(REFERENCE_PICKER_ESCAPE_DELAY_MS + 10);

		controller.picker.clear();

		expect(textarea.plainText).toBe("");
	});

	test("carries text typed during the grace period into the reference query", async () => {
		const { controller, textarea } = setupReferenceController();
		textarea.setText("@");
		controller.handleTextChange();
		textarea.setText("src");
		controller.handleTextChange();
		await Bun.sleep(REFERENCE_PICKER_ESCAPE_DELAY_MS + 10);

		controller.picker.accept();

		expect(textarea.plainText).toBe("@src/index.ts ");
	});

	test("cancellation removes only the tracked query after cursor movement", async () => {
		const { controller, textarea } = setupReferenceController();
		textarea.setText("before @after");
		textarea.cursorOffset = 8;
		controller.handleTextChange();
		textarea.setText("before srcafter");
		textarea.cursorOffset = 10;
		controller.handleTextChange();
		textarea.cursorOffset = 0;

		expect(controller.cancelReferenceInteraction()).toBe(true);
		expect(textarea.plainText).toBe("before after");
		expect(textarea.cursorOffset).toBe(0);
	});

	test("rebases the provisional anchor when text is inserted before it", () => {
		const { controller, textarea } = setupReferenceController();
		textarea.setText("before @after");
		textarea.cursorOffset = 8;
		controller.handleTextChange();
		textarea.setText("Xbefore after");
		textarea.cursorOffset = 1;
		controller.handleTextChange();

		expect(textarea.plainText).toBe("Xbefore @after");
		expect(textarea.cursorOffset).toBe(1);
	});

	test("rebases the provisional anchor when text is deleted before it", () => {
		const { controller, textarea } = setupReferenceController();
		textarea.setText("before @after");
		textarea.cursorOffset = 8;
		controller.handleTextChange();
		textarea.setText("efore after");
		textarea.cursorOffset = 0;
		controller.handleTextChange();

		expect(textarea.plainText).toBe("efore @after");
		expect(textarea.cursorOffset).toBe(0);
	});

	test("cursor movement restores the literal trigger instead of losing it", async () => {
		const { controller, textarea } = setupReferenceController();
		textarea.setText("before @after");
		textarea.cursorOffset = 8;
		controller.handleTextChange();
		textarea.setText("before srcafter");
		textarea.cursorOffset = 10;
		controller.handleTextChange();
		textarea.cursorOffset = 0;
		await Bun.sleep(REFERENCE_PICKER_ESCAPE_DELAY_MS + 10);

		controller.picker.accept();

		expect(textarea.plainText).toBe("before @srcafter");
		expect(textarea.cursorOffset).toBe(0);
	});

	test("message history invalidates a pending reference", async () => {
		const runtime = {
			getMessages: () => [
				{ role: "user", content: "previous message", timestamp: 1 },
			],
		} as unknown as AgentRuntime;
		const { controller, textarea } = setupReferenceController({ runtime });
		textarea.setText("@");
		controller.handleTextChange();
		expect(controller.showUserMessageHistoryPicker()).toBe(true);
		await Bun.sleep(REFERENCE_PICKER_ESCAPE_DELAY_MS + 10);

		controller.picker.accept();

		expect(textarea.plainText).toBe("previous message");
	});

	test("restores the literal trigger when suggestion loading fails", async () => {
		const { controller, textarea, toasts } = setupReferenceController({
			ensureLoaded: async () => {
				throw new Error("scan failed");
			},
		});
		textarea.setText("@");
		controller.handleTextChange();
		await Bun.sleep(0);

		expect(textarea.plainText).toBe("@");
		expect(toasts).toEqual([
			{
				title: "File references",
				subtitle: "scan failed",
				variant: "error",
			},
		]);
	});

	test("loads suggestions concurrently with the grace period", async () => {
		let resolveEntries:
			| ((entries: Array<{ path: string; isDir: boolean }>) => void)
			| undefined;
		const entries = new Promise<Array<{ path: string; isDir: boolean }>>(
			(resolve) => {
				resolveEntries = resolve;
			},
		);
		const { controller, textarea } = setupReferenceController({
			ensureLoaded: () => entries,
		});
		textarea.setText("@");
		controller.handleTextChange();
		await Bun.sleep(REFERENCE_PICKER_ESCAPE_DELAY_MS + 10);
		resolveEntries?.([{ path: "src/index.ts", isDir: false }]);
		await Bun.sleep(0);

		controller.picker.accept();

		expect(textarea.plainText).toBe("@src/index.ts ");
	});

	test("typing a doubled trigger cancels a file picker that is still loading", async () => {
		let resolveEntries:
			| ((entries: Array<{ path: string; isDir: boolean }>) => void)
			| undefined;
		const entries = new Promise<Array<{ path: string; isDir: boolean }>>(
			(resolve) => {
				resolveEntries = resolve;
			},
		);
		const { controller, textarea } = setupReferenceController({
			ensureLoaded: () => entries,
		});
		textarea.setText("@");
		controller.handleTextChange();
		textarea.setText("@");
		controller.handleTextChange();

		expect(textarea.plainText).toBe("@");
		resolveEntries?.([{ path: "src/index.ts", isDir: false }]);
		await Bun.sleep(REFERENCE_PICKER_ESCAPE_DELAY_MS + 10);
		controller.picker.accept();
		expect(textarea.plainText).toBe("@");
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
