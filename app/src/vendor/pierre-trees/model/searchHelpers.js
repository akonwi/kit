//#region src/model/searchHelpers.ts
const normalizeSearchQuery = (value) => {
	const trimmedValue = value.trim();
	if (trimmedValue.length === 0) return "";
	return (trimmedValue.includes("\\") ? trimmedValue.replaceAll("\\", "/") : trimmedValue).toLowerCase();
};

//#endregion
export { normalizeSearchQuery };
//# sourceMappingURL=searchHelpers.js.map