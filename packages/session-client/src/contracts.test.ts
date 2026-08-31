import { describe, expect, test } from "bun:test";
import type { SessionClientMessage } from "./contracts";

describe("session client message projections", () => {
	test("represents remote-safe image and unavailable records", () => {
		const imageMessage = {
			role: "user",
			messageId: "message-1",
			turnId: "turn-1",
			timestamp: 1,
			content: [
				{
					type: "image",
					dataOmitted: true,
					mimeType: "image/png",
					filename: "screenshot.png",
					attachmentId: "attachment-1",
				},
			],
		} as const satisfies SessionClientMessage;
		const unavailable = {
			type: "message_unavailable",
			messageIndex: 3,
			serializedBytes: 20_000_000,
			reason: "exceeds_recovery_limit",
		} as const satisfies SessionClientMessage;

		expect(imageMessage.content[0].dataOmitted).toBe(true);
		expect(unavailable.messageIndex).toBe(3);
	});

	test("represents assistant, review, and bash presentation metadata", () => {
		const messages = [
			{
				role: "assistant",
				messageId: "message-2",
				turnId: "turn-1",
				timestamp: 2,
				content: [{ type: "text", text: "done" }],
				stopReason: "aborted",
				synthetic: {
					kind: "handoff-summary",
					sourceSessionName: "Previous session",
					subagentSource: "agent",
				},
			},
			{
				role: "user",
				messageId: "message-3",
				turnId: "turn-2",
				timestamp: 3,
				content: [
					{
						type: "code-review",
						review: {
							submittedAt: "2026-01-01T00:00:00.000Z",
							source: "file",
							commit: {
								sha: "head",
								parentSha: "base",
								subject: "Review target",
							},
							submissionId: "submission-1",
							files: [],
						},
					},
				],
			},
			{
				role: "bashExecution",
				messageId: "message-4",
				turnId: "turn-2",
				timestamp: 4,
				command: "git diff",
				output: "...",
				truncated: true,
			},
		] as const satisfies readonly SessionClientMessage[];

		expect(messages).toHaveLength(3);
	});
});
