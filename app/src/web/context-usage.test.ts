import { describe, expect, test } from "bun:test";
import {
	clampContextPercent,
	contextProgressTone,
	formatContextUsage,
	parseRemoteContextUsage,
} from "./context-usage";

describe("remote context usage", () => {
	test("parses bounded protocol statistics", () => {
		expect(
			parseRemoteContextUsage({
				tokens: 162_000,
				contextWindow: 200_000,
				percent: 81,
				ignored: true,
			}),
		).toEqual({ tokens: 162_000, contextWindow: 200_000, percent: 81 });
		expect(parseRemoteContextUsage(null)).toBeNull();
		expect(
			parseRemoteContextUsage({
				tokens: -1,
				contextWindow: 0,
				percent: Number.NaN,
			}),
		).toBeNull();
	});

	test("matches TUI progress thresholds and clamps only the rendered width", () => {
		expect(contextProgressTone(79)).toBe("normal");
		expect(contextProgressTone(80)).toBe("warning");
		expect(contextProgressTone(90)).toBe("warning");
		expect(contextProgressTone(91)).toBe("critical");
		expect(clampContextPercent(125)).toBe(100);
		expect(
			formatContextUsage({
				tokens: 162_000,
				contextWindow: 200_000,
				percent: 81,
			}),
		).toBe("162,000 / 200,000 tokens (81%)");
	});
});
