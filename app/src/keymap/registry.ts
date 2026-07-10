import type { KeyEvent, Renderable } from "@opentui/core";
import type { Command } from "@opentui/keymap";
import type { BindingDefinition, CommandBindingDefinition } from "./bindings";

export type OpenTuiCommandRun = Command<Renderable, KeyEvent>["run"];

export type KeybindingCommandMetadata = {
	defaultKeys: string | readonly string[];
	desc: string;
	group?: string;
	hint?: string | false;
	title?: string;
	category?: string;
};

export type KeybindingCommandMetadataMap = Record<
	string,
	KeybindingCommandMetadata
>;

type KeybindingDomain = Record<string, KeybindingCommandMetadata>;
type KeybindingRegistry = Record<string, KeybindingDomain>;

export const KEYBINDING_REGISTRY = {
	"command-palette": {
		open: {
			defaultKeys: "ctrl+p",
			desc: "Open command palette",
			group: "App",
		},
	},
	"queue-editor": {
		open: {
			defaultKeys: "alt+q",
			desc: "Edit queued messages",
			group: "App",
		},
	},
	composer: {
		"clear-or-quit": {
			defaultKeys: "ctrl+c",
			desc: "Clear input or quit",
			group: "Composer",
		},
		abort: {
			defaultKeys: "escape",
			desc: "Abort response",
			group: "Composer",
		},
		steer: {
			defaultKeys: "return",
			desc: "Steer with queued follow-ups",
			group: "Composer",
		},
		"bash-history-older": {
			defaultKeys: "up",
			desc: "Show bash command history",
			group: "Composer",
		},
		"bash-history-newer": {
			defaultKeys: "down",
			desc: "Show bash command history",
			group: "Composer",
		},
		"restore-or-recall": {
			defaultKeys: "up",
			desc: "Restore queued follow-ups or show message history",
			group: "Composer",
		},
	},
	pager: {
		"previous-section": {
			defaultKeys: ["left", "h"],
			desc: "Show previous pager section",
			group: "pager",
			hint: "section",
		},
		"next-section": {
			defaultKeys: ["right", "l"],
			desc: "Show next pager section",
			group: "pager",
			hint: "section",
		},
		"scroll-up": {
			defaultKeys: ["up", "k"],
			desc: "Scroll pager up",
			group: "pager",
			hint: "scroll",
		},
		"scroll-down": {
			defaultKeys: ["down", "j"],
			desc: "Scroll pager down",
			group: "pager",
			hint: "scroll",
		},
		"edit-note": {
			defaultKeys: ["n", "i"],
			desc: "Edit note for current pager section",
			group: "pager",
			hint: "note",
		},
		"submit-feedback": {
			defaultKeys: "ctrl+return",
			desc: "Submit pager feedback",
			group: "pager",
			hint: "submit",
		},
		close: {
			defaultKeys: ["escape", "q"],
			desc: "Close pager",
			group: "pager",
			hint: "close",
		},
		back: {
			defaultKeys: "escape",
			desc: "Return to pager navigation",
			group: "pager",
			hint: "back",
		},
	},
	"mcp-status": {
		close: {
			defaultKeys: ["escape", "return"],
			desc: "Close MCP status",
			group: "mcp-status",
			hint: "close",
		},
	},
	subagents: {
		close: {
			defaultKeys: ["escape", "return"],
			desc: "Close sub-agents dialog",
			group: "subagents",
			hint: "close",
		},
	},
	"mcp-authorization-url": {
		continue: {
			defaultKeys: ["return", "escape"],
			desc: "Continue after MCP authorization",
			group: "mcp-authorization-url",
			hint: "continue",
		},
	},
	"turn-activity": {
		close: {
			defaultKeys: "escape",
			desc: "Close turn activity dialog",
			group: "turn-activity",
			hint: "close",
		},
	},
	"review-attachment": {
		close: {
			defaultKeys: "escape",
			desc: "Close code review attachment",
			group: "review-attachment",
			hint: "close",
		},
	},
	"review-draft": {
		edit: {
			defaultKeys: "e",
			desc: "Edit code review draft",
			group: "review-draft",
			hint: "edit",
		},
		close: {
			defaultKeys: "escape",
			desc: "Close code review draft",
			group: "review-draft",
			hint: "close",
		},
	},
	scratchpad: {
		"focus-next": {
			defaultKeys: "tab",
			desc: "Focus next input",
			group: "App",
		},
		close: {
			defaultKeys: "escape",
			desc: "Close scratchpad",
			group: "scratchpad",
			hint: "close",
		},
	},
	"guided-questions": {
		previous: {
			defaultKeys: "shift+tab",
			desc: "Go to previous question",
			group: "guided-questions",
			hint: "previous",
		},
		cancel: {
			defaultKeys: "escape",
			desc: "Cancel guided questions",
			group: "guided-questions",
			hint: "cancel",
		},
		"move-up": {
			defaultKeys: "up",
			desc: "Move to previous option",
			group: "guided-questions",
			hint: "move",
		},
		"move-down": {
			defaultKeys: "down",
			desc: "Move to next option",
			group: "guided-questions",
			hint: "move",
		},
		select: {
			defaultKeys: "return",
			desc: "Select focused option",
			group: "guided-questions",
			hint: "select",
		},
		"toggle-option": {
			defaultKeys: "space",
			desc: "Toggle focused option",
			group: "guided-questions",
			hint: "toggle",
		},
		"confirm-multiselect": {
			defaultKeys: "return",
			desc: "Confirm selected options",
			group: "guided-questions",
			hint: "confirm",
		},
		"submit-text": {
			defaultKeys: "return",
			desc: "Submit text answer",
			group: "guided-questions",
			hint: "submit",
		},
		back: {
			defaultKeys: "escape",
			desc: "Return to option selection",
			group: "guided-questions",
			hint: "back",
		},
	},
	review: {
		close: {
			defaultKeys: "escape",
			desc: "Close code review",
			group: "review",
			hint: "close",
		},
		"move-file-up": {
			defaultKeys: ["up", "k"],
			desc: "Move to previous file",
			group: "review",
			hint: "move",
		},
		"move-file-down": {
			defaultKeys: ["down", "j"],
			desc: "Move to next file",
			group: "review",
			hint: "move",
		},
		"focus-file": {
			defaultKeys: "return",
			desc: "Focus selected change group",
			group: "review",
			hint: "focus",
		},
		"toggle-file": {
			defaultKeys: "space",
			desc: "Collapse or expand selected file",
			group: "review",
			hint: "toggle",
		},
		"file-note": {
			defaultKeys: "f",
			desc: "Edit file note",
			group: "review",
			hint: "file note",
		},
		"clear-file-note": {
			defaultKeys: "x",
			desc: "Clear file note",
			group: "review",
			hint: "clear",
		},
		"toggle-view": {
			defaultKeys: "v",
			desc: "Toggle diff view",
			group: "review",
			hint: "view",
		},
		submit: {
			defaultKeys: "s",
			desc: "Attach review notes",
			group: "review",
			hint: "submit",
		},
		back: {
			defaultKeys: "escape",
			desc: "Return to file list",
			group: "review",
			hint: "back",
		},
		"previous-change": {
			defaultKeys: "shift+tab",
			desc: "Move to previous change group",
			group: "review",
			hint: "change",
		},
		"next-change": {
			defaultKeys: "tab",
			desc: "Move to next change group",
			group: "review",
			hint: "change",
		},
		"move-line-up": {
			defaultKeys: ["up", "k"],
			desc: "Move line cursor up",
			group: "review",
			hint: "move",
		},
		"move-line-down": {
			defaultKeys: ["down", "j"],
			desc: "Move line cursor down",
			group: "review",
			hint: "move",
		},
		"toggle-section": {
			defaultKeys: "space",
			desc: "Collapse or expand skipped section",
			group: "review",
			hint: "toggle",
		},
		"comment-line": {
			defaultKeys: "return",
			desc: "Comment selected line",
			group: "review",
			hint: "comment",
		},
		"start-range": {
			defaultKeys: "ctrl+return",
			desc: "Start range selection",
			group: "review",
			hint: "range",
		},
		"clear-line-note": {
			defaultKeys: "x",
			desc: "Clear line note",
			group: "review",
			hint: "clear",
		},
		"close-editor": {
			defaultKeys: "escape",
			desc: "Close note editor",
			group: "review",
			hint: "close",
		},
		"expand-dir": {
			defaultKeys: ["l", "right"],
			desc: "Expand directory",
			group: "review",
			hint: false,
		},
		"collapse-dir": {
			defaultKeys: ["h", "left"],
			desc: "Collapse directory or go to parent",
			group: "review",
			hint: false,
		},
		"toggle-tree-mode": {
			defaultKeys: "t",
			desc: "Toggle changes / all files",
			group: "review",
			hint: "tree mode",
		},
		"search-tree": {
			defaultKeys: "/",
			desc: "Search file tree",
			group: "review",
			hint: "search",
		},
		"cycle-target": {
			defaultKeys: "g",
			desc: "Swap review target (working tree / last commit)",
			group: "review",
			hint: "swap target",
		},
		"pick-commit": {
			defaultKeys: "shift+g",
			desc: "Pick a commit to review",
			group: "review",
			hint: "pick commit",
		},
		"clear-search": {
			defaultKeys: "escape",
			desc: "Clear search",
			group: "review",
			hint: "clear",
		},
	},
	"session-explorer": {
		close: {
			defaultKeys: ["escape", "ctrl+c"],
			desc: "Close session explorer",
			group: "session-explorer",
			hint: "close",
		},
		select: {
			defaultKeys: "return",
			desc: "Switch to selected session",
			group: "session-explorer",
			hint: "switch",
		},
		"move-up": {
			defaultKeys: ["up", "k"],
			desc: "Move to previous session",
			group: "session-explorer",
			hint: "move",
		},
		"move-down": {
			defaultKeys: ["down", "j"],
			desc: "Move to next session",
			group: "session-explorer",
			hint: "move",
		},
		"page-up": {
			defaultKeys: "pageup",
			desc: "Scroll sessions up",
			group: "session-explorer",
			hint: "scroll",
		},
		"page-down": {
			defaultKeys: "pagedown",
			desc: "Scroll sessions down",
			group: "session-explorer",
			hint: "scroll",
		},
		rename: {
			defaultKeys: "r",
			desc: "Rename selected session",
			group: "session-explorer",
			hint: "rename",
		},
		delete: {
			defaultKeys: "ctrl+d",
			desc: "Delete selected session",
			group: "session-explorer",
			hint: "delete",
		},
		squash: {
			defaultKeys: "s",
			desc: "Squash selected session",
			group: "session-explorer",
			hint: "squash",
		},
		"rename-save": {
			defaultKeys: "return",
			desc: "Save session name",
			group: "session-explorer",
			hint: "save",
		},
		"rename-cancel": {
			defaultKeys: ["escape", "ctrl+c"],
			desc: "Cancel session rename",
			group: "session-explorer",
			hint: "cancel",
		},
		confirm: {
			defaultKeys: "return",
			desc: "Confirm session action",
			group: "session-explorer",
			hint: "confirm",
		},
		cancel: {
			defaultKeys: ["escape", "ctrl+c"],
			desc: "Cancel session action",
			group: "session-explorer",
			hint: "cancel",
		},
	},
	debug: {
		close: {
			defaultKeys: ["return", "escape"],
			desc: "Close debug view",
			group: "debug",
			hint: "close",
		},
	},
} as const satisfies KeybindingRegistry;

type BuiltInKeybindingRegistry = typeof KEYBINDING_REGISTRY;
type BuiltInKeybindingDomain = keyof BuiltInKeybindingRegistry & string;
type BuiltInKeybindingAction<TDomain extends BuiltInKeybindingDomain> =
	keyof BuiltInKeybindingRegistry[TDomain] & string;

export type BuiltInKeybindingCommandId = {
	[TDomain in BuiltInKeybindingDomain]: `${TDomain}.${BuiltInKeybindingAction<TDomain>}`;
}[BuiltInKeybindingDomain];

const PICKER_KEYBINDING_COMMANDS = {
	"move-up": {
		defaultKeys: "up",
		desc: "Move picker selection up",
		hint: "up",
	},
	"move-down": {
		defaultKeys: "down",
		desc: "Move picker selection down",
		hint: "down",
	},
	complete: {
		defaultKeys: "tab",
		desc: "Complete picker selection",
		hint: "complete",
	},
	select: {
		defaultKeys: "return",
		desc: "Select or submit picker value",
		hint: "select",
	},
	close: {
		defaultKeys: "escape",
		desc: "Close picker",
		hint: "close",
	},
} as const satisfies KeybindingDomain;

export type PickerKeybindingAction = keyof typeof PICKER_KEYBINDING_COMMANDS &
	string;
export type PickerKeybindingCommandId<TNamespace extends string = string> =
	`${TNamespace}.${PickerKeybindingAction}`;

function splitCommandId(id: string): { domain: string; action: string } | null {
	const dot = id.indexOf(".");
	if (dot <= 0 || dot === id.length - 1) return null;
	return { domain: id.slice(0, dot), action: id.slice(dot + 1) };
}

export function getKeybindingCommand(
	id: string,
): KeybindingCommandMetadata | undefined {
	const parts = splitCommandId(id);
	if (!parts) return undefined;
	const registry: KeybindingRegistry = KEYBINDING_REGISTRY;
	return registry[parts.domain]?.[parts.action];
}

export function createBindingDefinitionForCommand(
	id: string,
	override?: KeybindingCommandMetadata,
): BindingDefinition {
	const metadata = override ?? getKeybindingCommand(id);
	if (!metadata) {
		throw new Error(`Unknown keybinding command: ${id}`);
	}
	return {
		cmd: id,
		key: metadata.defaultKeys,
		desc: metadata.desc,
		group: metadata.group ?? splitCommandId(id)?.domain,
	};
}

export function createCommandBindingDefinition(
	id: string,
	run: OpenTuiCommandRun,
	override?: KeybindingCommandMetadata,
): CommandBindingDefinition<Renderable, KeyEvent> {
	const metadata = override ?? getKeybindingCommand(id);
	if (!metadata) {
		throw new Error(`Unknown keybinding command: ${id}`);
	}
	return {
		binding: createBindingDefinitionForCommand(id, metadata),
		command: {
			run,
			...(metadata.hint !== undefined ? { hint: metadata.hint } : {}),
			...(metadata.title !== undefined ? { title: metadata.title } : {}),
			...(metadata.category !== undefined
				? { category: metadata.category }
				: {}),
		},
	};
}

export function createPickerCommandMetadata(
	namespace: string,
	options: {
		includeComplete?: boolean;
		selectHint?: string;
	} = {},
): KeybindingCommandMetadataMap {
	const metadata: KeybindingCommandMetadataMap = {};
	for (const [action, command] of Object.entries(PICKER_KEYBINDING_COMMANDS)) {
		if (action === "complete" && !options.includeComplete) continue;
		const id = `${namespace}.${action}`;
		metadata[id] = {
			...command,
			group: namespace,
			...(action === "select" && options.selectHint
				? { hint: options.selectHint }
				: {}),
		};
	}
	return metadata;
}
