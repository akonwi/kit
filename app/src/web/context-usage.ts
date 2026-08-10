export type RemoteContextUsage = {
	tokens: number;
	contextWindow: number;
	percent: number;
};

function isNonnegativeSafeInteger(value: unknown): value is number {
	return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

export function parseRemoteContextUsage(
	value: unknown,
): RemoteContextUsage | null {
	if (typeof value !== "object" || value === null || Array.isArray(value)) {
		return null;
	}
	const record = value as Record<string, unknown>;
	if (
		!isNonnegativeSafeInteger(record.tokens) ||
		!isNonnegativeSafeInteger(record.contextWindow) ||
		record.contextWindow === 0 ||
		!isNonnegativeSafeInteger(record.percent)
	) {
		return null;
	}
	return {
		tokens: record.tokens,
		contextWindow: record.contextWindow,
		percent: record.percent,
	};
}

export function contextProgressTone(
	percent: number,
): "normal" | "warning" | "critical" {
	if (percent > 90) return "critical";
	if (percent >= 80) return "warning";
	return "normal";
}

export function clampContextPercent(percent: number): number {
	return Math.max(0, Math.min(100, percent));
}

export function formatContextUsage(usage: RemoteContextUsage): string {
	return `${usage.tokens.toLocaleString()} / ${usage.contextWindow.toLocaleString()} tokens (${usage.percent}%)`;
}
