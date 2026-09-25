import { describe, expect, test } from "bun:test";
import { inferFiletype } from "./filetype";

describe("inferFiletype", () => {
	test("recognizes Ard files", () => {
		expect(inferFiletype("main.ard")).toBe("ard");
		expect(inferFiletype("/tmp/MAIN.ARD")).toBe("ard");
	});
});
