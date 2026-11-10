import { describe, expect, test } from "bun:test";
import { startTestPlugin } from "../kit-plugin-testing.ts";
import { ErrorCode, isTryLater, type Kit, KitError, t } from "../kit-plugin.ts";

describe("initialization", () => {
  test("setup runs after Kit's initialize is answered and sees the session", async () => {
    let seen: { version: string; instanceId: string; session: unknown; project: unknown } | undefined;
    const kit = await startTestPlugin(
      (plugin) => {
        seen = {
          version: plugin.version,
          instanceId: plugin.instanceId,
          session: { id: plugin.session.id, name: plugin.session.name },
          project: plugin.project,
        };
      },
      {
        kitVersion: "0.58.0",
        instanceId: "pluginhost_1:1",
        session: { id: "session_1", name: "Work" },
        project: { cwd: "/repo", git: null },
      },
    );
    expect(kit.written[0]).toEqual({ jsonrpc: "2.0", id: "kit-1", result: { protocolVersion: 2 } });
    expect(seen).toEqual({
      version: "0.58.0",
      instanceId: "pluginhost_1:1",
      session: { id: "session_1", name: "Work" },
      project: { cwd: "/repo", git: null },
    });
  });

  test("a failing setup exits with status 1 and reports why", async () => {
    const kit = await startTestPlugin(() => {
      throw new Error("missing token");
    });
    expect(kit.exitCode).toBe(1);
    expect(kit.diagnostics).toStartWith("Plugin setup failed: Error: missing token");
  });

  test("an unknown Kit request gets method not found and the plugin keeps serving", async () => {
    const kit = await startTestPlugin(async (plugin) => {
      await plugin.commands.register({ id: "go", description: "Go", execute: () => {} });
    });
    kit.send({ id: 99, method: "kit/future/request", params: {} });
    await kit.settle();
    expect(kit.written.at(-1)).toEqual({
      jsonrpc: "2.0",
      id: 99,
      error: { code: -32601, message: "Method not found: kit/future/request" },
    });
    await kit.runCommand("go");
  });
});

describe("commands", () => {
  test("register, run with the literal arguments, and unregister", async () => {
    const runs: string[] = [];
    let handle: Awaited<ReturnType<Kit["commands"]["register"]>> | undefined;
    const kit = await startTestPlugin(
      async (plugin) => {
        handle = await plugin.commands.register({
          id: "notes.add",
          description: "Add a note",
          argName: "text",
          execute: (args) => {
            runs.push(args);
          },
        });
      },
      { pluginId: "notes" },
    );
    expect(handle?.id).toBe("notes.notes.add");
    expect(handle?.localId).toBe("notes.add");
    expect(kit.commands.get("notes.add")).toEqual({ id: "notes.add", description: "Add a note", argName: "text" });
    await kit.runCommand("notes.add", "  buy milk ");
    expect(runs).toEqual(["  buy milk "]);
    await handle?.unregister();
    expect(kit.commands.has("notes.add")).toBe(false);
    await expect(kit.runCommand("notes.add")).rejects.toEqual(new KitError(ErrorCode.Failed, "Unknown command: notes.add"));
  });

  test("a failed registration rejects with Kit's error and keeps the first handler", async () => {
    const runs: string[] = [];
    let failure: unknown;
    const kit = await startTestPlugin(async (plugin) => {
      await plugin.commands.register({ id: "go", description: "First", execute: () => void runs.push("first") });
      try {
        await plugin.commands.register({ id: "go", description: "Second", execute: () => void runs.push("second") });
      } catch (error) {
        failure = error;
      }
    });
    expect(failure).toEqual(new KitError(ErrorCode.Conflict, "go is already registered"));
    await kit.runCommand("go");
    expect(runs).toEqual(["first"]);
  });

  test("any handler error is the command's failure, since Kit's requests fail only as failed or cancelled", async () => {
    const kit = await startTestPlugin(async (plugin) => {
      await plugin.commands.register({
        id: "fail",
        description: "Fail",
        execute: () => {
          throw new Error("disk full");
        },
      });
      await plugin.commands.register({
        id: "later",
        description: "Later",
        execute: () => {
          throw new KitError(ErrorCode.TryLater, "Busy", { reason: "busy" });
        },
      });
    });
    await expect(kit.runCommand("fail")).rejects.toEqual(new KitError(ErrorCode.Failed, "disk full"));
    await expect(kit.runCommand("later")).rejects.toEqual(new KitError(ErrorCode.Failed, "Busy"));
  });
});

describe("tools", () => {
  test("register the schema t builds, and turn a string outcome into a text result", async () => {
    let modelName = "";
    const kit = await startTestPlugin(
      async (plugin) => {
        const handle = await plugin.tools.register({
          id: "word_count",
          label: "Word count",
          description: "Count words",
          input: t.object({ text: t.string({ minLength: 1 }), unique: t.optional(t.boolean()) }),
          executionMode: "parallel",
          execute: (input) => {
            const words = input.text.split(/\s+/).filter(Boolean);
            return `${input.unique ? new Set(words).size : words.length} words`;
          },
        });
        modelName = handle.modelName;
      },
      { pluginId: "words" },
    );
    expect(modelName).toBe("words__word_count");
    expect(kit.tools.get("word_count")).toEqual({
      id: "word_count",
      label: "Word count",
      description: "Count words",
      executionMode: "parallel",
      inputSchema: {
        type: "object",
        properties: { text: { type: "string", minLength: 1 }, unique: { type: "boolean" } },
        required: ["text"],
        additionalProperties: false,
      },
    });
    expect(await kit.callTool("word_count", { text: "a b a", unique: true })).toEqual({
      content: [{ type: "text", text: "2 words" }],
    });
  });

  test("a full result passes through with its tool call ID in context", async () => {
    const kit = await startTestPlugin(async (plugin) => {
      await plugin.tools.register({
        id: "echo",
        description: "Echo",
        input: t.object({}),
        execute: (_input, context) => ({
          content: [{ type: "text", text: context.toolCallId }],
          details: { echoed: true },
          terminate: true,
        }),
      });
    });
    expect(await kit.callTool("echo", {}, { toolCallId: "call_7" })).toEqual({
      content: [{ type: "text", text: "call_7" }],
      details: { echoed: true },
      terminate: true,
    });
  });
});

describe("dialogs", () => {
  test("a handler's dialogs name its request as parent; top-level dialogs have none", async () => {
    const kit = await startTestPlugin(
      async (plugin) => {
        await plugin.commands.register({
          id: "ask",
          description: "Ask",
          execute: async (_args, context) => {
            const name = await context.ui.input({ title: "Name" });
            const color = await context.ui.select({
              title: "Color",
              options: [
                { label: "Red", value: { hex: "#f00" } },
                { label: "None", value: null },
              ],
            });
            const ok = await plugin.ui.confirm({ title: `Save ${name} in ${JSON.stringify(color?.value)}?` });
            plugin.ui.toast({ title: ok ? "Saved" : "Discarded" });
          },
        });
      },
      {
        dialogs: (dialog) =>
          dialog.method === "kit/ui/input" ? "Ada" : dialog.method === "kit/ui/select" ? { value: null } : true,
      },
    );
    await kit.runCommand("ask");
    expect(kit.dialogs.map(({ method, params }) => ({ method, params }))).toEqual([
      { method: "kit/ui/input", params: { title: "Name", parentId: "kit-2" } },
      {
        method: "kit/ui/select",
        params: {
          title: "Color",
          options: [
            { label: "Red", value: { hex: "#f00" } },
            { label: "None", value: null },
          ],
          parentId: "kit-2",
        },
      },
      { method: "kit/ui/confirm", params: { title: "Save Ada in null?" } },
    ]);
    expect(kit.toasts).toEqual([{ variant: "info", title: "Saved" }]);
  });

  test("dismissed dialogs resolve to false and null", async () => {
    const answers: unknown[] = [];
    await startTestPlugin(async (plugin) => {
      answers.push(await plugin.ui.confirm({ title: "Sure?" }));
      answers.push(await plugin.ui.input({ title: "Name" }));
      answers.push(await plugin.ui.select({ title: "Pick", options: [{ label: "A", value: "a" }] }));
    });
    expect(answers).toEqual([false, null, null]);
  });
});

describe("interceptor", () => {
  test("answers allow and reject, asking the user under the intercepted call", async () => {
    const kit = await startTestPlugin(
      async (plugin) => {
        await plugin.interceptor.register(async (call, context) => {
          if (call.name !== "bash") return "allow";
          const ok = await context.ui.confirm({ title: `Run ${String(call.input.command)}?` });
          return ok ? "allow" : { reject: "The user rejected this command." };
        });
      },
      { dialogs: () => false },
    );
    expect(kit.intercepting).toBe(true);
    expect(await kit.intercept({ name: "read", input: { path: "a" } })).toEqual({ action: "allow" });
    expect(await kit.intercept({ name: "bash", input: { command: "rm -rf /" } })).toEqual({
      action: "reject-and-continue",
      message: "The user rejected this command.",
    });
    expect(kit.dialogs.map((dialog) => dialog.params)).toEqual([{ title: "Run rm -rf /?", parentId: "kit-3" }]);
  });

  test("unregistering stops interception", async () => {
    const kit = await startTestPlugin(async (plugin) => {
      const handle = await plugin.interceptor.register(() => "allow");
      await handle.unregister();
    });
    expect(kit.intercepting).toBe(false);
    await expect(kit.intercept({ name: "bash", input: {} })).rejects.toEqual(
      new KitError(ErrorCode.Failed, "The plugin has no interceptor"),
    );
  });
});

describe("cancellation", () => {
  test("Kit cancelling a request aborts the handler and closes its dialogs", async () => {
    let aborted = false;
    let dialogClosed = false;
    const kit = await startTestPlugin(
      async (plugin) => {
        await plugin.tools.register({
          id: "slow",
          description: "Slow",
          input: t.object({}),
          execute: async (_input, context) => {
            context.signal.addEventListener("abort", () => {
              aborted = true;
            });
            await context.ui.confirm({ title: "Continue?" }).catch(() => {});
            return "done";
          },
        });
      },
      {
        dialogs: (dialog) =>
          new Promise((resolve) =>
            dialog.signal.addEventListener("abort", () => {
              dialogClosed = true;
              resolve(false);
            }),
          ),
      },
    );
    const controller = new AbortController();
    const call = kit.callTool("slow", {}, { signal: controller.signal });
    await kit.settle();
    controller.abort(new Error("turn aborted"));
    await expect(call).rejects.toEqual(new Error("turn aborted"));
    await kit.settle();
    expect(aborted).toBe(true);
    expect(dialogClosed).toBe(true);
    expect(kit.written).toContainEqual({
      jsonrpc: "2.0",
      id: "kit-2",
      error: { code: -32001, message: "Cancelled" },
    });
  });

  test("aborting a plugin request sends kit/cancel and rejects with the reason", async () => {
    let dialogClosed = false;
    let outcome: unknown;
    const controller = new AbortController();
    const kit = await startTestPlugin(
      (plugin) => {
        plugin.ui.input({ title: "Name" }, { signal: controller.signal }).catch((error: unknown) => {
          outcome = error;
        });
      },
      {
        dialogs: (dialog) =>
          new Promise((resolve) =>
            dialog.signal.addEventListener("abort", () => {
              dialogClosed = true;
              resolve(null);
            }),
          ),
      },
    );
    await kit.settle();
    controller.abort(new Error("no longer needed"));
    await kit.settle();
    expect(outcome).toEqual(new Error("no longer needed"));
    expect(kit.written).toContainEqual({ jsonrpc: "2.0", method: "kit/cancel", params: { id: "plugin-1" } });
    expect(dialogClosed).toBe(true);
  });

  test("an already aborted signal sends nothing", async () => {
    const kit = await startTestPlugin(async (plugin) => {
      await plugin.system.openUrl("https://example.com", { signal: AbortSignal.abort(new Error("stop")) }).catch(() => {});
    });
    expect(kit.written).toEqual([{ jsonrpc: "2.0", id: "kit-1", result: { protocolVersion: 2 } }]);
  });
});

describe("events", () => {
  test("session and project stay current before listeners run", async () => {
    const seen: unknown[] = [];
    let plugin: Kit | undefined;
    const kit = await startTestPlugin((connected) => {
      plugin = connected;
      connected.on("session.changed", (session) => void seen.push(["session", session, connected.session.name]));
      connected.on("project.changed", (project) => void seen.push(["project", project.cwd, connected.project.cwd]));
      connected.on("git.changed", ({ git }) => void seen.push(["git", git?.branch, connected.project.git?.branch]));
    });
    await kit.emit("session.changed", { id: "session-test", name: "Renamed" });
    await kit.emit("project.changed", { cwd: "/other", git: null });
    await kit.emit("git.changed", { git: { root: "/other", branch: "main", dirty: true, pullRequest: null } });
    expect(seen).toEqual([
      ["session", { id: "session-test", name: "Renamed" }, "Renamed"],
      ["project", "/other", "/other"],
      ["git", "main", "main"],
    ]);
    expect(plugin?.project).toEqual({ cwd: "/other", git: { root: "/other", branch: "main", dirty: true, pullRequest: null } });
  });

  test("turn events reach listeners, and stopping a listener stops it", async () => {
    const turns: string[] = [];
    let stop = () => {};
    const kit = await startTestPlugin((plugin) => {
      plugin.on("turn.started", ({ turnId }) => void turns.push(`started ${turnId}`));
      stop = plugin.on("turn.completed", ({ turn }) => void turns.push(`completed ${turn.id}`));
    });
    await kit.emit("turn.started", { sessionId: "session-test", turnId: "turn_1" });
    await kit.emit("turn.completed", { sessionId: "session-test", turn: { id: "turn_1", messages: [] } });
    stop();
    await kit.emit("turn.completed", { sessionId: "session-test", turn: { id: "turn_2", messages: [] } });
    expect(turns).toEqual(["started turn_1", "completed turn_1"]);
  });

  test("unknown notifications and members are ignored", async () => {
    const names: unknown[] = [];
    const kit = await startTestPlugin((plugin) => {
      plugin.on("session.changed", (session) => void names.push(session.name));
    });
    kit.send({ method: "kit/events/future.thing", params: { anything: 1 } });
    kit.send({ method: "kit/events/session.changed", params: { id: "session-test", name: "New", color: "blue" } });
    await kit.settle();
    expect(names).toEqual(["New"]);
    expect(kit.exitCode).toBeUndefined();
  });

  test("a listener's error goes to diagnostics and an error toast", async () => {
    const kit = await startTestPlugin((plugin) => {
      plugin.on("turn.started", () => {
        throw new Error("listener broke");
      });
    });
    await kit.emit("turn.started", { sessionId: "session-test", turnId: "turn_1" });
    expect(kit.diagnostics).toStartWith("Error: listener broke");
    expect(kit.toasts).toEqual([{ title: "Plugin error", subtitle: "listener broke", variant: "error" }]);
  });
});

describe("other capabilities", () => {
  test("footer items, hide claims, subagents, URLs, and toasts", async () => {
    const kit = await startTestPlugin(
      async (plugin) => {
        const item = await plugin.footer.set("status", ["speech ", { text: "on", style: { fg: "toolText", bold: true } }]);
        await item.set("speech off");
        await plugin.footer.set("other", "x");
        await plugin.footer.clear("other");
        await plugin.footer.hide("kit.footer.location");
        const reviewer = await plugin.subagents.register({ id: "reviewer", description: "Review", instructions: "Review it." });
        expect(reviewer.id).toBe("speech.reviewer");
        await plugin.system.openUrl("https://example.com/docs");
        plugin.ui.toast({ title: "Ready", variant: "warning", persistent: true });
      },
      { pluginId: "speech" },
    );
    expect([...kit.footer]).toEqual([["status", [{ text: "speech off" }]]]);
    expect([...kit.hidden]).toEqual(["kit.footer.location"]);
    expect([...kit.subagents.keys()]).toEqual(["reviewer"]);
    expect(kit.urls).toEqual(["https://example.com/docs"]);
    expect(kit.toasts).toEqual([{ variant: "warning", title: "Ready", persistent: true }]);
  });

  test("submitting a message, and recognizing a busy session", async () => {
    let busy = true;
    const outcomes: unknown[] = [];
    const kit = await startTestPlugin(
      async (plugin) => {
        outcomes.push(await plugin.session.submitMessage("Continue.", { idempotencyKey: "loop-1" }).catch((error) => error));
        busy = false;
        outcomes.push(await plugin.session.submitMessage("Continue."));
      },
      {
        respond: (method) => {
          if (method === "kit/session/submit-message" && busy) {
            throw new KitError(ErrorCode.TryLater, "Session is busy", { reason: "session_busy" });
          }
          return undefined;
        },
      },
    );
    expect(isTryLater(outcomes[0], "session_busy")).toBe(true);
    expect(outcomes[1]).toEqual({ messageId: "message-1", turnId: "turn-1" });
    expect(kit.messages).toEqual([{ text: "Continue." }]);
  });
});

describe("stopping", () => {
  test("shutdown runs the disposer, answers null, and exits 0", async () => {
    const order: string[] = [];
    const kit = await startTestPlugin(() => async () => {
      order.push("disposed");
    });
    await kit.shutdown();
    expect(order).toEqual(["disposed"]);
    expect(kit.written.at(-1)).toEqual({ jsonrpc: "2.0", id: "kit-2", result: null });
    expect(kit.exitCode).toBe(0);
  });

  test("Kit closing the connection runs the disposer and exits 0", async () => {
    let disposed = false;
    const kit = await startTestPlugin(() => () => {
      disposed = true;
    });
    await kit.close();
    expect(disposed).toBe(true);
    expect(kit.exitCode).toBe(0);
  });
});

describe("stopping while setup runs", () => {
  for (const how of ["shutdown", "close"] as const) {
    test(`${how} cancels setup's requests and runs the disposer setup returns afterwards`, async () => {
      const order: string[] = [];
      const kit = await startTestPlugin(
        async (plugin) => {
          await plugin.commands
            .register({ id: "go", description: "Go", execute: () => {} })
            .catch((error: unknown) => order.push(`register failed: ${(error as KitError).code}`));
          return () => {
            order.push("disposed");
          };
        },
        { waitForSetup: false, respond: (method) => (method === "kit/commands/register" ? new Promise(() => {}) : undefined) },
      );
      await (how === "shutdown" ? kit.shutdown() : kit.close());
      expect(order).toEqual([`register failed: ${ErrorCode.Unavailable}`, "disposed"]);
      expect(kit.exitCode).toBe(0);
    });
  }

  test("a setup blocked on anything else does not keep the plugin from stopping", async () => {
    let disposed = false;
    const kit = await startTestPlugin(
      async () => {
        await new Promise(() => {});
        return () => {
          disposed = true;
        };
      },
      { waitForSetup: false },
    );
    const started = Date.now();
    await kit.shutdown();
    expect(Date.now() - started).toBeLessThan(1900);
    expect(disposed).toBe(false);
    expect(kit.exitCode).toBe(0);
  });
});

describe("the in-memory Kit rejects what Kit rejects", () => {
  const rejection = async (register: (plugin: Kit) => Promise<unknown>) => {
    let failure: unknown;
    await startTestPlugin(async (plugin) => {
      failure = await register(plugin).then(
        () => undefined,
        (error: unknown) => error,
      );
    });
    return failure;
  };

  test("identifiers, required text, and tool schemas", async () => {
    expect(await rejection((plugin) => plugin.commands.register({ id: "Go", description: "Go", execute: () => {} }))).toEqual(
      new KitError(ErrorCode.InvalidParams, "Invalid parameter: id"),
    );
    expect(await rejection((plugin) => plugin.commands.register({ id: "go", description: " ", execute: () => {} }))).toEqual(
      new KitError(ErrorCode.InvalidParams, "Invalid parameter: description"),
    );
    expect(
      await rejection((plugin) =>
        plugin.tools.register({ id: "read-file", description: "Read", input: t.object({}), execute: () => "" }),
      ),
    ).toEqual(new KitError(ErrorCode.InvalidParams, "Invalid parameter: id"));
    expect(
      await rejection((plugin) =>
        plugin.tools.register({
          id: "read",
          description: "Read",
          input: t.raw({ type: "object", properties: { path: { $ref: "#/x" } as never } }),
          execute: () => "",
        }),
      ),
    ).toEqual(new KitError(ErrorCode.InvalidParams, "Invalid parameter: inputSchema keyword $ref"));
  });

  test("URLs and footer targets", async () => {
    expect(await rejection((plugin) => plugin.system.openUrl("ftp://example.com/"))).toEqual(
      new KitError(ErrorCode.InvalidParams, "Invalid parameter: url must use http or https"),
    );
    expect(await rejection((plugin) => plugin.system.openUrl("https://user:pw@example.com/"))).toEqual(
      new KitError(ErrorCode.InvalidParams, "Invalid parameter: url must not contain credentials"),
    );
    expect(await rejection((plugin) => plugin.footer.hide("status"))).toEqual(
      new KitError(ErrorCode.InvalidParams, "Invalid parameter: id"),
    );
    expect(await rejection((plugin) => plugin.footer.hide("kit.header.title"))).toEqual(
      new KitError(ErrorCode.InvalidParams, "Invalid parameter: id"),
    );
    expect(await rejection((plugin) => plugin.footer.hide("kit.footer.location"))).toBeUndefined();
    expect(await rejection((plugin) => plugin.system.openUrl("https://example.com/\ufeff"))).toBeUndefined();
    expect(await rejection((plugin) => plugin.system.openUrl("https://example.com/\u00a0"))).toEqual(
      new KitError(ErrorCode.InvalidParams, "Invalid parameter: url must not contain white space or controls"),
    );
  });

  test("white space is what Kit treats as white space", async () => {
    expect(await rejection((plugin) => plugin.session.submitMessage("\ufeff"))).toBeUndefined();
    expect(await rejection((plugin) => plugin.session.submitMessage("\u0085\u3000"))).toEqual(
      new KitError(ErrorCode.InvalidParams, "Invalid parameter: text"),
    );
  });

  test("a malformed schema is invalid params", async () => {
    expect(
      await rejection((plugin) =>
        plugin.tools.register({
          id: "read",
          description: "Read",
          input: t.raw({ type: "object", properties: [] as never }),
          execute: () => "",
        }),
      ),
    ).toEqual(new KitError(ErrorCode.InvalidParams, "Invalid parameter: inputSchema properties"));
  });

  test("a repeated idempotency key returns the original result, and conflicts with different text", async () => {
    const outcomes: unknown[] = [];
    const kit = await startTestPlugin(async (plugin) => {
      outcomes.push(await plugin.session.submitMessage("Continue.", { idempotencyKey: "loop-1" }));
      outcomes.push(await plugin.session.submitMessage("Continue.", { idempotencyKey: "loop-1" }));
      outcomes.push(await plugin.session.submitMessage("Stop.", { idempotencyKey: "loop-1" }).catch((error) => error));
    });
    expect(outcomes).toEqual([
      { messageId: "message-1", turnId: "turn-1" },
      { messageId: "message-1", turnId: "turn-1" },
      new KitError(ErrorCode.Conflict, "The idempotency key was used with different text"),
    ]);
    expect(kit.messages).toEqual([{ text: "Continue.", idempotencyKey: "loop-1" }]);
  });

  test("an invalid toast is ignored", async () => {
    const kit = await startTestPlugin((plugin) => {
      plugin.ui.toast({ title: " " });
      plugin.ui.toast({ title: "Kept" });
    });
    await kit.settle();
    expect(kit.toasts).toEqual([{ variant: "info", title: "Kept" }]);
  });
});

test("settle waits for asynchronous listeners", async () => {
  const seen: string[] = [];
  const kit = await startTestPlugin((plugin) => {
    plugin.on("turn.started", async ({ turnId }) => {
      await Bun.sleep(30);
      seen.push(turnId);
    });
  });
  await kit.emit("turn.started", { sessionId: "session-test", turnId: "turn_1" });
  expect(seen).toEqual(["turn_1"]);
});
