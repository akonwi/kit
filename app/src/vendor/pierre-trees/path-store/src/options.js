//#region ../path-store/src/options.ts
function resolvePathStoreOptions(options = {}) {
	return {
		flattenEmptyDirectories: options.flattenEmptyDirectories !== false,
		sort: options.sort ?? "default"
	};
}

//#endregion
export { resolvePathStoreOptions };
//# sourceMappingURL=options.js.map