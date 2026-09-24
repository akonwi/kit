import { appendChildReference, createDirectoryChildIndex, createPresortedDirectoryChildIndex, rebuildDirectoryChildAggregates } from "./child-index.js";
import { PATH_STORE_NODE_FLAG_EXPLICIT, PATH_STORE_NODE_FLAG_ROOT, PATH_STORE_NODE_KIND_DIRECTORY, addNodeFlag, createNodeDepthAndFlags, getNodeDepth, hasNodeFlag, isDirectoryNode } from "./internal-types.js";
import { getBenchmarkInstrumentation, setBenchmarkCounter, withBenchmarkPhase } from "./internal/benchmarkInstrumentation.js";
import { resolvePathStoreOptions } from "./options.js";
import { parseInputPath } from "./path.js";
import { comparePreparedPaths, comparePreparedPathsWithCachedSortKeys } from "./sort.js";
import { createSegmentTable, internSegment } from "./segments.js";

//#region ../path-store/src/builder.ts
const PREPARED_INPUT_KIND = Symbol("pathStorePreparedInputKind");
function attachPreparedInputKind(value, kind) {
	value[PREPARED_INPUT_KIND] = kind;
	return value;
}
function createCompareEntry(preparedPath) {
	return {
		basename: preparedPath.basename,
		depth: preparedPath.segments.length,
		isDirectory: preparedPath.isDirectory,
		path: preparedPath.path,
		segments: preparedPath.segments
	};
}
function compareWithSortOption(left, right, sort) {
	if (sort === "default") return comparePreparedPaths(left, right);
	return sort(createCompareEntry(left), createCompareEntry(right));
}
function createRootNode() {
	return {
		depthAndFlags: createNodeDepthAndFlags(0, PATH_STORE_NODE_FLAG_EXPLICIT | PATH_STORE_NODE_FLAG_ROOT, PATH_STORE_NODE_KIND_DIRECTORY),
		nameId: 0,
		parentId: 0,
		subtreeNodeCount: 1,
		visibleSubtreeCount: 1
	};
}
function computeSharedPrefixLength(left, right) {
	const maxLength = Math.min(left.length, right.length);
	for (let index = 0; index < maxLength; index++) if (left[index] !== right[index]) return index;
	return maxLength;
}
function getDirectoryDepth(preparedPath) {
	return preparedPath.isDirectory ? preparedPath.segments.length : preparedPath.segments.length - 1;
}
function isPreparedPathArray(value) {
	return Array.isArray(value) && value.every((entry) => entry != null && typeof entry === "object" && typeof entry.path === "string" && Array.isArray(entry.segments) && typeof entry.basename === "string" && typeof entry.isDirectory === "boolean");
}
function isStringArray(value) {
	return Array.isArray(value) && value.every((entry) => typeof entry === "string");
}
function preparePaths(paths, options = {}) {
	return preparePathEntries(paths, options).map((entry) => entry.path);
}
function prepareInput(paths, options = {}) {
	const preparedPaths = preparePathEntries(paths, options);
	return attachPreparedInputKind({
		paths: preparedPaths.map((entry) => entry.path),
		preparedPaths
	}, "prepared");
}
function preparePresortedInput(paths) {
	const pathCount = paths.length;
	let presortedPathsContainDirectories = false;
	for (let index = 0; index < pathCount; index += 1) {
		const path = paths[index];
		if (path.length > 0 && path.charCodeAt(path.length - 1) === 47) {
			presortedPathsContainDirectories = true;
			break;
		}
	}
	return attachPreparedInputKind({
		paths,
		presortedPaths: paths,
		presortedPathsContainDirectories
	}, "presorted");
}
function getPreparedInputEntries(preparedInput) {
	const internalPreparedInput = preparedInput;
	const preparedPaths = internalPreparedInput.preparedPaths;
	if (internalPreparedInput[PREPARED_INPUT_KIND] === "prepared" && preparedPaths != null) return preparedPaths;
	if (!isPreparedPathArray(preparedPaths)) throw new Error("preparedInput must come from PathStore.prepareInput()");
	return preparedPaths;
}
function getPreparedInputPresortedPaths(preparedInput) {
	const internalPreparedInput = preparedInput;
	if (internalPreparedInput[PREPARED_INPUT_KIND] === "presorted" && internalPreparedInput.presortedPaths != null) return internalPreparedInput.presortedPaths;
	return isStringArray(internalPreparedInput.presortedPaths) ? internalPreparedInput.presortedPaths : null;
}
function getPreparedInputPresortedPathsContainDirectories(preparedInput) {
	const internalPreparedInput = preparedInput;
	return typeof internalPreparedInput.presortedPathsContainDirectories === "boolean" ? internalPreparedInput.presortedPathsContainDirectories : null;
}
function preparePathEntries(paths, options = {}) {
	const resolvedOptions = resolvePathStoreOptions(options);
	const instrumentation = getBenchmarkInstrumentation(options);
	setBenchmarkCounter(instrumentation, "workload.inputFiles", paths.length);
	const preparedPaths = withBenchmarkPhase(instrumentation, "store.preparePathEntries.parse", () => paths.map((path) => parseInputPath(path)));
	withBenchmarkPhase(instrumentation, "store.preparePathEntries.sort", () => preparedPaths.sort((left, right) => compareWithSortOption(left, right, resolvedOptions.sort)));
	return preparedPaths;
}
var PathStoreBuilder = class {
	directories = /* @__PURE__ */ new Map();
	directoryStack = [0];
	presortedDirectoryNodeIds = [];
	initialExpandedPathSet;
	createdDirectoriesAllExpanded = false;
	createdDirectoryCount = 0;
	lastPreparedPath = null;
	nodes = [createRootNode()];
	options;
	instrumentation;
	segmentSortKeyCache = /* @__PURE__ */ new Map();
	segmentTable = createSegmentTable();
	hasDeferredDirectoryIndexes = false;
	constructor(options = {}) {
		this.instrumentation = getBenchmarkInstrumentation(options);
		this.options = resolvePathStoreOptions(options);
		const initialExpandedPaths = options.initialExpandedPaths ?? null;
		if (initialExpandedPaths == null || initialExpandedPaths.length === 0) this.initialExpandedPathSet = null;
		else {
			const normalizedPaths = /* @__PURE__ */ new Set();
			const hintCount = initialExpandedPaths.length;
			for (let index = 0; index < hintCount; index += 1) {
				const path = initialExpandedPaths[index];
				const length = path.length;
				normalizedPaths.add(length > 0 && path.charCodeAt(length - 1) === 47 ? path.slice(0, length - 1) : path);
			}
			this.initialExpandedPathSet = normalizedPaths;
			this.createdDirectoriesAllExpanded = true;
		}
		this.directories.set(0, createDirectoryChildIndex());
	}
	appendPaths(paths) {
		return withBenchmarkPhase(this.instrumentation, "store.builder.appendPaths.parse", () => this.appendPreparedPaths(paths.map((path) => parseInputPath(path))));
	}
	appendPreparedPaths(preparedPaths, validateOrder = true) {
		this.createdDirectoriesAllExpanded = false;
		withBenchmarkPhase(this.instrumentation, "store.builder.appendPreparedPaths", () => {
			for (const preparedPath of preparedPaths) this.appendPreparedPath(preparedPath, validateOrder);
		});
		return this;
	}
	appendPresortedPaths(paths, containsDirectories = null) {
		withBenchmarkPhase(this.instrumentation, "store.builder.appendPresortedPaths", () => {
			if (containsDirectories === false) {
				this.appendPresortedFilePaths(paths);
				return;
			}
			this.createdDirectoriesAllExpanded = false;
			let previousPath = null;
			let currentDepth = 0;
			const nodes = this.nodes;
			const segmentTable = this.segmentTable;
			const idByValue = segmentTable.idByValue;
			const valueById = segmentTable.valueById;
			const dirStack = this.directoryStack;
			let stackTop = 0;
			let cachedDirPrefix = "";
			let cachedDirDepth = 0;
			for (const path of paths) {
				if (previousPath === path) throw new Error(`Duplicate path: "${path}"`);
				const hasTrailingSlash = path.length > 0 && path.charCodeAt(path.length - 1) === 47;
				const endIndex = hasTrailingSlash ? path.length - 1 : path.length;
				let sharedDirectoryDepth = 0;
				let unsharedSegmentStart = 0;
				if (previousPath != null) if (cachedDirPrefix.length > 0 && path.length > cachedDirPrefix.length && path.startsWith(cachedDirPrefix)) {
					sharedDirectoryDepth = cachedDirDepth;
					unsharedSegmentStart = cachedDirPrefix.length;
				} else {
					const compareLength = Math.min(endIndex, previousPath.length);
					let prefixMatched = true;
					for (let ci = 0; ci < compareLength; ci++) {
						const cc = path.charCodeAt(ci);
						if (cc !== previousPath.charCodeAt(ci)) {
							prefixMatched = false;
							break;
						}
						if (cc === 47) {
							sharedDirectoryDepth++;
							unsharedSegmentStart = ci + 1;
						}
					}
					if (prefixMatched && hasTrailingSlash && compareLength === endIndex && previousPath.length > endIndex && previousPath.charCodeAt(endIndex) === 47) {
						sharedDirectoryDepth++;
						unsharedSegmentStart = endIndex + 1;
					}
				}
				stackTop = sharedDirectoryDepth;
				currentDepth = sharedDirectoryDepth;
				let segmentStart = unsharedSegmentStart;
				let slashPos = path.indexOf("/", segmentStart);
				while (slashPos >= 0 && slashPos < endIndex) {
					const parentId = dirStack[stackTop];
					if (parentId === void 0) throw new Error("Directory stack underflow while building the path store");
					currentDepth++;
					const dirSeg = path.slice(segmentStart, slashPos);
					let dirNameId = idByValue.get(dirSeg);
					if (dirNameId === void 0) {
						dirNameId = valueById.length;
						idByValue.set(dirSeg, dirNameId);
						valueById.push(dirSeg);
					}
					const nodeId = nodes.length;
					nodes.push({
						depthAndFlags: createNodeDepthAndFlags(currentDepth, 0, PATH_STORE_NODE_KIND_DIRECTORY),
						nameId: dirNameId,
						parentId,
						subtreeNodeCount: 1,
						visibleSubtreeCount: 1
					});
					this.recordCreatedDirectoryPath(path.slice(0, slashPos));
					stackTop++;
					dirStack[stackTop] = nodeId;
					segmentStart = slashPos + 1;
					slashPos = path.indexOf("/", segmentStart);
				}
				if (hasTrailingSlash) {
					if (segmentStart < endIndex) {
						const parentId = dirStack[stackTop];
						if (parentId === void 0) throw new Error(`Unable to resolve directory parent for "${path}"`);
						currentDepth++;
						const trailSeg = path.slice(segmentStart, endIndex);
						let trailNameId = idByValue.get(trailSeg);
						if (trailNameId === void 0) {
							trailNameId = valueById.length;
							idByValue.set(trailSeg, trailNameId);
							valueById.push(trailSeg);
						}
						const nodeId = nodes.length;
						nodes.push({
							depthAndFlags: createNodeDepthAndFlags(currentDepth, 0, PATH_STORE_NODE_KIND_DIRECTORY),
							nameId: trailNameId,
							parentId,
							subtreeNodeCount: 1,
							visibleSubtreeCount: 1
						});
						stackTop++;
						dirStack[stackTop] = nodeId;
					}
					const directoryId = dirStack[stackTop];
					if (directoryId === void 0) throw new Error(`Unable to resolve directory node for "${path}"`);
					this.promoteDirectoryToExplicit(directoryId, path);
				} else {
					const parentId = dirStack[stackTop];
					if (parentId === void 0) throw new Error(`Unable to resolve file parent for "${path}"`);
					const fileSeg = path.slice(segmentStart);
					let fileNameId = idByValue.get(fileSeg);
					if (fileNameId === void 0) {
						fileNameId = valueById.length;
						idByValue.set(fileSeg, fileNameId);
						valueById.push(fileSeg);
					}
					nodes.push({
						depthAndFlags: createNodeDepthAndFlags(currentDepth + 1, 0),
						nameId: fileNameId,
						parentId,
						subtreeNodeCount: 1,
						visibleSubtreeCount: 1
					});
				}
				if (segmentStart !== cachedDirPrefix.length) {
					cachedDirPrefix = path.substring(0, segmentStart);
					cachedDirDepth = currentDepth;
				}
				previousPath = path;
			}
			dirStack.length = stackTop + 1;
			if (previousPath != null) this.lastPreparedPath = parseInputPath(previousPath);
			this.hasDeferredDirectoryIndexes = true;
		});
		return this;
	}
	appendPresortedFilePaths(paths) {
		let previousPath = null;
		let currentDepth = 0;
		const nodes = this.nodes;
		const segmentTable = this.segmentTable;
		const idByValue = segmentTable.idByValue;
		const valueById = segmentTable.valueById;
		const dirStack = this.directoryStack;
		let stackTop = 0;
		let cachedDirPrefix = "";
		let cachedDirDepth = 0;
		for (const path of paths) {
			if (previousPath === path) throw new Error(`Duplicate path: "${path}"`);
			const endIndex = path.length;
			let sharedDirectoryDepth = 0;
			let unsharedSegmentStart = 0;
			if (previousPath != null) if (cachedDirPrefix.length > 0 && path.length > cachedDirPrefix.length && path.startsWith(cachedDirPrefix)) {
				sharedDirectoryDepth = cachedDirDepth;
				unsharedSegmentStart = cachedDirPrefix.length;
			} else {
				const compareLength = Math.min(endIndex, previousPath.length);
				for (let ci = 0; ci < compareLength; ci++) {
					const cc = path.charCodeAt(ci);
					if (cc !== previousPath.charCodeAt(ci)) break;
					if (cc === 47) {
						sharedDirectoryDepth++;
						unsharedSegmentStart = ci + 1;
					}
				}
			}
			stackTop = sharedDirectoryDepth;
			currentDepth = sharedDirectoryDepth;
			let segmentStart = unsharedSegmentStart;
			let slashPos = path.indexOf("/", segmentStart);
			while (slashPos >= 0) {
				const parentId$1 = dirStack[stackTop];
				if (parentId$1 === void 0) throw new Error("Directory stack underflow while building the path store");
				currentDepth++;
				const dirSeg = path.slice(segmentStart, slashPos);
				let dirNameId = idByValue.get(dirSeg);
				if (dirNameId === void 0) {
					dirNameId = valueById.length;
					idByValue.set(dirSeg, dirNameId);
					valueById.push(dirSeg);
				}
				const nodeId = nodes.length;
				nodes.push({
					depthAndFlags: createNodeDepthAndFlags(currentDepth, 0, PATH_STORE_NODE_KIND_DIRECTORY),
					nameId: dirNameId,
					parentId: parentId$1,
					subtreeNodeCount: 1,
					visibleSubtreeCount: 1
				});
				this.recordCreatedDirectoryPath(path.slice(0, slashPos));
				this.presortedDirectoryNodeIds.push(nodeId);
				stackTop++;
				dirStack[stackTop] = nodeId;
				segmentStart = slashPos + 1;
				slashPos = path.indexOf("/", segmentStart);
			}
			const parentId = dirStack[stackTop];
			if (parentId === void 0) throw new Error(`Unable to resolve file parent for "${path}"`);
			const fileSeg = path.slice(segmentStart);
			let fileNameId = idByValue.get(fileSeg);
			if (fileNameId === void 0) {
				fileNameId = valueById.length;
				idByValue.set(fileSeg, fileNameId);
				valueById.push(fileSeg);
			}
			nodes.push({
				depthAndFlags: createNodeDepthAndFlags(currentDepth + 1, 0),
				nameId: fileNameId,
				parentId,
				subtreeNodeCount: 1,
				visibleSubtreeCount: 1
			});
			if (segmentStart !== cachedDirPrefix.length) {
				cachedDirPrefix = path.substring(0, segmentStart);
				cachedDirDepth = currentDepth;
			}
			previousPath = path;
		}
		dirStack.length = stackTop + 1;
		if (previousPath != null) this.lastPreparedPath = parseInputPath(previousPath);
		this.hasDeferredDirectoryIndexes = true;
	}
	finish(options = {}) {
		const skipSubtreeCountPass = options.skipSubtreeCountPass === true;
		if (this.hasDeferredDirectoryIndexes) {
			withBenchmarkPhase(this.instrumentation, "store.builder.buildDirectoryIndexes", () => this.buildPresortedFinish(skipSubtreeCountPass));
			this.hasDeferredDirectoryIndexes = false;
		} else if (!skipSubtreeCountPass) withBenchmarkPhase(this.instrumentation, "store.builder.computeSubtreeCounts", () => this.computeSubtreeCounts(0));
		return {
			directories: this.directories,
			nodes: this.nodes,
			options: this.options,
			rootId: 0,
			segmentTable: this.segmentTable,
			presortedDirectoryNodeIds: this.presortedDirectoryNodeIds.length > 0 ? this.presortedDirectoryNodeIds : null
		};
	}
	didMatchAllInitialExpandedPaths() {
		return this.createdDirectoriesAllExpanded && this.initialExpandedPathSet != null && this.createdDirectoryCount === this.initialExpandedPathSet.size;
	}
	appendPreparedPath(preparedPath, validateOrder) {
		if (this.hasDeferredDirectoryIndexes) {
			this.buildDirectoryIndexes();
			this.hasDeferredDirectoryIndexes = false;
		}
		if (this.lastPreparedPath != null) {
			if (preparedPath.path === this.lastPreparedPath.path) throw new Error(`Duplicate path: "${preparedPath.path}"`);
			if (validateOrder) {
				if ((this.options.sort === "default" ? comparePreparedPathsWithCachedSortKeys(this.lastPreparedPath, preparedPath, this.segmentSortKeyCache) : compareWithSortOption(this.lastPreparedPath, preparedPath, this.options.sort)) > 0) throw new Error(`Builder input must be sorted before appendPaths(): "${preparedPath.path}"`);
			}
		}
		const previousPath = this.lastPreparedPath;
		const currentDirectoryDepth = getDirectoryDepth(preparedPath);
		const previousDirectoryDepth = previousPath == null ? 0 : getDirectoryDepth(previousPath);
		const sharedPrefixLength = previousPath == null ? 0 : computeSharedPrefixLength(previousPath.segments, preparedPath.segments);
		const sharedDirectoryDepth = Math.min(sharedPrefixLength, currentDirectoryDepth, previousDirectoryDepth);
		this.directoryStack.length = sharedDirectoryDepth + 1;
		for (let segmentIndex = sharedDirectoryDepth; segmentIndex < currentDirectoryDepth; segmentIndex++) {
			const parentId$1 = this.directoryStack[this.directoryStack.length - 1];
			if (parentId$1 === void 0) throw new Error("Directory stack underflow while building the path store");
			const childId = validateOrder ? this.getOrCreateDirectoryChild(parentId$1, preparedPath.segments[segmentIndex]) : this.createDirectoryChild(parentId$1, preparedPath.segments[segmentIndex]);
			this.directoryStack.push(childId);
		}
		if (preparedPath.isDirectory) {
			const directoryId = this.directoryStack[this.directoryStack.length - 1];
			if (directoryId === void 0) throw new Error(`Unable to resolve directory node for "${preparedPath.path}"`);
			this.promoteDirectoryToExplicit(directoryId, preparedPath.path);
			this.lastPreparedPath = preparedPath;
			return;
		}
		const parentId = this.directoryStack[this.directoryStack.length - 1];
		if (parentId === void 0) throw new Error(`Unable to resolve file parent for "${preparedPath.path}"`);
		if (validateOrder) this.createFileChild(parentId, preparedPath.basename, preparedPath.path);
		else this.createFileChildUnchecked(parentId, preparedPath.basename);
		this.lastPreparedPath = preparedPath;
	}
	recordCreatedDirectoryPath(path) {
		if (!this.createdDirectoriesAllExpanded || this.initialExpandedPathSet == null) return;
		this.createdDirectoryCount += 1;
		if (!this.initialExpandedPathSet.has(path)) this.createdDirectoriesAllExpanded = false;
	}
	createFileChild(parentId, basename, path) {
		const nameId = internSegment(this.segmentTable, basename);
		const parentIndex = this.getDirectoryIndex(parentId);
		const nameMap = parentIndex.childIdByNameId;
		if (nameMap != null) {
			if (nameMap.get(nameId) !== void 0) throw new Error(`Path collides with an existing entry: "${path}"`);
		}
		const parentNode = this.nodes[parentId];
		if (parentNode === void 0) throw new Error(`Unknown parent node ID: ${String(parentId)}`);
		const nodeId = this.nodes.length;
		this.nodes.push({
			depthAndFlags: createNodeDepthAndFlags(getNodeDepth(parentNode) + 1, 0),
			nameId,
			parentId,
			subtreeNodeCount: 1,
			visibleSubtreeCount: 1
		});
		if (nameMap != null) nameMap.set(nameId, nodeId);
		appendChildReference(parentIndex, nodeId);
		return nodeId;
	}
	createFileChildUnchecked(parentId, basename) {
		const nameId = internSegment(this.segmentTable, basename);
		const parentIndex = this.getDirectoryIndex(parentId);
		const parentNode = this.nodes[parentId];
		if (parentNode === void 0) throw new Error(`Unknown parent node ID: ${String(parentId)}`);
		const nodeId = this.nodes.length;
		this.nodes.push({
			depthAndFlags: createNodeDepthAndFlags(getNodeDepth(parentNode) + 1, 0),
			nameId,
			parentId,
			subtreeNodeCount: 1,
			visibleSubtreeCount: 1
		});
		if (parentIndex.childIdByNameId != null) parentIndex.childIdByNameId.set(nameId, nodeId);
		appendChildReference(parentIndex, nodeId);
		return nodeId;
	}
	getOrCreateDirectoryChild(parentId, segment) {
		const nameId = internSegment(this.segmentTable, segment);
		const parentIndex = this.getDirectoryIndex(parentId);
		if (parentIndex.childIdByNameId != null) {
			const existingChildId = parentIndex.childIdByNameId.get(nameId);
			if (existingChildId !== void 0) {
				const existingNode = this.nodes[existingChildId];
				if (existingNode != null && !isDirectoryNode(existingNode)) throw new Error(`Path collides with an existing file while creating directory "${segment}"`);
				return existingChildId;
			}
		}
		const parentNode = this.nodes[parentId];
		if (parentNode === void 0) throw new Error(`Unknown parent node ID: ${String(parentId)}`);
		const nodeId = this.nodes.length;
		this.nodes.push({
			depthAndFlags: createNodeDepthAndFlags(getNodeDepth(parentNode) + 1, 0, PATH_STORE_NODE_KIND_DIRECTORY),
			nameId,
			parentId,
			subtreeNodeCount: 1,
			visibleSubtreeCount: 1
		});
		if (parentIndex.childIdByNameId != null) parentIndex.childIdByNameId.set(nameId, nodeId);
		appendChildReference(parentIndex, nodeId);
		this.directories.set(nodeId, createDirectoryChildIndex());
		return nodeId;
	}
	createDirectoryChild(parentId, segment) {
		const nameId = internSegment(this.segmentTable, segment);
		const parentIndex = this.getDirectoryIndex(parentId);
		const parentNode = this.nodes[parentId];
		if (parentNode === void 0) throw new Error(`Unknown parent node ID: ${String(parentId)}`);
		const nodeId = this.nodes.length;
		this.nodes.push({
			depthAndFlags: createNodeDepthAndFlags(getNodeDepth(parentNode) + 1, 0, PATH_STORE_NODE_KIND_DIRECTORY),
			nameId,
			parentId,
			subtreeNodeCount: 1,
			visibleSubtreeCount: 1
		});
		if (parentIndex.childIdByNameId != null) parentIndex.childIdByNameId.set(nameId, nodeId);
		appendChildReference(parentIndex, nodeId);
		this.directories.set(nodeId, createDirectoryChildIndex());
		return nodeId;
	}
	promoteDirectoryToExplicit(directoryId, path) {
		const directoryNode = this.nodes[directoryId];
		if (directoryNode === void 0) throw new Error(`Unknown directory node ID: ${String(directoryId)}`);
		if (!isDirectoryNode(directoryNode)) throw new Error(`Path is not a directory: "${path}"`);
		if (hasNodeFlag(directoryNode, PATH_STORE_NODE_FLAG_EXPLICIT)) throw new Error(`Duplicate path: "${path}"`);
		addNodeFlag(directoryNode, PATH_STORE_NODE_FLAG_EXPLICIT);
	}
	getDirectoryIndex(directoryId) {
		const existingIndex = this.directories.get(directoryId);
		if (existingIndex !== void 0) return existingIndex;
		throw new Error(`Unknown directory child index for node ${String(directoryId)}`);
	}
	buildPresortedFinish(skipSubtreeCountPass) {
		const nodes = this.nodes;
		const directories = this.directories;
		directories.set(0, createPresortedDirectoryChildIndex());
		let cachedParentId = -1;
		let cachedParentIndex = null;
		for (let nodeId = 1; nodeId < nodes.length; nodeId++) {
			const node = nodes[nodeId];
			if (node == null) continue;
			if (isDirectoryNode(node)) {
				const dirIndex = createPresortedDirectoryChildIndex();
				directories.set(nodeId, dirIndex);
				cachedParentId = nodeId;
				cachedParentIndex = dirIndex;
			}
			let parentIndex;
			if (node.parentId === cachedParentId) parentIndex = cachedParentIndex;
			else {
				parentIndex = directories.get(node.parentId);
				cachedParentId = node.parentId;
				cachedParentIndex = parentIndex ?? null;
			}
			if (parentIndex != null) parentIndex.childIds.push(nodeId);
		}
		if (skipSubtreeCountPass) return;
		for (let nodeId = nodes.length - 1; nodeId >= 1; nodeId--) {
			const node = nodes[nodeId];
			if (node == null) continue;
			const parentNode = nodes[node.parentId];
			if (parentNode != null) {
				parentNode.subtreeNodeCount += node.subtreeNodeCount;
				parentNode.visibleSubtreeCount += node.visibleSubtreeCount;
			}
		}
	}
	buildDirectoryIndexes() {
		const nodes = this.nodes;
		for (let nodeId = 1; nodeId < nodes.length; nodeId++) {
			const node = nodes[nodeId];
			if (node == null) continue;
			if (isDirectoryNode(node)) this.directories.set(nodeId, createDirectoryChildIndex());
			const parentIndex = this.directories.get(node.parentId);
			if (parentIndex != null) {
				if (parentIndex.childIdByNameId != null) parentIndex.childIdByNameId.set(node.nameId, nodeId);
				appendChildReference(parentIndex, nodeId);
			}
		}
	}
	computeSubtreeCounts(nodeId) {
		const node = this.nodes[nodeId];
		if (node === void 0) throw new Error(`Unknown node ID: ${String(nodeId)}`);
		if (!isDirectoryNode(node)) {
			node.subtreeNodeCount = 1;
			node.visibleSubtreeCount = 1;
			return 1;
		}
		const directoryIndex = this.getDirectoryIndex(nodeId);
		let subtreeNodeCount = 1;
		for (const childId of directoryIndex.childIds) subtreeNodeCount += this.computeSubtreeCounts(childId);
		rebuildDirectoryChildAggregates(this.nodes, directoryIndex);
		node.subtreeNodeCount = subtreeNodeCount;
		node.visibleSubtreeCount = subtreeNodeCount;
		return subtreeNodeCount;
	}
};

//#endregion
export { PathStoreBuilder, getPreparedInputEntries, getPreparedInputPresortedPaths, getPreparedInputPresortedPathsContainDirectories, prepareInput, preparePathEntries, preparePaths, preparePresortedInput };
//# sourceMappingURL=builder.js.map