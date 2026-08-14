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
