//#region src/model/mutationEvents.ts
function isPathMutationEvent(event) {
	return event.operation === "add" || event.operation === "remove" || event.operation === "move" || event.operation === "batch";
}
function remapMovedPath(path, fromPath, toPath) {
	if (path === fromPath) return toPath;
	const descendantPrefix = fromPath.endsWith("/") ? fromPath : `${fromPath}/`;
	if (!path.startsWith(descendantPrefix)) return path;
	return `${toPath.endsWith("/") ? toPath : `${toPath}/`}${path.slice(descendantPrefix.length)}`;
}
function isPathRemoved(path, removedPath) {
	if (path === removedPath) return true;
	const descendantPrefix = removedPath.endsWith("/") ? removedPath : `${removedPath}/`;
	return path.startsWith(descendantPrefix);
}
function remapPathThroughMutation(path, event, preserveRemovedPath = false) {
	if (path == null) return null;
	switch (event.operation) {
		case "add":
		case "expand":
		case "collapse":
		case "mark-directory-unloaded":
		case "begin-child-load":
		case "apply-child-patch":
		case "complete-child-load":
		case "fail-child-load":
		case "cleanup": return path;
		case "remove": return isPathRemoved(path, event.path) ? preserveRemovedPath ? path : null : path;
		case "move": return remapMovedPath(path, event.from, event.to);
		case "batch": {
			let nextPath = path;
			for (const childEvent of event.events) {
				nextPath = remapPathThroughMutation(nextPath, childEvent, preserveRemovedPath);
				if (nextPath == null) return null;
			}
			return nextPath;
		}
	}
}
function createMutationInvalidation(event) {
	return {
		canonicalChanged: event.canonicalChanged,
		projectionChanged: event.projectionChanged,
		visibleCountDelta: event.visibleCountDelta
	};
}
function toTreesMutationSemanticEvent(event) {
	switch (event.operation) {
		case "add": return {
			...createMutationInvalidation(event),
			operation: "add",
			path: event.path
		};
		case "remove": return {
			...createMutationInvalidation(event),
			operation: "remove",
			path: event.path,
			recursive: event.recursive
		};
		case "move": return {
			...createMutationInvalidation(event),
			from: event.from,
			operation: "move",
			to: event.to
		};
	}
}
function toTreesBatchEvent(event) {
	return {
		...createMutationInvalidation(event),
		events: event.events.filter((childEvent) => childEvent.operation === "add" || childEvent.operation === "remove" || childEvent.operation === "move").map((childEvent) => toTreesMutationSemanticEvent(childEvent)),
		operation: "batch"
	};
}
function toTreesMutationEvent(event) {
	switch (event.operation) {
		case "add":
		case "remove":
		case "move": return toTreesMutationSemanticEvent(event);
		case "batch": return toTreesBatchEvent(event);
		default: return null;
	}
}

//#endregion
export { isPathMutationEvent, remapPathThroughMutation, toTreesMutationEvent };
//# sourceMappingURL=mutationEvents.js.map