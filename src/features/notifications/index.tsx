import { Plugin } from "../../plugins/Plugin";
import type { AgentRuntimeEvent } from "../../runtime/agent-runtime";
import type { Turn } from "../../session/types";
import { type Settings, saveSettings } from "../../settings";
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

type ResolvedSpeech = {
	enabled: boolean;
	maxChars: number;
	voice?: string;
};

function resolveSpeechConfig(speech: Settings["speech"]): ResolvedSpeech {
	if (typeof speech === "boolean") {
		return { enabled: speech, maxChars: 220 };
	}
	if (speech && typeof speech === "object") {
		return {
			enabled: speech.enabled ?? true,
			maxChars: speech.maxChars ?? 220,
			...(speech.voice ? { voice: speech.voice } : {}),
		};
	}
	return { enabled: true, maxChars: 220 };
}

export class NotificationsPlugin extends Plugin {
	private getSettings(): Settings {
		return this.ctx.settings.settings;
	}

	override initialize(): void {
		// Subscribe to turn_complete for notifications
		this.subscribeRuntime((event: AgentRuntimeEvent) => {
			if (event.type === "turn_complete") {
				this.notifyTurnComplete(event.turn);
			}
		});

		// Register /bells command
		this.registerCommand({
			name: "bells",
			description: "Toggle audible notification sounds on/off",
			execute: async () => {
				const bells = !this.getBells();
				await this.saveSettings({ ...this.getSettings(), bells });
			},
		});

		// Register /speech command
		this.registerCommand({
			name: "speech",
			description: "Toggle the agent's speech notifications",
			execute: async () => {
				const speech = this.getSpeech();
				await this.saveSettings({
					...this.getSettings(),
					speech: { ...speech, enabled: !speech.enabled },
				});
			},
		});
	}

	private getBells(): boolean {
		return this.getSettings().bells ?? true;
	}

	private getSpeech(): ResolvedSpeech {
		return resolveSpeechConfig(this.getSettings().speech);
	}

	private async saveSettings(settings: Settings): Promise<void> {
		await saveSettings(settings);
		this.ctx.settings.settings = settings;
		this.ctx.runtime.emitSettingsChanged(settings);
	}

	private notifyTurnComplete(turn: Turn | null): void {
		if (!turn) return;
		const isError = turn.messages.some(
			(message: { role: string; stopReason?: string }) =>
				message.role === "assistant" && message.stopReason === "error",
		);
		ringBell(isError, this.getBells());

		const speech = this.getSpeech();
		if (!speech.enabled) return;
		const assistantText = getLastAssistantText(turn.messages);
		if (!assistantText) return;
		const sessionId = this.ctx.runtime.getSession().id;
		speak(assistantText, sessionId, {
			maxChars: speech.maxChars,
			voice: speech.voice,
		});
	}
}
