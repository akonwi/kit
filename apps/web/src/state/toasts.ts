export type ToastVariant = "error" | "warning" | "info";

export type ToastInput = {
	variant: ToastVariant;
	title: string;
	subtitle?: string;
	persistent?: boolean;
};

export type Toast = ToastInput & {
	id: number;
};

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringField(record: Record<string, unknown>, key: string): string {
	const value = record[key];
	return typeof value === "string" && value ? value : "Unknown error.";
}

function countField(
	record: Record<string, unknown>,
	key: string,
): number | null {
	const value = record[key];
	return typeof value === "number" && Number.isSafeInteger(value) && value >= 0
		? value
		: null;
}

function compactedCounts(record: Record<string, unknown>): string | null {
	const compacted = countField(record, "compactedTurnCount");
	const kept = countField(record, "keptTurnCount");
	return compacted === null || kept === null
		? null
		: `${compacted} turns into 1 summary turn. Kept ${kept} recent turns unchanged.`;
}

export function toastForRuntimeRecord(record: unknown): ToastInput | null {
	if (!isRecord(record) || typeof record.type !== "string") return null;
	const counts = compactedCounts(record);
	switch (record.type) {
		case "chat.followups.promoted": {
			const count = countField(record, "count");
			return {
				title: "Steering",
				subtitle:
					count === null
						? "Promoted queued follow-ups into steering."
						: count === 1
							? "Promoted 1 queued follow-up into steering."
							: `Promoted ${count} queued follow-ups into steering.`,
				variant: "info",
			};
		}
		case "session.compaction.completed.auto": {
			const contextPercent = countField(record, "contextPercent");
			return {
				title: "Session compacted",
				subtitle:
					contextPercent === null || !counts
						? "Session context was compacted."
						: `Context reached ${contextPercent}%; compacted ${counts}`,
				variant: "info",
			};
		}
		case "session.compaction.completed.recovery":
			return {
				title: "Session compacted",
				subtitle: counts
					? `Recovered from a context overflow by compacting ${counts}`
					: "Recovered from a context overflow by compacting the session.",
				variant: "info",
			};
		case "session.compaction.completed.manual":
			return {
				title: "Session compacted",
				subtitle: counts
					? `Compacted ${counts}`
					: "Session context was compacted.",
				variant: "info",
			};
		case "session.compaction.failed.auto":
			return {
				title: "Auto-compaction failed",
				subtitle: stringField(record, "error"),
				variant: "error",
			};
		case "session.compaction.failed.recovery":
			return {
				title: "Context overflow recovery failed",
				subtitle: stringField(record, "error"),
				variant: "error",
			};
		case "session.compaction.failed.adaptation":
			return {
				title:
					record.cause === "compaction-error"
						? "Model switch compaction failed"
						: "Model too small for session",
				subtitle: `${stringField(record, "error")} Start a new session or hand off to continue with this model.`,
				variant: "error",
			};
		case "session.compaction.failed.manual":
			return {
				title: "Compaction failed",
				subtitle: stringField(record, "error"),
				variant: "error",
			};
		case "agent.retry.failed": {
			const error = stringField(record, "error");
			return error === "Retry cancelled before continue."
				? null
				: { title: "Retry failed", subtitle: error, variant: "error" };
		}
		case "agent.run.failed":
			return {
				title: "Agent run failed",
				subtitle: stringField(record, "error"),
				variant: "error",
			};
		case "error":
			return {
				title: "Session error",
				subtitle: stringField(record, "error"),
				variant: "error",
			};
		default:
			return null;
	}
}
