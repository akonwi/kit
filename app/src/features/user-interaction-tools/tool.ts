import type { ToolDefinition, ToolResult } from "../../plugins";
import type { InternalPluginUI } from "../../plugins/types";
import { type Static, Type } from "../../runtime/agent";
import { ringBell } from "../notifications/notifications";

export const USER_INTERACTION_TOOLS_POLICY = [
	"Use confirm_from_user when you need a single yes/no confirmation instead of asking the user to type yes or no in chat.",
	"Use input_from_user when you need one short freeform answer from the user.",
	"Use select_from_user when the user should choose exactly one option from a concrete list.",
	"Use guided_questions instead when you need 2 or more answers or a multi-step questionnaire.",
].join("\n");

type InteractionToolOptions = {
	ui: Pick<InternalPluginUI, "confirm" | "input" | "select">;
	notify?: () => void;
};

function notifyUser(options: InteractionToolOptions) {
	if (options.notify) {
		options.notify();
		return;
	}
	ringBell(false);
}

const confirmParameters = Type.Object({
	title: Type.String({
		description: "Short confirmation title shown to the user",
	}),
	message: Type.Optional(
		Type.String({ description: "Optional confirmation details" }),
	),
	confirmLabel: Type.Optional(
		Type.String({ description: "Optional label for the affirmative action" }),
	),
	cancelLabel: Type.Optional(
		Type.String({ description: "Optional label for the negative action" }),
	),
});

type ConfirmDetails = { confirmed: boolean };

function createConfirmFromUserTool(
	options: InteractionToolOptions,
): ToolDefinition<typeof confirmParameters, ConfirmDetails> {
	return {
		name: "confirm_from_user",
		label: "Confirm from User",
		description: "Ask the user for a yes/no confirmation in the terminal UI.",
		promptSnippet:
			"Ask the user for a direct confirmation with confirm_from_user when a yes/no decision is needed.",
		promptGuidelines: [
			"Use for a single yes/no decision.",
			"Keep the title and message short and concrete.",
			"Do not use for multi-question clarification; use guided_questions instead.",
		],
		parameters: confirmParameters,
		async execute(
			_toolCallId: string,
			input: Static<typeof confirmParameters>,
		): Promise<ToolResult<ConfirmDetails>> {
			notifyUser(options);
			const confirmed = await options.ui.confirm({
				title: input.title,
				message: input.message,
				confirmLabel: input.confirmLabel,
				cancelLabel: input.cancelLabel,
				defaultValue: false,
			});
			return {
				content: [
					{
						type: "text" as const,
						text: confirmed ? "User confirmed." : "User did not confirm.",
					},
				],
				details: { confirmed },
			};
		},
	};
}

const inputParameters = Type.Object({
	title: Type.String({ description: "Short input title shown to the user" }),
	message: Type.Optional(
		Type.String({ description: "Optional input details" }),
	),
	placeholder: Type.Optional(
		Type.String({ description: "Optional placeholder for the input field" }),
	),
});

type InputDetails = { value: string | null; cancelled: boolean };

function createInputFromUserTool(
	options: InteractionToolOptions,
): ToolDefinition<typeof inputParameters, InputDetails> {
	return {
		name: "input_from_user",
		label: "Input from User",
		description: "Ask the user for one short freeform text input.",
		promptSnippet:
			"Ask the user for one short freeform answer with input_from_user.",
		promptGuidelines: [
			"Use for a single freeform answer.",
			"Keep the title short and concrete.",
			"Use select_from_user instead when the answer should be one of a known set of options.",
		],
		parameters: inputParameters,
		async execute(
			_toolCallId: string,
			input: Static<typeof inputParameters>,
		): Promise<ToolResult<InputDetails>> {
			notifyUser(options);
			const value = await options.ui.input({
				title: input.title,
				message: input.message,
				placeholder: input.placeholder,
			});
			const cancelled = value === undefined;
			return {
				content: [
					{
						type: "text" as const,
						text: cancelled ? "Input cancelled." : "User provided input.",
					},
				],
				details: { value: value ?? null, cancelled },
			};
		},
	};
}

const selectOptionParameters = Type.Object({
	label: Type.String({ description: "Option label shown to the user" }),
	value: Type.String({ description: "Stable value returned if selected" }),
	description: Type.Optional(
		Type.String({ description: "Optional short option description" }),
	),
});

const selectParameters = Type.Object({
	title: Type.String({ description: "Short select title shown to the user" }),
	message: Type.Optional(
		Type.String({ description: "Optional select details" }),
	),
	placeholder: Type.Optional(
		Type.String({ description: "Optional placeholder for filtering" }),
	),
	filterable: Type.Optional(
		Type.Boolean({
			description: "Whether the option list should be filterable",
		}),
	),
	options: Type.Array(selectOptionParameters, { minItems: 1, maxItems: 20 }),
});

type SelectDetails = {
	value: string | null;
	label: string | null;
	cancelled: boolean;
};

function createSelectFromUserTool(
	options: InteractionToolOptions,
): ToolDefinition<typeof selectParameters, SelectDetails> {
	return {
		name: "select_from_user",
		label: "Select from User",
		description: "Ask the user to choose exactly one option from a list.",
		promptSnippet:
			"Ask the user to choose exactly one option with select_from_user when the choices are known.",
		promptGuidelines: [
			"Use for one choice from a concrete list.",
			"Keep option labels short and values stable.",
			"Use guided_questions instead when you need multiple answers.",
		],
		parameters: selectParameters,
		async execute(
			_toolCallId: string,
			input: Static<typeof selectParameters>,
		): Promise<ToolResult<SelectDetails>> {
			notifyUser(options);
			const selected = await options.ui.select({
				title: input.title,
				message: input.message,
				placeholder: input.placeholder,
				filterable: input.filterable,
				options: input.options.map((option) => ({
					label: option.label,
					value: option.value,
					description: option.description,
				})),
			});
			const selectedOption = input.options.find(
				(option) => option.value === selected,
			);
			const cancelled = selected === undefined;
			return {
				content: [
					{
						type: "text" as const,
						text: cancelled
							? "Selection cancelled."
							: `User selected: ${selectedOption?.label ?? selected}.`,
					},
				],
				details: {
					value: selected ?? null,
					label: selectedOption?.label ?? null,
					cancelled,
				},
			};
		},
	};
}

export function createUserInteractionTools(
	options: InteractionToolOptions,
): Array<ToolDefinition> {
	return [
		createConfirmFromUserTool(options),
		createInputFromUserTool(options),
		createSelectFromUserTool(options),
	];
}
