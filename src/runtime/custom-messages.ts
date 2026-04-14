/**
 * Custom AgentMessage types for Kit.
 *
 * Uses pi-agent-core's declaration merging to register app-specific
 * message roles that the transcript can render but the LLM never sees.
 */

export {};

declare module "@mariozechner/pi-agent-core" {
	interface CustomAgentMessages {
		bashExecution: {
			role: "bashExecution";
			command: string;
			output: string;
			exitCode: number | undefined;
			cancelled: boolean;
			truncated: boolean;
			/** When true, this message is excluded from LLM context */
			excludeFromContext: boolean;
			timestamp: number;
		};
	}
}
