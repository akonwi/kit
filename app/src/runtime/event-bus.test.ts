import { describe, expect, mock, test } from "bun:test";
import { EventBus } from "./event-bus";

type TestEventMap = {
	"agent.started": { turnId: string };
	"agent.completed": { turnId: string; result: string };
	"session.changed": { sessionId: string };
	"session.compaction.started": { percent: number };
	"session.compaction.completed": { percent: number; kept: number };
};

describe("EventBus", () => {
	test("wildcard subscriber receives all events", () => {
		const bus = new EventBus<TestEventMap>();
		const received: string[] = [];
		bus.subscribe((event) => received.push(event.type));

		bus.publish("agent.started", { turnId: "t1" });
		bus.publish("session.changed", { sessionId: "s1" });

		expect(received).toEqual(["agent.started", "session.changed"]);
	});

	test("exact subscriber receives only matching events", () => {
		const bus = new EventBus<TestEventMap>();
		const received: string[] = [];
		bus.subscribe("agent.started", (event) => {
			received.push(event.turnId);
		});

		bus.publish("agent.started", { turnId: "t1" });
		bus.publish("agent.completed", { turnId: "t2", result: "ok" });
		bus.publish("session.changed", { sessionId: "s1" });

		expect(received).toEqual(["t1"]);
	});

	test("prefix subscriber receives events matching the prefix", () => {
		const bus = new EventBus<TestEventMap>();
		const received: string[] = [];
		bus.subscribe({ prefix: "session.compaction" }, (event) => {
			received.push(event.type);
		});

		bus.publish("agent.started", { turnId: "t1" });
		bus.publish("session.changed", { sessionId: "s1" });
		bus.publish("session.compaction.started", { percent: 80 });
		bus.publish("session.compaction.completed", { percent: 80, kept: 3 });

		expect(received).toEqual([
			"session.compaction.started",
			"session.compaction.completed",
		]);
	});

	test("unsubscribe stops delivery", () => {
		const bus = new EventBus<TestEventMap>();
		const listener = mock(() => {});
		const unsub = bus.subscribe("agent.started", listener);

		bus.publish("agent.started", { turnId: "t1" });
		expect(listener).toHaveBeenCalledTimes(1);

		unsub();
		bus.publish("agent.started", { turnId: "t2" });
		expect(listener).toHaveBeenCalledTimes(1);
	});

	test("unsubscribe works for prefix listeners", () => {
		const bus = new EventBus<TestEventMap>();
		const listener = mock(() => {});
		const unsub = bus.subscribe({ prefix: "session" }, listener);

		bus.publish("session.changed", { sessionId: "s1" });
		expect(listener).toHaveBeenCalledTimes(1);

		unsub();
		bus.publish("session.changed", { sessionId: "s2" });
		expect(listener).toHaveBeenCalledTimes(1);
	});

	test("dispose clears all listeners", () => {
		const bus = new EventBus<TestEventMap>();
		const wildcard = mock(() => {});
		const exact = mock(() => {});
		const prefix = mock(() => {});

		bus.subscribe(wildcard);
		bus.subscribe("agent.started", exact);
		bus.subscribe({ prefix: "session" }, prefix);

		bus.publish("agent.started", { turnId: "t1" });
		expect(wildcard).toHaveBeenCalledTimes(1);
		expect(exact).toHaveBeenCalledTimes(1);

		bus.dispose();

		bus.publish("agent.started", { turnId: "t2" });
		bus.publish("session.changed", { sessionId: "s1" });
		expect(wildcard).toHaveBeenCalledTimes(1);
		expect(exact).toHaveBeenCalledTimes(1);
		expect(prefix).toHaveBeenCalledTimes(0);
	});

	test("multiple exact subscribers on the same event type", () => {
		const bus = new EventBus<TestEventMap>();
		const a = mock(() => {});
		const b = mock(() => {});

		bus.subscribe("agent.started", a);
		bus.subscribe("agent.started", b);

		bus.publish("agent.started", { turnId: "t1" });
		expect(a).toHaveBeenCalledTimes(1);
		expect(b).toHaveBeenCalledTimes(1);
	});

	test("published event includes type and payload", () => {
		const bus = new EventBus<TestEventMap>();
		let captured: unknown = null;
		bus.subscribe("agent.completed", (event) => {
			captured = event;
		});

		bus.publish("agent.completed", { turnId: "t1", result: "done" });

		expect(captured).toEqual({
			type: "agent.completed",
			turnId: "t1",
			result: "done",
		});
	});
});
