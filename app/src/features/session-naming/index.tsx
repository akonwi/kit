import { completeSimple } from "@earendil-works/pi-ai";
import { getApiKey } from "../../auth";
import type { InternalPluginAPI } from "../../plugins";
import type { AgentMessage, Api, Model } from "../../runtime/agent";
import type { Session } from "../../session";

const AUTO_TITLE_MIN_USER_MESSAGES = 2;
const AUTO_TITLE_MAX_TOKENS = 32;
const AUTO_TITLE_SYSTEM_PROMPT = [
	"You generate concise conversation titles.",
	"Return title only.",
	"No quotes.",
	"No markdown.",
	"Maximum 5 words.",
	"Focus on the concrete task or topic.",
	"If the topic is unclear, return Untitled.",
].join(" ");
const HANDOFF_PLACEHOLDER_PREFIX = "handoff: ";
const AUTO_HANDOFF_PREFIX = "-> ";
/** System-generated name prefixes that auto-naming may overwrite. */
const SYSTEM_NAME_PREFIXES = [HANDOFF_PLACEHOLDER_PREFIX, AUTO_HANDOFF_PREFIX];

/** True when the session has a user-given name rather than a system placeholder. */
function isUserGivenName(name: string | null | undefined): boolean {
	const trimmed = name?.trim();
	if (!trimmed) return false;
	return !SYSTEM_NAME_PREFIXES.some((p) => trimmed.startsWith(p));
}

/** True when the session name starts with a system-generated prefix (not user-given). */
function isSystemNamed(name: string | null | undefined): boolean {
	const trimmed = name?.trim();
	if (!trimmed) return false;
	return SYSTEM_NAME_PREFIXES.some((p) => trimmed.startsWith(p));
}

export function SessionNamingPlugin(kit: InternalPluginAPI): void {
	kit.on("agent.turn.completed", async () => {
		if (kit.settings.get().sessionNaming === false) return;
		await maybeAutoNameSession(kit);
	});
}

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
	model: Model<Api> | undefined,
	prompt: string,
): Promise<string> {
	if (!model) {
		throw new Error("No current model available for session auto-naming.");
	}

	const apiKey = await getApiKey(model.provider);
	if (!apiKey) {
		throw new Error(
			`No API key available for ${model.provider} session auto-naming.`,
		);
	}

	const response = await completeSimple(
		model as Model<Api>,
		{
			systemPrompt: AUTO_TITLE_SYSTEM_PROMPT,
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
		},
	);

	if (response.stopReason === "error" || response.stopReason === "aborted") {
		throw new Error(response.errorMessage || "Session auto-naming failed.");
	}

	return response.content
		.filter(
			(part): part is { type: "text"; text: string } => part.type === "text",
		)
		.map((part) => part.text)
		.join("\n");
}

/** Filter to only messages from turns after the fork boundary (handoff). */
function filterMessagesAfterFork(
	messages: AgentMessage[],
	session: Session,
): AgentMessage[] {
	if (!session.forkedFromTurnId || !session.turns) return messages;

	const boundaryIndex = session.turns.findIndex(
		(t) => t.id === session.forkedFromTurnId,
	);
	if (boundaryIndex < 0) return messages;

	const newTurnIds = new Set(
		session.turns.slice(boundaryIndex + 1).map((t) => t.id),
	);
	return messages.filter((m) =>
		newTurnIds.has((m as unknown as { turnId: string }).turnId),
	);
}

async function maybeAutoNameSession(kit: InternalPluginAPI): Promise<void> {
	const allMessages = kit.session.getMessages();
	if (lastAssistantFailed(allMessages)) return;

	const session = kit.session.get();
	const sessionId = session.id;
	if (!sessionId) return;
	if (isUserGivenName(session.name)) return;

	// For handoff sessions, only use post-fork messages for the title
	// so the name reflects the handoff's purpose, not the parent's history.
	const messages = session.forkedFromTurnId
		? filterMessagesAfterFork(allMessages, session)
		: allMessages;

	if (messages.length === 0) return;

	const userCount = messages.filter((m) => m.role === "user").length;
	if (userCount < AUTO_TITLE_MIN_USER_MESSAGES) return;

	const summary = buildConversationSummary(messages, 10, 900);
	if (!summary) return;

	const prompt = ["Conversation summary:", summary].join("\n\n");

	try {
		const rawTitle = await generateTitleWithCurrentModel(
			kit.model.getCurrent(),
			prompt,
		);
		const title = sanitizeGeneratedTitle(rawTitle);
		if (!title) {
			kit.ui.toast({
				title: "Session auto-name failed",
				subtitle: "The model did not return a usable session title.",
				variant: "warning",
			});
			return;
		}

		const currentSession = kit.session.get();
		if (currentSession.id !== sessionId) return;
		if (isUserGivenName(currentSession.name)) return;

		const wasSystemNamed = isSystemNamed(currentSession.name);
		const finalName = wasSystemNamed ? `${AUTO_HANDOFF_PREFIX}${title}` : title;
		await kit.session.setName(finalName);
	} catch (error) {
		kit.ui.toast({
			title: "Session auto-name failed",
			subtitle: error instanceof Error ? error.message : String(error),
			variant: "warning",
		});
	}
}
