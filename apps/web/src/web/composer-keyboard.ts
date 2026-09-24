type ComposerKeyInput = Pick<KeyboardEvent, "isComposing" | "key" | "shiftKey">;

export function hasComposerPayload(
	message: string,
	attachmentCount: number,
): boolean {
	return message.trim().length > 0 || attachmentCount > 0;
}

export function shouldSubmitComposerKey(
	event: ComposerKeyInput,
	coarsePointer: boolean,
): boolean {
	return (
		event.key === "Enter" &&
		!event.shiftKey &&
		!event.isComposing &&
		!coarsePointer
	);
}
