import { describe, expect, mock, test } from "bun:test";
import { createPullRequestOpenHandler } from "./plugin";

describe("VCS status contribution", () => {
	test("opens the current pull request when its footer contribution is clicked", async () => {
		const open = mock(async (_url: string) => {});
		const onClick = createPullRequestOpenHandler(
			{ url: "https://github.com/akonwi/kit/pull/25" },
			open,
		);

		expect(onClick).toBeDefined();
		await onClick?.();
		expect(open).toHaveBeenCalledWith("https://github.com/akonwi/kit/pull/25");
	});

	test("is not clickable without a pull request URL", () => {
		const open = mock(async (_url: string) => {});

		expect(createPullRequestOpenHandler(null, open)).toBeUndefined();
		expect(createPullRequestOpenHandler({ url: "" }, open)).toBeUndefined();
	});
});
