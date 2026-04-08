import { COMMANDS } from "../features/commands";
import type { FileIndex } from "../features/files";
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
};

export function createComposerController(deps: ComposerControllerDeps) {
	const { runtime, fileIndex } = deps;
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
					insertFileReference(path);
					ctx.dismiss();
				},
			})),
		});
		if (initialQuery) {
			palette.filter(initialQuery);
		}
	}

	function insertFileReference(path: string) {
		if (!textareaRef) return;
		const text = textareaRef.plainText;
		const cursor = textareaRef.cursorOffset;
		let start = cursor - 1;
		while (start >= 0) {
			const char = text[start];
			if (char === "@") break;
			if (/\s/.test(char)) {
				start = -1;
				break;
			}
			start--;
		}

		if (start < 0 || text[start] !== "@") {
			insertText(`@${path}`);
			return;
		}

		let end = cursor;
		while (end < text.length && !/\s/.test(text[end] ?? "")) {
			end++;
		}

		const nextText = `${text.slice(0, start)}@${path}${text.slice(end)}`;
		setTextareaText(nextText);
		if (textareaRef) textareaRef.cursorOffset = start + path.length + 1;
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

		if (!palette.visible && grew && cursor > 0 && text[cursor - 1] === "@") {
			void openFileReferences();
		}
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

		if (runtime.getStatus().isStreaming) {
			runtime.sendFollowUp(text);
			return;
		}

		try {
			await runtime.submitUserMessage(text);
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
		runtime.sendFollowUp(text);
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
