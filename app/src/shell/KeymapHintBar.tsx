import type { KeyEvent, Renderable } from "@opentui/core";
import {
	type ActiveBinding,
	type CommandEntry,
	stringifyKeySequence,
} from "@opentui/keymap";
import { useKeymapSelector } from "@opentui/keymap/solid";
import { type Binding, HintBar } from "./HintBar";

export type KeymapHintBarProps = {
	group: string;
	category?: string;
	borderless?: boolean;
	prefixBindings?: Binding[];
	suffixBindings?: Binding[];
};

function formatKeyToken(token: string): string {
	switch (token.toLowerCase()) {
		case "ctrl":
			return "Ctrl";
		case "shift":
			return "Shift";
		case "meta":
			return "Meta";
		case "alt":
			return "Alt";
		case "super":
			return "Super";
		case "return":
		case "enter":
			return "Enter";
		case "escape":
			return "Esc";
		case "tab":
			return "Tab";
		case "space":
			return "Space";
		case "up":
			return "↑";
		case "down":
			return "↓";
		case "left":
			return "←";
		case "right":
			return "→";
		default:
			return token.length === 1 ? token.toUpperCase() : token;
	}
}

type OpenTuiActiveBinding = ActiveBinding<Renderable, KeyEvent>;
type OpenTuiCommandEntry = CommandEntry<Renderable, KeyEvent>;

function formatKeySequence(binding: OpenTuiActiveBinding): string {
	return stringifyKeySequence(binding.sequence, { separator: " " })
		.split(" ")
		.map((stroke) => stroke.split("+").map(formatKeyToken).join("+"))
		.join(" ");
}

function formatCommandBindings(
	bindings: readonly OpenTuiActiveBinding[],
): string {
	return Array.from(new Set(bindings.map(formatKeySequence))).join("/");
}

function titleFromCommandName(name: string): string {
	const segment = name.split(".").at(-1) ?? name;
	return segment.replaceAll("-", " ");
}

function commandField(command: OpenTuiCommandEntry["command"], key: string) {
	return (command as Record<string, unknown>)[key];
}

function commandHint(entry: OpenTuiCommandEntry): string {
	const hint = commandField(entry.command, "hint");
	if (typeof hint === "string" && hint.trim()) return hint.trim();
	const title = commandField(entry.command, "title");
	if (typeof title === "string" && title.trim()) return title.trim();
	return titleFromCommandName(entry.command.name);
}

export function KeymapHintBar(props: KeymapHintBarProps) {
	const bindings = useKeymapSelector<Binding[]>((keymap) => {
		const entries = keymap.getCommandEntries({
			visibility: "active",
			filter: (command) => {
				if (commandField(command, "group") !== props.group) return false;
				if (commandField(command, "hint") === false) return false;
				if (
					props.category &&
					commandField(command, "category") !== props.category
				) {
					return false;
				}
				return true;
			},
		});

		return entries.flatMap((entry) => {
			if (entry.bindings.length === 0) return [];
			return [
				{
					key: formatCommandBindings(entry.bindings),
					action: commandHint(entry),
				},
			];
		});
	});

	return (
		<HintBar
			borderless={props.borderless}
			bindings={[
				...(props.prefixBindings ?? []),
				...bindings(),
				...(props.suffixBindings ?? []),
			]}
		/>
	);
}
