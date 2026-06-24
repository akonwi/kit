import { describe, expect, test } from "bun:test";
import type { Session } from "../../session";
import { SESSION_VERSION } from "../../session";
import { createScratchpadController } from "./controller";

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

type FakeSessionEvent =
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
	let listener: ((event: FakeSessionEvent) => void) | undefined;
	let subscribedEvent: string | undefined;
	const contextUpdates: string[] = [];
	function publish(event: FakeSessionEvent): void {
		if (subscribedEvent === event.type) listener?.(event);
	}
	return {
		runtime: {
			getSession: () => current,
			setScratchpadContent: (content: string) => {
				contextUpdates.push(content);
			},
			subscribe: (eventName: string, nextListener: typeof listener) => {
				subscribedEvent = eventName;
				listener = nextListener;
				return () => {
					listener = undefined;
					subscribedEvent = undefined;
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
