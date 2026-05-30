import { createSegmentSortKey } from "./sort.js";

//#region ../path-store/src/segments.ts
const ROOT_SEGMENT_VALUE = "";
function createSegmentTable() {
	const idByValue = /* @__PURE__ */ new Map();
	idByValue.set(ROOT_SEGMENT_VALUE, 0);
	return {
		idByValue,
		valueById: [ROOT_SEGMENT_VALUE],
		sortKeyById: [createSegmentSortKey(ROOT_SEGMENT_VALUE)]
	};
}
function internSegment(segmentTable, value) {
	const existingId = segmentTable.idByValue.get(value);
	if (existingId !== void 0) return existingId;
	const nextId = segmentTable.valueById.length;
	segmentTable.idByValue.set(value, nextId);
	segmentTable.valueById.push(value);
	return nextId;
}
function getSegmentValue(segmentTable, segmentId) {
	const value = segmentTable.valueById[segmentId];
	if (value === void 0) throw new Error(`Unknown segment ID: ${String(segmentId)}`);
	return value;
}

//#endregion
export { createSegmentTable, getSegmentValue, internSegment };
//# sourceMappingURL=segments.js.map