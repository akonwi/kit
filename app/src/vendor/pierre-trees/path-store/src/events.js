import { withBenchmarkPhase } from "./internal/benchmarkInstrumentation.js";
import { createTransactionFrame } from "./state.js";

//#region ../path-store/src/events.ts
function subscribe(state, type, handler) {
	const rawHandler = handler;
	const existingListeners = state.listeners.get(type);
	if (existingListeners != null) existingListeners.add(rawHandler);
	else state.listeners.set(type, new Set([rawHandler]));
	return () => {
		const listeners = state.listeners.get(type);
		if (listeners == null) return;
		listeners.delete(rawHandler);
		if (listeners.size === 0) state.listeners.delete(type);
	};
}
function createAddEvent(args) {
	return {
		affectedAncestorIds: args.affectedAncestorIds ?? [],
		affectedNodeIds: args.affectedNodeIds ?? [],
		canonicalChanged: true,
		operation: "add",
		path: args.path,
		projectionChanged: args.projectionChanged,
		visibleCountDelta: null
	};
}
function createRemoveEvent(args) {
	return {
		affectedAncestorIds: args.affectedAncestorIds ?? [],
		affectedNodeIds: args.affectedNodeIds ?? [],
		canonicalChanged: true,
		operation: "remove",
		path: args.path,
		projectionChanged: args.projectionChanged,
		recursive: args.recursive,
		visibleCountDelta: null
	};
}
function createMoveEvent(args) {
	return {
		affectedAncestorIds: args.affectedAncestorIds ?? [],
		affectedNodeIds: args.affectedNodeIds ?? [],
		canonicalChanged: true,
		from: args.from,
		operation: "move",
		projectionChanged: args.projectionChanged,
		to: args.to,
		visibleCountDelta: null
	};
}
function createExpandEvent(args) {
	return {
		affectedAncestorIds: args.affectedAncestorIds ?? [],
		affectedNodeIds: args.affectedNodeIds ?? [],
		canonicalChanged: false,
		operation: "expand",
		path: args.path,
		projectionChanged: true,
		visibleCountDelta: null
	};
}
function createCollapseEvent(args) {
	return {
		affectedAncestorIds: args.affectedAncestorIds ?? [],
		affectedNodeIds: args.affectedNodeIds ?? [],
		canonicalChanged: false,
		operation: "collapse",
		path: args.path,
		projectionChanged: true,
		visibleCountDelta: null
	};
}
function createMarkDirectoryUnloadedEvent(args) {
	return {
		affectedAncestorIds: args.affectedAncestorIds ?? [],
		affectedNodeIds: args.affectedNodeIds ?? [],
		canonicalChanged: false,
		operation: "mark-directory-unloaded",
		path: args.path,
		projectionChanged: args.projectionChanged,
		visibleCountDelta: null
	};
}
function createBeginChildLoadEvent(args) {
	return {
		affectedAncestorIds: args.affectedAncestorIds ?? [],
		affectedNodeIds: args.affectedNodeIds ?? [],
		attemptId: args.attemptId,
		canonicalChanged: false,
		operation: "begin-child-load",
		path: args.path,
		projectionChanged: args.projectionChanged,
		reused: args.reused,
		visibleCountDelta: null
	};
}
function createApplyChildPatchEvent(args) {
	return {
		affectedAncestorIds: args.affectedAncestorIds ?? [],
		affectedNodeIds: args.affectedNodeIds ?? [],
		attemptId: args.attemptId,
		canonicalChanged: args.childEvents.some((event) => event.canonicalChanged),
		childEvents: args.childEvents,
		operation: "apply-child-patch",
		path: args.path,
		projectionChanged: args.projectionChanged,
		visibleCountDelta: null
	};
}
function createCompleteChildLoadEvent(args) {
	return {
		affectedAncestorIds: args.affectedAncestorIds ?? [],
		affectedNodeIds: args.affectedNodeIds ?? [],
		attemptId: args.attemptId,
		canonicalChanged: false,
		operation: "complete-child-load",
		path: args.path,
		projectionChanged: args.projectionChanged,
		stale: args.stale,
		visibleCountDelta: null
	};
}
function createFailChildLoadEvent(args) {
	return {
		affectedAncestorIds: args.affectedAncestorIds ?? [],
		affectedNodeIds: args.affectedNodeIds ?? [],
		attemptId: args.attemptId,
		canonicalChanged: false,
		errorMessage: args.errorMessage,
		operation: "fail-child-load",
		path: args.path,
		projectionChanged: args.projectionChanged,
		stale: args.stale,
		visibleCountDelta: null
	};
}
function createCleanupEvent(args) {
	return {
		activeNodeCountAfter: args.activeNodeCountAfter,
		activeNodeCountBefore: args.activeNodeCountBefore,
		affectedAncestorIds: args.affectedAncestorIds ?? [],
		affectedNodeIds: args.affectedNodeIds ?? [],
		cachedPathEntryCountAfter: args.cachedPathEntryCountAfter,
		cachedPathEntryCountBefore: args.cachedPathEntryCountBefore,
		canonicalChanged: false,
		idsPreserved: args.idsPreserved,
		loadInfoEntryCountAfter: args.loadInfoEntryCountAfter,
		loadInfoEntryCountBefore: args.loadInfoEntryCountBefore,
		mode: args.mode,
		operation: "cleanup",
		projectionChanged: args.projectionChanged,
		reclaimedCachedPathEntryCount: args.reclaimedCachedPathEntryCount,
		reclaimedLoadInfoEntryCount: args.reclaimedLoadInfoEntryCount,
		reclaimedNodeSlotCount: args.reclaimedNodeSlotCount,
		reclaimedSegmentCount: args.reclaimedSegmentCount,
		segmentCountAfter: args.segmentCountAfter,
		segmentCountBefore: args.segmentCountBefore,
		totalNodeSlotCountAfter: args.totalNodeSlotCountAfter,
		totalNodeSlotCountBefore: args.totalNodeSlotCountBefore,
		visibleCountDelta: null
	};
}
function finalizeEvent(state, previousVisibleCount, event) {
	return {
		...event,
		visibleCountDelta: getCurrentVisibleCount(state) - previousVisibleCount
	};
}
function batchEvents(state, run) {
	const previousVisibleCount = getCurrentVisibleCount(state);
	const frame = createTransactionFrame();
	state.transactionStack.push(frame);
	try {
		run();
	} catch (error) {
		finishTransaction(state, frame, false);
		throw error;
	}
	finishTransaction(state, frame, true, getCurrentVisibleCount(state) - previousVisibleCount);
}
function recordEvent(state, event) {
	const instrumentation = state.instrumentation;
	if (instrumentation == null) {
		recordEventNow(state, event);
		return;
	}
	withBenchmarkPhase(instrumentation, "store.events.record", () => recordEventNow(state, event));
}
function recordEventNow(state, event) {
	const currentFrame = state.transactionStack[state.transactionStack.length - 1] ?? null;
	if (currentFrame == null) {
		emitEvent(state, event);
		return;
	}
	currentFrame.events.push(event);
	mergeEventMetadataIntoFrame(currentFrame, event);
}
function finishTransaction(state, frame, emit, visibleCountDelta = null) {
	if (state.transactionStack.pop() !== frame) throw new Error("Transaction stack underflow");
	if (!emit) return;
	const parentFrame = state.transactionStack[state.transactionStack.length - 1] ?? null;
	if (parentFrame != null) {
		const instrumentation$1 = state.instrumentation;
		if (instrumentation$1 == null) mergeBatchFrameIntoParent(parentFrame, frame);
		else withBenchmarkPhase(instrumentation$1, "store.events.batch.merge", () => mergeBatchFrameIntoParent(parentFrame, frame));
		return;
	}
	const batchEvent = createBatchEvent(frame, visibleCountDelta);
	const instrumentation = state.instrumentation;
	if (instrumentation == null) {
		emitEvent(state, batchEvent);
		return;
	}
	withBenchmarkPhase(instrumentation, "store.events.batch.commit", () => emitEvent(state, batchEvent));
}
function createBatchEvent(frame, visibleCountDelta) {
	return {
		affectedAncestorIds: [...frame.affectedAncestorIds],
		affectedNodeIds: [...frame.affectedNodeIds],
		canonicalChanged: frame.events.some((event) => event.canonicalChanged),
		events: [...frame.events],
		operation: "batch",
		projectionChanged: frame.events.some((event) => event.projectionChanged),
		visibleCountDelta
	};
}
function mergeFrameMetadata(target, source) {
	for (const nodeId of source.affectedAncestorIds) target.affectedAncestorIds.add(nodeId);
	for (const nodeId of source.affectedNodeIds) target.affectedNodeIds.add(nodeId);
}
function mergeBatchFrameIntoParent(parentFrame, frame) {
	for (const event of frame.events) parentFrame.events.push(event);
	mergeFrameMetadata(parentFrame, frame);
}
function mergeEventMetadataIntoFrame(frame, event) {
	for (const nodeId of event.affectedNodeIds) frame.affectedNodeIds.add(nodeId);
	for (const nodeId of event.affectedAncestorIds) frame.affectedAncestorIds.add(nodeId);
}
function emitEvent(state, event) {
	const instrumentation = state.instrumentation;
	if (instrumentation == null) {
		emitEventNow(state, event);
		return;
	}
	withBenchmarkPhase(instrumentation, "store.events.emit", () => emitEventNow(state, event));
}
function emitEventNow(state, event) {
	state.listeners.get(event.operation)?.forEach((handler) => handler(event));
	state.listeners.get("*")?.forEach((handler) => handler(event));
}
function getCurrentVisibleCount(state) {
	return state.snapshot.nodes[state.snapshot.rootId]?.visibleSubtreeCount ?? 0;
}

//#endregion
export { batchEvents, createAddEvent, createApplyChildPatchEvent, createBeginChildLoadEvent, createCleanupEvent, createCollapseEvent, createCompleteChildLoadEvent, createExpandEvent, createFailChildLoadEvent, createMarkDirectoryUnloadedEvent, createMoveEvent, createRemoveEvent, finalizeEvent, recordEvent, subscribe };
//# sourceMappingURL=events.js.map