import type { TurnActivityModel } from "./turn-activity-view";

/**
 * Stable copy for the activity view header title + metadata, shared by
 * both the modal and the sidebar variants so future tweaks propagate.
 * Callers are responsible for the surrounding chrome (Dialog.Header vs.
 * the inline panel's border-bottom strip).
 */
export const TURN_ACTIVITY_TITLE = "Turn activity";

export function turnActivityMetaText(model: TurnActivityModel): string {
	const toolCalls = model.toolCallCount();
	const steps = model.stepCount();
	return (
		`${toolCalls} tool call${toolCalls === 1 ? "" : "s"}` +
		` · ${steps} step${steps === 1 ? "" : "s"}`
	);
}
