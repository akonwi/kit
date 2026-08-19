export const WEB_TUI_PROTOCOL_VERSION = 2;
export const MAX_BROWSER_CLIPBOARD_BYTES = 1024 * 1024;

export type BrowserClipboardWrite = {
	type: "clipboard-write";
	id: number;
	text: string;
};

export function parseBrowserClipboardWrite(
	message: string,
): BrowserClipboardWrite | null {
	// JSON escaping can expand one-byte control characters to six ASCII bytes.
	if (message.length > MAX_BROWSER_CLIPBOARD_BYTES * 6 + 256) return null;
	let parsed: unknown;
	try {
		parsed = JSON.parse(message);
	} catch {
		return null;
	}
	if (typeof parsed !== "object" || parsed === null) return null;
	const record = parsed as Record<string, unknown>;
	if (
		record.type !== "clipboard-write" ||
		!Number.isSafeInteger(record.id) ||
		(record.id as number) <= 0 ||
		typeof record.text !== "string" ||
		new TextEncoder().encode(record.text).byteLength >
			MAX_BROWSER_CLIPBOARD_BYTES
	) {
		return null;
	}
	return record as BrowserClipboardWrite;
}
