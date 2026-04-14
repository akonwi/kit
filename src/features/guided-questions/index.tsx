import { createEffect } from "solid-js";
import { Plugin } from "../../plugins/Plugin";
import {
	createGuidedQuestionsController,
	type GuidedQuestionsController,
} from "./controller";
import { GuidedQuestionsContent } from "./GuidedQuestionsContent";
import { createGuidedQuestionsTool, GUIDED_QUESTIONS_POLICY } from "./tool";

export class GuidedQuestionsPlugin extends Plugin {
	private readonly controller: GuidedQuestionsController =
		createGuidedQuestionsController();

	override initialize(): void {
		// Append the policy to the system prompt so the model knows when to use
		// the tool. Plugin owns the policy — App.tsx no longer needs to import it.
		this.addSystemPromptAddition(GUIDED_QUESTIONS_POLICY);

		// Register the guided_questions tool. The tool reads the current
		// `guidedQuestions` setting on every invocation, so toggling the setting
		// takes effect immediately (next tool call returns a disabled response).
		const tool = createGuidedQuestionsTool(this.controller, this.ctx);
		this.registerTool(
			tool as import("@mariozechner/pi-agent-core").AgentTool<any>,
		);

		// Watch for activation and show overlay
		createEffect(() => {
			if (this.controller.active) {
				void this.ctx.ui.custom((props) => (
					<GuidedQuestionsContent
						guidedQuestions={this.controller}
						onClose={() => props.done(undefined)}
					/>
				));
			}
		});
	}
}
