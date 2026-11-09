import { describe, expect, test } from "bun:test";
import { ApiError, NetworkError } from "../api/client";
import type { Scratchpad } from "../api/types";
import {
  AUTOSAVE_MS,
  compareCopies,
  type DraftStore,
  forgetDrafts,
  laterRevision,
  localDrafts,
  Pad,
  type PadServer,
} from "./pad";

const OWNER = "session_owner";

function copy(content: string, revision: string, owner = OWNER): Scratchpad {
  return { ownerSessionId: owner, content, revision, updatedAt: "2025-11-06T10:00:00Z" };
}

/** A server that answers from a list and records what was put. */
function fakeServer(initial: Scratchpad) {
  let current = initial;
  const puts: { content: string; expectedRevision: string; keepalive: boolean }[] = [];
  let fail: unknown;
  let hold: Promise<void> | undefined;
  const server: PadServer = {
    get: async () => current,
    async put(content, expectedRevision, keepalive) {
      puts.push({ content, expectedRevision, keepalive });
      if (hold) await hold;
      if (fail) throw fail;
      if (expectedRevision !== current.revision) {
        throw new ApiError(409, "scratchpad_revision_conflict", "The scratchpad changed.", {
          scratchpad: current,
        });
      }
      current = copy(content, String(Number(current.revision) + 1));
      return current;
    },
  };
  return {
    server,
    puts,
    set current(value: Scratchpad) {
      current = value;
    },
    failWith(error: unknown) {
      fail = error;
    },
    holdWith(promise: Promise<void> | undefined) {
      hold = promise;
    },
  };
}

/** A schedule the test runs by hand. */
function manualTimers() {
  const pending: { run: () => void; ms: number; cancelled: boolean }[] = [];
  return {
    schedule(run: () => void, ms: number) {
      const entry = { run, ms, cancelled: false };
      pending.push(entry);
      return () => {
        entry.cancelled = true;
      };
    },
    /** The delays still waiting. */
    waiting: () => pending.filter((entry) => !entry.cancelled).map((entry) => entry.ms),
    fire() {
      const live = pending.filter((entry) => !entry.cancelled);
      pending.length = 0;
      for (const entry of live) entry.run();
    },
  };
}

async function settle() {
  for (let i = 0; i < 5; i += 1) await Promise.resolve();
}

describe("Pad", () => {
  test("loads the shared copy as saved", async () => {
    const fake = fakeServer(copy("# Notes\n", "3"));
    const pad = new Pad(fake.server, manualTimers().schedule, () => {});
    expect(pad.status).toEqual({ kind: "loading" });
    await pad.load();
    expect(pad.draft).toBe("# Notes\n");
    expect(pad.status).toEqual({ kind: "saved" });
  });

  test("saves an edit a moment after typing stops", async () => {
    const fake = fakeServer(copy("# Notes\n", "3"));
    const timers = manualTimers();
    const pad = new Pad(fake.server, timers.schedule, () => {});
    await pad.load();
    pad.edit("# Notes\n- a");
    pad.edit("# Notes\n- ab");
    expect(pad.status).toEqual({ kind: "unsaved" });
    expect(timers.waiting()).toEqual([AUTOSAVE_MS]);
    timers.fire();
    await settle();
    expect(fake.puts).toEqual([{ content: "# Notes\n- ab", expectedRevision: "3", keepalive: false }]);
    expect(pad.status).toEqual({ kind: "saved" });
    expect(pad.shared?.revision).toBe("4");
  });

  test("typing back to the shared copy is saved without a request", async () => {
    const fake = fakeServer(copy("a", "1"));
    const timers = manualTimers();
    const pad = new Pad(fake.server, timers.schedule, () => {});
    await pad.load();
    pad.edit("ab");
    pad.edit("a");
    expect(pad.status).toEqual({ kind: "saved" });
    expect(timers.waiting()).toEqual([]);
  });

  test("an edit made while saving is saved after", async () => {
    const fake = fakeServer(copy("a", "1"));
    const timers = manualTimers();
    const pad = new Pad(fake.server, timers.schedule, () => {});
    await pad.load();
    let release = () => {};
    fake.holdWith(
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );
    pad.edit("ab");
    const saving = pad.save();
    expect(pad.status).toEqual({ kind: "saving" });
    pad.edit("abc");
    release();
    await saving;
    expect(pad.shared?.content).toBe("ab");
    expect(pad.status).toEqual({ kind: "unsaved" });
    expect(timers.waiting()).toEqual([AUTOSAVE_MS]);
  });

  test("a shared change under unsaved edits keeps the draft as a conflict", async () => {
    const fake = fakeServer(copy("a", "1"));
    const timers = manualTimers();
    const pad = new Pad(fake.server, timers.schedule, () => {});
    await pad.load();
    pad.edit("mine");
    pad.observe(copy("theirs", "2"));
    expect(pad.draft).toBe("mine");
    expect(pad.status).toEqual({ kind: "conflict" });
    expect(timers.waiting()).toEqual([]);
    // Edits during a conflict wait for a choice.
    pad.edit("mine, more");
    expect(pad.status).toEqual({ kind: "conflict" });
    expect(await pad.save()).toBe(false);
    expect(fake.puts).toEqual([]);
  });

  test("a shared change with no unsaved edits replaces the text", async () => {
    const fake = fakeServer(copy("a", "1"));
    const pad = new Pad(fake.server, manualTimers().schedule, () => {});
    await pad.load();
    pad.observe(copy("theirs", "2"));
    expect(pad.draft).toBe("theirs");
    expect(pad.version).toBe(2);
    expect(pad.status).toEqual({ kind: "saved" });
  });

  test("ignores an older copy, another family's copy, and the same copy again", async () => {
    const fake = fakeServer(copy("a", "10"));
    const pad = new Pad(fake.server, manualTimers().schedule, () => {});
    await pad.load();
    pad.observe(copy("old", "9"));
    pad.observe(copy("other", "11", "session_other"));
    pad.observe(copy("a", "10"));
    expect(pad.draft).toBe("a");
    expect(pad.shared?.revision).toBe("10");
  });

  test("a save refused for a newer copy becomes a conflict over that copy", async () => {
    const fake = fakeServer(copy("a", "1"));
    const pad = new Pad(fake.server, manualTimers().schedule, () => {});
    await pad.load();
    fake.current = copy("theirs", "2");
    pad.edit("mine");
    expect(await pad.save()).toBe(false);
    expect(pad.status).toEqual({ kind: "conflict" });
    expect(pad.shared?.content).toBe("theirs");
    expect(pad.draft).toBe("mine");
  });

  test("review, then use the shared copy", async () => {
    const fake = fakeServer(copy("a", "1"));
    const pad = new Pad(fake.server, manualTimers().schedule, () => {});
    await pad.load();
    pad.edit("mine");
    pad.observe(copy("theirs", "2"));
    pad.review();
    expect(pad.reviewing).toBe(true);
    expect(pad.reviewed?.revision).toBe("2");
    pad.useShared();
    expect(pad.draft).toBe("theirs");
    expect(pad.reviewing).toBe(false);
    expect(pad.status).toEqual({ kind: "saved" });
  });

  test("review, then replace the shared copy with the draft", async () => {
    const fake = fakeServer(copy("a", "1"));
    const pad = new Pad(fake.server, manualTimers().schedule, () => {});
    await pad.load();
    pad.edit("mine");
    fake.current = copy("theirs", "2");
    pad.observe(copy("theirs", "2"));
    pad.review();
    expect(await pad.replaceShared()).toBe(true);
    expect(fake.puts.at(-1)).toEqual({ content: "mine", expectedRevision: "2", keepalive: false });
    expect(pad.status).toEqual({ kind: "saved" });
    expect(pad.reviewing).toBe(false);
  });

  test("a change during review updates the copy under review", async () => {
    const fake = fakeServer(copy("a", "1"));
    const pad = new Pad(fake.server, manualTimers().schedule, () => {});
    await pad.load();
    pad.edit("mine");
    pad.observe(copy("theirs", "2"));
    pad.review();
    pad.observe(copy("theirs again", "3"));
    expect(pad.reviewed?.content).toBe("theirs again");
  });

  test("a failed save says why and keeps the draft", async () => {
    const fake = fakeServer(copy("a", "1"));
    const pad = new Pad(fake.server, manualTimers().schedule, () => {});
    await pad.load();
    fake.failWith(new NetworkError("Kit’s server can’t be reached."));
    pad.edit("mine");
    expect(await pad.save()).toBe(false);
    expect(pad.status).toEqual({ kind: "failed", reason: "Kit can’t be reached" });
    expect(pad.draft).toBe("mine");
  });

  test("a draft over the limit isn't sent", async () => {
    const fake = fakeServer(copy("a", "1"));
    const pad = new Pad(fake.server, manualTimers().schedule, () => {});
    await pad.load();
    pad.edit("é".repeat(40_000));
    expect(await pad.save()).toBe(false);
    expect(pad.status).toEqual({ kind: "failed", reason: "Over the 64 KB limit" });
    expect(fake.puts).toEqual([]);
  });

  test("flush saves an unsaved draft with keepalive", async () => {
    const fake = fakeServer(copy("a", "1"));
    const pad = new Pad(fake.server, manualTimers().schedule, () => {});
    await pad.load();
    pad.edit("mine");
    pad.flush();
    await settle();
    expect(fake.puts).toEqual([{ content: "mine", expectedRevision: "1", keepalive: true }]);
  });
});

/** A Storage over a Map, as localStorage behaves. */
function memoryStorage(): Storage {
  const map = new Map<string, string>();
  return {
    get length() {
      return map.size;
    },
    clear: () => map.clear(),
    getItem: (key) => map.get(key) ?? null,
    key: (index) => Array.from(map.keys())[index] ?? null,
    removeItem: (key) => {
      map.delete(key);
    },
    setItem: (key, value) => {
      map.set(key, value);
    },
  };
}

describe("kept drafts", () => {
  test("an unsaved draft is kept on the device until the server has it", async () => {
    const fake = fakeServer(copy("a", "1"));
    const drafts = localDrafts(memoryStorage());
    const pad = new Pad(fake.server, manualTimers().schedule, () => {}, drafts);
    await pad.load();
    pad.edit("mine");
    expect(drafts.read(OWNER)).toEqual({ base: "1", draft: "mine" });
    await pad.save();
    expect(drafts.read(OWNER)).toBeUndefined();
  });

  test("a page opened later saves a draft written against the current copy", async () => {
    const fake = fakeServer(copy("a", "1"));
    const drafts: DraftStore = localDrafts(memoryStorage());
    drafts.write(OWNER, "1", "left unsaved");
    const timers = manualTimers();
    const pad = new Pad(fake.server, timers.schedule, () => {}, drafts);
    await pad.load();
    expect(pad.draft).toBe("left unsaved");
    expect(pad.status).toEqual({ kind: "unsaved" });
    timers.fire();
    await settle();
    expect(fake.puts.at(-1)).toEqual({ content: "left unsaved", expectedRevision: "1", keepalive: false });
    expect(drafts.read(OWNER)).toBeUndefined();
  });

  test("a draft written against an older copy comes back as a conflict", async () => {
    const fake = fakeServer(copy("theirs", "2"));
    const drafts = localDrafts(memoryStorage());
    drafts.write(OWNER, "1", "left unsaved");
    const pad = new Pad(fake.server, manualTimers().schedule, () => {}, drafts);
    await pad.load();
    expect(pad.draft).toBe("left unsaved");
    expect(pad.status).toEqual({ kind: "conflict" });
    pad.useShared();
    expect(drafts.read(OWNER)).toBeUndefined();
  });

  test("a conflict keeps the revision the draft was written against", async () => {
    const fake = fakeServer(copy("a", "1"));
    const drafts = localDrafts(memoryStorage());
    const pad = new Pad(fake.server, manualTimers().schedule, () => {}, drafts);
    await pad.load();
    pad.edit("mine");
    pad.observe(copy("theirs", "2"));
    pad.edit("mine, more");
    expect(drafts.read(OWNER)).toEqual({ base: "1", draft: "mine, more" });
  });

  test("forgetDrafts drops every kept draft and nothing else", () => {
    const storage = memoryStorage();
    localDrafts(storage).write("session_a", "1", "x");
    localDrafts(storage).write("session_b", "1", "y");
    storage.setItem("kit.device", "{}");
    forgetDrafts(storage);
    expect(storage.length).toBe(1);
    expect(storage.getItem("kit.device")).toBe("{}");
  });
});

test("a flush during a save sends the newest text once that save lands", async () => {
  const fake = fakeServer(copy("a", "1"));
  const pad = new Pad(fake.server, manualTimers().schedule, () => {});
  await pad.load();
  let release = () => {};
  fake.holdWith(
    new Promise<void>((resolve) => {
      release = resolve;
    }),
  );
  pad.edit("ab");
  const saving = pad.save();
  pad.edit("abc");
  pad.flush();
  fake.holdWith(undefined);
  release();
  await saving;
  await settle();
  expect(fake.puts).toEqual([
    { content: "ab", expectedRevision: "1", keepalive: false },
    { content: "abc", expectedRevision: "2", keepalive: true },
  ]);
  expect(pad.status).toEqual({ kind: "saved" });
});

test("keepalive is used only when the whole request body fits", async () => {
  const fake = fakeServer(copy("a", "1"));
  const pad = new Pad(fake.server, manualTimers().schedule, () => {});
  await pad.load();
  // 40,000 quotes are 40 KB of text but 80 KB once escaped in JSON.
  pad.edit('"'.repeat(40_000));
  pad.flush();
  await settle();
  expect(fake.puts.at(-1)?.keepalive).toBe(false);
});

test("laterRevision compares decimal strings as numbers", () => {
  expect(laterRevision("10", "9")).toBe(true);
  expect(laterRevision("9", "10")).toBe(false);
  expect(laterRevision("12", "11")).toBe(true);
  expect(laterRevision("11", "11")).toBe(false);
});

test("compareCopies lists each changed run's shared lines, then the draft's", () => {
  const shared = "# Notes\n## Done\n- a\n- b\n## Next\n- c";
  const draft = "# Notes\n## Done\n- a\n- b, done\n## Next\n- d";
  expect(compareCopies(shared, draft)).toEqual([
    { kind: "same", text: "# Notes" },
    { kind: "same", text: "## Done" },
    { kind: "same", text: "- a" },
    { kind: "shared", text: "- b" },
    { kind: "draft", text: "- b, done" },
    { kind: "same", text: "## Next" },
    { kind: "shared", text: "- c" },
    { kind: "draft", text: "- d" },
  ]);
});
