import type { InternalPluginAPI } from "../../plugins";
import { ringBell } from "../notifications/notifications";
import { createGuidedQuestionsController } from "./controller";
import { GuidedQuestionsContent } from "./GuidedQuestionsContent";
import { createGuidedQuestionsTool, GUIDED_QUESTIONS_POLICY } from "./tool";
import type { GuidedQuestionsRequester } from "./types";

export function createRemoteGuidedQuestionsPlugin(
	guidedQuestions: GuidedQuestionsRequester,
) {
	return function RemoteGuidedQuestionsPlugin(kit: InternalPluginAPI): void {
		kit.addSystemPrompt(GUIDED_QUESTIONS_POLICY);
		kit.registerTool(
			createGuidedQuestionsTool(guidedQuestions, {
				notify: () => {},
			}),
		);
	};
}

export function GuidedQuestionsPlugin(kit: InternalPluginAPI): () => void {
	const controller = createGuidedQuestionsController();

	// Append the policy to the system prompt so the model knows when to use
	// the tool. Plugin owns the policy — App.tsx no longer needs to import it.
	kit.addSystemPrompt(GUIDED_QUESTIONS_POLICY);

	const tool = createGuidedQuestionsTool(controller, {
		notify: () =>
			ringBell(false, {
				notify: kit.system.notify,
				bell: kit.system.bell,
				title: "Kit",
				message: "Input needed",
			}),
	});
	kit.registerTool(tool);

	let interactionAbort: AbortController | undefined;
	const unsubscribe = controller.subscribe((active) => {
		if (!active) {
			interactionAbort?.abort();
			interactionAbort = undefined;
			return;
		}
		const abort = new AbortController();
		interactionAbort = abort;
		void kit.ui
			.interaction(
				(props) => (
					<GuidedQuestionsContent
						guidedQuestions={controller}
						onClose={() => props.done(undefined)}
						active={props.active}
						maxHeight={props.maxHeight}
					/>
				),
				{ signal: abort.signal, abortValue: undefined },
			)
			.then(() => {
				if (controller.active) controller.cancelAll();
			});
	});

	return () => {
		unsubscribe();
		interactionAbort?.abort();
		controller.cancelAll();
	};
}
