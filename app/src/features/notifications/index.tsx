import type { InternalPluginAPI } from "../../plugins";
import type { Turn } from "../../session/types";
import { ringBell } from "./notifications";

function notifyTurnComplete(kit: InternalPluginAPI, turn: Turn | null): void {
	if (!turn) return;
	const isError = turn.messages.some(
		(message: { role: string; stopReason?: string }) =>
			message.role === "assistant" && message.stopReason === "error",
	);
	ringBell(isError, {
		notify: kit.system.notify,
		title: "Kit",
		message: isError ? "Agent turn failed" : "Agent turn complete",
	});
}

export function NotificationsPlugin(kit: InternalPluginAPI): void {
	kit.on("agent.turn.completed", (event) => {
		notifyTurnComplete(kit, event.turn);
	});
}
