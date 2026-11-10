import { expect, test } from "bun:test";
import { t } from "../kit-plugin.ts";

// The keywords of Kit's tool schema subset (docs/plugin-protocol/v2.md, Tools).
const subset = new Set([
  "$schema",
  "type",
  "description",
  "properties",
  "required",
  "additionalProperties",
  "items",
  "enum",
  "const",
  "anyOf",
  "minimum",
  "maximum",
  "exclusiveMinimum",
  "exclusiveMaximum",
  "minLength",
  "maxLength",
  "pattern",
  "minItems",
  "maxItems",
]);

const everything = t.object(
  {
    name: t.string({ description: "Name", minLength: 1, maxLength: 64, pattern: "^[a-z]+$" }),
    count: t.integer({ minimum: 0, maximum: 10 }),
    ratio: t.number({ exclusiveMinimum: 0, exclusiveMaximum: 1 }),
    flag: t.optional(t.boolean({ description: "Flag" })),
    nothing: t.null(),
    mode: t.enum(["fast", "slow"]),
    kind: t.literal("file"),
    tags: t.array(t.string(), { minItems: 1, maxItems: 5 }),
    target: t.union([t.string(), t.integer()]),
    parent: t.nullable(t.string()),
    extra: t.raw<{ a: number }>({ type: "object", properties: { a: { type: "number" } }, required: ["a"] }),
  },
  { description: "Everything", additionalProperties: true },
);

test("t builds exactly the schema it describes", () => {
  expect(everything.schema).toEqual({
    type: "object",
    description: "Everything",
    additionalProperties: true,
    required: ["name", "count", "ratio", "nothing", "mode", "kind", "tags", "target", "parent", "extra"],
    properties: {
      name: { type: "string", description: "Name", minLength: 1, maxLength: 64, pattern: "^[a-z]+$" },
      count: { type: "integer", minimum: 0, maximum: 10 },
      ratio: { type: "number", exclusiveMinimum: 0, exclusiveMaximum: 1 },
      flag: { type: "boolean", description: "Flag" },
      nothing: { type: "null" },
      mode: { enum: ["fast", "slow"] },
      kind: { const: "file" },
      tags: { type: "array", items: { type: "string" }, minItems: 1, maxItems: 5 },
      target: { anyOf: [{ type: "string" }, { type: "integer" }] },
      parent: { anyOf: [{ type: "string" }, { type: "null" }] },
      extra: { type: "object", properties: { a: { type: "number" } }, required: ["a"] },
    },
  });
});

test("t uses only keywords of Kit's tool schema subset", () => {
  const keywords = new Set<string>();
  const visit = (node: object) => {
    for (const [key, value] of Object.entries(node)) {
      keywords.add(key);
      if (key === "properties") for (const child of Object.values(value as object)) visit(child as object);
      if (key === "items") visit(value as object);
      if (key === "anyOf") for (const child of value as object[]) visit(child);
    }
  };
  visit(everything.schema);
  expect([...keywords].filter((keyword) => !subset.has(keyword))).toEqual([]);
});

test("objects are closed unless asked, and options left undefined are omitted", () => {
  expect(t.object({ a: t.string({ description: undefined }) }).schema).toEqual({
    type: "object",
    properties: { a: { type: "string" } },
    required: ["a"],
    additionalProperties: false,
  });
});

test("a property named __proto__ is an ordinary property", () => {
  const schema = t.object({ ["__proto__"]: t.string() }).schema;
  expect(JSON.parse(JSON.stringify(schema))).toEqual({
    type: "object",
    properties: { ["__proto__"]: { type: "string" } },
    required: ["__proto__"],
    additionalProperties: false,
  });
});
