/**
 * Working-tree refresh bookkeeping for the review pane.
 *
 * The pane publishes a diff derived from the working tree. Filesystem
 * watching triggers refreshes, but individual refreshes can be dropped —
 * a note editor is open, git failed transiently, or the resource was mid
 * load. The tracker compares each observed working-tree fingerprint
 * against the fingerprint of the last *published* refresh, so any dropped
 * refresh is retried on the next poll tick instead of leaving the pane
 * permanently stale once the tree stops changing.
 */
export function createWorkingTreeRefreshTracker() {
	// Latest observed working-tree fingerprint.
	let observed: string | undefined;
	// Fingerprint the published diff corresponds to. Starts unknown: until a
	// publish is marked with a real snapshot, every observation demands a
	// settling refresh rather than assuming the mount-time load matched.
	let published: string | undefined;

	return {
		/**
		 * Record a fingerprint observation. Returns true when the published
		 * diff is behind this observation and a refresh should be scheduled.
		 */
		observe(next: string): boolean {
			observed = next;
			return next !== published;
		},
		/** Fingerprint snapshot to associate with a refresh that is starting. */
		beginRefresh(): string | undefined {
			return observed;
		},
		/**
		 * Mark a refresh as published. Edits that landed while the refresh was
		 * in flight keep `published` older than the next observation, so the
		 * poll converges with one follow-up refresh. A dropped refresh never
		 * calls this, so the next observation keeps demanding a retry.
		 */
		markPublished(fingerprintAtStart: string | undefined): void {
			if (fingerprintAtStart !== undefined) published = fingerprintAtStart;
		},
	};
}

export type WorkingTreeRefreshTracker = ReturnType<
	typeof createWorkingTreeRefreshTracker
>;
