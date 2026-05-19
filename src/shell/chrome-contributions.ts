export type ChromeContributionSide = "left" | "right";

export type ChromeContribution = {
	id: string;
	label: string;
	side: ChromeContributionSide;
};

export type ChromeContributionInput = {
	id: string;
	label: string;
	side?: ChromeContributionSide;
};

export function createChromeContributionsController() {
	let contributions: ChromeContribution[] = [];
	const hiddenContributions = new Map<string, Set<symbol>>();
	const listeners = new Set<() => void>();

	function notify() {
		for (const listener of listeners) listener();
	}

	function setContribution(input: ChromeContributionInput) {
		const label = input.label.trim();
		if (!label) {
			clearContribution(input.id);
			return;
		}
		const next = {
			id: input.id,
			label,
			side: input.side ?? "right",
		};
		const existingIndex = contributions.findIndex(
			(contribution) => contribution.id === input.id,
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

	function clearContribution(id: string) {
		const next = contributions.filter((contribution) => contribution.id !== id);
		if (next.length === contributions.length) return;
		contributions = next;
		notify();
	}

	function clearNamespace(namespace: string) {
		const prefix = `${namespace}:`;
		const next = contributions.filter(
			(contribution) => !contribution.id.startsWith(prefix),
		);
		if (next.length === contributions.length) return;
		contributions = next;
		notify();
	}

	function hideContribution(id: string): () => void {
		const token = Symbol(id);
		const tokens = hiddenContributions.get(id) ?? new Set<symbol>();
		tokens.add(token);
		hiddenContributions.set(id, tokens);
		notify();
		return () => {
			const current = hiddenContributions.get(id);
			if (!current?.delete(token)) return;
			if (current.size === 0) hiddenContributions.delete(id);
			notify();
		};
	}

	function isHidden(id: string): boolean {
		return (hiddenContributions.get(id)?.size ?? 0) > 0;
	}

	function getContributions(
		side?: ChromeContributionSide,
	): ChromeContribution[] {
		const visible = contributions.filter(
			(contribution) => !isHidden(contribution.id),
		);
		return side
			? visible.filter((contribution) => contribution.side === side)
			: visible;
	}

	function subscribe(listener: () => void): () => void {
		listeners.add(listener);
		return () => listeners.delete(listener);
	}

	return {
		setContribution,
		clearContribution,
		clearNamespace,
		hideContribution,
		isHidden,
		getContributions,
		subscribe,
	};
}

export type ChromeContributionsController = ReturnType<
	typeof createChromeContributionsController
>;
