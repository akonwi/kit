export type ActiveSubagentStatus = "idle" | "running" | "failed" | "aborted";

export interface ActiveSubagentConversationState {
	agentName: string;
	subagentConversationId: string;
	status: ActiveSubagentStatus;
	model?: string;
	description?: string;
	lastActivityAt: string;
	failureMessage?: string;
	abortReason?: string;
}

export class SubagentManager {
	private readonly conversationsByAgent = new Map<
		string,
		ActiveSubagentConversationState
	>();

	reset(): void {
		this.conversationsByAgent.clear();
	}

	getActive(agentName: string): ActiveSubagentConversationState | undefined {
		return this.conversationsByAgent.get(agentName);
	}

	setActive(conversation: ActiveSubagentConversationState): void {
		this.conversationsByAgent.set(conversation.agentName, conversation);
	}

	dismiss(agentName: string): boolean {
		return this.conversationsByAgent.delete(agentName);
	}

	listActive(): ActiveSubagentConversationState[] {
		return [...this.conversationsByAgent.values()];
	}
}
