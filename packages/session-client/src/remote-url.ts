export const MAX_REMOTE_URL_LENGTH = 8_192;

function hasControlCharacter(value: string): boolean {
	for (let index = 0; index < value.length; index += 1) {
		const code = value.charCodeAt(index);
		if (code < 32 || code === 127) return true;
	}
	return false;
}

export function normalizeRemoteHttpUrl(value: unknown): string | null {
	if (
		typeof value !== "string" ||
		value.length === 0 ||
		value.length > MAX_REMOTE_URL_LENGTH ||
		hasControlCharacter(value)
	) {
		return null;
	}
	try {
		const url = new URL(value);
		return url.protocol === "http:" || url.protocol === "https:"
			? url.href
			: null;
	} catch {
		return null;
	}
}
