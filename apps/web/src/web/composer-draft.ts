export function mergeQueuedFollowUpsIntoDraft(
	messages: readonly string[],
	currentDraft: string,
): string {
	return [...messages, ...(currentDraft ? [currentDraft] : [])].join("\n\n");
}
