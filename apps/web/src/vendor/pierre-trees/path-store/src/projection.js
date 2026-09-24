import { ensureChildPositions, getVisibleChildPrefixCount, selectChildIndexByVisibleIndex } from "./child-index.js";
import { isDirectoryNode } from "./internal-types.js";
import { setBenchmarkCounter, withBenchmarkPhase } from "./internal/benchmarkInstrumentation.js";
import { getSegmentValue } from "./segments.js";
import { getDirectoryLoadState, isDirectoryExpanded, setDirectoryExpanded } from "./state.js";
import { createCollapseEvent, createExpandEvent } from "./events.js";
import { collectFlattenedDirectoryChainIds, getFlattenedChildDirectoryId, getFlattenedTerminalDirectoryId } from "./flatten.js";
import { collectAncestorIds, findNodeId, getDirectoryIndex, materializeNodePath, recomputeCountsUpwardFrom, requireNode } from "./canonical.js";

//#region ../path-store/src/projection.ts
const INITIAL_PROJECTION_DEPTH_CAPACITY = 64;
function ensureProjectionDepthCapacity(depthTable, depth) {
	const requiredLength = depth + 2;
	if (requiredLength <= depthTable.length) return depthTable;
	let nextLength = depthTable.length;
	while (nextLength < requiredLength) nextLength *= 2;
	const nextDepthTable = new Int32Array(nextLength);
	nextDepthTable.fill(-1);
	nextDepthTable.set(depthTable);
	return nextDepthTable;
}
function getVisibleCount(state) {
	return requireNode(state, state.snapshot.rootId).visibleSubtreeCount;
}
function getVisibleRowSubtreeEndIndex(state, cursor, index, totalVisibleCount) {
	const terminalNode = requireNode(state, cursor.terminalNodeId);
	const subtreeSize = Math.max(1, terminalNode.visibleSubtreeCount);
	return Math.min(totalVisibleCount - 1, index + subtreeSize - 1);
}
function materializeVisibleAncestorRow(state, entry, totalVisibleCount, ancestorPaths) {
	return {
		ancestorPaths,
		index: entry.index,
		posInSet: entry.posInSet,
		row: materializeVisibleRow(state, entry.cursor),
		setSize: entry.setSize,
		subtreeEndIndex: getVisibleRowSubtreeEndIndex(state, entry.cursor, entry.index, totalVisibleCount)
	};
}
function selectVisibleRowContextWithinDirectory(state, directoryNodeId, index, directoryStartIndex, parentVisibleDepth, ancestors) {
	const directoryIndex = getDirectoryIndex(state, directoryNodeId);
	const { childIndex, childVisibleIndex, localVisibleIndex } = selectChildIndexByVisibleIndex(state.snapshot.nodes, directoryIndex, index);
	const childId = directoryIndex.childIds[childIndex];
	if (childId == null) throw new Error(`Visible index ${String(index)} is out of range`);
	return selectVisibleRowContextWithinSubtree(state, childId, localVisibleIndex, directoryStartIndex + childVisibleIndex, parentVisibleDepth + 1, childIndex, directoryIndex.childIds.length, ancestors);
}
function selectVisibleRowContextWithinSubtree(state, nodeId, index, rowIndex, visibleDepth, posInSet, setSize, ancestors) {
	if (!isDirectoryNode(requireNode(state, nodeId))) {
		if (index === 0) return {
			ancestors,
			cursor: {
				headNodeId: nodeId,
				terminalNodeId: nodeId,
				visibleDepth
			},
			index: rowIndex,
			posInSet,
			setSize
		};
		throw new Error(`Visible index ${String(index)} is out of range for file`);
	}
	const currentCursor = createVisibleRowCursor(state, nodeId, visibleDepth);
	if (index === 0) return {
		ancestors,
		cursor: currentCursor,
		index: rowIndex,
		posInSet,
		setSize
	};
	const terminalNode = requireNode(state, currentCursor.terminalNodeId);
	if (!isDirectoryNode(terminalNode) || !isDirectoryExpanded(state, currentCursor.terminalNodeId, terminalNode)) throw new Error(`Visible index ${String(index)} is out of range for collapsed directory`);
	return selectVisibleRowContextWithinDirectory(state, currentCursor.terminalNodeId, index - 1, rowIndex + 1, currentCursor.visibleDepth, [...ancestors, {
		cursor: currentCursor,
		index: rowIndex,
		posInSet,
		setSize
	}]);
}
function getVisibleRowContext(state, index) {
	const totalVisibleCount = getVisibleCount(state);
	if (index < 0 || index >= totalVisibleCount) return null;
	const selected = selectVisibleRowContextWithinDirectory(state, state.snapshot.rootId, index, 0, -1, []);
	const ancestorPaths = selected.ancestors.map((ancestor) => materializeNodePath(state, ancestor.cursor.terminalNodeId));
	let cachedAncestorRows = null;
	return {
		ancestorPaths,
		get ancestorRows() {
			if (cachedAncestorRows != null) return cachedAncestorRows;
			const ancestorRows = [];
			const rowAncestorPaths = [];
			for (const ancestor of selected.ancestors) {
				const ancestorRow = materializeVisibleAncestorRow(state, ancestor, totalVisibleCount, [...rowAncestorPaths]);
				ancestorRows.push(ancestorRow);
				rowAncestorPaths.push(ancestorRow.row.path);
			}
			cachedAncestorRows = ancestorRows;
			return cachedAncestorRows;
		},
		index: selected.index,
		posInSet: selected.posInSet,
		row: materializeVisibleRow(state, selected.cursor),
		setSize: selected.setSize,
		subtreeEndIndex: getVisibleRowSubtreeEndIndex(state, selected.cursor, selected.index, totalVisibleCount)
	};
}
function getVisibleSlice(state, start, end) {
	const instrumentation = state.instrumentation;
	const totalVisibleCount = getVisibleCount(state);
	if (totalVisibleCount <= 0 || end < start) return [];
	const normalizedStart = Math.max(0, Math.min(start, totalVisibleCount - 1));
	const normalizedEnd = Math.max(normalizedStart, Math.min(end, totalVisibleCount - 1));
	if (instrumentation == null) {
		if (normalizedStart === 0) return collectVisibleRowsDFS(state, normalizedEnd + 1);
		const rows$1 = [];
		let currentCursor$1 = selectVisibleRow(state, normalizedStart);
		for (let visibleIndex = normalizedStart; visibleIndex <= normalizedEnd && currentCursor$1 != null; visibleIndex++) {
			const row = materializeVisibleRow(state, currentCursor$1);
			rows$1.push(row);
			currentCursor$1 = getNextVisibleRowCursor(state, currentCursor$1);
		}
		return rows$1;
	}
	const rows = [];
	let flattenedRowCount = 0;
	let flattenedSegmentCount = 0;
	let currentCursor = withBenchmarkPhase(instrumentation, "store.getVisibleSlice.selectFirstRow", () => selectVisibleRow(state, normalizedStart));
	for (let visibleIndex = normalizedStart; visibleIndex <= normalizedEnd && currentCursor != null; visibleIndex++) {
		const row = withBenchmarkPhase(instrumentation, "store.getVisibleSlice.materializeRow", () => materializeVisibleRow(state, currentCursor));
		rows.push(row);
		if (row.isFlattened) {
			flattenedRowCount++;
			flattenedSegmentCount += row.flattenedSegments?.length ?? 0;
		}
		currentCursor = withBenchmarkPhase(instrumentation, "store.getVisibleSlice.advanceCursor", () => getNextVisibleRowCursor(state, currentCursor));
	}
	setBenchmarkCounter(instrumentation, "workload.visibleRowsRead", rows.length);
	setBenchmarkCounter(instrumentation, "workload.flattenedRowsRead", flattenedRowCount);
	setBenchmarkCounter(instrumentation, "workload.flattenedSegmentsRead", flattenedSegmentCount);
	return rows;
}
function getVisibleTreeProjectionData(state, maxRows = getVisibleCount(state)) {
	const instrumentation = state.instrumentation;
	if (instrumentation == null) return buildVisibleTreeProjectionDataDFS(state, maxRows);
	return withBenchmarkPhase(instrumentation, "store.getVisibleTreeProjection", () => buildVisibleTreeProjectionDataDFS(state, maxRows));
}
function getVisibleTreeProjection(state) {
	return createVisibleTreeProjectionFromData(getVisibleTreeProjectionData(state));
}
function getVisibleIndexByPath(state, path) {
	const nodeId = findNodeId(state, path);
	if (nodeId == null || nodeId === state.snapshot.rootId) return null;
	if (isDirectoryNode(requireNode(state, nodeId)) && getFlattenedTerminalDirectoryId(state, nodeId) !== nodeId) return null;
	let visibleIndex = 0;
	let currentNodeId = nodeId;
	const { nodes, rootId } = state.snapshot;
	while (currentNodeId !== rootId) {
		const parentId = requireNode(state, currentNodeId).parentId;
		const parentIndex = getDirectoryIndex(state, parentId);
		const childPosition = ensureChildPositions(parentIndex).get(currentNodeId);
		if (childPosition == null) throw new Error(`Child ${String(currentNodeId)} was not found in its parent index`);
		visibleIndex += getVisibleChildPrefixCount(nodes, parentIndex, childPosition);
		if (parentId !== rootId) {
			const parentNode = requireNode(state, parentId);
			const flattenedChildDirectoryId = getFlattenedChildDirectoryId(state, parentId);
			if (!isDirectoryExpanded(state, parentId, parentNode) && flattenedChildDirectoryId !== currentNodeId) return null;
			if (getFlattenedTerminalDirectoryId(state, parentId) === parentId) visibleIndex += 1;
		}
		currentNodeId = parentId;
	}
	return visibleIndex;
}
function expandPath(state, path) {
	const directoryNodeId = findNodeId(state, path);
	if (directoryNodeId == null) throw new Error(`Path does not exist: "${path}"`);
	const directoryNode = requireNode(state, directoryNodeId);
	if (!isDirectoryNode(directoryNode)) throw new Error(`Path is not a directory: "${path}"`);
	if (isDirectoryExpanded(state, directoryNodeId, directoryNode)) return null;
	setDirectoryExpanded(state, directoryNodeId, true, directoryNode);
	recomputeCountsUpwardFrom(state, directoryNodeId);
	return createExpandEvent({
		affectedAncestorIds: collectAncestorIds(state, directoryNodeId),
		affectedNodeIds: [directoryNodeId],
		path,
		projectionChanged: true
	});
}
function collapsePath(state, path) {
	const directoryNodeId = findNodeId(state, path);
	if (directoryNodeId == null) throw new Error(`Path does not exist: "${path}"`);
	const directoryNode = requireNode(state, directoryNodeId);
	if (!isDirectoryNode(directoryNode)) throw new Error(`Path is not a directory: "${path}"`);
	if (!isDirectoryExpanded(state, directoryNodeId, directoryNode)) return null;
	setDirectoryExpanded(state, directoryNodeId, false, directoryNode);
	recomputeCountsUpwardFrom(state, directoryNodeId);
	return createCollapseEvent({
		affectedAncestorIds: collectAncestorIds(state, directoryNodeId),
		affectedNodeIds: [directoryNodeId],
		path,
		projectionChanged: true
	});
}
function selectVisibleRow(state, index) {
	if (index < 0 || index >= getVisibleCount(state)) return null;
	return selectVisibleRowWithinDirectory(state, state.snapshot.rootId, index, -1);
}
function selectVisibleRowWithinDirectory(state, directoryNodeId, index, parentVisibleDepth) {
	const directoryIndex = getDirectoryIndex(state, directoryNodeId);
	const instrumentation = state.instrumentation;
	const { childIndex, localVisibleIndex } = instrumentation == null ? selectChildIndexByVisibleIndex(state.snapshot.nodes, directoryIndex, index) : withBenchmarkPhase(instrumentation, "store.getVisibleSlice.selectChildIndex", () => selectChildIndexByVisibleIndex(state.snapshot.nodes, directoryIndex, index));
	const childId = directoryIndex.childIds[childIndex];
	if (childId != null) return selectVisibleRowWithinSubtree(state, childId, localVisibleIndex, parentVisibleDepth + 1);
	throw new Error(`Visible index ${String(index)} is out of range`);
}
function selectVisibleRowWithinSubtree(state, nodeId, index, visibleDepth) {
	if (!isDirectoryNode(requireNode(state, nodeId))) {
		if (index === 0) return {
			headNodeId: nodeId,
			terminalNodeId: nodeId,
			visibleDepth
		};
		throw new Error(`Visible index ${String(index)} is out of range for file`);
	}
	const currentCursor = createVisibleRowCursor(state, nodeId, visibleDepth);
	if (index === 0) return currentCursor;
	const terminalNode = requireNode(state, currentCursor.terminalNodeId);
	if (!isDirectoryNode(terminalNode) || !isDirectoryExpanded(state, currentCursor.terminalNodeId, terminalNode)) throw new Error(`Visible index ${String(index)} is out of range for collapsed directory`);
	return selectVisibleRowWithinDirectory(state, currentCursor.terminalNodeId, index - 1, currentCursor.visibleDepth);
}
function createVisibleRowCursor(state, nodeId, visibleDepth) {
	if (!isDirectoryNode(requireNode(state, nodeId))) return {
		headNodeId: nodeId,
		terminalNodeId: nodeId,
		visibleDepth
	};
	if (state.instrumentation == null) return {
		headNodeId: nodeId,
		terminalNodeId: getFlattenedTerminalDirectoryId(state, nodeId),
		visibleDepth
	};
	return {
		headNodeId: nodeId,
		terminalNodeId: withBenchmarkPhase(state.instrumentation, "store.getVisibleSlice.flatten.resolveTerminalDirectory", () => getFlattenedTerminalDirectoryId(state, nodeId)),
		visibleDepth
	};
}
function isVisibleRowHeadNode(state, nodeId) {
	const node = requireNode(state, nodeId);
	if (!isDirectoryNode(node)) return true;
	const parentId = node.parentId;
	if (parentId === state.snapshot.rootId) return true;
	return getFlattenedChildDirectoryId(state, parentId) !== nodeId;
}
function getNextVisibleRowCursor(state, currentCursor) {
	const terminalNode = requireNode(state, currentCursor.terminalNodeId);
	if (isDirectoryNode(terminalNode)) {
		const currentIndex = getDirectoryIndex(state, currentCursor.terminalNodeId);
		if (isDirectoryExpanded(state, currentCursor.terminalNodeId, terminalNode) && currentIndex.childIds.length > 0) {
			const firstChildId = currentIndex.childIds[0];
			return firstChildId == null ? null : selectVisibleRowWithinSubtree(state, firstChildId, 0, currentCursor.visibleDepth + 1);
		}
	}
	let currentNodeId = currentCursor.terminalNodeId;
	let currentVisibleDepth = currentCursor.visibleDepth;
	while (true) {
		const currentNode = requireNode(state, currentNodeId);
		if (currentNodeId === state.snapshot.rootId) return null;
		const parentId = currentNode.parentId;
		const parentIndex = getDirectoryIndex(state, parentId);
		const siblingIndex = ensureChildPositions(parentIndex).get(currentNodeId) ?? -1;
		if (siblingIndex < 0) throw new Error(`Child ${String(currentNodeId)} was not found in its parent index`);
		const nextSiblingId = parentIndex.childIds[siblingIndex + 1] ?? null;
		if (nextSiblingId != null) return selectVisibleRowWithinSubtree(state, nextSiblingId, 0, currentVisibleDepth);
		if (isVisibleRowHeadNode(state, currentNodeId)) currentVisibleDepth--;
		currentNodeId = parentId;
	}
}
function createVisibleTreeProjectionFromData(projection) {
	const rowCount = projection.paths.length;
	const projectionRows = new Array(rowCount);
	for (let rowIndex = 0; rowIndex < rowCount; rowIndex += 1) {
		const parentIndex = projection.getParentIndex(rowIndex);
		projectionRows[rowIndex] = {
			index: rowIndex,
			parentPath: parentIndex >= 0 ? projection.paths[parentIndex] ?? null : null,
			path: projection.paths[rowIndex] ?? "",
			posInSet: projection.posInSetByIndex[rowIndex] ?? 0,
			setSize: projection.setSizeByIndex[rowIndex] ?? 0
		};
	}
	return {
		getParentIndex: projection.getParentIndex,
		rows: projectionRows,
		get visibleIndexByPath() {
			return projection.visibleIndexByPath;
		}
	};
}
function buildVisibleTreeProjectionDataDFS(state, maxRows) {
	const paths = new Array(maxRows);
	const parentRowIndex = new Int32Array(maxRows);
	const posInSetByIndex = new Int32Array(maxRows);
	const setSizeByIndex = new Int32Array(maxRows);
	let lastRowAtDepth = new Int32Array(INITIAL_PROJECTION_DEPTH_CAPACITY);
	lastRowAtDepth.fill(-1);
	let rowCount = 0;
	const { nodes, directories, segmentTable } = state.snapshot;
	const stack = [[
		directories.get(state.snapshot.rootId),
		0,
		-1,
		""
	]];
	const flattenEnabled = state.snapshot.options.flattenEmptyDirectories;
	const pathCacheByNodeId = state.pathCacheByNodeId;
	const pathCacheVersion = state.pathCacheVersion;
	const segmentValues = segmentTable.valueById;
	while (stack.length > 0 && rowCount < maxRows) {
		const frame = stack[stack.length - 1];
		const dirIndex = frame[0];
		if (frame[1] >= dirIndex.childIds.length) {
			stack.pop();
			continue;
		}
		const childOffset = frame[1];
		const childId = dirIndex.childIds[frame[1]++];
		const childNode = nodes[childId];
		const visibleDepth = frame[2] + 1;
		const parentPath = frame[3];
		lastRowAtDepth = ensureProjectionDepthCapacity(lastRowAtDepth, visibleDepth);
		let path;
		let terminalNodeId = childId;
		if (!isDirectoryNode(childNode)) {
			const cachedPathEntry = pathCacheByNodeId.get(childId);
			path = cachedPathEntry != null && cachedPathEntry.version === pathCacheVersion ? cachedPathEntry.path : `${parentPath}${segmentValues[childNode.nameId]}`;
		} else {
			terminalNodeId = flattenEnabled ? getFlattenedTerminalDirectoryId(state, childId) : childId;
			path = terminalNodeId === childId ? `${parentPath}${segmentValues[childNode.nameId]}/` : materializeNodePath(state, terminalNodeId);
		}
		parentRowIndex[rowCount] = lastRowAtDepth[visibleDepth];
		paths[rowCount] = path;
		posInSetByIndex[rowCount] = childOffset;
		setSizeByIndex[rowCount] = dirIndex.childIds.length;
		lastRowAtDepth[visibleDepth + 1] = rowCount;
		rowCount += 1;
		const terminalNode = nodes[terminalNodeId];
		if (terminalNode != null && isDirectoryNode(terminalNode) && isDirectoryExpanded(state, terminalNodeId, terminalNode)) stack.push([
			directories.get(terminalNodeId),
			0,
			visibleDepth,
			path
		]);
	}
	if (rowCount < maxRows) paths.length = rowCount;
	const finalParentRowIndex = parentRowIndex.subarray(0, rowCount);
	const finalPosInSetByIndex = posInSetByIndex.subarray(0, rowCount);
	const finalSetSizeByIndex = setSizeByIndex.subarray(0, rowCount);
	let cachedVisibleIndexByPath = null;
	return {
		getParentIndex(index) {
			return index < 0 || index >= rowCount ? -1 : finalParentRowIndex[index] ?? -1;
		},
		paths,
		posInSetByIndex: finalPosInSetByIndex,
		setSizeByIndex: finalSetSizeByIndex,
		get visibleIndexByPath() {
			if (cachedVisibleIndexByPath == null) {
				cachedVisibleIndexByPath = /* @__PURE__ */ new Map();
				for (let rowIndex = 0; rowIndex < rowCount; rowIndex += 1) cachedVisibleIndexByPath.set(paths[rowIndex] ?? "", rowIndex);
			}
			return cachedVisibleIndexByPath;
		}
	};
}
function collectVisibleRowsDFS(state, maxRows) {
	const rows = new Array(maxRows);
	let rowCount = 0;
	const { nodes, directories, segmentTable } = state.snapshot;
	const stack = [[
		directories.get(state.snapshot.rootId),
		0,
		-1
	]];
	const segmentValues = segmentTable.valueById;
	const flattenEnabled = state.snapshot.options.flattenEmptyDirectories;
	const pathCacheByNodeId = state.pathCacheByNodeId;
	const pathCacheVersion = state.pathCacheVersion;
	while (stack.length > 0 && rowCount < maxRows) {
		const frame = stack[stack.length - 1];
		const dirIndex = frame[0];
		if (frame[1] >= dirIndex.childIds.length) {
			stack.pop();
			continue;
		}
		const childId = dirIndex.childIds[frame[1]++];
		const childNode = nodes[childId];
		const visibleDepth = frame[2] + 1;
		if (!isDirectoryNode(childNode)) {
			const cachedPathEntry = pathCacheByNodeId.get(childId);
			rows[rowCount++] = {
				depth: visibleDepth,
				flattenedSegments: void 0,
				hasChildren: false,
				id: childId,
				isExpanded: false,
				isFlattened: false,
				isLoading: false,
				kind: "file",
				loadState: void 0,
				name: segmentValues[childNode.nameId],
				path: cachedPathEntry != null && cachedPathEntry.version === pathCacheVersion ? cachedPathEntry.path : materializeNodePath(state, childId)
			};
			continue;
		}
		const terminalNodeId = flattenEnabled ? getFlattenedTerminalDirectoryId(state, childId) : childId;
		const cursor = {
			headNodeId: childId,
			terminalNodeId,
			visibleDepth
		};
		rows[rowCount++] = materializeVisibleRow(state, cursor);
		const terminalNode = nodes[terminalNodeId];
		if (terminalNode != null && isDirectoryNode(terminalNode) && isDirectoryExpanded(state, terminalNodeId, terminalNode)) stack.push([
			directories.get(terminalNodeId),
			0,
			visibleDepth
		]);
	}
	if (rowCount < maxRows) rows.length = rowCount;
	return rows;
}
function materializeVisibleRow(state, cursor) {
	const terminalNode = requireNode(state, cursor.terminalNodeId);
	const loadState = isDirectoryNode(terminalNode) ? getVisibleRowLoadState(state, cursor) : null;
	const path = materializeNodePath(state, cursor.terminalNodeId);
	const name = getSegmentValue(state.snapshot.segmentTable, terminalNode.nameId);
	const hasChildren = isDirectoryNode(terminalNode) && getDirectoryIndex(state, cursor.terminalNodeId).childIds.length > 0;
	const isFlattened = cursor.headNodeId !== cursor.terminalNodeId;
	const instrumentation = state.instrumentation;
	const flattenedSegments = isFlattened ? instrumentation == null ? collectFlattenedDirectoryChainIds(state, cursor.headNodeId).map((nodeId) => {
		const node = requireNode(state, nodeId);
		return {
			isTerminal: nodeId === cursor.terminalNodeId,
			name: getSegmentValue(state.snapshot.segmentTable, node.nameId),
			nodeId,
			path: materializeNodePath(state, nodeId)
		};
	}) : withBenchmarkPhase(instrumentation, "store.getVisibleSlice.flatten.collectSegments", () => collectFlattenedDirectoryChainIds(state, cursor.headNodeId).map((nodeId) => {
		const node = requireNode(state, nodeId);
		return {
			isTerminal: nodeId === cursor.terminalNodeId,
			name: getSegmentValue(state.snapshot.segmentTable, node.nameId),
			nodeId,
			path: materializeNodePath(state, nodeId)
		};
	})) : void 0;
	return {
		depth: cursor.visibleDepth,
		flattenedSegments,
		hasChildren,
		id: cursor.terminalNodeId,
		isExpanded: isDirectoryNode(terminalNode) && isDirectoryExpanded(state, cursor.terminalNodeId, terminalNode),
		isFlattened,
		isLoading: loadState === "loading",
		kind: isDirectoryNode(terminalNode) ? "directory" : "file",
		loadState: loadState == null || loadState === "loaded" ? void 0 : loadState,
		name,
		path
	};
}
function getVisibleRowLoadState(state, cursor) {
	if (cursor.headNodeId === cursor.terminalNodeId) return getDirectoryLoadState(state, cursor.terminalNodeId);
	const chainNodeIds = collectFlattenedDirectoryChainIds(state, cursor.headNodeId);
	let hasUnloaded = false;
	let hasError = false;
	for (const nodeId of chainNodeIds) {
		const loadState = getDirectoryLoadState(state, nodeId);
		if (loadState === "loading") return "loading";
		if (loadState === "error") {
			hasError = true;
			continue;
		}
		if (loadState === "unloaded") hasUnloaded = true;
	}
	if (hasError) return "error";
	if (hasUnloaded) return "unloaded";
	return "loaded";
}

//#endregion
export { collapsePath, expandPath, getVisibleCount, getVisibleIndexByPath, getVisibleRowContext, getVisibleSlice, getVisibleTreeProjection, getVisibleTreeProjectionData };
//# sourceMappingURL=projection.js.map