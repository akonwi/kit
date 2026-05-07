/**
 * guided_questions tool — registered with the agent session so the model
 * can invoke it to ask the user structured questions.
 */

import type { AgentToolResult } from "@mariozechner/pi-agent-core";
import { type Static, Type } from "@mariozechner/pi-ai";
import type { PluginContext } from "../../plugins/types";
import { ringBell } from "../notifications/notifications";
import type { GuidedQuestionsController } from "./controller";
import { type GuidedQuestionsInput, normalizeQuestion } from "./types";

const GUIDED_QUESTIONS_POLICY = [
	"When you need clarification from the user and there are 2 or more missing inputs, call guided_questions instead of asking a long list in plain chat.",
	"Keep questions short and concrete.",
	"Prefer select/boolean questions when possible, and only use free text when necessary.",
	"After guided_questions returns, proceed using details.answers as source-of-truth.",
].join("\n");

export { GUIDED_QUESTIONS_POLICY };

// Extracted so execute() can reference `Static<typeof parameters>` instead of `any`.
const parameters = Type.Object({
	title: Type.Optional(
		Type.String({ description: "Short title shown to the user" }),
	),
	intro: Type.Optional(
		Type.String({
			description: "Optional intro shown before the first question",
		}),
	),
	questions: Type.Array(
		Type.Object({
			id: Type.String({ description: "Stable key for the answer" }),
			kind: Type.Optional(
				Type.String({
					description: "text | select | multiselect | boolean",
				}),
			),
			label: Type.String({ description: "Question shown to the user" }),
			help: Type.Optional(Type.String({ description: "Optional helper text" })),
			placeholder: Type.Optional(
				Type.String({ description: "Placeholder for text input" }),
			),
			required: Type.Optional(
				Type.Boolean({
					description: "Whether answer is required (default true)",
				}),
			),
			options: Type.Optional(
				Type.Array(Type.String(), {
					description: "Options for select questions",
				}),
			),
		}),
		{ minItems: 1, maxItems: 12 },
	),
});

export function createGuidedQuestionsTool(
	guidedQuestions: GuidedQuestionsController,
	ctx: PluginContext,
) {
	return {
		name: "guided_questions",
		label: "Guided Questions",
		description:
			"Ask the user a structured, one-question-at-a-time questionnaire in the terminal UI.",
		promptSnippet:
			"Collect structured user answers via an interactive questionnaire when multiple clarifying questions are needed.",
		promptGuidelines: [
			"Use this tool when you need 2+ clarifying answers from the user.",
			"Prefer short labels and constrained choices for select questions.",
			"After the tool returns, continue using the structured answers directly.",
		],
		parameters,
		async execute(
			_toolCallId: string,
			input: Static<typeof parameters>,
			_signal: AbortSignal | undefined,
			_onUpdate: unknown,
		): Promise<AgentToolResult<Record<string, unknown>>> {
			const params: GuidedQuestionsInput = {
				title: input.title,
				intro: input.intro,
				questions: (input.questions || []).map(normalizeQuestion),
			};

			if (ctx.settings.settings.guidedQuestions === false) {
				return {
					content: [
						{
							type: "text" as const,
							text: "Guided questions are currently disabled by the user. Ask the clarifying questions directly in plain chat instead.",
						},
					],
					details: {
						disabled: true,
						answers: {},
						answeredCount: 0,
						totalQuestions: params.questions.length,
					},
				};
			}

			ringBell(false, ctx.settings.settings.bells !== false);
			const result = await guidedQuestions.activate(params);
			const title =
				typeof params.title === "string" && params.title.trim()
					? params.title.trim()
					: "Guided questionnaire";

			if (result.cancelled) {
				return {
					content: [
						{ type: "text" as const, text: "Questionnaire cancelled." },
					],
					details: {
						cancelled: true,
						answers: result.answers,
						answeredCount: Object.keys(result.answers).length,
						totalQuestions: params.questions.length,
					},
				};
			}

			const summaryLines = params.questions.map((q) => {
				const value = result.answers[q.id];
				const rendered = Array.isArray(value)
					? value.length > 0
						? value.join(", ")
						: "(skipped)"
					: typeof value === "boolean"
						? value
							? "Yes"
							: "No"
						: String(value || "").trim() || "(skipped)";
				return `- ${q.label}: ${rendered}`;
			});

			return {
				content: [
					{
						type: "text" as const,
						text: [`${title} complete.`, "", ...summaryLines].join("\n"),
					},
				],
				details: {
					title,
					answers: result.answers,
					answeredCount: Object.keys(result.answers).length,
					totalQuestions: params.questions.length,
					completed: true,
				},
			};
		},
	};
}
