import type { ReviewFile } from "./model";

function sameReviewFile(current: ReviewFile, next: ReviewFile): boolean {
	return (
		current.id === next.id &&
		current.status === next.status &&
		current.rawPatch === next.rawPatch
	);
}

/** Preserve object identity for files whose rendered diff did not change. */
export function reconcileReviewFiles(
	current: ReviewFile[] | undefined,
	next: readonly ReviewFile[],
	preserveMissing?: (file: ReviewFile) => boolean,
): ReviewFile[] {
	if (!current) return [...next];
	const currentById = new Map(current.map((file) => [file.id, file]));
	const reconciled = next.map((file) => {
		const previous = currentById.get(file.id);
		return previous && sameReviewFile(previous, file) ? previous : file;
	});
	if (preserveMissing) {
		const nextIds = new Set(next.map((file) => file.id));
		for (const file of current) {
			if (!nextIds.has(file.id) && preserveMissing(file)) reconciled.push(file);
		}
	}
	return current.length === reconciled.length &&
		current.every((file, index) => file === reconciled[index])
		? current
		: reconciled;
}
