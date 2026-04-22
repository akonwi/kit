export type TextMessagePart = {
	type: "text";
	text: string;
};

export type CustomMessagePart = {
	type: string;
	promptText: string;
	[key: string]: unknown;
};

export type MessagePart = TextMessagePart | CustomMessagePart;

export type UserMultipartMessage = {
	role: "user";
	content: MessagePart[];
	timestamp: number;
};

export function messagePartToPromptText(part: MessagePart): string {
	if (part.type === "text" && "text" in part && typeof part.text === "string") {
		return part.text;
	}
	if ("promptText" in part && typeof part.promptText === "string") {
		return part.promptText;
	}
	return "";
}
