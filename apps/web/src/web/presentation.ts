import { isRecord } from "@akonwi/kit-session-client";

export function displayValue(value: unknown): string {
	if (typeof value === "string") return value;
	if (value === undefined || value === null) return "";
	try {
		return JSON.stringify(value, null, 2);
	} catch {
		return String(value);
	}
}

export function messageParts(message: unknown): Array<Record<string, unknown>> {
	if (!isRecord(message)) return [];
	if (typeof message.content === "string") {
		return [{ type: "text", text: message.content }];
	}
	return Array.isArray(message.content) ? message.content.filter(isRecord) : [];
}

export function messageRole(message: unknown): string {
	return isRecord(message) && typeof message.role === "string"
		? message.role
		: "message";
}

export function shortSessionId(value: unknown): string {
	return typeof value === "string" ? value.slice(0, 8) : "";
}
