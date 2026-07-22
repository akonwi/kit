import { type ChildProcess, spawn } from "node:child_process";
import { platform } from "node:os";
import type { PluginAPI, RuntimeEvent } from "@akonwi/kit/plugin";

/**
 * Project plugin: speak completed assistant responses on macOS.
 *
 * Copy this file to ~/.kit/plugins/ to make it available in every project,
 * then run /speech to enable it. Adjust the constants below as desired.
 */
const DEFAULT_ENABLED = false;
const MAX_CHARS = 220;
const VOICE: string | undefined = undefined;
const HEADER_ID = "speech:status";

type AssistantMessage = {
	role?: string;
	content?: string | unknown[];
};

type CompletedTurnEvent = RuntimeEvent<"agent.turn.completed"> & {
	turn?: { messages?: AssistantMessage[] } | null;
};

function cleanForSpeech(text: string): string {
	return text
		.replace(/```[\s\S]*?```/g, " code block omitted ")
		.replace(/`([^`]+)`/g, "$1")
		.replace(/\[(.*?)\]\((.*?)\)/g, "$1")
		.replace(/[*_~#>]/g, "")
		.replace(/\s+/g, " ")
		.trim();
}

function shortenForSpeech(text: string, maxChars: number): string {
	const cleaned = cleanForSpeech(text);
	if (!cleaned || cleaned.length <= maxChars) return cleaned;

	const sentence = cleaned.match(/(.+?[.!?])(\s|$)/)?.[1]?.trim();
	if (sentence && sentence.length <= maxChars) return sentence;
	return `${cleaned.slice(0, Math.max(0, maxChars - 3))}...`;
}

function lastAssistantText(event: CompletedTurnEvent): string | null {
	const messages = Array.isArray(event.turn?.messages)
		? event.turn.messages
		: [];
	for (let index = messages.length - 1; index >= 0; index--) {
		const message = messages[index];
		if (!message || message.role !== "assistant") continue;
		if (typeof message.content === "string") return message.content;
		if (!Array.isArray(message.content)) continue;
		const text = message.content
			.filter(
				(block): block is { type: "text"; text: string } =>
					Boolean(block) &&
					typeof block === "object" &&
					"type" in block &&
					block.type === "text" &&
					"text" in block &&
					typeof block.text === "string",
			)
			.map((block) => block.text)
			.join("\n");
		return text || null;
	}
	return null;
}

export default function SpeechPlugin(kit: PluginAPI): () => void {
	if (platform() !== "darwin") return () => {};

	let enabled = DEFAULT_ENABLED;
	const lastSpoken = new Map<string, string>();
	const children = new Set<ChildProcess>();

	const stopSpeaking = () => {
		for (const child of children) child.kill();
		children.clear();
	};
	const toggle = (): boolean => {
		enabled = !enabled;
		if (!enabled) stopSpeaking();
		renderStatus();
		return enabled;
	};
	const renderStatus = () => {
		kit.header.set(HEADER_ID, `speech ${enabled ? "on" : "off"}`, {
			side: "right",
			onClick: toggle,
		});
	};

	renderStatus();

	kit.on("agent.turn.completed", (rawEvent, ctx) => {
		if (!enabled) return;
		const text = lastAssistantText(rawEvent as CompletedTurnEvent);
		if (!text) return;
		const speech = shortenForSpeech(text, MAX_CHARS);
		if (!speech) return;

		const sessionId = ctx.session.get().id;
		if (lastSpoken.get(sessionId) === speech) return;
		lastSpoken.set(sessionId, speech);

		const args = VOICE ? ["-v", VOICE, speech] : [speech];
		const child = spawn("say", args, { stdio: "ignore" });
		children.add(child);
		const forget = () => children.delete(child);
		child.once("error", (error) => {
			forget();
			if (lastSpoken.get(sessionId) === speech) {
				lastSpoken.delete(sessionId);
			}
			kit.logger.log("Could not launch macOS speech", error);
		});
		child.once("exit", forget);
	});

	kit.registerCommand(
		"speech",
		{
			description: "Toggle spoken assistant responses",
			category: "plugins",
		},
		async (ctx) => {
			const next = toggle();
			ctx.ui.toast({
				title: `Speech ${next ? "enabled" : "disabled"}`,
				variant: "info",
			});
		},
	);

	return stopSpeaking;
}
