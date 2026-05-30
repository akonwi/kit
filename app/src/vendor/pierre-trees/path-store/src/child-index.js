//#region ../path-store/src/child-index.ts
const PATH_STORE_CHILD_INDEX_CHUNK_SHIFT = 5;
const PATH_STORE_CHILD_INDEX_CHUNK_SIZE = 1 << PATH_STORE_CHILD_INDEX_CHUNK_SHIFT;
const PATH_STORE_CHILD_INDEX_CHUNK_THRESHOLD = PATH_STORE_CHILD_INDEX_CHUNK_SIZE * 4;
const PATH_STORE_CHILD_INDEX_CHUNK_THRESHOLD_EXTERNAL = PATH_STORE_CHILD_INDEX_CHUNK_THRESHOLD;
function createDirectoryChildIndex() {
	return {
		childIdByNameId: /* @__PURE__ */ new Map(),
		childIds: [],
		childPositionById: /* @__PURE__ */ new Map(),
		childVisibleChunkSums: null,
		totalChildSubtreeNodeCount: 0,
		totalChildVisibleSubtreeCount: 0
	};
}
function createPresortedDirectoryChildIndex() {
	return {
		childIdByNameId: null,
		childIds: [],
		childPositionById: null,
		childVisibleChunkSums: null,
		totalChildSubtreeNodeCount: 0,
		totalChildVisibleSubtreeCount: 0
	};
}
function ensureChildIdByNameId(nodes, index) {
	if (index.childIdByNameId != null) return index.childIdByNameId;
	const map = /* @__PURE__ */ new Map();
	for (const childId of index.childIds) {
		const childNode = nodes[childId];
		if (childNode != null) map.set(childNode.nameId, childId);
	}
	index.childIdByNameId = map;
	return map;
}
function ensureChildPositions(index) {
	if (index.childPositionById != null) return index.childPositionById;
	const positions = /* @__PURE__ */ new Map();
	for (let i = 0; i < index.childIds.length; i++) {
		const childId = index.childIds[i];
		if (childId != null) positions.set(childId, i);
	}
	index.childPositionById = positions;
	return positions;
}
function appendChildReference(index, childId) {
	if (index.childPositionById != null) index.childPositionById.set(childId, index.childIds.length);
	index.childIds.push(childId);
}
function updateChildPositionsFrom(index, startIndex) {
	if (index.childPositionById == null) return;
	for (let position = startIndex; position < index.childIds.length; position++) {
		const childId = index.childIds[position];
		if (childId != null) index.childPositionById.set(childId, position);
	}
}
function rebuildDirectoryChildAggregates(nodes, index) {
	let totalChildSubtreeNodeCount = 0;
	let totalChildVisibleSubtreeCount = 0;
	for (const childId of index.childIds) {
		const childNode = nodes[childId];
		if (childNode == null) continue;
		totalChildSubtreeNodeCount += childNode.subtreeNodeCount;
		totalChildVisibleSubtreeCount += childNode.visibleSubtreeCount;
	}
	index.totalChildSubtreeNodeCount = totalChildSubtreeNodeCount;
	index.totalChildVisibleSubtreeCount = totalChildVisibleSubtreeCount;
	rebuildVisibleChildChunks(nodes, index);
}
function applyChildAggregateDelta(index, childId, subtreeNodeDelta, visibleSubtreeDelta) {
	index.totalChildSubtreeNodeCount += subtreeNodeDelta;
	index.totalChildVisibleSubtreeCount += visibleSubtreeDelta;
	if (index.childVisibleChunkSums == null || visibleSubtreeDelta === 0) return;
	const childPosition = ensureChildPositions(index).get(childId);
	if (childPosition === void 0) return;
	const chunkIndex = childPosition >> PATH_STORE_CHILD_INDEX_CHUNK_SHIFT;
	index.childVisibleChunkSums[chunkIndex] += visibleSubtreeDelta;
}
function selectChildIndexByVisibleIndex(nodes, index, visibleIndex) {
	const chunkSums = index.childVisibleChunkSums;
	if (chunkSums != null) {
		let remainingIndex$1 = visibleIndex;
		let childIndex = 0;
		for (const chunkVisibleCount of chunkSums) {
			if (remainingIndex$1 < chunkVisibleCount) {
				const selected = selectChildIndexWithinChunk(nodes, index, childIndex, remainingIndex$1);
				return {
					...selected,
					childVisibleIndex: visibleIndex - selected.localVisibleIndex
				};
			}
			remainingIndex$1 -= chunkVisibleCount;
			childIndex += PATH_STORE_CHILD_INDEX_CHUNK_SIZE;
		}
		throw new Error(`Visible child index ${String(visibleIndex)} is out of range`);
	}
	let remainingIndex = visibleIndex;
	for (let childIndex = 0; childIndex < index.childIds.length; childIndex++) {
		const childId = index.childIds[childIndex];
		if (childId == null) continue;
		const childNode = nodes[childId];
		if (childNode == null) continue;
		if (remainingIndex < childNode.visibleSubtreeCount) return {
			childIndex,
			childVisibleIndex: visibleIndex - remainingIndex,
			localVisibleIndex: remainingIndex
		};
		remainingIndex -= childNode.visibleSubtreeCount;
	}
	throw new Error(`Visible child index ${String(visibleIndex)} is out of range`);
}
function getVisibleChildPrefixCount(nodes, index, childPosition) {
	let visibleCount = 0;
	const chunkSums = index.childVisibleChunkSums;
	let scanStart = 0;
	if (chunkSums != null) {
		const chunkIndex = childPosition >> PATH_STORE_CHILD_INDEX_CHUNK_SHIFT;
		for (let chunkOffset = 0; chunkOffset < chunkIndex; chunkOffset += 1) visibleCount += chunkSums[chunkOffset] ?? 0;
		scanStart = chunkIndex << PATH_STORE_CHILD_INDEX_CHUNK_SHIFT;
	}
	for (let childIndex = scanStart; childIndex < childPosition; childIndex += 1) {
		const childId = index.childIds[childIndex];
		if (childId == null) continue;
		const childNode = nodes[childId];
		if (childNode == null) continue;
		visibleCount += childNode.visibleSubtreeCount;
	}
	return visibleCount;
}
function rebuildVisibleChildChunks(nodes, index) {
	if (index.childIds.length < PATH_STORE_CHILD_INDEX_CHUNK_THRESHOLD) {
		index.childVisibleChunkSums = null;
		return;
	}
	const chunkCount = Math.ceil(index.childIds.length / PATH_STORE_CHILD_INDEX_CHUNK_SIZE);
	const chunkSums = new Int32Array(chunkCount);
	for (let childIndex = 0; childIndex < index.childIds.length; childIndex++) {
		const childId = index.childIds[childIndex];
		if (childId == null) continue;
		const childNode = nodes[childId];
		if (childNode == null) continue;
		chunkSums[childIndex >> PATH_STORE_CHILD_INDEX_CHUNK_SHIFT] += childNode.visibleSubtreeCount;
	}
	index.childVisibleChunkSums = chunkSums;
}
function selectChildIndexWithinChunk(nodes, index, chunkStartIndex, visibleIndex) {
	const chunkEndIndex = Math.min(index.childIds.length, chunkStartIndex + PATH_STORE_CHILD_INDEX_CHUNK_SIZE);
	let remainingIndex = visibleIndex;
	for (let childIndex = chunkStartIndex; childIndex < chunkEndIndex; childIndex++) {
		const childId = index.childIds[childIndex];
		if (childId == null) continue;
		const childNode = nodes[childId];
		if (childNode == null) continue;
		if (remainingIndex < childNode.visibleSubtreeCount) return {
			childIndex,
			localVisibleIndex: remainingIndex
		};
		remainingIndex -= childNode.visibleSubtreeCount;
	}
	throw new Error(`Visible child index ${String(visibleIndex)} is out of range`);
}

//#endregion
export { PATH_STORE_CHILD_INDEX_CHUNK_THRESHOLD_EXTERNAL, appendChildReference, applyChildAggregateDelta, createDirectoryChildIndex, createPresortedDirectoryChildIndex, ensureChildIdByNameId, ensureChildPositions, getVisibleChildPrefixCount, rebuildDirectoryChildAggregates, rebuildVisibleChildChunks, selectChildIndexByVisibleIndex, updateChildPositionsFrom };
//# sourceMappingURL=child-index.js.map