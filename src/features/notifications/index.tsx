import { Plugin } from "../../plugins/Plugin";
import type { AgentRuntimeEvent } from "../../runtime/agent-runtime";
import type { Turn } from "../../session/types";
import {
	loadNotificationConfigSync,
	loadNotificationConfig,
	saveNotificationConfig,
	saveNotificationConfigSync,
	type NotificationConfig,
} from "./notification-config";
import { ringBell, speak } from "./notifications";

export {
	loadNotificationConfig,
	loadNotificationConfigSync,
	saveNotificationConfig,
	saveNotificationConfigSync,
};
export type { NotificationConfig };

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

export class NotificationsPlugin extends Plugin {
	private config: NotificationConfig;

	constructor(ctx: ConstructorParameters<typeof Plugin>[0]) {
		super(ctx);
		this.config = loadNotificationConfigSync();
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
				await this.toggleBells();
			},
		});

		// Register /speech command
		this.registerCommand({
			name: "speech",
			description: "Toggle the agent's speech notifications",
			execute: async () => {
				await this.toggleSpeech();
			},
		});
	}

	private notifyTurnComplete(turn: Turn | null): void {
		if (!turn) return;
		const isError = turn.messages.some(
			(message: { role: string; stopReason?: string }) =>
				message.role === "assistant" && message.stopReason === "error",
		);
		ringBell(isError, this.config.bells.enabled);

		if (!this.config.speech.enabled) return;
		const assistantText = getLastAssistantText(turn.messages);
		if (!assistantText) return;
		const sessionId = this.ctx.runtime.getSession().id;
		speak(assistantText, sessionId, {
			maxChars: this.config.speech.maxChars,
			voice: this.config.speech.voice ?? undefined,
		});
	}

	private async toggleBells(): Promise<boolean> {
		this.config = {
			...this.config,
			bells: { enabled: !this.config.bells.enabled },
		};
		await saveNotificationConfig(this.config);
		this.ctx.runtime.emitNotificationConfigChanged(this.config);
		this.ctx.ui.notify(
			this.config.bells.enabled ? "Bells enabled" : "Bells disabled",
		);
		return this.config.bells.enabled;
	}

	private async toggleSpeech(): Promise<boolean> {
		this.config = {
			...this.config,
			speech: {
				...this.config.speech,
				enabled: !this.config.speech.enabled,
			},
		};
		await saveNotificationConfig(this.config);
		this.ctx.runtime.emitNotificationConfigChanged(this.config);
		this.ctx.ui.notify(
			this.config.speech.enabled ? "Speech enabled" : "Speech disabled",
		);
		return this.config.speech.enabled;
	}
}