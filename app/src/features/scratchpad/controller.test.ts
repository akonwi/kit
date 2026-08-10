import { describe, expect, test } from "bun:test";
import type { Session } from "../../session";
import { SESSION_VERSION } from "../../session";
import type { FileOperationHandler } from "../../tools";
import { createScratchpadController } from "./controller";
import { scratchpadPath } from "./storage";

function session(id: string, parentSessionId?: string): Session {
	return {
		id,
		version: SESSION_VERSION,
		cwd: "/tmp/project",
		parentSessionId,
		createdAt: "2026-01-01T00:00:00.000Z",
		updatedAt: "2026-01-01T00:00:00.000Z",
		turns: [],
	};
}

type FakeEvent =
	| { type: "session.active.changed"; session: Session }
	| {
			type: "session.active.changed.cwd";
			session: Session;
			cwd: string;
			previousCwd: string;
			source: "user";
	  };

function createFakeRuntime(initial: Session) {
	let current = initial;
	const listeners = new Map<string, Set<(event: FakeEvent) => void>>();
	const contextUpdates: string[] = [];
	let fileHandler: FileOperationHandler | undefined;
	function publish(event: FakeEvent): void {
		for (const listener of listeners.get(event.type) ?? []) listener(event);
	}
	return {
		runtime: {
			getSession: () => current,
			setScratchpadContent: (content: string) => {
				contextUpdates.push(content);
			},
			subscribe: (eventName: string, listener: (event: FakeEvent) => void) => {
				const eventListeners = listeners.get(eventName) ?? new Set();
				eventListeners.add(listener);
				listeners.set(eventName, eventListeners);
				return () => eventListeners.delete(listener);
			},
			registerFileOperationHandler: (handler: FileOperationHandler) => {
				fileHandler = handler;
				return () => {
					if (fileHandler === handler) fileHandler = undefined;
				};
			},
		},
		contextUpdates,
		switchSession(next: Session) {
			current = next;
			publish({ type: "session.active.changed", session: next });
		},
		emitCwdChange(next = current) {
			publish({
				type: "session.active.changed.cwd",
				session: next,
				cwd: next.cwd,
				previousCwd: "/tmp/previous",
				source: "user",
			});
		},
		fileOperations() {
			if (!fileHandler) throw new Error("File handler not registered");
			return fileHandler;
		},
	};
}

describe("createScratchpadController", () => {
	test("updates agent context as the draft is edited", () => {
		const fake = createFakeRuntime(session("parent"));
		const files = new Map([["parent", "saved notes"]]);
		const controller = createScratchpadController(fake.runtime as never, {
			read: (id) => files.get(id) ?? "",
			write: (id, content) => files.set(id, content),
		});

		controller.enterEdit();
		controller.setDraft("live draft");

		expect(fake.contextUpdates.at(-1)).toBe("live draft");
		expect(controller.content()).toBe("live draft");
		expect(files.get("parent")).toBe("saved notes");
		controller.dispose();
	});

	test("flushes pending autosaves", () => {
		const fake = createFakeRuntime(session("parent"));
		const files = new Map([["parent", "saved notes"]]);
		const controller = createScratchpadController(fake.runtime as never, {
			read: (id) => files.get(id) ?? "",
			write: (id, content) => files.set(id, content),
		});

		controller.enterEdit();
		controller.setDraft("live draft");
		controller.flushAutosave();

		expect(files.get("parent")).toBe("live draft");
		expect(controller.dirty()).toBe(false);
	});

	test("ignores cwd change events for the active session", () => {
		const fake = createFakeRuntime(session("parent"));
		const files = new Map([["parent", "saved notes"]]);
		let writes = 0;
		const controller = createScratchpadController(fake.runtime as never, {
			read: (id) => files.get(id) ?? "",
			write: (id, content) => {
				writes += 1;
				files.set(id, content);
			},
		});

		controller.enterEdit();
		controller.setDraft("live draft");
		fake.emitCwdChange();

		expect(controller.draft()).toBe("live draft");
		expect(controller.content()).toBe("live draft");
		expect(files.get("parent")).toBe("saved notes");
		expect(writes).toBe(0);
		controller.dispose();
	});

	test("does not persist a clean editor when switching sessions", () => {
		const fake = createFakeRuntime(session("parent"));
		const files = new Map([
			["parent", "saved notes"],
			["other", "other notes"],
		]);
		let writes = 0;
		const controller = createScratchpadController(fake.runtime as never, {
			read: (id) => files.get(id) ?? "",
			write: (id, content) => {
				writes += 1;
				files.set(id, content);
			},
		});

		controller.enterEdit();
		files.set("parent", "external notes");
		fake.switchSession(session("other"));

		expect(writes).toBe(0);
		expect(files.get("parent")).toBe("external notes");
		expect(controller.content()).toBe("other notes");
	});

	test("flushes a pending debounced save to the previous session on switch", () => {
		const fake = createFakeRuntime(session("parent"));
		const files = new Map([
			["parent", "saved notes"],
			["other", "other notes"],
		]);
		const controller = createScratchpadController(fake.runtime as never, {
			read: (id) => files.get(id) ?? "",
			write: (id, content) => files.set(id, content),
		});

		controller.enterEdit();
		controller.setDraft("live draft");
		fake.switchSession(session("other"));

		expect(files.get("parent")).toBe("live draft");
		expect(controller.content()).toBe("other notes");
		expect(controller.dirty()).toBe(false);
	});

	test("preserves failed autosaves in memory across session switches", () => {
		const fake = createFakeRuntime(session("parent"));
		const files = new Map([
			["parent", "saved notes"],
			["other", "other notes"],
		]);
		let failParentWrites = true;
		const controller = createScratchpadController(fake.runtime as never, {
			read: (id) => files.get(id) ?? "",
			write: (id, content) => {
				if (id === "parent" && failParentWrites) throw new Error("disk full");
				files.set(id, content);
			},
		});

		controller.enterEdit();
		controller.setDraft("unsaved draft");
		expect(controller.autosaveDraft()).toBe(false);
		expect(controller.editing()).toBe(true);
		expect(controller.dirty()).toBe(true);
		fake.switchSession(session("other"));
		fake.switchSession(session("parent"));

		expect(controller.content()).toBe("unsaved draft");
		expect(files.get("parent")).toBe("saved notes");
		expect(controller.dirty()).toBe(true);

		failParentWrites = false;
		controller.flushAutosave();
		expect(files.get("parent")).toBe("unsaved draft");
		expect(controller.dirty()).toBe(false);
	});

	test("flushes user edits and applies normal tool changes atomically", async () => {
		const fake = createFakeRuntime(session("parent"));
		const files = new Map([["parent", "saved notes"]]);
		const controller = createScratchpadController(fake.runtime as never, {
			read: (id) => files.get(id) ?? "",
			write: (id, content) => files.set(id, content),
			mutate: (id, update) => {
				const current = files.get(id) ?? "";
				const next = update(current);
				if (next === null || next === current) {
					return { updated: false, content: current };
				}
				files.set(id, next);
				return { updated: true, content: next };
			},
		});
		const filePath = scratchpadPath("parent");
		const changes: Array<{ sessionId: string; content: string }> = [];
		controller.subscribe((snapshot) => changes.push(snapshot));

		controller.enterEdit();
		controller.setDraft("user draft");
		const result = await fake
			.fileOperations()
			.mutate(filePath, (current) => `${current}\nagent edit`);

		expect(result.content).toBe("user draft\nagent edit");
		expect(files.get("parent")).toBe("user draft\nagent edit");
		expect(controller.content()).toBe("user draft\nagent edit");
		expect(controller.draft()).toBe("user draft\nagent edit");
		expect(controller.dirty()).toBe(false);
		expect(fake.contextUpdates.at(-1)).toBe("user draft\nagent edit");
		expect(changes.at(-1)).toEqual({
			sessionId: "parent",
			content: "user draft\nagent edit",
		});
	});

	test("preserves conflicting panel edits instead of overwriting the file", () => {
		const fake = createFakeRuntime(session("parent"));
		const files = new Map([["parent", "saved notes"]]);
		const controller = createScratchpadController(fake.runtime as never, {
			read: (id) => files.get(id) ?? "",
			write: (id, content) => files.set(id, content),
			mutate: (id, update) => {
				const current = files.get(id) ?? "";
				const next = update(current);
				if (next === null || next === current) {
					return { updated: false, content: current };
				}
				files.set(id, next);
				return { updated: true, content: next };
			},
		});

		controller.enterEdit();
		controller.setDraft("user draft");
		files.set("parent", "external notes");

		expect(controller.flushAutosave()).toBe(false);
		expect(files.get("parent")).toBe("external notes");
		expect(controller.draft()).toBe("user draft");
		expect(controller.dirty()).toBe(true);

		fake.switchSession(session("other"));
		fake.switchSession(session("parent"));
		expect(controller.flushAutosave()).toBe(false);
		expect(files.get("parent")).toBe("external notes");
		expect(controller.draft()).toBe("user draft");
		expect(controller.dirty()).toBe(true);
	});

	test("reads external changes without rewriting a clean editor draft", async () => {
		const fake = createFakeRuntime(session("parent"));
		const files = new Map([["parent", "saved notes"]]);
		let writes = 0;
		const controller = createScratchpadController(fake.runtime as never, {
			read: (id) => files.get(id) ?? "",
			readForTool: (id) => files.get(id) ?? "",
			write: (id, content) => {
				writes += 1;
				files.set(id, content);
			},
		});

		controller.enterEdit();
		files.set("parent", "external notes");
		const content = await fake.fileOperations().read(scratchpadPath("parent"));

		expect(content).toBe("external notes");
		expect(writes).toBe(0);
		expect(controller.content()).toBe("external notes");
		expect(controller.draft()).toBe("external notes");
		expect(controller.editing()).toBe(true);
		expect(fake.contextUpdates.at(-1)).toBe("external notes");
	});

	test("preserves a pending draft when pre-operation persistence fails", () => {
		const fake = createFakeRuntime(session("parent"));
		let mutations = 0;
		const controller = createScratchpadController(fake.runtime as never, {
			read: () => "saved notes",
			write: () => {
				throw new Error("disk full");
			},
			mutate: () => {
				mutations += 1;
				throw new Error("disk full");
			},
		});

		controller.enterEdit();
		controller.setDraft("unsaved draft");

		expect(() =>
			fake
				.fileOperations()
				.mutate(scratchpadPath("parent"), () => "agent edit"),
		).toThrow("Could not save pending scratchpad edits");
		expect(mutations).toBe(1);
		expect(controller.draft()).toBe("unsaved draft");
		expect(controller.dirty()).toBe(true);
	});

	test("syncs clean controller state after an atomic update is rejected", () => {
		const fake = createFakeRuntime(session("parent"));
		const controller = createScratchpadController(fake.runtime as never, {
			read: () => "saved notes",
			write: () => {},
			mutate: () => ({ updated: false, content: "external notes" }),
		});

		const result = controller.applyAtomicUpdate("parent", () => null);

		expect(result).toEqual({ updated: false, content: "external notes" });
		expect(controller.content()).toBe("external notes");
		expect(controller.draft()).toBe("external notes");
		expect(fake.contextUpdates.at(-1)).toBe("external notes");
	});

	test("copies current scratchpad content into forked child sessions", () => {
		const fake = createFakeRuntime(session("parent"));
		const files = new Map([["parent", "parent notes"]]);
		const controller = createScratchpadController(fake.runtime as never, {
			read: (id) => files.get(id) ?? "",
			write: (id, content) => files.set(id, content),
		});

		fake.switchSession(session("child", "parent"));

		expect(files.get("child")).toBe("parent notes");
		expect(controller.content()).toBe("parent notes");
		expect(fake.contextUpdates.at(-1)).toBe("parent notes");
	});

	test("fork transfer uses the live draft when editing", () => {
		const fake = createFakeRuntime(session("parent"));
		const files = new Map([["parent", "saved notes"]]);
		const controller = createScratchpadController(fake.runtime as never, {
			read: (id) => files.get(id) ?? "",
			write: (id, content) => files.set(id, content),
		});
		controller.enterEdit();
		controller.setDraft("draft notes");

		fake.switchSession(session("child", "parent"));

		expect(files.get("parent")).toBe("draft notes");
		expect(files.get("child")).toBe("draft notes");
		expect(controller.content()).toBe("draft notes");
		expect(controller.editing()).toBe(false);
	});
});
