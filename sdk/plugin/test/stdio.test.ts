import { describe, expect, test } from "bun:test";

// The helper runs under Bun, and under Node 22.6 or later with type stripping.
const runtimes: Array<[string, string[]]> = [["bun", ["bun"]]];
const node = Bun.which("node");
if (node) {
  const [major = 0, minor = 0] = (await Bun.$`${node} --version`.text()).trim().slice(1).split(".").map(Number);
  if (major > 22 || (major === 22 && minor >= 6)) {
    runtimes.push(["node", [node, "--experimental-strip-types", "--no-warnings"]]);
  }
}

const initialize = {
  jsonrpc: "2.0",
  id: "kit-1",
  method: "initialize",
  params: {
    protocolVersion: 2,
    kit: { version: "test" },
    instance: { id: "test:1" },
    context: { project: { cwd: "/project", git: null }, session: { id: "session-test", name: null } },
  },
};

type Written = { jsonrpc?: string; id?: string; method?: string; params?: { id?: string; title?: string; [key: string]: unknown }; result?: unknown };

/** The stdio fixture running in a subprocess, answering its requests as Kit would. */
function start(runtime: string[], mode: string) {
  const child = Bun.spawn([...runtime, `${import.meta.dir}/fixtures/stdio-plugin.ts`, mode], {
    stdin: "pipe",
    stdout: "pipe",
    stderr: "pipe",
  });
  const messages: Written[] = [];
  const write = async (message: object) => {
    child.stdin.write(`${JSON.stringify(message)}\n`);
    await child.stdin.flush();
  };
  const reading = (async () => {
    let buffered = "";
    for await (const chunk of child.stdout.pipeThrough(new TextDecoderStream())) {
      buffered += chunk;
      for (let newline = buffered.indexOf("\n"); newline >= 0; newline = buffered.indexOf("\n")) {
        const message: Written = JSON.parse(buffered.slice(0, newline));
        buffered = buffered.slice(newline + 1);
        messages.push(message);
        if (message.method === "kit/commands/register") {
          await write({ jsonrpc: "2.0", id: message.id, result: { id: `test.${message.params?.id}` } });
        }
      }
    }
  })();
  const stderr = new Response(child.stderr).text();
  return {
    messages,
    send: write,
    /** Waits until the plugin has written a message matching predicate. */
    async until(predicate: (message: Written) => boolean) {
      for (let waited = 0; !messages.some(predicate); waited += 10) {
        if (waited > 5000) throw new Error(`timed out; the plugin wrote ${JSON.stringify(messages)}`);
        await Bun.sleep(10);
      }
    },
    closeInput: () => child.stdin.end(),
    /** Waits for the plugin to exit. */
    async finish() {
      const timer = setTimeout(() => child.kill(), 5000);
      const [status, text] = await Promise.all([child.exited, stderr, reading]);
      clearTimeout(timer);
      return { status, stderr: text };
    },
  };
}

const ready = (message: Written) => message.method === "kit/ui/toast" && message.params?.title === "ready";

describe.each(runtimes)("under %s", (_name, runtime) => {
  test("only protocol messages reach stdout; console output goes to stderr", async () => {
    const plugin = start(runtime, "log");
    await plugin.send(initialize);
    await plugin.until(ready);
    await plugin.send({ jsonrpc: "2.0", id: "kit-2", method: "kit/commands/execute", params: { id: "shout", args: "hi" } });
    await plugin.until((message) => message.id === "kit-2");
    await plugin.send({ jsonrpc: "2.0", id: "kit-3", method: "shutdown" });
    const { status, stderr } = await plugin.finish();
    expect(plugin.messages).toEqual([
      { jsonrpc: "2.0", id: "kit-1", result: { protocolVersion: 2 } },
      {
        jsonrpc: "2.0",
        id: "plugin-1",
        method: "kit/commands/register",
        params: { id: "shout", description: "Log loudly" },
      },
      { jsonrpc: "2.0", method: "kit/ui/toast", params: { variant: "info", title: "ready" } },
      { jsonrpc: "2.0", id: "kit-2", result: null },
      { jsonrpc: "2.0", id: "kit-3", result: null },
    ] satisfies Written[]);
    expect(stderr).toBe(
      "console.log from setup\nconsole.info from setup\nprocess.stdout.write from setup\nshouting hi\ndisposed\n",
    );
    expect(status).toBe(0);
  });

  test("closing stdin runs the disposer and exits 0", async () => {
    const plugin = start(runtime, "log");
    await plugin.send(initialize);
    await plugin.until(ready);
    await plugin.closeInput();
    const { status, stderr } = await plugin.finish();
    expect(stderr.endsWith("disposed\n")).toBe(true);
    expect(status).toBe(0);
  });

  test("a failing setup exits 1 with the error on stderr", async () => {
    const plugin = start(runtime, "fail");
    await plugin.send(initialize);
    const { status, stderr } = await plugin.finish();
    expect(plugin.messages).toEqual([{ jsonrpc: "2.0", id: "kit-1", result: { protocolVersion: 2 } }] satisfies Written[]);
    expect(stderr).toStartWith("Plugin setup failed: Error: setup failed on purpose");
    expect(status).toBe(1);
  });

  test("an unhandled rejection is written to stderr and shown as an error toast", async () => {
    const plugin = start(runtime, "reject");
    await plugin.send(initialize);
    await plugin.until((message) => message.method === "kit/ui/toast" && message.params?.title === "Plugin error");
    await plugin.closeInput();
    const { stderr } = await plugin.finish();
    expect(plugin.messages).toContainEqual({
      jsonrpc: "2.0",
      method: "kit/ui/toast",
      params: { title: "Plugin error", subtitle: "nobody caught this", variant: "error" },
    } satisfies Written);
    expect(stderr).toContain("Error: nobody caught this");
  });
});
