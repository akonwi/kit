export type ReviewWorkspaceController = {
	open(): void;
	subscribe(listener: () => void): () => void;
};

export function createReviewWorkspaceController(): ReviewWorkspaceController {
	const listeners = new Set<() => void>();
	return {
		open() {
			for (const listener of listeners) listener();
		},
		subscribe(listener) {
			listeners.add(listener);
			return () => listeners.delete(listener);
		},
	};
}
