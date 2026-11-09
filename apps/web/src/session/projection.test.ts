import { describe, expect, test } from "bun:test";
import type { SessionEvent, SessionSnapshot, TranscriptMessage } from "../api/types";
import { Projection, transcript, WORKING } from "./projection";

const info = {
  id: "s1",
  configurationRevision: 1,
  createdAt: "2025-01-01T00:00:00Z",
  updatedAt: "2025-01-01T00:00:00Z",
  cwd: "/repo",
  model: "anthropic/claude",
  thinkingLevel: "medium",
  name: "Fix SSE reconnect",
};

const usage = {
  cacheRead: 0,
  cacheWrite: 0,
  cost: { cacheRead: 0, cacheWrite: 0, input: 0, output: 0, total: 0 },
  input: 0,
  output: 0,
  reasoning: 0,
  totalTokens: 0,
};

function snapshot(messages: TranscriptMessage[], extra: Partial<SessionSnapshot> = {}): SessionSnapshot {
  return {
    session: info,
    messages,
    usage,
    followUps: { count: 0 },
    eventStreamId: "st",
    eventCursor: 10,
    ...extra,
  } as SessionSnapshot;
}

function message(
  value: Partial<TranscriptMessage> & Pick<TranscriptMessage, "id" | "role" | "turnId">,
): TranscriptMessage {
  return { content: [], createdAt: "2025-01-01T00:00:00Z", sequence: 1, ...value };
}

let sequence = 10;
function event(value: Record<string, unknown>): SessionEvent {
  sequence += 1;
  return { sessionId: "s1", streamId: "st", sequence, ...value } as unknown as SessionEvent;
}

describe("transcript", () => {
  test("groups an active turn's tool calls with their results between its prose", () => {
    const rows = transcript(
      [
        message({ id: "u1", role: "user", turnId: "t1", content: [{ kind: "text", text: "Fix it" }] }),
        message({
          id: "a1",
          role: "assistant",
          turnId: "t1",
          content: [
            { kind: "thinking", text: "hmm" },
            { kind: "text", text: "Looking." },
            { kind: "toolCall", toolCallId: "c1", toolName: "read", arguments: '{"path":"a.ts"}' },
            { kind: "toolCall", toolCallId: "c2", toolName: "grep", arguments: '{"pattern":"x"}' },
          ],
        }),
        message({
          id: "r1",
          role: "tool",
          turnId: "t1",
          toolCallId: "c1",
          content: [{ kind: "text", text: "1\n2" }],
        }),
        message({ id: "a2", role: "assistant", turnId: "t1", content: [{ kind: "text", text: "Done." }] }),
      ],
      () => false,
      () => false,
      [],
      "t1",
    );
    expect(rows).toEqual([
      { kind: "user", id: "u1", text: "Fix it", attachments: [], comments: [] },
      { kind: "assistant", id: "a1", text: "Looking.", attachments: [] },
      {
        kind: "tools",
        id: "tools-c1",
        tools: [
          {
            id: "c1",
            name: "read",
            arguments: '{"path":"a.ts"}',
            output: "1\n2",
            failed: false,
            status: "done",
            details: undefined,
            attachments: [],
          },
          {
            id: "c2",
            name: "grep",
            arguments: '{"pattern":"x"}',
            output: "",
            failed: false,
            status: "running",
            details: undefined,
            attachments: [],
          },
        ],
      },
      { kind: "assistant", id: "a2", text: "Done.", attachments: [] },
    ]);
  });

  test("closes a finished turn that failed with its reason", () => {
    const rows = transcript(
      [
        message({ id: "u1", role: "user", turnId: "t1", content: [{ kind: "text", text: "Go" }] }),
        message({
          id: "a1",
          role: "assistant",
          turnId: "t1",
          errorMessage: "Rate limited",
          stopReason: "error",
        }),
      ],
      () => true,
      () => true,
    );
    expect(rows.at(-1)).toEqual({
      kind: "turnEnd",
      id: "turn-end:t1",
      end: "failed",
      message: "Rate limited",
    });
  });
});

describe("Projection", () => {
  test("keeps the image show_image shows with its call", () => {
    const projection = new Projection(snapshot([]));
    projection.apply(event({ kind: "turn.started", turnId: "t1", status: "running" }), 1);
    projection.apply(
      event({
        kind: "tool.completed",
        turnId: "t1",
        toolCallId: "c1",
        toolName: "show_image",
        content: [
          { kind: "text", text: "Image displayed successfully." },
          { kind: "image", attachmentId: "attachment_a", filename: "sent.png", mediaType: "image/png" },
        ],
      }),
      2,
    );
    const row = projection.session.rows[0];
    expect(row?.kind === "tools" ? row.tools.map((tool) => [tool.output, tool.attachments]) : []).toEqual([
      [
        "Image displayed successfully.",
        [{ id: "attachment_a", filename: "sent.png", mediaType: "image/png", image: true }],
      ],
    ]);
  });

  test("fills a live prompt with the attachments its stored message has", () => {
    const projection = new Projection(snapshot([]));
    projection.apply(event({ kind: "turn.started", turnId: "t1", status: "running" }), 1000);
    projection.apply(event({ kind: "user.message.added", turnId: "t1", text: "Same crash" }), 1000);
    const stored = message({
      id: "u1",
      role: "user",
      turnId: "t1",
      content: [
        { kind: "text", text: "Same crash" },
        { kind: "image", attachmentId: "attachment_a", filename: "shot.png", mediaType: "image/png" },
        { kind: "file", attachmentId: "attachment_b", filename: "trace.log", mediaType: "text/plain" },
      ],
    });
    expect(projection.fillUserRow(stored)).toBe(true);
    expect(projection.session.rows).toEqual([
      {
        kind: "user",
        id: "live-user-t1",
        text: "Same crash",
        attachments: [
          { id: "attachment_a", filename: "shot.png", mediaType: "image/png", image: true },
          { id: "attachment_b", filename: "trace.log", mediaType: "text/plain", image: false },
        ],
        comments: [],
      },
    ]);
    expect(projection.fillUserRow({ ...stored, turnId: "t2" })).toBe(false);
  });

  test("adds a live turn's prompt, tools, and reply in order", () => {
    const projection = new Projection(snapshot([]));
    projection.apply(event({ kind: "turn.started", turnId: "t1", status: "running" }), 1000);
    projection.apply(event({ kind: "user.message.added", turnId: "t1", text: "Run the tests" }), 1000);
    projection.apply(event({ kind: "assistant.started", turnId: "t1", messageId: "a1" }), 1000);
    projection.apply(
      event({ kind: "assistant.text.delta", turnId: "t1", messageId: "a1", delta: "Running " }),
      1000,
    );
    projection.apply(
      event({
        kind: "tool.started",
        turnId: "t1",
        toolCallId: "c1",
        toolName: "bash",
        arguments: '{"command":"bun test"}',
      }),
      2000,
    );
    expect(projection.session.status).toBe("running");
    expect(projection.session.activity).toBe(WORKING);
    projection.apply(
      event({
        kind: "tool.completed",
        turnId: "t1",
        toolCallId: "c1",
        toolName: "bash",
        content: [{ kind: "text", text: "ok" }],
      }),
      5000,
    );
    projection.apply(
      event({ kind: "assistant.completed", turnId: "t1", messageId: "a2", text: "All pass." }),
      6000,
    );
    projection.apply(event({ kind: "turn.completed", turnId: "t1", status: "completed" }), 6000);

    expect(projection.session.rows).toEqual([
      { kind: "user", id: "live-user-t1", text: "Run the tests", attachments: [], comments: [] },
      {
        kind: "tools",
        id: "tools-c1",
        tools: [
          {
            id: "c1",
            name: "bash",
            arguments: '{"command":"bun test"}',
            output: "ok",
            failed: false,
            status: "done",
            details: undefined,
            attachments: [],
            startedAt: 2000,
            finishedAt: 5000,
          },
        ],
      },
      { kind: "assistant", id: "a2", text: "All pass.", attachments: [] },
    ]);
    expect(projection.session.status).toBe("idle");
    expect(projection.session.activity).toBeUndefined();
  });

  test("keeps unchanged rows by identity across events", () => {
    const projection = new Projection(
      snapshot([message({ id: "u1", role: "user", turnId: "t0", content: [{ kind: "text", text: "Hi" }] })]),
    );
    const first = projection.session.rows[0];
    projection.apply(event({ kind: "user.message.added", turnId: "t1", text: "Next" }), 1);
    expect(projection.session.rows[0]).toBe(first);
  });

  test("shows an interaction for the active turn and waits for the user", () => {
    const projection = new Projection(snapshot([], { activeTurnId: "t1" }));
    const interaction = {
      id: "i1",
      kind: "confirm",
      title: "Run this command?",
      createdAt: "",
      sessionId: "s1",
      turnId: "t1",
    };
    projection.apply(event({ kind: "interaction.requested", turnId: "t1", interaction }), 1);
    expect(projection.session.status).toBe("awaitingResponse");
    expect(projection.session.pendingInteractions).toEqual([interaction]);
    projection.apply(
      event({
        kind: "interaction.resolved",
        turnId: "t1",
        interactionId: "i1",
        interactionResolution: "confirmed",
      }),
      2,
    );
    expect(projection.session.status).toBe("running");
    expect(projection.session.pendingInteractions).toEqual([]);
  });

  test("closes a stopped turn and marks its planned calls not run", () => {
    const projection = new Projection(snapshot([], { activeTurnId: "t1" }));
    projection.apply(
      event({ kind: "tool.planned", turnId: "t1", messageId: "a1", toolCallId: "c1", toolName: "edit" }),
      1,
    );
    projection.apply(
      event({ kind: "turn.completed", turnId: "t1", status: "aborted", errorMessage: "Stopped" }),
      2,
    );
    const rows = projection.session.rows;
    expect(rows[0]).toMatchObject({ kind: "tools", tools: [{ id: "c1", status: "notRun" }] });
    expect(rows[1]).toEqual({ kind: "turnEnd", id: "turn-end:t1", end: "stopped", message: "Stopped" });
  });

  test("shows a scheduled provider retry until it starts", () => {
    const projection = new Projection(snapshot([], { activeTurnId: "t1" }));
    const retry = { count: 2, retryAt: "2025-01-01T00:00:05Z" };
    projection.apply(event({ kind: "provider.retry.scheduled", turnId: "t1", providerRetry: retry }), 1);
    expect(projection.session).toMatchObject({ retryAt: "2025-01-01T00:00:05Z", retryCount: 2 });

    projection.apply(event({ kind: "provider.retry.started", turnId: "t1", providerRetry: retry }), 2);
    expect(projection.session).toMatchObject({ retryAt: undefined, retryCount: undefined });
  });

  test("ignores an older configuration than the one shown", () => {
    const projection = new Projection(snapshot([]));
    projection.apply(
      event({
        kind: "session.configuration.changed",
        configuration: { configurationRevision: 3, model: "openai/gpt-5", thinkingLevel: "high" },
      }),
    );
    projection.apply(
      event({
        kind: "session.configuration.changed",
        configuration: { configurationRevision: 2, model: "old", thinkingLevel: "low" },
      }),
    );
    expect(projection.session.info.model).toBe("openai/gpt-5");
    expect(projection.session.info.thinkingLevel).toBe("high");
  });
});

describe("comments", () => {
  const anchor = {
    kind: "working_tree_diff",
    workingTreeDiff: {
      targetId: "difftarget_A",
      targetRevision: "diffrev_A",
      path: "src/watch.ts",
      fileRevision: "diff_file_A",
      side: "new",
      startLine: 89,
      endLine: 91,
    },
  };
  const annotation = (id: number, body: string, extra: Record<string, unknown> = {}) => ({
    id,
    sessionId: "s1",
    anchor,
    body,
    preview: { startLine: 89, endLine: 91, text: "a\nb\nc\n" },
    ...extra,
  });
  const summary = (id: number, extra: Record<string, unknown> = {}) => ({
    id,
    anchor,
    bodyPreview: `Start of ${id}`,
    preview: "a",
    ...extra,
  });

  test("starts from the snapshot's comments, which carry only their start", () => {
    const projection = new Projection(
      snapshot([], { annotations: [summary(2), summary(1, { stale: true, staleReason: "file_changed" })] }),
    );
    expect(
      projection.session.comments.map((value) => [value.id, value.body, value.complete, value.stale]),
    ).toEqual([
      [1, "Start of 1", false, true],
      [2, "Start of 2", false, false],
    ]);
  });

  test("follows comments made, edited, deleted, and sent", () => {
    const projection = new Projection(snapshot([], { annotations: [summary(1, { stale: true })] }));
    projection.apply(
      event({ kind: "annotation.created", annotationId: 2, annotation: annotation(2, "Why?") }),
    );
    projection.apply(
      event({ kind: "annotation.created", annotationId: 3, annotation: annotation(3, "And this?") }),
    );
    // An edit keeps the stale mark the server gave before.
    projection.apply(
      event({ kind: "annotation.updated", annotationId: 1, annotation: annotation(1, "Edited") }),
    );
    expect(
      projection.session.comments.map((value) => [value.id, value.body, value.complete, value.stale]),
    ).toEqual([
      [1, "Edited", true, true],
      [2, "Why?", true, false],
      [3, "And this?", true, false],
    ]);
    projection.apply(event({ kind: "annotation.deleted", annotationId: 1 }));
    projection.apply(event({ kind: "annotation.submitted", annotationIds: [2], acceptedMessageId: "m1" }));
    expect(projection.session.comments.map((value) => value.id)).toEqual([3]);
  });

  test("a list read across a comment event keeps the comments it doesn't name", () => {
    const projection = new Projection(snapshot([], { annotations: [summary(1), summary(2)] }));
    const mark = projection.commentMark;
    projection.apply(
      event({ kind: "annotation.created", annotationId: 3, annotation: annotation(3, "New") }),
    );
    projection.setComments([annotation(1, "Whole", { stale: true, staleReason: "target_changed" })], mark);
    expect(projection.session.comments.map((value) => [value.id, value.body, value.stale])).toEqual([
      [1, "Whole", true],
      [2, "Start of 2", false],
      [3, "New", false],
    ]);
    // An edit made after the list was asked for keeps its words.
    const before = projection.commentMark;
    projection.apply(
      event({ kind: "annotation.updated", annotationId: 2, annotation: annotation(2, "Newer") }),
    );
    projection.setComments([annotation(2, "Older"), annotation(3, "New")], before);
    expect(projection.session.comments.find((value) => value.id === 2)?.body).toBe("Newer");
    projection.setComments([annotation(3, "New")], projection.commentMark);
    expect(projection.session.comments.map((value) => value.id)).toEqual([3]);
  });

  test("shows the comments a message carried, and fills them into its live row", () => {
    const sent = {
      anchor,
      body: "Why?",
      originalAnnotationId: 2,
      preview: { startLine: 89, endLine: 91, text: "a\n" },
    };
    const rows = transcript([
      message({
        id: "u1",
        role: "user",
        turnId: "t1",
        content: [
          { kind: "text", text: "Look" },
          { kind: "annotations", annotations: [sent] },
        ],
      }),
    ]);
    expect(rows).toEqual([{ kind: "user", id: "u1", text: "Look", attachments: [], comments: [sent] }]);

    const projection = new Projection(snapshot([]));
    projection.apply(event({ kind: "user.message.added", turnId: "t2", text: "" }));
    const stored = message({
      id: "u2",
      role: "user",
      turnId: "t2",
      content: [{ kind: "annotations", annotations: [sent] }],
    });
    expect(projection.fillUserRow(stored)).toBe(true);
    expect(projection.session.rows).toEqual([
      { kind: "user", id: "live-user-t2", text: "", attachments: [], comments: [sent] },
    ]);
  });
});
