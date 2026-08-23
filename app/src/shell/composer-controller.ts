import { randomUUID } from "node:crypto";
import { access, readFile } from "node:fs/promises";
import { basename } from "node:path";
import { fileURLToPath } from "node:url";
import type { PasteEvent } from "@opentui/core";
import type { OverlayComponentProps } from "../app/overlay-ui";
import type { Command, CommandRegistry } from "../features/commands";
import type { FileIndex } from "../features/files";
import { ImageAttachment } from "../features/images/attachment";
import {
	CodeReviewAttachment,
	type CodeReviewSubmission,
} from "../features/review/attachment";
import type { ReviewDraftController } from "../features/review/draft-controller";
import type { ReviewWorkspaceController } from "../features/review/workspace-controller";
import { expandThreadReferences, type ThreadIndex } from "../features/threads";
import { type MessagePart, messagePartToPromptText } from "../messages/parts";
import type { AgentMessage } from "../runtime/agent";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { PickerContext } from "../state/picker";
import {
	createPickerManager,
	type PickerManager,
} from "../state/picker-manager";
import type { ToastInput } from "../state/toasts";
import type { AttachmentsController } from "./attachments-controller";
import { MIDDLE_DOT } from "./glyphs";

export function extractPendingComposerText(message: AgentMessage): string {
	if (!("content" in message)) return "";
	if (typeof message.content === "string") return message.content;
	if (!Array.isArray(message.content)) return "";
	return message.content
		.flatMap((part) => {
			if (
				typeof part === "object" &&
				part !== null &&
				"type" in part &&
				part.type === "text" &&
				"text" in part &&
				typeof part.text === "string"
			) {
				return [part.text];
			}
			return [];
		})
		.join("\n");
}

export function mergePendingMessagesIntoComposer(
	pending: string[],
	composerText: string,
): string {
	return [...pending, ...(composerText ? [composerText] : [])].join("\n\n");
}

export type TextareaHandle = {
	plainText: string;
	cursorOffset: number;
	setText: (value: string) => void;
	insertText: (text: string) => void;
	focus: () => void;
};

export function commandDisplayName(command: Command): string {
	return command.displayName ?? command.name;
}

export function commandPaletteDescription(command: Command): string {
	const displayName = commandDisplayName(command);
	if (displayName === command.name) return command.description;
	const localSuffix = `.${displayName}`;
	const owner = command.name.endsWith(localSuffix)
		? command.name.slice(0, -localSuffix.length)
		: command.name;
	return `${command.description} ${MIDDLE_DOT} ${owner}`;
}

export type ComposerControllerDeps = {
	runtime: AgentRuntime;
	persistSessions?: boolean;
	commands: CommandRegistry;
	fileIndex: FileIndex;
	threadIndex: ThreadIndex | null;
	attachments: AttachmentsController;
	reviewDrafts: ReviewDraftController;
	reviewWorkspace: ReviewWorkspaceController;
	toast: (toast: ToastInput) => void;
	_reload: () => Promise<void>;
	openCustomOverlay: <T>(
		component: (
			props: OverlayComponentProps<T>,
		) => import("solid-js").JSX.Element,
	) => Promise<T>;
};

export function createComposerController(deps: ComposerControllerDeps) {
	const {
		runtime,
		commands,
		fileIndex,
		threadIndex,
		attachments,
		reviewDrafts,
		reviewWorkspace,
		toast,
		_reload,
		openCustomOverlay,
	} = deps;
	const picker: PickerManager = createPickerManager();
	const commandPalette: PickerManager = createPickerManager();

	let textareaRef: TextareaHandle | undefined;
	let prevTextLength = 0;
	let expectedBashHistoryText: string | null = null;

	function setTextarea(ref: TextareaHandle | undefined) {
		textareaRef = ref;
	}

	function focusTextarea(): void {
		textareaRef?.focus();
	}

	function getTextareaText(): string {
		return textareaRef?.plainText ?? "";
	}

	function getTextareaCursorOffset(): number {
		return textareaRef?.cursorOffset ?? 0;
	}

	function setTextareaText(text: string) {
		textareaRef?.setText(text);
		prevTextLength = text.length;
	}

	function insertText(text: string) {
		if (!textareaRef) return;
		textareaRef.insertText(text);
		prevTextLength = textareaRef.plainText.length;
	}

	async function executeCommand(command: Command, args: string): Promise<void> {
		try {
			await command.execute({
				runtime,
				persistSessions: deps.persistSessions ?? true,
				picker: commandPalette,
				args,
				toast,
				attachments,
				reviewDrafts,
				reviewWorkspace,
				_reload,
				openCustomOverlay,
			});
		} catch (error) {
			toast({
				title: `/${commandDisplayName(command)} failed`,
				subtitle: error instanceof Error ? error.message : String(error),
				variant: "error",
			});
		}
	}

	function openCommandPalette() {
		if (commandPalette.visible) return;
		let resolvedCommand: Command | null = null;
		let currentArgs = "";
		const availableCommands = commands.getAll();
		const options = availableCommands
			.slice()
			.sort(
				(a, b) =>
					commandDisplayName(a).localeCompare(commandDisplayName(b)) ||
					a.name.localeCompare(b.name),
			)
			.map((cmd) => ({
				name: commandDisplayName(cmd),
				description: commandPaletteDescription(cmd),
				argHint: cmd.argName,
				value: cmd,
				action: (ctx: PickerContext) => {
					ctx.dismiss();
					void executeCommand(cmd, currentArgs);
				},
			}));
		const findOption = (command: Command) =>
			options.find((option) => option.value === command);

		commandPalette.show(
			{
				filterable: true,
				options,
				onFilterChange: (text) => {
					const trimmed = text.trimStart();
					const firstSpace = trimmed.search(/\s/);
					const commandToken = (
						firstSpace === -1 ? trimmed : trimmed.slice(0, firstSpace)
					).trim();

					if (resolvedCommand) {
						const displayName = commandDisplayName(resolvedCommand);
						if (
							trimmed === displayName ||
							trimmed.startsWith(`${displayName} `)
						) {
							currentArgs = trimmed.slice(displayName.length).trim();
							const pinned = findOption(resolvedCommand);
							return pinned
								? {
										options: [pinned],
										selectedIndex: 0,
										query: displayName,
									}
								: { query: displayName };
						}
						resolvedCommand = null;
					}

					currentArgs =
						firstSpace === -1 ? "" : trimmed.slice(firstSpace + 1).trim();
					return { query: commandToken };
				},
			},
			{
				tab: (option) => {
					const cmd = option.value as Command | undefined;
					if (!cmd) return;
					resolvedCommand = cmd;
					currentArgs = "";
					commandPalette.filter(`${commandDisplayName(cmd)} `);
				},
			},
		);
	}

	async function openFileReferences(initialQuery = "") {
		const entries = await fileIndex.ensureLoaded();
		picker.show({
			filterable: true,
			options: entries.map((entry) => ({
				name: entry.path,
				description: entry.isDir ? "directory" : "",
				value: entry.path,
				action: (ctx) => {
					const path = String(entry.path);
					insertReference("@", path);
					ctx.dismiss();
				},
			})),
		});
		if (initialQuery) {
			picker.filter(initialQuery);
		}
	}

	async function openThreadReferences(initialQuery = "") {
		if (!threadIndex) return;
		const suggestions = await threadIndex.suggest(initialQuery);
		picker.show({
			filterable: true,
			options: suggestions.map((entry) => ({
				name: entry.name,
				description: entry.description,
				value: entry.value,
				action: (ctx) => {
					insertReference("#", formatThreadReference(entry.value, entry.name));
					ctx.dismiss();
				},
			})),
		});
		if (initialQuery) {
			picker.filter(initialQuery);
		}
	}

	function insertReference(prefix: "@" | "#", value: string) {
		if (!textareaRef) return;
		const text = textareaRef.plainText;
		const cursor = textareaRef.cursorOffset;
		const token = `${prefix}${value}`;
		const tokenStart = findReferenceTokenStart(text, cursor, prefix);

		if (tokenStart < 0) {
			insertText(`${token} `);
			return;
		}

		let end = cursor;
		while (end < text.length && !/\s/.test(text[end] ?? "")) {
			end++;
		}

		const nextText = `${text.slice(0, tokenStart)}${token} ${text.slice(end)}`;
		setTextareaText(nextText);
		if (textareaRef) textareaRef.cursorOffset = tokenStart + token.length + 1;
	}

	async function handlePaste(event: PasteEvent) {
		const mimeType = event.metadata?.mimeType ?? "";
		const pastedText = new TextDecoder()
			.decode(event.bytes)
			.replace(/\r\n/g, "\n")
			.replace(/\r/g, "\n");
		const candidatePaths = getPastedPathCandidates(pastedText);

		if (mimeType.startsWith("image/")) {
			event.preventDefault();
			event.stopPropagation();
			const extension = mimeType.split("/")[1] ?? "bin";
			const filename = `pasted-${new Date().toISOString().replace(/[:.]/g, "-")}.${extension}`;
			const data = Buffer.from(event.bytes).toString("base64");
			console.log("[composer] attaching pasted image bytes", {
				filename,
				mimeType,
				base64Length: data.length,
			});
			attachments.attach(
				new ImageAttachment(randomUUID(), filename, mimeType, data),
			);
			return;
		}

		if (candidatePaths.length > 0) {
			event.preventDefault();
			event.stopPropagation();
			const attachedFromPaths =
				await attachImagesFromPastedPaths(candidatePaths);
			if (attachedFromPaths > 0) {
				console.log("[composer] attached images from pasted path(s)", {
					count: attachedFromPaths,
				});
				return;
			}
			console.log(
				"[composer] path-like paste did not resolve to images; falling back to text paste",
			);
			if (pastedText.length > 0) {
				insertText(pastedText);
				return;
			}
		}

		if (pastedText.length > 0) {
			event.preventDefault();
			event.stopPropagation();
			insertText(pastedText);
			return;
		}

		console.log("[composer] paste ignored: empty payload");
	}

	async function attachImagesFromPastedPaths(
		candidates: string[],
	): Promise<number> {
		let attached = 0;
		for (const candidate of candidates) {
			const image = await readImageAttachmentFromPath(candidate);
			if (!image) continue;
			attachments.attach(image);
			attached += 1;
		}
		return attached;
	}

	function handleTextChange() {
		const text = textareaRef?.plainText ?? "";
		if (expectedBashHistoryText === text) {
			expectedBashHistoryText = null;
		} else {
			resetBashHistoryNavigation();
		}

		const cursor = textareaRef?.cursorOffset ?? text.length;
		const grew = text.length > prevTextLength;
		prevTextLength = text.length;

		if (
			text.trimStart() === "/" &&
			!picker.visible &&
			!commandPalette.visible &&
			grew
		) {
			textareaRef?.setText("");
			prevTextLength = 0;
			openCommandPalette();
			return;
		}

		if (!picker.visible && grew && cursor > 0 && text[cursor - 1] === "#") {
			void openThreadReferences();
			return;
		}

		if (!picker.visible && grew && cursor > 0 && text[cursor - 1] === "@") {
			void openFileReferences();
		}
	}

	async function prepareMessageText(text: string): Promise<string | null> {
		const result = await expandThreadReferences(text, runtime.getSession().id);
		if (result.errors.length > 0) {
			toast({
				title: "Thread references",
				subtitle: result.errors.join(" · "),
				variant: "error",
			});
			return null;
		}
		return result.text;
	}

	async function handleSubmit(options: { executeBash?: boolean } = {}) {
		if (commandPalette.visible) return;
		if (picker.visible && !picker.isFilterable) {
			picker.accept();
			return;
		}
		if (picker.visible) return;

		const text = textareaRef?.plainText ?? "";
		const submitSession = runtime.getSession();
		const submitSessionId = submitSession.id;
		const submitSessionCwd = submitSession.cwd;
		const submissionScopeIsCurrent = (): boolean => {
			const current = runtime.getSession();
			return current.id === submitSessionId && current.cwd === submitSessionCwd;
		};
		const restoreSubmittedTextIfEmpty = (): void => {
			if (!text || !textareaRef || textareaRef.plainText !== "") return;
			textareaRef.setText(text);
			prevTextLength = text.length;
		};
		const pendingAttachments = attachments.attachments();
		if (!text.trim() && pendingAttachments.length === 0) {
			if (
				runtime.getStatus().isStreaming &&
				runtime.getPendingMessageCount() > 0
			) {
				runtime.promotePendingFollowUpsToSteering();
			}
			return;
		}

		// Handle bash command: ! for context, !! for excluded from context.
		// Review's submit-now action bypasses this branch so it always sends the
		// composer's contents with the projected review attachment.
		if ((options.executeBash ?? true) && text.trim() && text.startsWith("!")) {
			const excludeFromContext = text.startsWith("!!");
			const command = excludeFromContext
				? text.slice(2).trim()
				: text.slice(1).trim();
			if (command) {
				textareaRef?.setText("");
				prevTextLength = 0;
				resetBashHistoryNavigation();
				try {
					await runtime.executeBash(command, excludeFromContext);
				} catch (error) {
					toast({
						title: "Bash failed",
						subtitle: error instanceof Error ? error.message : String(error),
						variant: "error",
					});
				}
				return;
			}
		}

		textareaRef?.setText("");
		prevTextLength = 0;
		resetBashHistoryNavigation();

		const preparedText = text.trim() ? await prepareMessageText(text) : "";
		// Thread expansion is asynchronous. If the active session or its cwd
		// changed while it ran, never submit old-scope text or attachments.
		if (!submissionScopeIsCurrent()) {
			if (runtime.getSession().id === submitSessionId) {
				restoreSubmittedTextIfEmpty();
			}
			return;
		}
		if (text.trim() && !preparedText) {
			restoreSubmittedTextIfEmpty();
			return;
		}
		for (const attachment of pendingAttachments) {
			const validationError = attachment.validate?.();
			if (!validationError) continue;
			restoreSubmittedTextIfEmpty();
			toast({
				title: "Attachment is no longer valid",
				subtitle: validationError,
				variant: "warning",
			});
			return;
		}

		const parts: MessagePart[] = [];
		if ((preparedText ?? "").trim()) {
			parts.push({ type: "text", text: preparedText ?? "" });
		}
		for (const attachment of pendingAttachments) {
			parts.push(attachment.toMessagePart());
		}

		// Remove previews immediately so submission feels responsive. Draft
		// attachments treat this as provisional: success finalizes consumption,
		// while failure reattaches the original objects below.
		for (const attachment of pendingAttachments) {
			attachments.detach(attachment.id, "pending");
		}
		const pendingAttachmentIds = new Set(
			pendingAttachments.map((attachment) => attachment.id),
		);
		const changedPendingAttachmentIds = new Set<string>();
		const unsubscribeAttachments = attachments.subscribe((event) => {
			const id = event.type === "attached" ? event.attachment.id : event.id;
			if (pendingAttachmentIds.has(id)) changedPendingAttachmentIds.add(id);
		});

		try {
			await runtime.submitMessage(parts);
			for (const attachment of pendingAttachments) {
				attachment.onDetach?.("consumed");
			}
		} catch (error) {
			// A session or cwd switch owns a fresh composer scope. Never resurrect
			// text or attachments from a failed submission started in the old scope.
			if (submissionScopeIsCurrent()) {
				const currentAttachmentIds = new Set(
					attachments.attachments().map((attachment) => attachment.id),
				);
				for (const attachment of pendingAttachments) {
					// A live editor may have projected or explicitly removed a newer
					// revision while this submission was pending. Never resurrect the
					// stale snapshot after either transition.
					if (
						!changedPendingAttachmentIds.has(attachment.id) &&
						!currentAttachmentIds.has(attachment.id)
					) {
						attachments.attach(attachment);
					}
				}
				toast({
					title: "Agent error",
					subtitle: error instanceof Error ? error.message : String(error),
					variant: "error",
				});
				restoreSubmittedTextIfEmpty();
			}
			console.error(error);
		} finally {
			unsubscribeAttachments();
		}
	}

	async function handleMessageSubmit() {
		await handleSubmit({ executeBash: false });
	}

	async function handleFollowUp() {
		const text = textareaRef?.plainText ?? "";
		if (!text.trim()) return;
		textareaRef?.setText("");
		prevTextLength = 0;
		const preparedText = await prepareMessageText(text);
		if (!preparedText) {
			textareaRef?.setText(text);
			prevTextLength = text.length;
			return;
		}
		runtime.sendFollowUp(preparedText);
	}

	function restorePendingMessages(): boolean {
		const pending = runtime.drainPendingMessages();
		if (pending.length === 0) return false;
		const restored = mergePendingMessagesIntoComposer(
			pending.map(extractPendingComposerText).filter(Boolean),
			textareaRef?.plainText ?? "",
		);
		setTextareaText(restored);
		if (textareaRef) textareaRef.cursorOffset = restored.length;

		for (const message of pending) {
			if (!("content" in message) || !Array.isArray(message.content)) continue;
			for (const part of message.content) {
				const restoredPart = part as unknown as Record<string, unknown>;
				if (
					restoredPart.type === "code-review" &&
					typeof restoredPart.review === "object" &&
					restoredPart.review !== null
				) {
					attachments.attach(
						new CodeReviewAttachment(
							randomUUID(),
							restoredPart.review as CodeReviewSubmission,
						),
					);
					continue;
				}
				if (
					typeof part !== "object" ||
					part === null ||
					!("type" in part) ||
					part.type !== "image" ||
					!("data" in part) ||
					typeof part.data !== "string" ||
					!("mimeType" in part) ||
					typeof part.mimeType !== "string"
				) {
					continue;
				}
				const filename =
					"filename" in part && typeof part.filename === "string"
						? part.filename
						: "queued-image";
				const sourcePath =
					"sourcePath" in part && typeof part.sourcePath === "string"
						? part.sourcePath
						: undefined;
				attachments.attach(
					new ImageAttachment(
						randomUUID(),
						filename,
						part.mimeType,
						part.data,
						sourcePath,
					),
				);
			}
		}
		return true;
	}

	function showBashHistoryPicker(onSelect?: () => void): boolean {
		const text = textareaRef?.plainText ?? "";
		if (!text.startsWith("!")) return false;

		const history = getBashExecutionHistory();
		if (history.length === 0) return false;

		const query = text.replace(/^!+/, "").trimStart();
		picker.show({
			filterable: true,
			label: "Bash history",
			inputValue: query,
			options: history.map((entry) => {
				const prefix = entry.excludeFromContext ? "!!" : "!";
				const value = `${prefix}${entry.command}`;
				return {
					name: entry.command,
					description: entry.excludeFromContext
						? "excluded from context"
						: "included in context",
					value,
					action: (ctx: PickerContext) => {
						ctx.dismiss();
						applyBashHistoryText(value);
						onSelect?.();
					},
				};
			}),
		});
		if (query) picker.filter(query);
		return true;
	}

	function applyBashHistoryText(text: string) {
		expectedBashHistoryText = text;
		setTextareaText(text);
		if (textareaRef) textareaRef.cursorOffset = text.length;
	}

	function resetBashHistoryNavigation() {
		expectedBashHistoryText = null;
	}

	function getBashExecutionHistory(): Array<{
		command: string;
		excludeFromContext: boolean;
	}> {
		const messages = runtime.getMessages();
		const history: Array<{ command: string; excludeFromContext: boolean }> = [];
		for (let index = messages.length - 1; index >= 0; index--) {
			const msg = messages[index];
			if (msg.role !== "bashExecution") continue;
			const command = msg.command.trim();
			if (!command) continue;
			history.push({
				command,
				excludeFromContext: msg.excludeFromContext ?? false,
			});
		}
		return history;
	}

	function showUserMessageHistoryPicker(onSelect?: () => void): boolean {
		const history = getUserMessageHistory();
		if (history.length === 0) return false;

		picker.show({
			filterable: true,
			label: "Message history",
			options: history.map((entry) => ({
				name: singleLineSummary(entry.text),
				description: formatMessageHistoryTimestamp(entry.timestamp),
				value: entry.text,
				action: (ctx: PickerContext) => {
					ctx.dismiss();
					setTextareaText(entry.text);
					if (textareaRef) textareaRef.cursorOffset = entry.text.length;
					onSelect?.();
				},
			})),
		});
		return true;
	}

	function getUserMessageHistory(): Array<{
		text: string;
		timestamp?: number;
	}> {
		const messages = runtime.getMessages();
		const history: Array<{ text: string; timestamp?: number }> = [];
		for (let index = messages.length - 1; index >= 0; index--) {
			const msg = messages[index];
			if (msg.role !== "user") continue;
			const text = textFromUserMessageContent(
				(msg as { content?: unknown }).content,
			);
			if (!text.trim()) continue;
			history.push({
				text,
				...(typeof msg.timestamp === "number"
					? { timestamp: msg.timestamp }
					: {}),
			});
		}
		return history;
	}

	function textFromUserMessageContent(content: unknown): string {
		if (typeof content === "string") return content;
		if (!Array.isArray(content)) return "";
		return content
			.map((part) => {
				if (!isMessagePart(part)) return "";
				return messagePartToPromptText(part);
			})
			.filter((text) => text.trim().length > 0)
			.join("\n");
	}

	function isMessagePart(value: unknown): value is MessagePart {
		return (
			typeof value === "object" &&
			value !== null &&
			"type" in value &&
			typeof value.type === "string"
		);
	}

	function singleLineSummary(text: string): string {
		return text.replace(/\s+/g, " ").trim();
	}

	function formatMessageHistoryTimestamp(
		timestamp: number | undefined,
	): string {
		if (timestamp === undefined) return "previous message";
		return new Date(timestamp).toLocaleString();
	}

	function abort() {
		runtime.abort();
	}
	function isStreaming(): boolean {
		return runtime.getStatus().isStreaming;
	}
	function getPendingMessageCount(): number {
		return runtime.getPendingMessageCount();
	}
	function promotePendingFollowUpsToSteering() {
		runtime.promotePendingFollowUpsToSteering();
	}
	function quit() {
		runtime.quit();
	}

	return {
		picker,
		commandPalette,
		openCommandPalette,
		runCommand: executeCommand,
		setTextarea,
		focusTextarea,
		handlePaste,
		handleTextChange,
		handleSubmit,
		handleMessageSubmit,
		handleFollowUp,
		restorePendingMessages,
		showBashHistoryPicker,
		showUserMessageHistoryPicker,
		insertText,
		getTextareaText,
		getTextareaCursorOffset,
		setTextareaText,
		abort,
		isStreaming,
		getPendingMessageCount,
		promotePendingFollowUpsToSteering,
		quit,
	};
}

export type ComposerController = ReturnType<typeof createComposerController>;

function formatThreadReference(id: string, name: string): string {
	const safeName = name
		.replace(/[\]\r\n]+/g, " ")
		.replace(/\s+/g, " ")
		.trim();
	return `[thread:${id}:${safeName}]`;
}

function findReferenceTokenStart(
	text: string,
	cursor: number,
	prefix: "@" | "#",
): number {
	let start = cursor - 1;
	while (start >= 0) {
		const char = text[start];
		if (char === prefix) return start;
		if (/\s/.test(char)) return -1;
		start--;
	}
	return -1;
}

async function readImageAttachmentFromPath(
	candidatePath: string,
): Promise<ImageAttachment | null> {
	try {
		await access(candidatePath);
	} catch {
		return null;
	}
	const mimeType = inferImageMimeType(candidatePath);
	if (!mimeType) return null;
	const bytes = await readFile(candidatePath);
	return new ImageAttachment(
		randomUUID(),
		basename(candidatePath),
		mimeType,
		bytes.toString("base64"),
		candidatePath,
	);
}

function getPastedPathCandidates(text: string): string[] {
	if (!text) return [];
	return text
		.split(/\r?\n/)
		.map((line) => normalizePastedPath(line))
		.filter((line): line is string => Boolean(line));
}

function normalizePastedPath(value: string): string | null {
	const trimmed = value.trim();
	if (!trimmed) return null;
	const unwrapped = trimmed.replace(/^['"]|['"]$/g, "");
	if (unwrapped.startsWith("file://")) {
		try {
			return fileURLToPath(unwrapped);
		} catch {
			return null;
		}
	}
	if (!unwrapped.startsWith("/")) return null;
	return unwrapped.replace(/\\([\\\s])/g, "$1");
}

function inferImageMimeType(path: string): string | null {
	const lower = path.toLowerCase();
	if (lower.endsWith(".png")) return "image/png";
	if (lower.endsWith(".jpg") || lower.endsWith(".jpeg")) return "image/jpeg";
	if (lower.endsWith(".gif")) return "image/gif";
	if (lower.endsWith(".webp")) return "image/webp";
	if (lower.endsWith(".bmp")) return "image/bmp";
	if (lower.endsWith(".svg")) return "image/svg+xml";
	return null;
}
