import { COMMANDS } from "../features/commands";
import type { FileIndex } from "../features/files";
import { expandThreadReferences, type ThreadIndex } from "../features/threads";
import type { AgentRuntime } from "../runtime/agent-runtime";
import {
	createPaletteManager,
	type PaletteManager,
} from "../state/palette-manager";

export type TextareaHandle = {
	plainText: string;
	cursorOffset: number;
	setText: (value: string) => void;
	insertText: (text: string) => void;
};

export type ComposerControllerDeps = {
	runtime: AgentRuntime;
	fileIndex: FileIndex;
	threadIndex: ThreadIndex | null;
};

export function createComposerController(deps: ComposerControllerDeps) {
	const { runtime, fileIndex, threadIndex } = deps;
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

	function openSlashCommands() {
		palette.show({
			filterable: true,
			options: COMMANDS.map((cmd) => ({
				name: cmd.name,
				description: cmd.description,
				value: cmd,
				action: (ctx) => {
					textareaRef?.setText("");
					prevTextLength = 0;
					ctx.dismiss();
					cmd.execute({ runtime, palette });
				},
			})),
		});
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
			insertText(token);
			return;
		}

		let end = cursor;
		while (end < text.length && !/\s/.test(text[end] ?? "")) {
			end++;
		}

		const nextText = `${text.slice(0, tokenStart)}${token}${text.slice(end)}`;
		setTextareaText(nextText);
		if (textareaRef) textareaRef.cursorOffset = tokenStart + token.length;
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
			runtime.emitError("Thread references", result.errors);
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
		if (!text.trim()) {
			if (
				runtime.getStatus().isStreaming &&
				runtime.getPendingMessageCount() > 0
			) {
				runtime.promotePendingFollowUpsToSteering();
			}
			return;
		}

		textareaRef?.setText("");
		prevTextLength = 0;

		const preparedText = await prepareMessageText(text);
		if (!preparedText) {
			textareaRef?.setText(text);
			prevTextLength = text.length;
			return;
		}

		if (runtime.getStatus().isStreaming) {
			runtime.sendFollowUp(preparedText);
			return;
		}

		try {
			await runtime.submitUserMessage(preparedText);
		} catch (error) {
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
