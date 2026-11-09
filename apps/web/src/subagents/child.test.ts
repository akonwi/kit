import { describe, expect, test } from "bun:test";
import type {
  SubagentLiveEvent,
  SubagentLiveEventPage,
  SubagentTranscript,
  TranscriptMessage,
} from "../api/types";
import { InvalidPayload } from "../session/projection";
import { ChildConversation, scopedToolId, sessionEvent } from "./child";

function message(
  value: Partial<TranscriptMessage> & Pick<TranscriptMessage, "id" | "role" | "turnId" | "sequence">,
): TranscriptMessage {
  return { content: [], createdAt: "2025-01-01T00:00:00Z", ...value };
}

function page(messages: TranscriptMessage[], extra: Partial<SubagentTranscript> = {}): SubagentTranscript {
  return { conversationId: "c1", messages, ...extra };
}

function events(stream: string, list: SubagentLiveEvent[]): SubagentLiveEventPage {
  return {
    streamId: stream,
    events: list,
    firstSequence: list[0]?.sequence ?? 0,
    lastSequence: list.at(-1)?.sequence ?? 0,
  };
}

const task = message({
  id: "u1",
  role: "user",
  turnId: "t1",
  sequence: 1,
  content: [{ kind: "text", text: "Review the change" }],
});

describe("sessionEvent", () => {
  test("reads a subagent's events as the session projection's", () => {
    expect(
      sessionEvent(
        { kind: "message.text.delta", sequence: 4, turnId: "t1", messageId: "m1", delta: "Hi" },
        "c1",
        "st",
      ),
    ).toEqual({
      sessionId: "c1",
      streamId: "st",
      sequence: 4,
      kind: "assistant.text.delta",
      turnId: "t1",
      messageId: "m1",
      contentIndex: undefined,
      delta: "Hi",
    });
    expect(sessionEvent({ kind: "turn.settled", sequence: 5, turnId: "t1" }, "c1", "st")).toMatchObject({
      kind: "turn.completed",
      status: "completed",
    });
    expect(
      sessionEvent(
        { kind: "tool.started", sequence: 6, turnId: "t2", toolCallId: "call", toolName: "read" },
        "c1",
        "st",
      ),
    ).toMatchObject({ kind: "tool.started", toolCallId: scopedToolId("t2", "call") });
  });

  test("tells apart a call id the provider reuses in another turn", () => {
    expect(scopedToolId("t1", "c1")).not.toBe(scopedToolId("t2", "c1"));
    expect(scopedToolId("t1", "1c")).not.toBe(scopedToolId("t1c", "1"));
  });
});

describe("ChildConversation", () => {
  test("shows the stored transcript with live events on top", () => {
    const child = new ChildConversation("c1");
    child.history(page([task]));
    const accepted = child.accept(
      events("st", [
        { kind: "turn.started", sequence: 1, turnId: "t1" },
        { kind: "message.text.delta", sequence: 2, turnId: "t1", messageId: "m1", delta: "Looking" },
      ]),
      1_000,
    );
    expect(accepted).toEqual({ reset: false, added: 2, boundary: false, first: true });
    child.catchUp();
    expect(child.state?.activeTurnId).toBe("t1");
    expect(child.state?.rows.map((row) => row.kind)).toEqual(["user"]);

    const next = child.accept(
      events("st", [
        { kind: "message.text.delta", sequence: 3, turnId: "t1", messageId: "m1", delta: " closer" },
        { kind: "message.completed", sequence: 4, turnId: "t1", messageId: "m1" },
      ]),
      2_000,
    );
    expect(next).toEqual({ reset: false, added: 2, boundary: true, first: false });
    child.catchUp();
    expect(child.state?.rows.at(-1)).toMatchObject({ kind: "assistant", text: "Looking closer" });
  });

  test("skips events it has, and drops the ones the stored transcript settles", () => {
    const child = new ChildConversation("c1");
    child.history(page([task]));
    const first = events("st", [
      { kind: "turn.started", sequence: 1, turnId: "t1" },
      { kind: "message.text.delta", sequence: 2, turnId: "t1", messageId: "m1", delta: "Done" },
      { kind: "message.completed", sequence: 3, turnId: "t1", messageId: "m1" },
      { kind: "turn.settled", sequence: 4, turnId: "t1" },
    ]);
    expect(child.accept(first, 0)).toMatchObject({ added: 4, boundary: true });
    // The server returns every kept event after the cursor; the ones taken add nothing.
    expect(child.accept(first, 0)).toMatchObject({ reset: false, added: 0 });

    child.history(
      page([
        task,
        message({
          id: "m1",
          role: "assistant",
          turnId: "t1",
          sequence: 2,
          stopReason: "stop",
          content: [{ kind: "text", text: "Done" }],
        }),
      ]),
    );
    expect(child.state?.activeTurnId).toBeUndefined();
    expect(child.state?.rows.filter((row) => row.kind === "assistant")).toHaveLength(1);
  });

  test("starts over on another stream, a request to, or a skipped event", () => {
    const child = new ChildConversation("c1");
    child.history(page([task]));
    child.accept(events("st", [{ kind: "turn.started", sequence: 1, turnId: "t1" }]), 0);
    expect(child.accept(events("other", [{ kind: "turn.started", sequence: 1, turnId: "t2" }]), 0)).toEqual({
      reset: true,
    });
    expect(child.stream).toBe("");

    child.accept(events("st", [{ kind: "turn.started", sequence: 1, turnId: "t1" }]), 0);
    expect(child.accept({ streamId: "st", events: [], resyncRequired: true }, 0)).toEqual({ reset: true });

    child.accept(events("st", [{ kind: "turn.started", sequence: 1, turnId: "t1" }]), 0);
    expect(
      child.accept(
        events("st", [{ kind: "message.text.delta", sequence: 3, turnId: "t1", messageId: "m", delta: "x" }]),
        0,
      ),
    ).toEqual({ reset: true });
  });

  test("refuses malformed pages and events", () => {
    const child = new ChildConversation("c1");
    expect(() =>
      child.accept(
        events("st", [
          { kind: "turn.started", sequence: 1, turnId: "t1" },
          { kind: "turn.settled", sequence: 3, turnId: "t1" },
        ]),
        0,
      ),
    ).toThrow(InvalidPayload);
    expect(() =>
      child.accept(
        events("st", [{ kind: "tool.started", sequence: 1, turnId: "t1", toolCallId: "", toolName: "read" }]),
        0,
      ),
    ).toThrow(InvalidPayload);
    expect(() => child.history({ conversationId: "c2", messages: [] })).toThrow(InvalidPayload);
  });

  test("puts older pages first, and keeps them while the newest page meets them", () => {
    const child = new ChildConversation("c1");
    const reply = (n: number) =>
      message({
        id: `m${n}`,
        role: "assistant",
        turnId: "t1",
        sequence: n,
        stopReason: "stop",
        content: [{ kind: "text", text: `reply ${n}` }],
      });
    child.history(page([reply(3), reply(4)], { hasMoreMessages: true, previousMessageCursor: "p3" }));
    expect(child.before).toBe("p3");
    child.older(page([task, reply(2)], { hasMoreMessages: false }));
    expect(child.before).toBeUndefined();
    expect(() => child.older(page([reply(4)]))).toThrow(InvalidPayload);

    // A newer page that overlaps what was read keeps the older pages.
    child.history(page([reply(4), reply(5)], { hasMoreMessages: true, previousMessageCursor: "p4" }));
    expect(child.state?.rows.map((row) => ("text" in row ? row.text : ""))).toEqual([
      "Review the change",
      "reply 2",
      "reply 3",
      "reply 4",
      "reply 5",
    ]);
    expect(child.before).toBeUndefined();

    // One past a gap replaces them, so nothing goes missing unseen.
    child.history(page([reply(9)], { hasMoreMessages: true, previousMessageCursor: "p9" }));
    expect(child.state?.rows.map((row) => ("text" in row ? row.text : ""))).toEqual(["reply 9"]);
    expect(child.before).toBe("p9");
  });
});
