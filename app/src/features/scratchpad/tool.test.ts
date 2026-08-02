import { describe, expect, test } from "bun:test";
import {
	buildScratchpadUpdate,
	createUpdateScratchpadTool,
	MAX_SCRATCHPAD_LENGTH,
} from "./tool";

function createTool(options: {
	current?: string;
	approved?: boolean;
	lifecycleSignal?: AbortSignal;
	onConfirm?: (input: { signal?: AbortSignal }) => void;
}) {
	let content = options.current ?? "";
	let persisted = content;
	let sessionId = "session-1";
	const saves: string[] = [];
	const tool = createUpdateScratchpadTool({
		controller: {
			content: () => content,
			dirty: () => false,
			sessionId: () => sessionId,
			applyAtomicUpdate: (targetSessionId, update) => {
				if (targetSessionId !== sessionId) return null;
				const next = update(persisted);
				if (next === null || next === persisted) {
					return { updated: false, content: persisted };
				}
				persisted = next;
				content = next;
				saves.push(next);
				return { updated: true, content: next };
			},
		},
		lifecycleSignal: options.lifecycleSignal,
		ui: {
			confirm: async (input) => {
				options.onConfirm?.(input);
				return options.approved ?? false;
			},
		},
	});
	return {
		tool,
		saves,
		content: () => content,
		setContent: (next: string) => {
			content = next;
			persisted = next;
		},
		setPersisted: (next: string) => {
			persisted = next;
		},
		setSessionId: (next: string) => {
			sessionId = next;
		},
	};
}

describe("update_scratchpad", () => {
	test("appends approved content with a readable separator", async () => {
		let confirmation: unknown;
		const harness = createTool({
			current: "Existing notes",
			approved: true,
			onConfirm: (input) => {
				confirmation = input;
			},
		});

		const result = await harness.tool.execute("call", {
			content: "Remember this",
		});

		expect(harness.saves).toEqual(["Existing notes\n\nRemember this"]);
		expect(confirmation).toMatchObject({
			title: "Update scratchpad?",
			confirmLabel: "Update",
			defaultValue: false,
		});
		expect(result.details).toEqual({ mode: "append", status: "updated" });
	});

	test("replaces the complete scratchpad after approval", async () => {
		const harness = createTool({ current: "Old", approved: true });

		await harness.tool.execute("call", {
			content: "New complete notes",
			mode: "replace",
		});

		expect(harness.content()).toBe("New complete notes");
	});

	test("does not mutate the scratchpad when approval is declined", async () => {
		const harness = createTool({ current: "Keep me", approved: false });

		const result = await harness.tool.execute("call", {
			content: "Do not add",
		});

		expect(harness.saves).toHaveLength(0);
		expect(harness.content()).toBe("Keep me");
		expect(result.details).toEqual({ mode: "append", status: "declined" });
	});

	test("rebases an approved append onto a concurrent user edit", async () => {
		let harness: ReturnType<typeof createTool>;
		harness = createTool({
			current: "Original",
			approved: true,
			onConfirm: () => harness.setContent("User edit"),
		});

		await harness.tool.execute("call", { content: "Agent note" });

		expect(harness.content()).toBe("User edit\n\nAgent note");
	});

	test("rebases an append onto a concurrent external-process edit", async () => {
		let harness: ReturnType<typeof createTool>;
		harness = createTool({
			current: "Original",
			approved: true,
			onConfirm: () => harness.setPersisted("External edit"),
		});

		await harness.tool.execute("call", { content: "Agent note" });

		expect(harness.content()).toBe("External edit\n\nAgent note");
	});

	test("rejects a stale replacement after a concurrent edit", async () => {
		let harness: ReturnType<typeof createTool>;
		harness = createTool({
			current: "Original",
			approved: true,
			onConfirm: () => harness.setContent("User edit"),
		});

		const result = await harness.tool.execute("call", {
			content: "Replacement",
			mode: "replace",
		});

		expect(harness.saves).toHaveLength(0);
		expect(harness.content()).toBe("User edit");
		expect(result.details).toEqual({ mode: "replace", status: "stale" });
	});

	test("rejects replacement after a concurrent external-process edit", async () => {
		let harness: ReturnType<typeof createTool>;
		harness = createTool({
			current: "Original",
			approved: true,
			onConfirm: () => harness.setPersisted("External edit"),
		});

		const result = await harness.tool.execute("call", {
			content: "Replacement",
			mode: "replace",
		});

		expect(harness.saves).toHaveLength(0);
		expect(result.details).toEqual({ mode: "replace", status: "stale" });
	});

	test("cancels when the confirmation closes during a session change", async () => {
		let harness: ReturnType<typeof createTool>;
		harness = createTool({
			current: "Original",
			approved: false,
			onConfirm: () => harness.setSessionId("session-2"),
		});

		const result = await harness.tool.execute("call", {
			content: "Agent note",
		});

		expect(harness.saves).toHaveLength(0);
		expect(result.details).toEqual({ mode: "append", status: "cancelled" });
	});

	test("does not save if the turn aborts during approval", async () => {
		const abort = new AbortController();
		const harness = createTool({
			current: "Original",
			approved: true,
			onConfirm: () => abort.abort(),
		});

		const result = await harness.tool.execute(
			"call",
			{ content: "Agent note" },
			abort.signal,
		);

		expect(harness.saves).toHaveLength(0);
		expect(result.details).toEqual({ mode: "append", status: "cancelled" });
	});

	test("rejects updates that exceed the scratchpad size limit", async () => {
		let confirmations = 0;
		const harness = createTool({
			current: "Existing",
			onConfirm: () => {
				confirmations += 1;
			},
		});

		const result = await harness.tool.execute("call", {
			content: "x".repeat(MAX_SCRATCHPAD_LENGTH),
		});

		expect(confirmations).toBe(0);
		expect(result.details).toEqual({
			mode: "append",
			status: "too_large",
		});
	});

	test("does not save if the plugin lifecycle ends during approval", async () => {
		const lifecycle = new AbortController();
		const harness = createTool({
			current: "Original",
			approved: true,
			lifecycleSignal: lifecycle.signal,
			onConfirm: () => lifecycle.abort(),
		});

		const result = await harness.tool.execute("call", {
			content: "Agent note",
		});

		expect(harness.saves).toHaveLength(0);
		expect(result.details).toEqual({ mode: "append", status: "cancelled" });
	});

	test("skips approval for an empty append", async () => {
		let confirmations = 0;
		const harness = createTool({
			current: "Keep me",
			onConfirm: () => {
				confirmations += 1;
			},
		});

		const result = await harness.tool.execute("call", { content: "  " });

		expect(confirmations).toBe(0);
		expect(result.details).toEqual({ mode: "append", status: "unchanged" });
	});
});

describe("buildScratchpadUpdate", () => {
	test("preserves existing trailing newlines when appending", () => {
		expect(buildScratchpadUpdate("Notes\n", "More", "append")).toBe(
			"Notes\n\nMore",
		);
		expect(buildScratchpadUpdate("Notes\n\n", "More", "append")).toBe(
			"Notes\n\nMore",
		);
	});

	test("supports clearing through replace mode", () => {
		expect(buildScratchpadUpdate("Notes", "", "replace")).toBe("");
	});
});
