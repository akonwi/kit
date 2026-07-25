import { describe, expect, test } from "bun:test";
import { codeReviewCommand } from "./review";
import type { CommandContext } from "./types";

describe("code review command", () => {
	test("opens the persistent review workspace", async () => {
		let opens = 0;
		await codeReviewCommand.execute({
			reviewWorkspace: {
				open: () => {
					opens += 1;
				},
				subscribe: () => () => {},
			},
		} as unknown as CommandContext);

		expect(opens).toBe(1);
	});
});
