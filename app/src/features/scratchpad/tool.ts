import type { ToolDefinition, ToolResult } from "../../plugins";
import type { InternalPluginUI } from "../../plugins/types";
import { type Static, Type } from "../../runtime/agent";
import type { ScratchpadController } from "./controller";

export const MAX_SCRATCHPAD_LENGTH = 32_000;

const updateScratchpadParameters = Type.Object({
	content: Type.String({
		description: "Content to append or use as the complete scratchpad",
		maxLength: MAX_SCRATCHPAD_LENGTH,
	}),
	mode: Type.Optional(
		Type.Union([Type.Literal("append"), Type.Literal("replace")], {
			description: "Append to the scratchpad (default) or replace it entirely",
		}),
	),
});

type ScratchpadUpdateMode = "append" | "replace";

type ScratchpadUpdateDetails = {
	mode: ScratchpadUpdateMode;
	status:
		| "updated"
		| "declined"
		| "unchanged"
		| "stale"
		| "cancelled"
		| "too_large";
};

type ScratchpadToolOptions = {
	controller: Pick<
		ScratchpadController,
		"applyAtomicUpdate" | "content" | "dirty" | "sessionId"
	>;
	ui: Pick<InternalPluginUI, "confirm">;
	lifecycleSignal?: AbortSignal;
	notify?: () => void;
};

type CombinedSignal = {
	signal: AbortSignal;
	dispose: () => void;
};

function combineSignals(
	signals: Array<AbortSignal | undefined>,
): CombinedSignal {
	const controller = new AbortController();
	const removers: Array<() => void> = [];
	for (const signal of signals) {
		if (!signal) continue;
		if (signal.aborted) {
			controller.abort();
			break;
		}
		const abort = () => controller.abort();
		signal.addEventListener("abort", abort, { once: true });
		removers.push(() => signal.removeEventListener("abort", abort));
	}
	return {
		signal: controller.signal,
		dispose: () => {
			for (const remove of removers) remove();
		},
	};
}

export function buildScratchpadUpdate(
	current: string,
	content: string,
	mode: ScratchpadUpdateMode,
): string {
	if (mode === "replace") return content;
	const addition = content.trim();
	if (!addition) return current;
	if (!current) return addition;
	const separator = current.endsWith("\n\n")
		? ""
		: current.endsWith("\n")
			? "\n"
			: "\n\n";
	return `${current}${separator}${addition}`;
}

function approvalMessage(content: string, mode: ScratchpadUpdateMode): string {
	if (mode === "replace" && content.length === 0) {
		return "Clear the current scratchpad?";
	}
	return `${mode === "append" ? "Append this content" : "Replace the scratchpad with this content"}:\n\n${content}`;
}

export function createUpdateScratchpadTool(
	options: ScratchpadToolOptions,
): ToolDefinition<typeof updateScratchpadParameters, ScratchpadUpdateDetails> {
	return {
		name: "update_scratchpad",
		label: "Update Scratchpad",
		description:
			"Propose an append or replacement to the current session scratchpad. The user must approve before it is saved.",
		promptSnippet:
			"Propose persistent session notes with update_scratchpad when information should be retained in the user's scratchpad.",
		promptGuidelines: [
			"Use only for concise information worth retaining across later turns in the current session.",
			"Default to append; use replace only when rewriting or clearing the full scratchpad is intentional.",
			"The tool requests user approval itself; do not ask for a separate confirmation first.",
		],
		parameters: updateScratchpadParameters,
		async execute(
			_toolCallId: string,
			input: Static<typeof updateScratchpadParameters>,
			signal?: AbortSignal,
		): Promise<ToolResult<ScratchpadUpdateDetails>> {
			const mode = input.mode ?? "append";
			const proposedContent =
				mode === "append" ? input.content.trim() : input.content;
			const targetSessionId = options.controller.sessionId();
			const original = options.controller.content();
			if (options.controller.dirty()) {
				return {
					content: [
						{
							type: "text",
							text: "Scratchpad has unsaved user edits; no update was proposed.",
						},
					],
					details: { mode, status: "stale" },
				};
			}
			const proposed = buildScratchpadUpdate(original, proposedContent, mode);
			if (proposed === original) {
				return {
					content: [{ type: "text", text: "Scratchpad is unchanged." }],
					details: { mode, status: "unchanged" },
				};
			}
			if (proposed.length > MAX_SCRATCHPAD_LENGTH) {
				return {
					content: [
						{
							type: "text",
							text: `Scratchpad update exceeds the ${MAX_SCRATCHPAD_LENGTH}-character limit.`,
						},
					],
					details: { mode, status: "too_large" },
				};
			}

			const combined = combineSignals([signal, options.lifecycleSignal]);
			try {
				if (combined.signal.aborted) {
					return {
						content: [{ type: "text", text: "Scratchpad update cancelled." }],
						details: { mode, status: "cancelled" },
					};
				}
				options.notify?.();
				const approved = await options.ui.confirm({
					title: "Update scratchpad?",
					message: approvalMessage(proposedContent, mode),
					confirmLabel: "Update",
					cancelLabel: "Keep current",
					defaultValue: false,
					signal: combined.signal,
				});
				if (combined.signal.aborted) {
					return {
						content: [{ type: "text", text: "Scratchpad update cancelled." }],
						details: { mode, status: "cancelled" },
					};
				}
				if (options.controller.sessionId() !== targetSessionId) {
					return {
						content: [
							{
								type: "text",
								text: "Scratchpad update cancelled because the active session changed.",
							},
						],
						details: { mode, status: "cancelled" },
					};
				}
				if (!approved) {
					return {
						content: [
							{ type: "text", text: "Scratchpad update was not approved." },
						],
						details: { mode, status: "declined" },
					};
				}

				const latest = options.controller.content();
				if (mode === "replace" && latest !== original) {
					return {
						content: [
							{
								type: "text",
								text: "Scratchpad changed before approval; no replacement was applied.",
							},
						],
						details: { mode, status: "stale" },
					};
				}
				let grewTooLarge = false;
				const result = options.controller.applyAtomicUpdate(
					targetSessionId,
					(persisted) => {
						if (mode === "replace" && persisted !== original) return null;
						const next = buildScratchpadUpdate(
							persisted,
							proposedContent,
							mode,
						);
						if (next.length > MAX_SCRATCHPAD_LENGTH) {
							grewTooLarge = true;
							return null;
						}
						return next;
					},
				);
				if (!result) {
					return {
						content: [
							{
								type: "text",
								text: "Scratchpad update cancelled because the active session changed.",
							},
						],
						details: { mode, status: "cancelled" },
					};
				}
				if (!result.updated) {
					return {
						content: [
							{
								type: "text",
								text: grewTooLarge
									? "Scratchpad grew before approval; no update was applied."
									: "Scratchpad changed in another process; no update was applied.",
							},
						],
						details: {
							mode,
							status: grewTooLarge ? "too_large" : "stale",
						},
					};
				}

				return {
					content: [{ type: "text", text: "Scratchpad updated." }],
					details: { mode, status: "updated" },
				};
			} finally {
				combined.dispose();
			}
		},
	};
}
