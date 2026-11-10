// Type-level tests for the helper, checked by `bun run typecheck`.
import { type Infer, type Kit, t } from "../kit-plugin.ts";

type Equal<A, B> = (<T>() => T extends A ? 1 : 2) extends <T>() => T extends B ? 1 : 2 ? true : false;
function expectType<_T extends true>() {}

const input = t.object({
  path: t.string(),
  limit: t.optional(t.integer()),
  mode: t.enum(["read", "write"]),
  kind: t.literal("file"),
  tags: t.array(t.string()),
  target: t.union([t.string(), t.number()]),
  parent: t.nullable(t.string()),
  extra: t.raw<{ a: number }>({ type: "object" }),
});

expectType<
  Equal<
    Infer<typeof input>,
    {
      path: string;
      limit?: number;
      mode: "read" | "write";
      kind: "file";
      tags: string[];
      target: string | number;
      parent: string | null;
      extra: { a: number };
    }
  >
>();

export async function setup(kit: Kit) {
  await kit.tools.register({
    id: "read_file",
    description: "Read a file",
    input,
    execute(value, context) {
      expectType<Equal<typeof value.path, string>>();
      expectType<Equal<typeof value.limit, number | undefined>>();
      expectType<Equal<typeof context.toolCallId, string>>();
      // @ts-expect-error The schema has no such property.
      value.missing;
      return value.path;
    },
  });

  type State = { name: string; note?: string; extra: Record<string, unknown> };
  const state: State = { name: "x", extra: {} };
  await kit.tools.register({
    id: "with_state",
    description: "Returns a plugin's own state type as details",
    input: t.object({}),
    execute: () => ({ content: [{ type: "text", text: "ok" }], details: { state } }),
  });

  await kit.tools.register({
    id: "bad_result",
    description: "Returns a number",
    input: t.object({}),
    // @ts-expect-error A tool returns a string or a tool result.
    execute: () => 42,
  });

  const chosen = await kit.ui.select({
    title: "Pick",
    options: [
      { label: "One", value: 1 },
      { label: "Two", value: "two" },
    ],
  });
  expectType<Equal<typeof chosen, { value: 1 | "two" } | null>>();

  await kit.interceptor.register((call) => (call.name === "bash" ? { reject: "No shell" } : "allow"));
  // @ts-expect-error A decision is "allow" or { reject }.
  await kit.interceptor.register(() => "deny");

  kit.on("git.changed", ({ git }) => {
    expectType<Equal<typeof git, import("../kit-plugin.ts").Git | null>>();
  });
  // @ts-expect-error Unknown events are not offered.
  kit.on("unknown.event", () => {});

  // @ts-expect-error Footer styles take theme tokens.
  await kit.footer.set("status", { text: "x", style: { fg: "red" } });
}
