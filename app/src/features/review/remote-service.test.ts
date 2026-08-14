import { describe, expect, test } from "bun:test";
import type { AgentRuntimeEvent } from "../../runtime/agent-runtime";
import type { ReviewFile } from "./model";
import { RemoteReviewService } from "./remote-service";

function reviewFile(): ReviewFile {
	return {
		id: "src/example.ts",
		noteKey: "src/example.ts",
		path: "src/example.ts",
		status: "change",
		source: "working",
		filetype: "typescript",
		rawPatch: "@@ -1 +1 @@\n-old\n+new",
		hunks: [
			{
				id: "hunk-1",
				noteKey: "hunk-1",
				header: "@@ -1 +1 @@",
				context: "",
				lines: [
					{ kind: "delete", text: "old", deletionLineNumber: 1 },
					{ kind: "add", text: "new", additionLineNumber: 1 },
				],
				changeCount: 2,
				rawPatch: "@@ -1 +1 @@\n-old\n+new",
				patchStartLine: 0,
				patchLineCount: 2,
				additionStart: 1,
				additionCount: 1,
				deletionStart: 1,
				deletionCount: 1,
				collapsedBefore: 0,
			},
		],
		skippedSections: [],
		changeCount: 2,
		unifiedLineCount: 2,
		splitLineCount: 1,
	};
}

function harness() {
	let session = { id: "session-1", cwd: "/repo" };
	let listener: ((event: AgentRuntimeEvent) => void) | undefined;
	const service = new RemoteReviewService(
		{
			getMessages: () => [],
			getSession: () => session,
			subscribe: (next: (event: AgentRuntimeEvent) => void) => {
				listener = next;
				return () => {};
			},
		} as never,
		{
			loadFiles: async () => [reviewFile()],
			getRepoRoot: () => "/repo",
		},
	);
	return {
		service,
		switchSession(id: string) {
			session = { ...session, id };
			listener?.({ type: "session.active.changed" } as AgentRuntimeEvent);
		},
		changeCwd(cwd: string) {
			session = { ...session, cwd };
			listener?.({ type: "session.active.changed" } as AgentRuntimeEvent);
		},
	};
}

describe("RemoteReviewService", () => {
	test("loads file summaries and file detail separately", async () => {
		const { service } = harness();
		const state = await service.refresh();

		expect(state.files).toEqual([
			{
				id: "src/example.ts",
				path: "src/example.ts",
				status: "change",
				source: "working",
				additions: 1,
				deletions: 1,
				changeCount: 2,
			},
		]);
		expect(
			service.getFile(state.sessionId, "src/example.ts").file.rawPatch,
		).toContain("+new");
	});

	test("validates and builds review submissions against the loaded diff", async () => {
		const { service } = harness();
		const state = await service.refresh();

		await expect(
			service.prepareSubmission(
				"submission-1",
				state.sessionId,
				state.generation,
				[
					{
						path: "src/example.ts",
						side: "deletions",
						startLine: 1,
						endLine: 1,
						comment: "Keep this behavior",
					},
				],
			),
		).resolves.toMatchObject({
			submissionId: "submission-1",
			part: {
				type: "code-review",
				review: {
					files: [
						{
							path: "src/example.ts",
							fileComment: "",
							ranges: [
								{
									side: "deletions",
									startLine: 1,
									endLine: 1,
									comment: "Keep this behavior",
								},
							],
						},
					],
				},
			},
		});
	});

	test("deduplicates accepted submission IDs and rejects payload reuse", async () => {
		const { service } = harness();
		const state = await service.refresh();
		const note = {
			path: "src/example.ts",
			side: "additions" as const,
			startLine: 1,
			endLine: 1,
			comment: "Check this",
		};
		const prepared = await service.prepareSubmission(
			"submission-1",
			state.sessionId,
			state.generation,
			[note],
		);
		service.markSubmissionAccepted(prepared);

		await expect(
			service.prepareSubmission(
				"submission-1",
				state.sessionId,
				state.generation,
				[note],
			),
		).resolves.toMatchObject({ part: null });
		if (!prepared.part) throw new Error("Expected a prepared review part");
		const restarted = new RemoteReviewService(
			{
				getMessages: () => [{ role: "user", content: [prepared.part] }],
				getSession: () => ({ id: state.sessionId, cwd: "/repo" }),
				subscribe: () => () => {},
			} as never,
			{
				loadFiles: async () => [reviewFile()],
				getRepoRoot: () => "/repo",
			},
		);
		await expect(
			restarted.prepareSubmission(
				"submission-1",
				state.sessionId,
				state.generation,
				[note],
			),
		).resolves.toMatchObject({ part: null });
		await expect(
			service.prepareSubmission(
				"submission-1",
				state.sessionId,
				state.generation,
				[{ ...note, comment: "Different" }],
			),
		).rejects.toThrow("reused with different notes");
	});

	test("rejects stale generations and ranges outside the loaded diff", async () => {
		const { service } = harness();
		const state = await service.refresh();
		const note = {
			path: "src/example.ts",
			side: "additions" as const,
			startLine: 1,
			endLine: 1,
			comment: "Check this",
		};

		await expect(
			service.prepareSubmission(
				"submission-1",
				state.sessionId,
				state.generation - 1,
				[note],
			),
		).rejects.toThrow("Code review changed");
		await expect(
			service.prepareSubmission(
				"submission-1",
				state.sessionId,
				state.generation,
				[{ ...note, startLine: 2, endLine: 2 }],
			),
		).rejects.toThrow("Review range is no longer present");
	});

	test("rejects a file load completed after the working directory changes", async () => {
		let session = { id: "session-1", cwd: "/repo" };
		let listener: ((event: AgentRuntimeEvent) => void) | undefined;
		let finishLoad: ((files: ReviewFile[]) => void) | undefined;
		const service = new RemoteReviewService(
			{
				getMessages: () => [],
				getSession: () => session,
				subscribe: (next: (event: AgentRuntimeEvent) => void) => {
					listener = next;
					return () => {};
				},
			} as never,
			{
				loadFiles: () =>
					new Promise((resolve) => {
						finishLoad = resolve;
					}),
				getRepoRoot: (cwd) => cwd,
			},
		);
		const refresh = service.refresh();
		await Bun.sleep(0);
		session = { ...session, cwd: "/other-repo" };
		listener?.({ type: "session.active.changed" } as AgentRuntimeEvent);
		finishLoad?.([reviewFile()]);

		await expect(refresh).rejects.toThrow("Active session changed");
	});

	test("invalidates review state when the working directory changes", async () => {
		const { service, changeCwd } = harness();
		const state = await service.refresh();
		changeCwd("/other-repo");

		expect(() =>
			service.assertCurrent(state.sessionId, state.generation),
		).toThrow("Code review changed");
		expect((await service.refresh()).generation).toBeGreaterThan(
			state.generation,
		);
	});

	test("rejects stale clients when the session changes", async () => {
		const { service, switchSession } = harness();
		await service.refresh();
		switchSession("session-2");

		expect(() => service.getFile("session-1", "src/example.ts")).toThrow(
			"Active session changed",
		);
		expect((await service.refresh()).sessionId).toBe("session-2");
	});
});
