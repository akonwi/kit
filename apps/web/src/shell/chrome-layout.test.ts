import { describe, expect, test } from "bun:test";
import type { ChromeContribution } from "./chrome-contributions";
import {
	chromeLayoutWidth,
	packChromeContributions,
	terminalTextWidth,
	transcriptContextProgressColumns,
	truncateEnd,
	truncateStart,
} from "./chrome-layout";

function contribution(
	id: string,
	text: string,
	side: "left" | "right" = "right",
): ChromeContribution {
	return {
		id,
		content: [{ text }],
		plainText: text,
		side,
	};
}

describe("shell chrome layout", () => {
	test("measures terminal cells rather than JavaScript string length", () => {
		expect(terminalTextWidth("界")).toBe(2);
		expect(terminalTextWidth("é")).toBe(1);
		expect(terminalTextWidth("👍")).toBe(2);
	});

	test("packs complete contributions and reserves a counted overflow item", () => {
		const privileged = [contribution("model", "model")];
		const standard = [
			contribution("one", "one"),
			contribution("two", "two"),
			contribution("three", "three"),
		];
		const width = chromeLayoutWidth([
			...privileged,
			standard[0],
			contribution("overflow", "… +2"),
		]);

		expect(
			packChromeContributions({
				availableWidth: width,
				privileged,
				standard,
			}),
		).toMatchObject({
			visible: [{ id: "one" }],
			hidden: [{ id: "two" }, { id: "three" }],
			overflowLabel: "… +2",
		});
	});

	test("keeps registration order instead of skipping a long contribution", () => {
		const standard = [
			contribution("long", "a very long contribution"),
			contribution("short", "ok"),
		];
		const packed = packChromeContributions({
			availableWidth: terminalTextWidth("… +2"),
			privileged: [],
			standard,
		});

		expect(packed.visible).toEqual([]);
		expect(packed.hidden.map((item) => item.id)).toEqual(["long", "short"]);
		expect(packed.overflowLabel).toBe("… +2");
	});

	test("limits context progress to the transcript boundary", () => {
		expect(
			transcriptContextProgressColumns({
				shellWidth: 120,
				transcriptWidth: 80,
				percent: 50,
			}),
		).toBe(40);
		expect(
			transcriptContextProgressColumns({
				shellWidth: 120,
				transcriptWidth: 80,
				percent: 100,
			}),
		).toBe(79);
	});

	test("truncates privileged labels at terminal-cell boundaries", () => {
		expect(truncateEnd("session name", 8)).toBe("session…");
		expect(terminalTextWidth(truncateEnd("界界界", 5))).toBeLessThanOrEqual(5);
		expect(truncateStart("/Users/example/project", 10)).toBe("…e/project");
	});
});
