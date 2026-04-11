import { createEffect } from "solid-js";
import { Plugin } from "../../plugins/Plugin";
import {
	createGuidedQuestionsController,
	type GuidedQuestionsController,
} from "./controller";
import { GuidedQuestionsContent } from "./GuidedQuestionsContent";
import { createGuidedQuestionsTool } from "./tool";

export { GUIDED_QUESTIONS_POLICY } from "./tool";

export class GuidedQuestionsPlugin extends Plugin {
	private readonly controller: GuidedQuestionsController =
		createGuidedQuestionsController();

	override initialize(): void {
		// Register the guided_questions tool
		const tool = createGuidedQuestionsTool(this.controller);
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
