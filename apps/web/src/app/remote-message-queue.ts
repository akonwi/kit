export const MAX_REMOTE_MESSAGE_PREVIEWS = 3;
export const MAX_REMOTE_MESSAGE_PREVIEW_LENGTH = 240;

function normalizePreview(message: string): string {
	return message.replace(/\s+/g, " ").trim();
}

function truncatePreview(message: string): string {
	const characters = Array.from(message);
	if (characters.length <= MAX_REMOTE_MESSAGE_PREVIEW_LENGTH) return message;
	return `${characters
		.slice(0, MAX_REMOTE_MESSAGE_PREVIEW_LENGTH - 3)
		.join("")}...`;
}

export function remoteMessagePreviews(messages: readonly string[]): string[] {
	return messages
		.slice(0, MAX_REMOTE_MESSAGE_PREVIEWS)
		.map(normalizePreview)
		.filter(Boolean)
		.map(truncatePreview);
}
