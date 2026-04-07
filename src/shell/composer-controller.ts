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

	function handleTextChange() {
		const text = textareaRef?.plainText ?? "";
		const grew = text.length > prevTextLength;
		prevTextLength = text.length;

		if (text.trimStart() === "/" && !palette.visible && grew) {
			openSlashCommands();
		}
	}

	async function handleSubmit() {
		if (palette.visible && !palette.isFilterable) {
			palette.selectCurrent();
			return;
		}
		if (palette.visible) return;

		const text = textareaRef?.plainText ?? "";
		if (!text.trim()) return;

		textareaRef?.setText("");
		prevTextLength = 0;
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

	function restorePendingMessages() {
		runtime.clearPendingMessages();
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
					.filter((b: any) => b?.type === "text")
					.map((b: any) => b.text)
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
		quit,
	};
}

export type ComposerController = ReturnType<typeof createComposerController>;
