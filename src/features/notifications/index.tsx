import type { PluginAPI } from "../../plugins";
import type { Turn } from "../../session/types";
import { resolveSpeechSettings } from "../../settings";
import { ringBell, speak } from "./notifications";

function getLastAssistantText(messages: Turn["messages"]): string | null {
	for (let i = messages.length - 1; i >= 0; i--) {
		const msg = messages[i];
		if (msg.role !== "assistant") continue;
		const content: unknown = (msg as { content?: unknown }).content;
		if (typeof content === "string") return content;
		if (Array.isArray(content)) {
			const text = content
				.filter(
					(block): block is { type: "text"; text: string } =>
						typeof block === "object" &&
						block !== null &&
						"type" in block &&
						block.type === "text" &&
						"text" in block &&
						typeof block.text === "string",
				)
				.map((b) => b.text)
				.join("\n");
			return text || null;
		}
	}
	return null;
}

function notifyTurnComplete(kit: PluginAPI, turn: Turn | null): void {
	if (!turn) return;
	const settings = kit.settings.get();
	const isError = turn.messages.some(
		(message: { role: string; stopReason?: string }) =>
			message.role === "assistant" && message.stopReason === "error",
	);
	ringBell(isError, settings.bells ?? true);

	const speech = resolveSpeechSettings(settings.speech);
	if (!speech.enabled) return;
	const assistantText = getLastAssistantText(turn.messages);
	if (!assistantText) return;
	const sessionId = kit.session.get().id;
	speak(assistantText, sessionId, {
		maxChars: speech.maxChars,
		voice: speech.voice,
	});
}

export function NotificationsPlugin(kit: PluginAPI): void {
	// Subscribe to turn completion for notifications
	kit.on("agent.turn.completed", (event) => {
		notifyTurnComplete(kit, event.turn);
	});

	// Register /bells command
	kit.registerCommand(
		"bells",
		{ description: "Toggle audible notification sounds on/off" },
		async () => {
			const settings = kit.settings.get();
			await kit.settings.update({ bells: !(settings.bells ?? true) });
		},
	);

	// Register /speech command
	kit.registerCommand(
		"speech",
		{ description: "Toggle the agent's speech notifications" },
		async () => {
			const settings = kit.settings.get();
			const speech = resolveSpeechSettings(settings.speech);
			await kit.settings.update({
				speech: { ...speech, enabled: !speech.enabled },
			});
		},
	);
}
