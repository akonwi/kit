import { randomUUID } from "node:crypto";
import { access, readFile } from "node:fs/promises";
import { basename } from "node:path";
import { fileURLToPath } from "node:url";
import type { PasteEvent } from "@opentui/core";
import type { OverlayComponentProps } from "../app/overlay-ui";
import type { Command, CommandRegistry } from "../features/commands";
import type { FileIndex } from "../features/files";
import { ImageAttachment } from "../features/images/attachment";
import { expandThreadReferences, type ThreadIndex } from "../features/threads";
import type { MessagePart } from "../messages/parts";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { PaletteContext } from "../state/palette";
import {
	createPaletteManager,
	type PaletteManager,
} from "../state/palette-manager";
import type { ToastInput } from "../state/toasts";
import type { AttachmentsController } from "./attachments-controller";

export type TextareaHandle = {
	plainText: string;
	cursorOffset: number;
	setText: (value: string) => void;
	insertText: (text: string) => void;
};

export type ComposerControllerDeps = {
	runtime: AgentRuntime;
	commands: CommandRegistry;
	fileIndex: FileIndex;
	threadIndex: ThreadIndex | null;
	attachments: AttachmentsController;
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
		toast,
		_reload,
		openCustomOverlay,
	} = deps;
	const palette: PaletteManager = createPaletteManager();

	let textareaRef: TextareaHandle | undefined;
	let prevTextLength = 0;

	function setTextarea(ref: TextareaHandle | undefined) {
		textareaRef = ref;
	}

	function getTextareaText(): string {
		return textareaRef?.plainText ?? "";
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
				palette,
				args,
				toast,
				_reload,
				openCustomOverlay,
			});
		} catch (error) {
			toast({
				title: `/${command.name} failed`,
				lines: [error instanceof Error ? error.message : String(error)],
				variant: "error",
			});
		}
	}

	function openSlashCommands() {
		let resolvedCommandName: string | null = null;
		let currentArgs = "";
		const availableCommands = commands.getAll();
		const options = availableCommands
			.slice()
			.sort((a, b) => a.name.localeCompare(b.name))
			.map((cmd) => ({
				name: cmd.name,
				description: cmd.description,
				argHint: cmd.argName,
				value: cmd,
				action: (ctx: PaletteContext) => {
					textareaRef?.setText("");
					prevTextLength = 0;
					ctx.dismiss();
					void executeCommand(cmd, currentArgs);
				},
			}));
		const findOption = (name: string) =>
			options.find((option) => option.name === name);

		palette.show(
			{
				filterable: true,
				hint: "Tab complete · Enter run · Esc close",
				options,
				onFilterChange: (text) => {
					const trimmed = text.trimStart();
					const firstSpace = trimmed.search(/\s/);
					const commandToken = (
						firstSpace === -1 ? trimmed : trimmed.slice(0, firstSpace)
					).trim();

					if (resolvedCommandName) {
						if (
							trimmed === resolvedCommandName ||
							trimmed.startsWith(`${resolvedCommandName} `)
						) {
							currentArgs = trimmed.slice(resolvedCommandName.length).trim();
							const pinned = findOption(resolvedCommandName);
							return pinned
								? {
										options: [pinned],
										selectedIndex: 0,
										query: resolvedCommandName,
									}
								: { query: resolvedCommandName };
						}
						resolvedCommandName = null;
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
					resolvedCommandName = cmd.name;
					currentArgs = "";
					palette.filter(`${cmd.name} `);
				},
			},
		);
	}

	async function openFileReferences(initialQuery = "") {
		const entries = await fileIndex.ensureLoaded();
		palette.show({
			filterable: true,
			hint: "Enter insert · Esc close",
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
			palette.filter(initialQuery);
		}
	}

	async function openThreadReferences(initialQuery = "") {
		if (!threadIndex) return;
		const suggestions = await threadIndex.suggest(initialQuery);
		palette.show({
			filterable: true,
			hint: "Enter insert · Esc close",
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
			palette.filter(initialQuery);
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
		const cursor = textareaRef?.cursorOffset ?? text.length;
		const grew = text.length > prevTextLength;
		prevTextLength = text.length;

		if (text.trimStart() === "/" && !palette.visible && grew) {
			openSlashCommands();
			return;
		}

		if (!palette.visible && grew && cursor > 0 && text[cursor - 1] === "#") {
			void openThreadReferences();
			return;
		}

		if (!palette.visible && grew && cursor > 0 && text[cursor - 1] === "@") {
			void openFileReferences();
		}
	}

	async function prepareMessageText(text: string): Promise<string | null> {
		const result = await expandThreadReferences(text, runtime.getSession().id);
		if (result.errors.length > 0) {
			toast({
				title: "Thread references",
				lines: result.errors,
				variant: "error",
			});
			return null;
		}
		return result.text;
	}

	async function handleSubmit() {
		if (palette.visible && !palette.isFilterable) {
			palette.selectCurrent();
			return;
		}
		if (palette.visible) return;

		const text = textareaRef?.plainText ?? "";
		const pendingAttachments = attachments.attachments();
		const slashCommand = parseSlashCommand(text, commands.getAll());
		if (slashCommand) {
			textareaRef?.setText("");
			prevTextLength = 0;
			await executeCommand(slashCommand.command, slashCommand.args);
			return;
		}
		if (!text.trim() && pendingAttachments.length === 0) {
			if (
				runtime.getStatus().isStreaming &&
				runtime.getPendingMessageCount() > 0
			) {
				runtime.promotePendingFollowUpsToSteering();
			}
			return;
		}

		// Handle bash command: ! for context, !! for excluded from context
		if (text.trim() && text.startsWith("!")) {
			const excludeFromContext = text.startsWith("!!");
			const command = excludeFromContext
				? text.slice(2).trim()
				: text.slice(1).trim();
			if (command) {
				textareaRef?.setText("");
				prevTextLength = 0;
				try {
					await runtime.executeBash(command, excludeFromContext);
				} catch (error) {
					toast({
						title: "Bash failed",
						lines: [error instanceof Error ? error.message : String(error)],
						variant: "error",
					});
				}
				return;
			}
		}

		textareaRef?.setText("");
		prevTextLength = 0;

		const preparedText = text.trim() ? await prepareMessageText(text) : "";
		if (text.trim() && !preparedText) {
			textareaRef?.setText(text);
			prevTextLength = text.length;
			return;
		}

		if (runtime.getStatus().isStreaming) {
			if (pendingAttachments.length > 0) {
				toast({
					title: "Attachments not supported in queued follow-ups",
					lines: [
						"Wait for the current turn to finish before sending attached reviews.",
					],
					variant: "error",
				});
				textareaRef?.setText(text);
				prevTextLength = text.length;
				return;
			}
			runtime.sendFollowUp(preparedText ?? "");
			return;
		}

		const parts: MessagePart[] = [];
		if ((preparedText ?? "").trim()) {
			parts.push({ type: "text", text: preparedText ?? "" });
		}
		for (const attachment of pendingAttachments) {
			parts.push(attachment.toMessagePart());
		}

		for (const attachment of pendingAttachments) {
			attachments.detach(attachment.id);
		}

		try {
			await runtime.submitUserMessage(parts);
		} catch (error) {
			for (const attachment of pendingAttachments) {
				attachments.attach(attachment);
			}
			toast({
				title: "Agent error",
				lines: [error instanceof Error ? error.message : String(error)],
				variant: "error",
			});
			console.error(error);
			textareaRef?.setText(text);
			prevTextLength = text.length;
		}
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
		const restored = pending.join("\n\n");
		setTextareaText(restored);
		if (textareaRef) textareaRef.cursorOffset = restored.length;
		return true;
	}

	function recallLastUserMessage() {
		const messages = runtime.getMessages();
		for (let i = messages.length - 1; i >= 0; i--) {
			const msg = messages[i];
			if (msg.role !== "user") continue;
			const content = (msg as { content?: unknown }).content;
			let text = "";
			if (typeof content === "string") text = content;
			else if (Array.isArray(content)) {
				text = content
					.filter(
						(
							block,
						): block is {
							type: "text";
							text: string;
						} =>
							typeof block === "object" &&
							block !== null &&
							"type" in block &&
							block.type === "text" &&
							"text" in block &&
							typeof block.text === "string",
					)
					.map((block) => block.text)
					.join("\n");
			}
			if (text.trim()) {
				setTextareaText(text);
				if (textareaRef) textareaRef.cursorOffset = text.length;
				return;
			}
		}
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
		palette,
		setTextarea,
		handlePaste,
		handleTextChange,
		handleSubmit,
		handleFollowUp,
		restorePendingMessages,
		insertText,
		getTextareaText,
		setTextareaText,
		recallLastUserMessage,
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

function parseSlashCommand(
	text: string,
	commands: Command[],
): { command: Command; args: string } | null {
	const trimmed = text.trim();
	if (!trimmed.startsWith("/")) return null;

	const withoutSlash = trimmed.slice(1);
	const firstSpace = withoutSlash.search(/\s/);
	const name = (
		firstSpace === -1 ? withoutSlash : withoutSlash.slice(0, firstSpace)
	).trim();
	if (!name) return null;

	const command = commands.find((candidate) => candidate.name === name);
	if (!command) return null;

	const args =
		firstSpace === -1 ? "" : withoutSlash.slice(firstSpace + 1).trim();
	return { command, args };
}
