import type { InternalPluginAPI } from "../../plugins";
import { ringBell } from "../notifications/notifications";
import { createGuidedQuestionsController } from "./controller";
import { GuidedQuestionsContent } from "./GuidedQuestionsContent";
import { createGuidedQuestionsTool, GUIDED_QUESTIONS_POLICY } from "./tool";

export function GuidedQuestionsPlugin(kit: InternalPluginAPI): () => void {
	const controller = createGuidedQuestionsController();

	// Append the policy to the system prompt so the model knows when to use
	// the tool. Plugin owns the policy — App.tsx no longer needs to import it.
	kit.addSystemPrompt(GUIDED_QUESTIONS_POLICY);

	// Register the guided_questions tool. The tool reads the current
	// `guidedQuestions` setting on every invocation, so toggling the setting
	// takes effect immediately (next tool call returns a disabled response).
	const tool = createGuidedQuestionsTool(controller, {
		getSettings: () => kit.settings.get(),
		notify: () =>
			ringBell(false, {
				notify: kit.system.notify,
				title: "Kit",
				message: "Input needed",
			}),
	});
	kit.registerTool(tool);

	const unsubscribe = controller.subscribe((active) => {
		if (!active) return;
		void kit.ui.custom((props) => (
			<GuidedQuestionsContent
				guidedQuestions={controller}
				onClose={() => props.done(undefined)}
				surfaceProps={props.surfaceProps}
			/>
		));
	});

	return unsubscribe;
}
