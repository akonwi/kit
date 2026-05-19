export type FooterStatusSide = "left" | "right";

export type FooterStatusContribution = {
	key: string;
	id: string;
	label: string;
	side: FooterStatusSide;
};

export type FooterStatusContributionInput = {
	key: string;
	id: string;
	label: string;
	side?: FooterStatusSide;
};

export function createFooterStatusController() {
	let contributions: FooterStatusContribution[] = [];
	const listeners = new Set<() => void>();

	function notify() {
		for (const listener of listeners) listener();
	}

	function addContribution(input: FooterStatusContributionInput) {
		const label = input.label.trim();
		if (!label) return;
		const next = {
			key: input.key,
			id: input.id,
			label,
			side: input.side ?? "right",
		};
		const existingIndex = contributions.findIndex(
			(contribution) => contribution.key === input.key,
		);

		if (existingIndex >= 0) {
			contributions = [
				...contributions.slice(0, existingIndex),
				next,
				...contributions.slice(existingIndex + 1),
			];
		} else {
			contributions = [...contributions, next];
		}

		notify();
	}

	function removeContribution(key: string) {
		const next = contributions.filter(
			(contribution) => contribution.key !== key,
		);
		if (next.length === contributions.length) return;
		contributions = next;
		notify();
	}

	function clearNamespace(namespace: string) {
		const prefix = `${namespace}:`;
		const next = contributions.filter(
			(contribution) => !contribution.key.startsWith(prefix),
		);
		if (next.length === contributions.length) return;
		contributions = next;
		notify();
	}

	function getContributions(
		side?: FooterStatusSide,
	): FooterStatusContribution[] {
		return side
			? contributions.filter((contribution) => contribution.side === side)
			: [...contributions];
	}

	function subscribe(listener: () => void): () => void {
		listeners.add(listener);
		return () => listeners.delete(listener);
	}

	return {
		addContribution,
		removeContribution,
		clearNamespace,
		getContributions,
		subscribe,
	};
}

export type FooterStatusController = ReturnType<
	typeof createFooterStatusController
>;
