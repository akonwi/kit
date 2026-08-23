import { describe, expect, test } from "bun:test";
import { reviewDraftDetachAction } from "./draft-detach";

describe("review draft attachment detachment", () => {
	test("preserves pending projections while consuming or clearing explicit removals", () => {
		expect(reviewDraftDetachAction("pending")).toBe("preserve");
		expect(reviewDraftDetachAction("consumed")).toBe("consume");
		expect(reviewDraftDetachAction("removed")).toBe("clear");
	});
});
