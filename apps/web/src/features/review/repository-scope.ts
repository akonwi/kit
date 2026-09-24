import { type Accessor, createSignal } from "solid-js";
import { getRepoRoot } from "./model";

export type ReviewRepositoryChange =
	| { type: "session.active.changed"; session: { cwd: string } }
	| {
			type: "session.active.changed.cwd";
			session: { cwd: string };
			cwd: string;
	  };

export function createReviewRepositoryScope(options: {
	initialCwd: string;
	subscribe: (listener: (event: ReviewRepositoryChange) => void) => () => void;
	resolveRepoRoot?: (cwd: string) => string;
}): {
	repoRoot: Accessor<string>;
	dispose: () => void;
} {
	const resolveRepoRoot = options.resolveRepoRoot ?? getRepoRoot;
	const [repoRoot, setRepoRoot] = createSignal(
		resolveRepoRoot(options.initialCwd),
	);
	const dispose = options.subscribe((event) => {
		setRepoRoot(
			resolveRepoRoot(
				event.type === "session.active.changed.cwd"
					? event.cwd
					: event.session.cwd,
			),
		);
	});
	return { repoRoot, dispose };
}
