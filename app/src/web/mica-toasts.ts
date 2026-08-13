import { toast } from "@akonwi/mica/toast.js";
import type { WebToastInput } from "./web-toasts";

const activeToasts = new Map<string, HTMLElement>();

export function showMicaToast(input: WebToastInput): void {
	const key = `${input.variant}\u0000${input.duration ?? "default"}\u0000${input.title}\u0000${input.description ?? ""}`;
	const existing = activeToasts.get(key);
	if (existing?.isConnected && existing.matches(":popover-open")) return;
	const element = toast(input.title, {
		description: input.description,
		...(input.variant === "info"
			? {}
			: { variant: input.variant === "error" ? "danger" : input.variant }),
		duration: input.duration,
	});
	activeToasts.set(key, element);
	element.addEventListener("toggle", () => {
		if (
			!element.matches(":popover-open") &&
			activeToasts.get(key) === element
		) {
			activeToasts.delete(key);
		}
	});
}
