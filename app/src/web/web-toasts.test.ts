import { describe, expect, test } from "bun:test";
import { toastForProtocolRecord } from "./web-toasts";

describe("web protocol toasts", () => {
	test("formats compaction outcomes", () => {
		expect(
			toastForProtocolRecord({
				type: "session.compaction.completed.auto",
				contextPercent: 91,
				compactedTurnCount: 4,
				keptTurnCount: 2,
			}),
		).toMatchObject({
			title: "Session compacted",
			description:
				"Context reached 91%; compacted 4 turns into 1 summary turn. Kept 2 recent turns unchanged.",
			variant: "info",
		});
		expect(
			toastForProtocolRecord({
				type: "session.compaction.failed.adaptation",
				cause: "cannot-fit",
				error: "The session exceeds the model context window.",
			}),
		).toMatchObject({
			title: "Model too small for session",
			variant: "error",
		});
	});

	test("formats remote plugin toasts", () => {
		expect(
			toastForProtocolRecord({
				type: "ui.toast.requested",
				toast: {
					title: "UI demo complete",
					subtitle: "project · info",
					variant: "warning",
					persistent: true,
				},
			}),
		).toEqual({
			title: "UI demo complete",
			description: "project · info",
			variant: "warning",
			duration: 0,
		});
	});

	test("formats promoted follow-ups", () => {
		expect(
			toastForProtocolRecord({ type: "chat.followups.promoted", count: 2 }),
		).toMatchObject({
			title: "Steering",
			description: "Promoted 2 queued follow-ups into steering.",
			variant: "info",
		});
	});

	test("formats run failures and suppresses cancelled retries", () => {
		expect(
			toastForProtocolRecord({
				type: "agent.run.failed",
				error: "Provider unavailable",
			}),
		).toMatchObject({
			title: "Agent run failed",
			description: "Provider unavailable",
			variant: "error",
		});
		expect(
			toastForProtocolRecord({
				type: "agent.retry.failed",
				error: "Retry cancelled before continue.",
			}),
		).toBeNull();
	});

	test("uses safe copy for malformed records", () => {
		expect(
			toastForProtocolRecord({ type: "session.compaction.completed.manual" }),
		).toMatchObject({
			description: "Session context was compacted.",
		});
		expect(toastForProtocolRecord({ type: "agent.turn.started" })).toBeNull();
		expect(toastForProtocolRecord(null)).toBeNull();
	});
});
