import { generateSummary } from "@mariozechner/pi-coding-agent";
import type { Command } from "./types";

function clip(text: string, max: number): string {
	return text.length <= max ? text : `${text.slice(0, max - 1)}…`;
}

export const handoffCommand: Command = {
	name: "handoff",
	description: "Create a child thread with compact context and switch to it",
	async execute(ctx) {
		const session = ctx.runtime.getSession();
		const sessionFile = session.sessionFile;

		if (!sessionFile) {
			return;
		}

		const sourceId = session.sessionId;
		const sourceIdShort = sourceId.slice(0, 8);

		ctx.palette.show({
			mode: "input",
			label: "Handoff prompt (what should the child thread work on?)",
			onSubmit: async (value, palCtx) => {
				palCtx.dismiss();
				const prompt = value.trim() || "Continue from this handoff context.";
				const childName = `Handoff: ${clip(prompt, 48)}`;

				// Generate an LLM summary of the current session
				const agentSession = ctx.runtime.getAgentSession();
				const model = agentSession.model;
				if (!model) return;

				const apiKey = await agentSession.modelRegistry.getApiKey(model);
				if (!apiKey) return;

				ctx.runtime.showPanel("Generating handoff summary...");
				let summary: string;
				try {
					const messages = ctx.runtime.getMessages();
					summary = await generateSummary(
						messages,
						model,
						4096,
						apiKey,
						undefined,
						"Summarize this conversation for handoff to a new coding assistant thread. Focus on: what was accomplished, what files were changed, what remains to be done, and any important decisions or context.",
					);
				} catch (error) {
					ctx.runtime.hidePanel();
					throw error;
				}
				ctx.runtime.hidePanel();

				const seededPrompt = [
					prompt,
					"",
					"---",
					"",
					"Handoff context from parent thread:",
					summary,
					"",
					`Parent thread reference: [[thread:${sourceIdShort}]]`,
					`Parent session ID: ${sourceId}`,
				].join("\n");

				const ok = await ctx.runtime.newSession({
					parentSession: sessionFile,
					setup: async (sm) => {
						sm.appendSessionInfo(childName);
					},
				});

				if (!ok) return;

				await ctx.runtime.submitUserMessage(seededPrompt);
			},
		});
	},
};
