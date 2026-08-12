type ComposerKeyInput = Pick<KeyboardEvent, "isComposing" | "key" | "shiftKey">;

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
