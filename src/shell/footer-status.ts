export type FooterStatusContribution = {
	id: string;
	label: string;
};

export function createFooterStatusController() {
	let vcsContributions: FooterStatusContribution[] = [];
	const listeners = new Set<() => void>();

	function notify() {
		for (const listener of listeners) listener();
	}

	function setVcsContribution(id: string, label: string | null) {
		const trimmed = label?.trim() ?? "";
		const next = trimmed ? { id, label: trimmed } : null;
		const existingIndex = vcsContributions.findIndex(
			(contribution) => contribution.id === id,
		);

		if (existingIndex >= 0) {
			if (next) {
				vcsContributions = [
					...vcsContributions.slice(0, existingIndex),
					next,
					...vcsContributions.slice(existingIndex + 1),
				];
			} else {
				vcsContributions = vcsContributions.filter(
					(contribution) => contribution.id !== id,
				);
			}
		} else if (next) {
			vcsContributions = [...vcsContributions, next];
		} else {
			return;
		}

		notify();
	}

	function getVcsContributions(): FooterStatusContribution[] {
		return [...vcsContributions];
	}

	function subscribe(listener: () => void): () => void {
		listeners.add(listener);
		return () => listeners.delete(listener);
	}

	return {
		setVcsContribution,
		getVcsContributions,
		subscribe,
	};
}

export type FooterStatusController = ReturnType<
	typeof createFooterStatusController
>;
