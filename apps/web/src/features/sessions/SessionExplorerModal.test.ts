import { describe, expect, test } from "bun:test";
import {
	resolveSessionExplorerColumns,
	resolveSessionExplorerRows,
} from "./SessionExplorerModal";

describe("session explorer resources", () => {
	test("does not read a failed sessions resource", () => {
		let read = false;
		const rows = resolveSessionExplorerRows(
			() => {
				read = true;
				throw new Error("resource read escaped");
			},
			new Error("load failed"),
			"current",
		);

		expect(rows).toEqual([]);
		expect(read).toBe(false);
	});
});

describe("session explorer columns", () => {
	test("reveals metadata in user-value order", () => {
		expect(resolveSessionExplorerColumns(39)).toMatchObject({
			showUpdated: false,
			showCwd: false,
			showId: false,
		});
		expect(resolveSessionExplorerColumns(40)).toMatchObject({
			showUpdated: true,
			showCwd: false,
			showId: false,
		});
		expect(resolveSessionExplorerColumns(68)).toMatchObject({
			showUpdated: true,
			showCwd: true,
			showId: false,
		});
		expect(resolveSessionExplorerColumns(104)).toMatchObject({
			showUpdated: true,
			showCwd: true,
			showId: true,
		});
	});

	test("keeps a useful title column at every width", () => {
		for (const width of [1, 39, 40, 68, 80, 104, 120]) {
			expect(resolveSessionExplorerColumns(width).titleWidth).toBeGreaterThan(
				0,
			);
		}
	});
});
