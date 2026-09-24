import { describe, expect, test } from "bun:test";
import { createWorkingTreeRefreshTracker } from "./working-tree-refresh";

describe("working-tree refresh tracker", () => {
	test("observations before any publish demand a settling refresh", () => {
		// The tracker never assumes the mount-time load matched the first
		// observation; one settling refresh establishes the baseline.
		const tracker = createWorkingTreeRefreshTracker();
		expect(tracker.observe("a")).toBe(true);
		expect(tracker.observe("a")).toBe(true);
		tracker.markPublished(tracker.beginRefresh());
		expect(tracker.observe("a")).toBe(false);
	});

	test("changed observation triggers a refresh", () => {
		const tracker = createWorkingTreeRefreshTracker();
		tracker.observe("a");
		tracker.markPublished(tracker.beginRefresh());
		expect(tracker.observe("b")).toBe(true);
	});

	test("published refresh settles the baseline", () => {
		const tracker = createWorkingTreeRefreshTracker();
		tracker.observe("a");
		tracker.observe("b");
		tracker.markPublished(tracker.beginRefresh());
		expect(tracker.observe("b")).toBe(false);
	});

	test("dropped refresh keeps retrying on an unchanged tree", () => {
		// Regression: a refresh dropped mid-edit (open editor, transient git
		// failure) previously went unnoticed once the tree stopped changing,
		// leaving the pane stale until the user toggled the review target.
		const tracker = createWorkingTreeRefreshTracker();
		tracker.observe("a");
		tracker.observe("b");
		const snapshot = tracker.beginRefresh();
		// The refresh was dropped: markPublished is never called.
		expect(tracker.observe("b")).toBe(true);
		expect(tracker.observe("b")).toBe(true);
		// A later successful refresh finally settles it.
		tracker.markPublished(snapshot);
		expect(tracker.observe("b")).toBe(false);
	});

	test("edits landing mid-refresh converge with one follow-up", () => {
		const tracker = createWorkingTreeRefreshTracker();
		tracker.observe("a");
		tracker.observe("b");
		const snapshot = tracker.beginRefresh();
		// Tree changed again while the refresh was reading it.
		tracker.observe("c");
		tracker.markPublished(snapshot);
		// Published state is only as fresh as "b" — refresh once more.
		expect(tracker.observe("c")).toBe(true);
		tracker.markPublished(tracker.beginRefresh());
		expect(tracker.observe("c")).toBe(false);
	});

	test("publish without a prior observation stays unsettled", () => {
		// The mount-time load can finish before the first fingerprint sample;
		// marking it published with no snapshot must not fake a baseline.
		const tracker = createWorkingTreeRefreshTracker();
		tracker.markPublished(tracker.beginRefresh());
		expect(tracker.observe("a")).toBe(true);
	});
});
