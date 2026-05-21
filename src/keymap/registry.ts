import type { KeyEvent, Renderable } from "@opentui/core";
import type { CommandDefinition } from "@opentui/keymap";
import type { BindingDefinition, CommandBindingDefinition } from "./bindings";

export type OpenTuiCommandRun = CommandDefinition<Renderable, KeyEvent>["run"];

export type KeybindingCommandMetadata = {
	defaultKeys: string | readonly string[];
	desc: string;
	group?: string;
	hint?: string | false;
	title?: string;
	category?: string;
};

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
			desc: "Recall previous bash command",
			group: "Composer",
		},
		"bash-history-newer": {
			defaultKeys: "down",
			desc: "Recall next bash command",
			group: "Composer",
		},
		"restore-or-recall": {
			defaultKeys: "up",
			desc: "Restore queued follow-ups or recall previous message",
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
	"mcp-authorization-url": {
		continue: {
			defaultKeys: ["return", "escape"],
			desc: "Continue after MCP authorization",
			group: "mcp-authorization-url",
			hint: "continue",
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
): BindingDefinition {
	const metadata = getKeybindingCommand(id);
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
): CommandBindingDefinition<Renderable, KeyEvent> {
	const metadata = getKeybindingCommand(id);
	if (!metadata) {
		throw new Error(`Unknown keybinding command: ${id}`);
	}
	return {
		binding: createBindingDefinitionForCommand(id),
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
