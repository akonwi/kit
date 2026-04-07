// @ts-nocheck — disabled pending rewrite
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { completeSimple } from "@mariozechner/pi-ai";
import type { AgentRuntime } from "../../backend";

const AUTO_TITLE_COOLDOWN_MS = 4 * 60 * 1000;
const AUTO_TITLE_MIN_USER_MESSAGES = 2;
const AUTO_TITLE_MAX_TOKENS = 32;
const AUTO_TITLE_DISABLED = process.env.KIT_NO_AUTO_TITLE === "1";
const lastAutoTitleAttemptBySession = new Map<string, number>();

function clip(text: string, maxChars: number): string {
	if (text.length <= maxChars) return text;
	return `${text.slice(0, Math.max(0, maxChars - 1)).trimEnd()}…`;
}

function messageText(msg: AgentMessage): string {
	const content: unknown = (msg as { content?: unknown }).content;
	if (typeof content === "string") return content;
	if (!Array.isArray(content)) return "";

	return content
		.map((part) => {
			if (!part || typeof part !== "object") return "";
			const block = part as Record<string, unknown>;
			if (block.type === "text" && typeof block.text === "string") {
				return block.text;
			}
			return "";
		})
		.filter(Boolean)
		.join("\n")
		.trim();
}

function buildConversationSummary(
	messages: AgentMessage[],
	maxMessages: number,
	maxChars: number,
): string {
	const items = messages
		.filter((m) => m.role === "user" || m.role === "assistant")
		.map((m) => {
			const role = m.role === "user" ? "User" : "Assistant";
			const text = messageText(m).replace(/\s+/g, " ").trim();
			return text ? `${role}: ${text}` : "";
		})
		.filter(Boolean)
		.slice(-maxMessages);

	if (items.length === 0) {
		return "";
	}

	return clip(items.join("\n"), maxChars);
}

function sanitizeGeneratedTitle(raw: string): string {
	const firstLine = raw.split(/\r?\n/)[0] || "";
	const cleaned = firstLine
		.replace(/^['"`]+|['"`]+$/g, "")
		.replace(/^title\s*:\s*/i, "")
		.replace(/\s+/g, " ")
		.trim();

	const words = cleaned.split(" ").filter(Boolean).slice(0, 6);
	const compact = words
		.join(" ")
		.replace(/[.!,;:]+$/g, "")
		.trim();
	if (!compact) return "";
	if (/^untitled$/i.test(compact)) return "";
	return clip(compact, 48);
}

function lastAssistantFailed(messages: AgentMessage[]): boolean {
	for (let i = messages.length - 1; i >= 0; i--) {
		const msg = messages[i] as AgentMessage & { stopReason?: string };
		if (msg.role !== "assistant") continue;
		return msg.stopReason === "error" || msg.stopReason === "aborted";
	}
	return false;
}

async function generateTitleWithCurrentModel(
	runtime: AgentRuntime,
	prompt: string,
): Promise<string> {
	const agentSession = runtime.getAgentSession();
	const model = agentSession.model;
	if (!model) return "";

	const apiKey = await agentSession.modelRegistry.getApiKey(model);
	if (!apiKey) return "";

	const response = await completeSimple(
		model,
		{
			messages: [
				{
					role: "user",
					content: prompt,
					timestamp: Date.now(),
				},
			],
		},
		{
			apiKey,
			maxTokens: AUTO_TITLE_MAX_TOKENS,
			...(model.reasoning ? { reasoning: "minimal" as const } : {}),
		},
	);

	return response.content
		.filter(
			(part): part is { type: "text"; text: string } => part.type === "text",
		)
		.map((part) => part.text)
		.join("\n");
}

export async function maybeAutoNameSession(
	runtime: AgentRuntime,
	messages: AgentMessage[],
): Promise<void> {
	if (AUTO_TITLE_DISABLED) return;
	if (lastAssistantFailed(messages)) return;

	const session = runtime.getSession();
	const sessionId = session.sessionId;
	if (!sessionId) return;
	if (session.sessionName?.trim()) return;

	const now = Date.now();
	const lastAttempt = lastAutoTitleAttemptBySession.get(sessionId) || 0;
	if (now - lastAttempt < AUTO_TITLE_COOLDOWN_MS) return;

	const userCount = messages.filter((m) => m.role === "user").length;
	if (userCount < AUTO_TITLE_MIN_USER_MESSAGES) return;

	const summary = buildConversationSummary(messages, 10, 900);
	if (!summary) return;

	lastAutoTitleAttemptBySession.set(sessionId, now);

	const prompt = [
		"Generate a concise conversation title.",
		"Rules:",
		"- Return title only, no quotes, no markdown.",
		"- Max 5 words.",
		"- Focus on concrete task/topic.",
		"- If unclear, return Untitled.",
		"",
		"Conversation summary:",
		summary,
	].join("\n");

	try {
		const title = sanitizeGeneratedTitle(
			await generateTitleWithCurrentModel(runtime, prompt),
		);
		if (!title) return;

		const currentSession = runtime.getSession();
		if (currentSession.sessionId !== sessionId) return;
		if (currentSession.sessionName?.trim()) return;

		runtime.setSessionName(title);
	} catch {
		// Best effort only.
	}
}
